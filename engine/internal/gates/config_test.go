package gates

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

// TestEngineGates loads the engine's own gate set and checks stage selection
// and {targets} expansion per scope against the repository layout.
func TestEngineGates(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "engine-gates.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	inStage := func(stage string) []string {
		var ids []string
		for i := range cfg.Gates {
			if cfg.Gates[i].InStage(stage) {
				ids = append(ids, cfg.Gates[i].ID)
			}
		}
		return ids
	}
	edit := inStage(StageEdit)
	for _, id := range []string{"go-fmt", "go-imports", "go-lint", "suppressions"} {
		if !slices.Contains(edit, id) {
			t.Errorf("edit stage lacks %s: %v", id, edit)
		}
	}
	if slices.Contains(edit, "go-test") || slices.Contains(edit, "go-vuln") {
		t.Errorf("slow gates in edit stage: %v", edit)
	}
	if !slices.Contains(inStage(StageCommit), "go-vuln") || slices.Contains(inStage(StageStop), "go-vuln") {
		t.Error("go-vuln belongs to commit/ci only")
	}

	files := []string{
		"engine/internal/gates/runner.go", "engine/internal/gates/runner_test.go",
		"engine/internal/cli/gates.go", "engine/main.go",
		"engine/internal/gates/testdata/tools/x.go", "plugin/hooks/a.sh", "README.md",
	}
	cases := []struct {
		id      string
		targets []string
	}{
		{"go-fmt", []string{"internal/gates/runner.go", "internal/gates/runner_test.go", "internal/cli/gates.go", "main.go"}},
		{"go-lint", []string{".", "./internal/cli", "./internal/gates"}},
		{"go-test", []string{".", "./internal/cli", "./internal/gates", "./internal/gates/testdata/tools"}},
		{"go-vuln", []string{"./..."}},
	}
	for _, tc := range cases {
		g, ok := cfg.Get(tc.id)
		if !ok {
			t.Fatalf("no gate %s", tc.id)
		}
		got := g.Targets(g.Select(files))
		if !slices.Equal(got, tc.targets) {
			t.Errorf("%s targets %v want %v", tc.id, got, tc.targets)
		}
	}
	g, _ := cfg.Get("go-lint")
	argv := Argv(g.Cmd, g.Targets(g.Select(files)))
	if argv[0] != "golangci-lint" || !slices.Contains(argv, "./internal/gates") || slices.Contains(argv, "{targets}") {
		t.Errorf("argv %v", argv)
	}
	if g.Workdir != "engine" || !g.IsBlocking() {
		t.Errorf("go-lint workdir/blocking %q %v", g.Workdir, g.IsBlocking())
	}
}

func TestParseDefaultsAndErrors(t *testing.T) {
	t.Parallel()
	cfg := cfgOf(t, "[[gate]]\nid = \"x\"\ncmd = [\"true\"]\nworkdir = \"./\"\n")
	g := cfg.Gates[0]
	if g.Mode != ModeCheck || g.Scope != ScopeFile || g.Parser != ParserExitcode || g.Timeout != "60s" ||
		g.RepoTarget != "." || g.Workdir != "" || !slices.Equal(g.Stages, []string{StageStop, StageCommit, StageCI}) {
		t.Fatalf("defaults %+v", g)
	}
	if _, ok := cfg.Get("y"); ok {
		t.Fatal("Get(y)")
	}
	bad := map[string]string{
		"unknown key":  "[[gate]]\nid = \"x\"\ncmd = [\"a\"]\nbogus = 1\n",
		"syntax":       "[[gate]\n",
		"id":           "[[gate]]\nid = \"Bad_ID\"\ncmd = [\"a\"]\n",
		"no cmd":       "[[gate]]\nid = \"x\"\n",
		"builtin":      "[[gate]]\nid = \"x\"\nbuiltin = \"nope\"\n",
		"mode":         "[[gate]]\nid = \"x\"\ncmd = [\"a\"]\nmode = \"auto\"\n",
		"scope":        "[[gate]]\nid = \"x\"\ncmd = [\"a\"]\nscope = \"tree\"\n",
		"stage":        "[[gate]]\nid = \"x\"\ncmd = [\"a\"]\nstages = [\"push\"]\n",
		"parser":       "[[gate]]\nid = \"x\"\ncmd = [\"a\"]\nparser = \"xml\"\n",
		"timeout":      "[[gate]]\nid = \"x\"\ncmd = [\"a\"]\ntimeout = \"soon\"\n",
		"disabled":     "[[gate]]\nid = \"x\"\ncmd = [\"a\"]\ndisabled = true\n",
		"lines regex":  "[[gate]]\nid = \"x\"\ncmd = [\"a\"]\nlines_regex = \"(\"\n",
		"glob":         "[[gate]]\nid = \"x\"\ncmd = [\"a\"]\napplies_to = [\"[\"]\n",
		"duplicate id": "[[gate]]\nid = \"x\"\ncmd = [\"a\"]\n[[gate]]\nid = \"x\"\ncmd = [\"a\"]\n",
	}
	for name, raw := range bad {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLoadAndMatches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.WriteFile(t, dir, ".strata/config.toml", "")
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(inst)
	if err != nil || len(cfg.Gates) != 0 {
		t.Fatalf("missing gates.toml: %v %v", cfg, err)
	}
	testutil.WriteFile(t, dir, File, "[[gate]]\nid = \"g\"\ncmd = [\"a\"]\nworkdir = \"sub\"\napplies_to = [\"sub/**/*.go\"]\nexclude = [\"**/gen/**\"]\n")
	cfg, err = Load(inst)
	if err != nil {
		t.Fatal(err)
	}
	g := &cfg.Gates[0]
	for p, want := range map[string]bool{"sub/a.go": true, "sub/gen/a.go": false, "other/a.go": false, "sub/a.txt": false} {
		if g.Matches(p) != want {
			t.Errorf("Matches(%s) != %v", p, want)
		}
	}
	testutil.WriteFile(t, dir, File, "nope = [")
	if _, err := Load(inst); err == nil {
		t.Fatal("bad toml loaded")
	}
}

func TestFilesSelections(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	inst, dir := repo(t, map[string]string{"a.go": "a\n", "b/c.go": "c\n", "b/.hidden/h.go": "h\n"})
	root := inst.Root
	testutil.WriteFile(t, dir, "a.go", "changed\n")
	testutil.WriteFile(t, dir, "new.go", "n\n")
	testutil.WriteFile(t, dir, "ignored.txt", "i\n")
	testutil.WriteFile(t, dir, "staged.go", "s\n")
	sh(t, dir, "git", "add", "staged.go")

	check := func(sel Selection, explicit []string, want ...string) {
		t.Helper()
		got, err := Files(ctx, root, sel, explicit)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, want) {
			t.Errorf("selection %d: %v want %v", sel, got, want)
		}
	}
	check(SelectChanged, nil, "a.go", "new.go", "staged.go")
	check(SelectStaged, nil, "staged.go")
	check(SelectAll, nil, ".gitignore", ".strata/config.toml", "a.go", "b/.hidden/h.go", "b/c.go", "new.go", "staged.go")
	check(SelectPaths, []string{"b", "./a.go"}, "a.go", "b/c.go")
	if _, err := Files(ctx, root, SelectPaths, []string{"missing"}); err == nil {
		t.Fatal("missing path")
	}
	if _, err := Files(ctx, t.TempDir(), SelectAll, nil); err == nil {
		t.Fatal("outside git")
	}

	// A repository with no commits: changed = everything in the index.
	fresh := t.TempDir()
	testutil.WriteFile(t, fresh, "x.go", "x\n")
	sh(t, fresh, "git", "init", "-q")
	sh(t, fresh, "git", "add", "x.go")
	got, err := Files(ctx, fresh, SelectChanged, nil)
	if err != nil || !slices.Equal(got, []string{"x.go"}) {
		t.Fatalf("no HEAD: %v %v", got, err)
	}

	for sel, want := range map[Selection]string{SelectChanged: StageStop, SelectStaged: StageCommit, SelectAll: StageCI, SelectPaths: StageStop} {
		if DefaultStage(sel) != want {
			t.Errorf("DefaultStage(%d)", sel)
		}
	}
}

