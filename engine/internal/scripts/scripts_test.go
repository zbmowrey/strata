package scripts

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/finding"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func openInst(t *testing.T, dir string) *config.Instance {
	t.Helper()
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

func fixture(t *testing.T) *config.Instance {
	t.Helper()
	return openInst(t, testutil.CopyFixture(t, "instance-basic"))
}

func run(t *testing.T, inst *config.Instance, id string, opt RunOptions) (RunResult, string, string, error) {
	t.Helper()
	s, err := Find(inst, id)
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	opt.Stdout, opt.Stderr = &out, &errb
	if opt.Stdin == nil {
		opt.Stdin = strings.NewReader("")
	}
	res, err := Run(context.Background(), inst, s, opt)
	return res, out.String(), errb.String(), err
}

func TestLoadFindAllowList(t *testing.T) {
	t.Parallel()
	inst := fixture(t)
	all, err := Load(inst)
	if err != nil || len(all) != 5 {
		t.Fatalf("load %d %v", len(all), err)
	}
	s, err := Find(inst, "render-release-notes")
	if err != nil || s.Domain != "infra" || s.SideEffect != WritesLocal || s.Timeout.Seconds() != 60 || !slices.Equal(s.Writes, []string{"CHANGELOG.md"}) {
		t.Fatalf("find %+v %v", s, err)
	}
	if fs := Validate(inst, s); len(fs) != 0 {
		t.Fatalf("fixture manifest invalid: %+v", fs)
	}
	if _, err := Find(inst, "nope"); fail.CodeOf(err) != fail.Usage {
		t.Fatalf("missing: %v", err)
	}
	ids, err := AllowListable(inst)
	if err != nil || len(ids) != 5 {
		t.Fatalf("allow %v %v", ids, err)
	}
	testutil.WriteFile(t, inst.Root, "scripts/x/nuke-all/script.yaml", "id: nuke-all\nside_effect: destructive\nstatus: active\n")
	testutil.WriteFile(t, inst.Root, "scripts/x/old-tool/script.yaml", "id: old-tool\nside_effect: read-only\nstatus: deprecated\n")
	testutil.WriteFile(t, inst.Root, "scripts/x/broken/script.yaml", "id: [\n")
	ids, _ = AllowListable(inst)
	if slices.Contains(ids, "nuke-all") || slices.Contains(ids, "old-tool") || len(ids) != 5 {
		t.Fatalf("allow filtering %v", ids)
	}
}

func TestRunFixtureGoAndPython(t *testing.T) {
	t.Parallel()
	inst := fixture(t)
	tel := telemetry.New(inst, nil)
	res, out, _, err := run(t, inst, "check-migrations", RunOptions{Args: []string{"--help"}, Tel: tel})
	if err != nil || !strings.Contains(out, "usage: check-migrations") || res.ExitCode != 0 {
		t.Fatalf("go run: %q %v", out, err)
	}
	if _, err := os.Stat(BinPath(inst.Root, "check-migrations")); err != nil {
		t.Fatal("binary not cached")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not installed")
	}
	_, out, errOut, err := run(t, inst, "find-leaked-fixtures", RunOptions{Tel: tel})
	if err != nil || strings.TrimSpace(out) != "find-leaked-fixtures: ok" {
		t.Fatalf("python run: %q %q %v", out, errOut, err)
	}
	entries, _ := os.ReadDir(filepath.Join(inst.Root, ".strata", "telemetry"))
	if len(entries) != 1 {
		t.Fatal("no script_run telemetry")
	}
	log, _ := os.ReadFile(filepath.Join(inst.Root, ".strata", "telemetry", entries[0].Name()))
	if strings.Count(string(log), `"event":"script_run"`) != 2 {
		t.Fatalf("telemetry %s", log)
	}
}

