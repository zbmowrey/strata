package gates

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/telemetry"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func cfgOf(t *testing.T, toml string) *Config {
	t.Helper()
	c, err := Parse([]byte(toml))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestFixBeforeCheckAndBlocking(t *testing.T) {
	t.Parallel()
	inst, _ := repo(t, map[string]string{"a.txt": "ok\nUGLY line\nBAD thing\n", "b.txt": "fine\n"})
	fixer := tool(t, "fixer", `for f in "$@"; do grep -v UGLY "$f" > "$f.tmp"; mv "$f.tmp" "$f"; done`)
	ugly, _ := countingGrep(t, "UGLY")
	bad, _ := countingGrep(t, "BAD")
	cfg := cfgOf(t, `
[[gate]]
id = "fmt"
mode = "fix"
stages = ["edit"]
cmd = ["`+fixer+`", "{targets}"]
check_cmd = ["`+ugly+`", "{targets}"]
parser = "lines"
[[gate]]
id = "ugly"
stages = ["edit"]
cmd = ["`+ugly+`", "{targets}"]
parser = "lines"
[[gate]]
id = "bad"
stages = ["edit"]
cmd = ["`+bad+`", "{targets}"]
parser = "lines"
[[gate]]
id = "advice"
blocking = false
stages = ["edit"]
cmd = ["`+bad+`", "{targets}"]
parser = "lines"
[[gate]]
id = "other-stage"
stages = ["ci"]
cmd = ["`+bad+`", "{targets}"]
parser = "lines"
`)
	tel := telemetry.New(inst, nil)
	r := &Runner{Inst: inst, Cfg: cfg, Tel: tel}
	// Without --fix, the fix gate runs its check_cmd and reports UGLY.
	rep, err := r.Run(context.Background(), Request{Stage: StageEdit, Files: []string{"a.txt", "b.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(rep.Blocking); !slices.Equal(got, []string{"bad", "fmt", "ugly"}) {
		t.Fatalf("blocking gates %v", got)
	}
	if len(rep.Advisory) != 1 || rep.Advisory[0].Gate != "advice" {
		t.Fatalf("advisory %v", rep.Advisory)
	}
	// With fix, the formatter runs first so the UGLY check sees fixed files.
	rep, err = r.Run(context.Background(), Request{Stage: StageEdit, Files: []string{"a.txt", "b.txt"}, Fix: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(rep.Blocking); !slices.Equal(got, []string{"bad"}) {
		t.Fatalf("after fix: %v", got)
	}
	if rep.Blocking[0].File != "a.txt" || rep.Blocking[0].Line != 2 {
		t.Fatalf("finding %+v", rep.Blocking[0])
	}
	if !rep.Blocked() || !strings.Contains(Format(rep), "strata gates ✗ 1 blocking finding (edit stage)") {
		t.Fatalf("format:\n%s", Format(rep))
	}
	if _, err := os.Stat(inst.Path(".strata", "telemetry")); err != nil {
		t.Fatal("no gate telemetry")
	}
	if _, err := r.Run(context.Background(), Request{Stage: "bogus"}); err == nil {
		t.Fatal("unknown stage accepted")
	}
}

func ids(fs []findingT) []string {
	var out []string
	for _, f := range fs {
		if !slices.Contains(out, f.Gate) {
			out = append(out, f.Gate)
		}
	}
	slices.Sort(out)
	return out
}

func TestInfrastructureFailures(t *testing.T) {
	t.Parallel()
	inst, _ := repo(t, map[string]string{"a.txt": "x\n"})
	slow := tool(t, "slow", "sleep 5")
	crash := tool(t, "crash", "echo 'panic: boom'; exit 3")
	quiet := tool(t, "quiet", "exit 0")
	// semgrep with a missing --config path: valid, empty SARIF and exit 7.
	sarifErr := tool(t, "sarif-error", `echo '{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"semgrep"}},"results":[]}]}'; exit 7`)
	cfg := cfgOf(t, `
[[gate]]
id = "missing"
cmd = ["definitely-not-installed-strata-tool", "{targets}"]
[[gate]]
id = "slow"
cmd = ["`+slow+`"]
timeout = "1s"
[[gate]]
id = "crash"
cmd = ["`+crash+`"]
parser = "golangci-json"
[[gate]]
id = "quiet"
cmd = ["`+quiet+`"]
parser = "golangci-json"
[[gate]]
id = "sarif-error"
cmd = ["`+sarifErr+`"]
parser = "semgrep-sarif"
severity = "warning"
`)
	r := &Runner{Inst: inst, Cfg: cfg, NoCache: true}
	start := time.Now()
	rep, err := r.Run(context.Background(), Request{Stage: StageStop, Files: []string{"a.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatal("timeout not enforced")
	}
	status := map[string]string{}
	for _, g := range rep.Gates {
		status[g.ID] = g.Status
	}
	want := map[string]string{"missing": StatusMissing, "slow": StatusTimeout, "crash": StatusError, "quiet": StatusPass, "sarif-error": StatusError}
	for k, v := range want {
		if status[k] != v {
			t.Errorf("%s: status %s want %s", k, status[k], v)
		}
	}
	if got := ids(rep.Blocking); !slices.Equal(got, []string{"crash", "sarif-error"}) {
		t.Fatalf("only the tool failures block: %v", got)
	}
}

func TestCacheHit(t *testing.T) {
	t.Parallel()
	inst, dir := repo(t, map[string]string{"a.txt": "BAD\n", "conf.yml": "v1\n"})
	bad, counter := countingGrep(t, "BAD")
	cfg := cfgOf(t, `
[[gate]]
id = "bad"
cmd = ["`+bad+`", "{targets}"]
parser = "lines"
config_files = ["conf.yml"]
`)
	r := &Runner{Inst: inst, Cfg: cfg}
	run := func() *Report {
		rep, err := r.Run(context.Background(), Request{Stage: StageStop, Files: []string{"a.txt"}})
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}
	first, second := run(), run()
	if runs(t, counter) != 1 || second.Gates[0].Status != StatusCached || len(second.Blocking) != 1 || first.Gates[0].Status != StatusFail {
		t.Fatalf("runs=%d statuses %s/%s", runs(t, counter), first.Gates[0].Status, second.Gates[0].Status)
	}
	testutil.WriteFile(t, dir, "a.txt", "BAD\nBAD\n")
	if rep := run(); runs(t, counter) != 2 || len(rep.Blocking) != 2 {
		t.Fatal("content change did not invalidate the cache")
	}
	testutil.WriteFile(t, dir, "conf.yml", "v2\n")
	run()
	if runs(t, counter) != 3 {
		t.Fatal("config change did not invalidate the cache")
	}
}

func TestCacheConfigDirectory(t *testing.T) {
	t.Parallel()
	inst, dir := repo(t, map[string]string{"a.txt": "BAD\n", "rules/one.yml": "v1\n"})
	bad, counter := countingGrep(t, "BAD")
	cfg := cfgOf(t, `
[[gate]]
id = "bad"
cmd = ["`+bad+`", "{targets}"]
parser = "lines"
config_files = ["rules/"]
`)
	r := &Runner{Inst: inst, Cfg: cfg}
	run := func() {
		if _, err := r.Run(context.Background(), Request{Stage: StageStop, Files: []string{"a.txt"}}); err != nil {
			t.Fatal(err)
		}
	}
	run()
	run()
	if runs(t, counter) != 1 {
		t.Fatal("unchanged directory missed the cache")
	}
	testutil.WriteFile(t, dir, "rules/two.yml", "v1\n")
	run()
	if runs(t, counter) != 2 {
		t.Fatal("a file added to a config directory did not invalidate the cache")
	}
	testutil.WriteFile(t, dir, "rules/one.yml", "v2\n")
	run()
	if runs(t, counter) != 3 {
		t.Fatal("a changed file in a config directory did not invalidate the cache")
	}
}

func TestBaselineRatchet(t *testing.T) {
	t.Parallel()
	inst, dir := repo(t, map[string]string{"a.go": "package a\n\nfunc F() {\n\tBAD one\n\tBAD two\n}\n"})
	bad, _ := countingGrep(t, "BAD")
	cfg := cfgOf(t, `
[[gate]]
id = "bad"
cmd = ["`+bad+`", "{targets}"]
parser = "lines"
lines_regex = '^(?P<file>[^:]+):(?P<line>\d+):(?P<message>.*)$'
baseline = ".strata/baselines/bad.json"
stages = ["stop", "ci"]
`)
	ctx := context.Background()
	// Recording requires --accept.
	n, err := AcceptBaseline(ctx, inst, cfg, "bad", false)
	if err != nil || n != 2 {
		t.Fatalf("dry run: %d %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".strata", "baselines", "bad.json")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote the baseline")
	}
	if n, err = AcceptBaseline(ctx, inst, cfg, "bad", true); err != nil || n != 2 {
		t.Fatalf("accept: %d %v", n, err)
	}
	r := &Runner{Inst: inst, Cfg: cfg, Ratchet: true, NoCache: true}
	run := func() *Report {
		rep, err := r.Run(ctx, Request{Stage: StageStop, Files: []string{"a.go"}})
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}
	if rep := run(); rep.Blocked() || rep.Baselined != 2 {
		t.Fatalf("baselined findings blocked: %+v", rep)
	}
	// A new finding blocks; an unrelated line shift keeps the old ones baselined.
	testutil.WriteFile(t, dir, "a.go", "package a\n\n// shifted\nfunc F() {\n\tBAD one\n\tBAD two\n\tBAD three\n}\n")
	rep := run()
	if len(rep.Blocking) != 1 || rep.Baselined != 2 || !strings.Contains(rep.Blocking[0].Message, "three") {
		t.Fatalf("new finding: %+v", rep)
	}
	// Fixing a baselined finding shrinks the baseline (the ratchet).
	testutil.WriteFile(t, dir, "a.go", "package a\n\nfunc F() {\n\tBAD one\n}\n")
	if rep := run(); rep.Blocked() || rep.Baselined != 1 {
		t.Fatalf("after fix: %+v", rep)
	}
	b, err := LoadBaseline(filepath.Join(dir, ".strata", "baselines", "bad.json"), "bad")
	if err != nil || len(b.Entries) != 1 {
		t.Fatalf("baseline not shrunk: %+v %v", b, err)
	}
	// The edit hook path (Ratchet false) never rewrites baselines.
	if n, _ := AcceptBaseline(ctx, inst, cfg, "bad", false); n != 0 {
		t.Fatalf("nothing new expected, got %d", n)
	}
	if _, err := AcceptBaseline(ctx, inst, cfg, "nope", true); err == nil {
		t.Fatal("unknown gate")
	}
	cfg.Gates[0].Baseline = ""
	if _, err := AcceptBaseline(ctx, inst, cfg, "bad", true); err == nil {
		t.Fatal("gate without baseline file")
	}
}

func TestBuiltinsAndWorkdir(t *testing.T) {
	t.Parallel()
	inst, dir := repo(t, map[string]string{
		"engine/go.mod":           "module example.com/eng\n\ngo 1.26\n",
		"engine/p/x.go":           "package p\n\nfunc X() {} //nolint\n",
		"knowledge/a/b/broken.md": "---\nid: broken\n---\nbody\n",
		"engine/p/clean.go":       "package p\n",
	})
	gotest := tool(t, "gotest", `printf '%s\n' '{"Action":"output","Package":"example.com/eng/p","Test":"TestX","Output":"    x_test.go:9: nope\n"}'
echo '{"Action":"fail","Package":"example.com/eng/p","Test":"TestX"}'
echo '{"Action":"fail","Package":"example.com/eng/p"}'
exit 1`)
	rel := tool(t, "relpaths", `echo "p/x.go:3:1: from workdir"`)
	testutil.WriteFile(t, dir, File, `
[[gate]]
id = "suppressions"
builtin = "suppressions"
stages = ["edit"]
[[gate]]
id = "strata-lint"
builtin = "strata-lint"
applies_to = ["knowledge/**/*.md"]
stages = ["edit"]
[[gate]]
id = "go-test"
scope = "package"
workdir = "engine"
applies_to = ["engine/**/*.go"]
cmd = ["`+gotest+`", "{targets}"]
parser = "go-test-json"
stages = ["edit"]
[[gate]]
id = "rel"
workdir = "engine"
applies_to = ["engine/**/*.go"]
cmd = ["`+rel+`"]
parser = "lines"
stages = ["edit"]
`)
	rep, err := RunStage(context.Background(), inst, StageEdit, []string{"engine/p/clean.go", "engine/p/x.go", "knowledge/a/b/broken.md"},
		StageOptions{NoCache: true, Session: "s1", Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range append(rep.Blocking, rep.Advisory...) {
		files[f.Gate] = f.File
	}
	want := map[string]string{"suppressions": "engine/p/x.go", "strata-lint": "knowledge/a/b/broken.md", "go-test": "engine/p/x_test.go", "rel": "engine/p/x.go"}
	for g, f := range want {
		if files[g] != f {
			t.Errorf("%s finding file %q want %q (all: %v)", g, files[g], f, files)
		}
	}
}

func TestSetHashIgnoresPositions(t *testing.T) {
	t.Parallel()
	a := []findingT{{Gate: "g", Rule: "r", File: "f", Message: "m", Line: 1}, {Gate: "g", Rule: "s", File: "f", Message: "n"}}
	b := []findingT{{Gate: "g", Rule: "s", File: "f", Message: "n", Line: 9}, {Gate: "g", Rule: "r", File: "f", Message: "m", Line: 2}}
	if SetHash(a) != SetHash(b) || SetHash(a) == SetHash(a[:1]) {
		t.Fatal("SetHash")
	}
}

func TestModuleGatesRunPerModule(t *testing.T) {
	t.Parallel()
	inst, dir := repo(t, map[string]string{
		"scripts/go.mod":     "module example.com/s\n\ngo 1.26\n",
		"scripts/a/main.go":  "package main\n",
		"tools/x/go.mod":     "module example.com/x\n\ngo 1.26\n",
		"tools/x/y/z.go":     "package y\n",
		"loose/no_module.go": "package loose\n",
	})
	// Reports one finding per run: the workdir it ran in and its targets.
	where := tool(t, "where", `printf 'main.go:1:1: ran in %s with %s\n' "$(basename "$PWD")" "$*"`)
	testutil.WriteFile(t, dir, File, `
[[gate]]
id = "mod"
scope = "repo"
repo_target = "./..."
module = "go.mod"
applies_to = ["**/*.go"]
cmd = ["`+where+`", "{targets}"]
parser = "lines"
stages = ["edit"]
`)
	rep, err := RunStage(context.Background(), inst, StageEdit, []string{"scripts/a/main.go", "tools/x/y/z.go", "loose/no_module.go"},
		StageOptions{NoCache: true, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, f := range append(rep.Blocking, rep.Advisory...) {
		msgs = append(msgs, f.File+": "+f.Message)
	}
	slices.Sort(msgs)
	want := []string{"scripts/main.go: ran in scripts with ./...", "tools/x/main.go: ran in x with ./..."}
	if !slices.Equal(msgs, want) {
		t.Fatalf("per-module runs:\n got %v\nwant %v", msgs, want)
	}
	if _, err := Parse([]byte("[[gate]]\nid = \"m\"\ncmd = [\"x\"]\nmodule = \"go.mod\"\nworkdir = \"a\"\n")); err == nil {
		t.Fatal("module + workdir accepted")
	}
}
