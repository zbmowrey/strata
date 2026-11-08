package bench

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

// stream builds a stream-json transcript: tool calls (name, input JSON),
// optional subagent calls, and a result.
func stream(calls, sub [][2]string, answer string, cost float64) []byte {
	var b strings.Builder
	b.WriteString(`{"type":"system","subtype":"init","model":"claude-test-1"}` + "\n")
	b.WriteString("not json\n")
	for _, c := range calls {
		b.WriteString(`{"type":"assistant","parent_tool_use_id":null,"message":{"model":"claude-test-1","content":[{"type":"text","text":"working"},{"type":"tool_use","name":"` + c[0] + `","input":` + c[1] + `}]}}` + "\n")
	}
	for _, c := range sub {
		b.WriteString(`{"type":"assistant","parent_tool_use_id":"toolu_1","message":{"content":[{"type":"tool_use","name":"` + c[0] + `","input":` + c[1] + `}]}}` + "\n")
	}
	res, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "result": answer, "total_cost_usd": cost, "num_turns": 4})
	b.Write(res)
	b.WriteString("\n")
	return []byte(b.String())
}

func TestLoadSuiteAndSelect(t *testing.T) {
	t.Parallel()
	all, err := LoadSuite(SuiteStandard)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"capture-correction", "capture-gotcha", "correct-shard", "gate-response", "kb-first", "no-generated-edit",
		"project-continuity", "respect-user-stated", "script-create", "script-new-not-mutate", "script-reuse", "show-batching",
	}
	if len(all) != len(want) {
		t.Fatalf("got %d scenarios", len(all))
	}
	for i, s := range all {
		if s.ID != want[i] || s.Fixture != "fixtures/basic" || s.Runs != 3 {
			t.Errorf("scenario %d: %+v", i, s.ID)
		}
		if _, err := s.fixtureFS(); err != nil {
			t.Errorf("%s fixture: %v", s.ID, err)
		}
	}
	if _, err := LoadSuite("nope"); fail.CodeOf(err) != fail.Usage {
		t.Fatal("unknown suite")
	}
	sel, err := Select(all, []string{"kb-first"})
	if err != nil || len(sel) != 1 {
		t.Fatalf("select: %v", err)
	}
	if _, err := Select(all, []string{"missing"}); fail.CodeOf(err) != fail.Usage {
		t.Fatal("unknown scenario")
	}
	if got, _ := Select(all, nil); len(got) != len(all) {
		t.Fatal("select all")
	}
}

func TestParseScenarioErrors(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"id: [", "prompt: x\nassert: [{called: Read}]", "id: x\nassert: [{called: Read}]", "id: x\nprompt: y"} {
		if _, err := parseScenario([]byte(raw), "f.yaml"); fail.CodeOf(err) != fail.Usage {
			t.Errorf("%q accepted", raw)
		}
	}
	sc := Scenario{ID: "x", Fixture: "fixtures/none"}
	if _, err := sc.fixtureFS(); err == nil {
		t.Fatal("unknown fixture accepted")
	}
}

func TestTraceAndMatchers(t *testing.T) {
	t.Parallel()
	tr := ParseTrace(stream([][2]string{
		{"mcp__strata__strata_search", `{"query":"hang at exit"}`},
		{"Bash", `{"command":"cd /x && /tmp/bin/strata show a b | head"}`},
		{"Read", `{"file_path":"/tmp/i/projects/ci-flake/handoff.md"}`},
		{"Grep", `{"pattern":"pool"}`},
		{"Other", `{"k":1}`},
	}, [][2]string{{"Bash", `{"command":"strata run find-leaked-fixtures -- --path tests"}`}}, "", 0.12))
	if tr.Model != "claude-test-1" || tr.CostUSD != 0.12 || tr.Turns != 4 || tr.Answer != "working" || len(tr.Calls) != 6 || !tr.Calls[5].Sub {
		t.Fatalf("trace %+v", tr)
	}
	cases := map[string]int{
		"strata_search":                          1,
		"Bash(strata show*)":                     1,
		"Bash(head)":                             1,
		"Bash(strata search*)":                   0,
		"Read(*projects/ci-flake/*)":             1,
		"Grep(pool)":                             1,
		"Bash(strata run find-leaked-fixtures*)": 1,
		"Other(*\"k\"*)":                         1,
		"Write":                                  0,
	}
	for p, want := range cases {
		if n, _ := tr.count([]string{p}); n != want {
			t.Errorf("%s: %d calls, want %d", p, n, want)
		}
	}
	nb := ParseTrace([]byte(`{"type":"assistant","message":{"model":"m2","content":[{"type":"tool_use","name":"NotebookEdit","input":{"notebook_path":"a.ipynb"}}]}}`))
	if nb.Model != "m2" || callArgs(nb.Calls[0])[0] != "a.ipynb" {
		t.Fatalf("notebook: %+v", nb)
	}
}