func TestBuildCache(t *testing.T) {
	t.Parallel()
	inst := fixture(t)
	s, _ := Find(inst, "normalize-csv")
	ctx := context.Background()
	r1, err := Build(ctx, inst, s, false)
	if err != nil || !r1.Built {
		t.Fatalf("first build %+v %v", r1, err)
	}
	r2, err := Build(ctx, inst, s, false)
	if err != nil || r2.Built {
		t.Fatalf("cache miss %+v %v", r2, err)
	}
	testutil.WriteFile(t, inst.Root, "scripts/lib/go/x/x.go", "package x\n")
	r3, _ := Build(ctx, inst, s, false)
	if !r3.Built {
		t.Fatal("lib change did not invalidate")
	}
	r4, _ := Build(ctx, inst, s, true)
	if !r4.Built {
		t.Fatal("force did not rebuild")
	}
	py, _ := Find(inst, "find-leaked-fixtures")
	if _, err := Build(ctx, inst, py, false); fail.CodeOf(err) != fail.Usage {
		t.Fatalf("python build: %v", err)
	}
	testutil.WriteFile(t, inst.Root, "scripts/tooling/normalize-csv/main.go", "package main\nfunc main() { syntax error }\n")
	if _, err := Build(ctx, inst, s, false); err == nil || !strings.Contains(err.Error(), "build normalize-csv failed") {
		t.Fatalf("broken build: %v", err)
	}
}

// gitInstance creates a git-initialized instance holding bash scripts.
func gitInstance(t *testing.T, manifests map[string]string) *config.Instance {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	testutil.WriteFile(t, dir, ".strata/config.toml", "")
	testutil.WriteFile(t, dir, ".gitignore", "/.strata/telemetry/\n")
	for id, body := range manifests {
		parts := strings.SplitN(body, "\n---\n", 2)
		testutil.WriteFile(t, dir, "scripts/t/"+id+"/script.yaml", "id: "+id+"\nruntime: bash\nentry: run.sh\nstatus: active\n"+parts[0])
		testutil.WriteFile(t, dir, "scripts/t/"+id+"/run.sh", parts[1])
	}
	cmd := exec.CommandContext(t.Context(), "git", "init", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return openInst(t, dir)
}

func TestWritesLocalPolicy(t *testing.T) {
	t.Parallel()
	inst := gitInstance(t, map[string]string{
		"write-ok":  "side_effect: writes-local\nwrites: [\"out/**\", CHANGELOG.md]\n---\nmkdir -p out && echo x > out/a.txt && echo y > CHANGELOG.md\n",
		"write-bad": "side_effect: writes-local\nwrites: [\"out/**\"]\n---\necho x > stray.txt && echo z > out/b.txt\n",
	})
	tel := telemetry.New(inst, nil)
	if _, _, errOut, err := run(t, inst, "write-ok", RunOptions{Tel: tel}); err != nil {
		t.Fatalf("declared writes rejected: %v %s", err, errOut)
	}
	res, _, _, err := run(t, inst, "write-bad", RunOptions{Tel: tel})
	if !errors.Is(err, ErrViolation) || !slices.Equal(res.Violations, []string{"stray.txt"}) {
		t.Fatalf("violation not detected: %+v %v", res, err)
	}
	entries, _ := os.ReadDir(filepath.Join(inst.Root, ".strata", "telemetry"))
	log, _ := os.ReadFile(filepath.Join(inst.Root, ".strata", "telemetry", entries[0].Name()))
	if !strings.Contains(string(log), `"violation":["stray.txt"]`) {
		t.Fatalf("violation not in telemetry: %s", log)
	}
}

func TestRunPolicies(t *testing.T) {
	t.Parallel()
	inst := gitInstance(t, map[string]string{
		"nuke":     "side_effect: destructive\nwrites: [\"**\"]\nsummary: deletes everything\n---\necho boom\n",
		"fetch":    "side_effect: network\nnetwork: [api.example.com]\n---\necho fetched\n",
		"old":      "side_effect: read-only\nreplaced_by: fetch\n---\necho old\n",
		"needy":    "side_effect: read-only\nrequires: [definitely-not-a-real-tool-xyz]\n---\necho\n",
		"slow":     "side_effect: read-only\ntimeout: 1s\n---\nsleep 5\n",
		"findings": "side_effect: read-only\n---\necho found; exit 3\n",
		"weird":    "side_effect: telepathic\n---\necho\n",
	})
	p := filepath.Join(inst.Root, "scripts", "t", "old", "script.yaml")
	b, _ := os.ReadFile(p)
	_ = os.WriteFile(p, bytes.Replace(b, []byte("status: active"), []byte("status: deprecated"), 1), 0o600)

	if _, _, errOut, err := run(t, inst, "nuke", RunOptions{}); fail.CodeOf(err) != fail.Usage || !strings.Contains(errOut, "DESTRUCTIVE") {
		t.Fatalf("destructive without --yes: %v %s", err, errOut)
	}
	if _, out, _, err := run(t, inst, "nuke", RunOptions{Yes: true}); err != nil || out != "boom\n" {
		t.Fatalf("destructive with --yes: %q %v", out, err)
	}
	if _, out, errOut, err := run(t, inst, "fetch", RunOptions{}); err != nil || out != "fetched\n" || !strings.Contains(errOut, "api.example.com") {
		t.Fatalf("network: %q %q %v", out, errOut, err)
	}
	if _, _, errOut, err := run(t, inst, "old", RunOptions{}); err != nil || !strings.Contains(errOut, "deprecated; use fetch") {
		t.Fatalf("deprecated: %q %v", errOut, err)
	}
	if _, _, _, err := run(t, inst, "needy", RunOptions{}); err == nil || !strings.Contains(fail.HintOf(err), "install definitely-not") {
		t.Fatalf("requires: %v", err)
	}
	if _, _, _, err := run(t, inst, "slow", RunOptions{}); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout: %v", err)
	}
	if res, out, _, err := run(t, inst, "findings", RunOptions{}); fail.CodeOf(err) != 3 || res.ExitCode != 3 || out != "found\n" {
		t.Fatalf("exit code propagation: %+v %v", res, err)
	}
	if _, _, _, err := run(t, inst, "weird", RunOptions{}); fail.CodeOf(err) != fail.Runtime {
		t.Fatalf("unknown side effect: %v", err)
	}
}