func TestSuppressionsTable(t *testing.T) {
	t.Parallel()
	ok := "this is a sufficiently long reason"
	cases := []struct {
		line, rule string // rule "" = acceptable
	}{
		{"x := 1 //nolint:gosec // " + ok, ""},
		{"x := 1 //nolint", RuleSuppBare},
		{"x := 1 //nolint:gosec", RuleSuppReason},
		{"x := 1 //nolint:gosec // short", RuleSuppReason},
		{"x := 1 //nolint:all // " + ok, RuleSuppBlanket},
		{"package p //nolint:gosec // " + ok, RuleSuppBlanket},
		{`s := "//nolint"`, ""},
		{"import os  # noqa: S101 -- " + ok, ""},
		{"import os  # noqa", RuleSuppBare},
		{"# ruff: noqa", RuleSuppBlanket},
		{"x = f()  # type: ignore[attr-defined] -- " + ok, ""},
		{"x = f()  # type: ignore", RuleSuppBare},
		{"x = f()  # pyright: ignore[reportFoo]", RuleSuppReason},
		{"// eslint-disable-next-line no-undef -- " + ok, ""},
		{"/* eslint-disable */", RuleSuppBlanket},
		{"// eslint-disable-line", RuleSuppBare},
		{"// @ts-expect-error -- " + ok, ""},
		{"// @ts-ignore", RuleSuppBare},
		{"// @ts-nocheck", RuleSuppBlanket},
		{"x() # nosemgrep: rule.id -- " + ok, ""},
		{"x() // nosemgrep", RuleSuppBare},
		{`#[allow(dead_code, reason = "` + ok + `")]`, ""},
		{"#[allow(dead_code)]", RuleSuppReason},
		{"#![allow(dead_code)]", RuleSuppBlanket},
		{"# shellcheck disable=SC2086 -- " + ok, ""},
		{"# shellcheck disable=all", RuleSuppBlanket},
	}
	for _, tc := range cases {
		fs := scanSuppressions("f", tc.line+"\n")
		var got string
		if len(fs) > 0 {
			got = fs[0].Rule
		}
		if got != tc.rule {
			t.Errorf("%q: rule %q want %q (%v)", tc.line, got, tc.rule, fs)
		}
	}
	dir := t.TempDir()
	testutil.WriteFile(t, dir, "a.go", "x //nolint\n")
	testutil.WriteFile(t, dir, "bin", "\x00//nolint\n")
	fs := CheckSuppressions(dir, []string{"a.go", "bin", "missing"})
	if len(fs) != 1 || fs[0].Line != 1 || fs[0].Col != 3 || !strings.Contains(fs[0].Message, "linter id") {
		t.Fatalf("CheckSuppressions %v", fs)
	}
}