func TestAssertions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.WriteFile(t, dir, "keep.md", "same")
	testutil.WriteFile(t, dir, "edit.md", "old")
	before, err := takeSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, dir, "edit.md", "new")
	testutil.WriteFile(t, dir, "knowledge/_inbox/x.md", "---\nid: x\ntype: gotcha\n---\nbody\n")
	testutil.WriteFile(t, dir, ".git/HEAD", "ignored")
	after, _ := takeSnapshot(dir)
	gateOK := true
	rs := &runState{
		dir: dir, before: before, after: after, gate: func() (bool, string) { return gateOK, "x: 1 blocking\nmore" },
		trace: ParseTrace(stream([][2]string{
			{"Bash", `{"command":"strata search pool"}`},
			{"Read", `{"file_path":"a.py"}`},
			{"Bash", `{"command":"strata capture --type gotcha \"x\""}`},
		}, nil, "Use teardown.", 0)),
	}
	pass := []Assertion{
		{FirstOf: []string{"Bash(strata search*)"}, Before: []string{"Read"}},
		{FirstOf: []string{"Bash(strata search*)"}},
		{Called: "Read"},
		{CalledAny: []string{"Write", "Read"}},
		{NotCalled: "Write"},
		{CalledWith: &CalledWith{Tool: "Bash", Match: "capture.*--type[ =]gotcha"}},
		{MaxCalls: &MaxCalls{Tools: []string{"Bash(strata search*)"}, Max: 1}},
		{FileCreated: &FileMatch{Glob: "knowledge/_inbox/*.md", Frontmatter: map[string]string{"type": "gotcha"}}},
		{FileCreated: &FileMatch{Glob: "knowledge/**"}},
		{FileChanged: "edit.md"},
		{FileUnchanged: "keep.md"},
		{GateClean: true},
		{AnswerMentions: []string{"TEARDOWN"}},
	}
	if f := rs.evaluate(pass); len(f) != 0 {
		t.Fatalf("expected pass: %v", f)
	}
	gateOK = false
	failing := []Assertion{
		{FirstOf: []string{"Bash(strata capture*)"}, Before: []string{"Read"}},
		{FirstOf: []string{"Write"}},
		{Called: "Write"},
		{CalledAny: []string{"Write"}},
		{NotCalled: "Read"},
		{CalledWith: &CalledWith{Tool: "Bash", Match: "correction"}},
		{CalledWith: &CalledWith{Tool: "Bash", Match: "("}},
		{MaxCalls: &MaxCalls{Tools: []string{"Bash"}, Max: 1}},
		{FileCreated: &FileMatch{Glob: "knowledge/_inbox/*.md", Frontmatter: map[string]string{"type": "fact"}}},
		{FileCreated: &FileMatch{Glob: "keep.md"}},
		{FileChanged: "keep.md"},
		{FileUnchanged: "edit.md"},
		{FileUnchanged: "none/*"},
		{GateClean: true},
		{AnswerMentions: []string{"pool"}},
		{},
	}
	if f := rs.evaluate(failing); len(f) != len(failing) {
		t.Fatalf("expected %d failures, got %d: %v", len(failing), len(f), f)
	}
}

func fakeClaude(calls [][2]string, answer, touch string) ClaudeFunc {
	return func(_ context.Context, dir string, args, _ []string) ([]byte, error) {
		if touch != "" {
			_ = os.WriteFile(filepath.Join(dir, touch), []byte("changed"), 0o600)
		}
		if len(args) < 2 || args[0] != "-p" {
			return nil, errors.New("bad args")
		}
		return stream(calls, nil, answer, 0.05), nil
	}
}

func TestRunProtocolEndToEnd(t *testing.T) {
	t.Parallel()
	sc, err := Select(must(LoadSuite(SuiteStandard)), []string{"kb-first", "correct-shard"})
	if err != nil {
		t.Fatal(err)
	}
	good := fakeClaude([][2]string{
		{"Bash", `{"command":"strata search tests hang at exit"}`},
		{"Bash", `{"command":"strata show pytest-fixture-teardown postgres-connection-pool"}`},
		{"Read", `{"file_path":"tests/conftest.py"}`},
	}, "The session fixture has no teardown; pool is max_size=50.", "knowledge/database/postgres/postgres-connection-pool.md")
	rep := RunProtocol(context.Background(), sc, ProtocolOptions{
		Claude: good, Runs: 2, Parallel: 2, PluginDir: "/p", Model: "m",
		Now:  func() time.Time { return time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC) },
		Gate: func(context.Context, string) (bool, string) { return true, "" },
	})
	if rep.Passed != 2 || rep.Total != 2 || rep.Model != "claude-test-1" || rep.CostUSD != 0.2 || rep.Date != "2026-11-08" {
		t.Fatalf("report %+v", rep)
	}
	bad := fakeClaude([][2]string{{"Read", `{"file_path":"tests/conftest.py"}`}}, "no idea", "")
	rep = RunProtocol(context.Background(), sc[:1], ProtocolOptions{Claude: bad, Gate: func(context.Context, string) (bool, string) { return true, "" }})
	if rep.Passed != 0 || len(rep.Scenarios[0].Runs) != 3 || len(rep.Scenarios[0].Runs[0].Failures) != 3 {
		t.Fatalf("failing report %+v", rep.Scenarios[0])
	}
}