func TestEnvAndArgs(t *testing.T) { //nolint:paralleltest // t.Setenv forbids t.Parallel
	inst := gitInstance(t, map[string]string{
		"envy":      "side_effect: read-only\nenv: [MY_DECLARED]\n---\necho \"d=$MY_DECLARED s=$MY_SECRET\"\n",
		"echo-args": "side_effect: read-only\n---\nprintf '%s|' \"$@\"\n",
	})
	t.Setenv("MY_DECLARED", "yes")
	t.Setenv("MY_SECRET", "leak")
	if _, out, _, err := run(t, inst, "envy", RunOptions{}); err != nil || out != "d=yes s=\n" {
		t.Fatalf("env filtering: %q %v", out, err)
	}
	if _, out, _, err := run(t, inst, "echo-args", RunOptions{Args: []string{"a b", "$(rm -rf /)", "c"}}); err != nil || out != "a b|$(rm -rf /)|c|" {
		t.Fatalf("argv passthrough: %q %v", out, err)
	}
	env := FilterEnv([]string{"PATH=/bin", "LC_ALL=C", "UV_CACHE_DIR=/x", "AWS_SECRET=1", "NOEQ"}, []string{"AWS_SECRET"})
	if !slices.Equal(env, []string{"PATH=/bin", "LC_ALL=C", "UV_CACHE_DIR=/x", "AWS_SECRET=1"}) {
		t.Fatalf("FilterEnv %v", env)
	}
}

func TestRuntimeErrors(t *testing.T) {
	t.Parallel()
	inst := gitInstance(t, map[string]string{"plain": "side_effect: read-only\n---\necho\n"})
	s, _ := Find(inst, "plain")
	ctx := context.Background()
	for _, mut := range []func(*Script){
		func(s *Script) { s.Runtime = "cobol" },
		func(s *Script) { s.Entry = "missing.sh" },
		func(s *Script) { s.Runtime = "typescript" }, // entry exists; pnpm/tsx may be missing → still an argv
	} {
		c := *s
		mut(&c)
		if c.Runtime == "typescript" {
			if _, _, err := command(ctx, inst, &c, nil); err != nil && !strings.Contains(err.Error(), "needs pnpm") {
				t.Fatalf("typescript argv: %v", err)
			}
			continue
		}
		if _, _, err := command(ctx, inst, &c, nil); fail.CodeOf(err) != fail.Runtime {
			t.Fatalf("%+v: %v", c, err)
		}
	}
	r := Script{Runtime: "rust", ID: "x", Dir: "scripts/t/plain"}
	argv, _, err := command(ctx, inst, &r, []string{"--flag"})
	if err != nil || argv[0] != "cargo" || argv[len(argv)-1] != "--flag" {
		t.Fatalf("rust argv %v %v", argv, err)
	}
	notGit := openInst(t, testutil.CopyFixture(t, "instance-basic"))
	rel, _ := Find(notGit, "render-release-notes")
	var errb bytes.Buffer
	if _, err := Run(ctx, notGit, rel, RunOptions{Stdout: &bytes.Buffer{}, Stderr: &errb}); err != nil || !strings.Contains(errb.String(), "cannot verify writes") {
		t.Fatalf("writes-local outside git: %v %s", err, errb.String())
	}
}

func TestCatalog(t *testing.T) {
	t.Parallel()
	inst := fixture(t)
	all, _ := Load(inst)
	all[0].Status, all[0].ReplacedBy = "deprecated", "check-migrations"
	files := Catalog(all, "scripts", 50)
	testutil.Golden(t, "testdata/catalog.golden", files["scripts/CATALOG.md"])
	split := Catalog(all, "scripts", 2)
	keys := make([]string, 0, len(split))
	for k := range split {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b bytes.Buffer
	for _, k := range keys {
		b.WriteString("=== " + k + "\n")
		b.Write(split[k])
	}
	testutil.Golden(t, "testdata/catalog-split.golden", b.Bytes())

	// WriteCatalog: split, then unsplit removes the stale domain catalogs.
	inst.Config.Scripts.CatalogSplitAt = 2
	changed, err := WriteCatalog(inst)
	if err != nil || len(changed) != 5 {
		t.Fatalf("split write %v %v", changed, err)
	}
	if changed, _ := WriteCatalog(inst); len(changed) != 0 {
		t.Fatalf("not idempotent: %v", changed)
	}
	inst.Config.Scripts.CatalogSplitAt = 50
	changed, err = WriteCatalog(inst)
	if err != nil || len(changed) != 5 || !slices.Contains(changed, "scripts/infra/CATALOG.md") {
		t.Fatalf("unsplit write %v %v", changed, err)
	}
	if _, err := os.Stat(filepath.Join(inst.Root, "scripts", "infra", "CATALOG.md")); !os.IsNotExist(err) {
		t.Fatal("stale catalog kept")
	}
	if code("") != "" || cell("a|b\n c") != `a\|b c` {
		t.Fatal("cell helpers")
	}
}

func lintScript(t *testing.T, inst *config.Instance, id string) []finding.Finding {
	t.Helper()
	s, err := Find(inst, id)
	if err != nil {
		t.Fatal(err)
	}
	return Validate(inst, s)
}

func TestScaffoldLintLifecycle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.WriteFile(t, dir, ".strata/config.toml", "")
	inst := openInst(t, dir)
	ctx := context.Background()
	created, err := Scaffold(ctx, inst, NewOptions{Domain: "data", Name: "convert-units"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"scripts/go.mod", "scripts/lib/go/cli/cli.go", "scripts/data/convert-units/script.yaml", "scripts/data/convert-units/main.go", "scripts/data/convert-units/main_test.go"} {
		if !slices.Contains(created, want) {
			t.Errorf("missing %s in %v", want, created)
		}
	}
	if fs := lintScript(t, inst, "convert-units"); len(fs) == 0 {
		t.Fatal("scaffolded manifest passed lint before filling")
	}
	// Fill the placeholders: lint passes.
	p := filepath.Join(dir, "scripts", "data", "convert-units", "script.yaml")
	raw, _ := os.ReadFile(p)
	filled := strings.NewReplacer(
		"TODO one-line summary of what convert-units does (≤ 160 chars)", "Convert measurement units in CSV columns.",
		"TODO: what it reads, what it reports, and when to use it.", "Reads a CSV and rewrites unit columns.",
		"TODO explain what this invocation shows", "Machine-readable output.",
		"TODO describe the output", "converted rows",
	).Replace(string(raw))
	if err := os.WriteFile(p, []byte(filled), 0o600); err != nil {
		t.Fatal(err)
	}
	if fs := lintScript(t, inst, "convert-units"); len(fs) != 0 {
		t.Fatalf("filled manifest still fails lint: %+v", fs)
	}
	// The scaffold compiles and its placeholder test fails.
	vet := exec.CommandContext(t.Context(), "go", "vet", "./...")
	vet.Dir = filepath.Join(dir, "scripts")
	if out, err := vet.CombinedOutput(); err != nil {
		t.Fatalf("scaffold does not vet: %v\n%s", err, out)
	}
	test := exec.CommandContext(t.Context(), "go", "test", "./data/...")
	test.Dir = vet.Dir
	if out, err := test.CombinedOutput(); err == nil || !strings.Contains(string(out), "TODO: write tests") {
		t.Fatalf("scaffold test should fail: %s", out)
	}
	if _, err := Scaffold(ctx, inst, NewOptions{Domain: "data", Name: "convert-units"}); fail.CodeOf(err) != fail.Usage {
		t.Fatalf("duplicate: %v", err)
	}
	py, err := Scaffold(ctx, inst, NewOptions{Domain: "data", Name: "parse-ast", Runtime: "python"})
	if err != nil || !slices.Contains(py, "scripts/pyproject.toml") || !slices.Contains(py, "scripts/lib/py/strata_lib/cli.py") {
		t.Fatalf("python scaffold %v %v", py, err)
	}
	fs := lintScript(t, inst, "parse-ast")
	if !slices.ContainsFunc(fs, func(f finding.Finding) bool {
		return f.Rule == "script/placeholder" && strings.Contains(f.Message, "runtime_justification")
	}) {
		t.Fatalf("python justification placeholder not flagged: %+v", fs)
	}
	if again, err := WriteLib(ctx, inst, []string{"go", "python", "rust"}); err != nil || len(again) != 0 {
		t.Fatalf("WriteLib not idempotent: %v %v", again, err)
	}
}

func TestScaffoldErrors(t *testing.T) {
	t.Parallel()
	inst := fixture(t)
	ctx := context.Background()
	for _, opt := range []NewOptions{
		{Domain: "Data", Name: "x-tool"},
		{Domain: "data", Name: "Bad_Name"},
		{Domain: "data", Name: "ab"},
		{Domain: "data", Name: "check-migrations"},
		{Domain: "data", Name: "rusty", Runtime: "rust"},
	} {
		if _, err := Scaffold(ctx, inst, opt); fail.CodeOf(err) != fail.Usage {
			t.Errorf("%+v: %v", opt, err)
		}
	}
	testutil.WriteFile(t, inst.Root, "scripts/data/taken-dir/notes.txt", "x")
	if _, err := Scaffold(ctx, inst, NewOptions{Domain: "data", Name: "taken-dir"}); fail.CodeOf(err) != fail.Usage {
		t.Errorf("existing dir: %v", err)
	}
	// Existing go.mod module path is reused.
	d, err := templateData(ctx, inst, NewOptions{})
	if err != nil || d.Module != "example.com/acme/scripts" {
		t.Fatalf("module %+v %v", d, err)
	}
}

func TestModulePath(t *testing.T) {
	t.Parallel()
	for remote, want := range map[string]string{
		"git@github.com:zbmowrey/strata.git":   "github.com/zbmowrey/strata/scripts",
		"https://github.com/Acme/App.git":      "github.com/Acme/App/scripts",
		"ssh://git@gitea.example.com/team/svc": "gitea.example.com/team/svc/scripts",
		"":                                     "strata.local/my-repo/scripts",
	} {
		if got := ModulePath(remote, "My Repo"); got != want {
			t.Errorf("ModulePath(%q) = %q want %q", remote, got, want)
		}
	}
}

func TestFromMetaDefaults(t *testing.T) {
	t.Parallel()
	m, _ := doc.ParseYAML([]byte("timeout: bogus\n"))
	s := fromMeta("scripts/tool-x/script.yaml", m, "scripts")
	if s.ID != "tool-x" || s.Domain != "" || s.Timeout != DefaultTimeout {
		t.Fatalf("%+v", s)
	}
	if orNone(nil) != "(none declared)" {
		t.Fatal("orNone")
	}
}