func TestBudgetAndSetupFailures(t *testing.T) {
	t.Parallel()
	sc := Scenario{ID: "b", Budget: Budget{MaxCostUSD: 0.01}}
	tr := &Trace{Result: "error_max_turns", CostUSD: 0.5}
	if f := budgetFailures(&sc, tr, nil); len(f) != 2 {
		t.Fatalf("budget: %v", f)
	}
	if f := budgetFailures(&sc, &Trace{}, context.DeadlineExceeded); len(f) != 1 || f[0] != "session timed out" {
		t.Fatalf("timeout: %v", f)
	}
	if f := budgetFailures(&sc, &Trace{}, errors.New("boom")); len(f) != 1 {
		t.Fatalf("crash: %v", f)
	}
	args := claudeArgs(&Scenario{Prompt: "p", AllowedTools: []string{"Read"}, Budget: Budget{MaxTurns: 3}}, ProtocolOptions{})
	if strings.Join(args, " ") != "-p p --output-format stream-json --verbose --permission-mode acceptEdits --allowedTools Read --max-turns 3" {
		t.Fatalf("args %v", args)
	}
	bad := Scenario{ID: "s", Prompt: "p", Fixture: "fixtures/basic", Setup: []string{"exit 3"}, Assert: []Assertion{{Called: "Read"}}}
	r := runOnce(context.Background(), &bad, ProtocolOptions{}.withDefaults())
	if r.Pass || !strings.HasPrefix(r.Failures[0], "setup: sh -c exit 3") {
		t.Fatalf("setup failure: %+v", r)
	}
	if err := writeFiles(t.TempDir(), map[string]string{"../x": "y"}); err == nil {
		t.Fatal("escaping file accepted")
	}
	env := sessionEnv("/opt/strata/bin/strata")
	found := false
	for _, kv := range env {
		found = found || strings.HasPrefix(kv, "PATH=/opt/strata/bin"+string(os.PathListSeparator))
	}
	if !found {
		t.Fatal("strata dir not first on PATH")
	}
}

func TestInstanceScenariosAndSave(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inst := &config.Instance{Root: dir, Config: config.Default()}
	if sc, err := LoadInstanceScenarios(inst); err != nil || len(sc) != 0 {
		t.Fatalf("none: %v %v", sc, err)
	}
	testutil.WriteFile(t, dir, ProtocolDir+"/own.yaml", "id: own\nfixture: fx\nprompt: hi\nsetup: [\"touch made.txt\"]\nassert: [{file_unchanged: \"a.txt\"}]\nruns: 1\n")
	testutil.WriteFile(t, dir, ProtocolDir+"/fx/a.txt", "a")
	testutil.WriteFile(t, dir, ProtocolDir+"/fx/go.mod.fixture", "module x\n")
	scs, err := LoadInstanceScenarios(inst)
	if err != nil || len(scs) != 1 {
		t.Fatalf("load: %v", err)
	}
	rep := RunProtocol(context.Background(), scs, ProtocolOptions{Claude: fakeClaude(nil, "ok", ""), Gate: func(context.Context, string) (bool, string) { return true, "" }})
	if rep.Passed != 1 {
		t.Fatalf("own scenario: %+v", rep.Scenarios[0].Runs)
	}
	testutil.WriteFile(t, dir, ProtocolDir+"/bad.yaml", "id: [")
	if _, err := LoadInstanceScenarios(inst); err == nil {
		t.Fatal("bad scenario accepted")
	}
	first := &ProtocolReport{Date: "d1", Model: "m", Scenarios: []ScenarioResult{{ID: "b", Pass: true}, {ID: "a"}}}
	if _, err := SaveProtocol(inst, first); err != nil {
		t.Fatal(err)
	}
	merged, err := SaveProtocol(inst, &ProtocolReport{Date: "d2", Scenarios: []ScenarioResult{{ID: "a", Pass: true}}})
	if err != nil || merged.Passed != 2 || merged.Total != 2 || merged.Model != "m" || merged.Scenarios[0].ID != "a" || merged.Date != "d2" {
		t.Fatalf("merge: %v %+v", err, merged)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
