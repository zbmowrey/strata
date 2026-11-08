package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/setup"
	"github.com/zbmowrey/strata/engine/internal/testutil"
	"github.com/zbmowrey/strata/engine/internal/version"
)

var now = func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) }

func fakeTools(out map[string]string) func(context.Context, []string) (string, error) {
	return func(_ context.Context, argv []string) (string, error) {
		if o, ok := out[argv[0]]; ok {
			return o, nil
		}
		return "", errors.New("not found")
	}
}

func instance(t *testing.T) *config.Instance {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "hooks"), 0o750); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, dir, "go.mod", "module x\n")
	if _, err := setup.Init(context.Background(), dir, setup.Options{Now: now}); err != nil {
		t.Fatal(err)
	}
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

func byName(cs []Check) map[string]Check {
	m := map[string]Check{}
	for _, c := range cs {
		m[c.Name] = c
	}
	return m
}

func TestHealthyInstance(t *testing.T) {
	t.Parallel()
	inst := instance(t)
	tools := fakeTools(map[string]string{
		"gofumpt": "v0.12.0", "go": "goimports: go1.27\n\tmod\tgolang.org/x/tools\tv0.52.0", "golangci-lint": "golangci-lint has version 2.15.0 built with go1.27",
		"govulncheck": "Go: go1.27.3\nScanner: govulncheck@v1.9.0", "gitleaks": "8.30.1", "semgrep": "1.169.0", "typos": "typos-cli 1.52.0", "osv-scanner": "osv-scanner version: 2.7.0",
	})
	cs := Run(context.Background(), inst, Options{Now: now, ToolsRun: tools})
	m := byName(cs)
	for _, name := range []string{"engine", "managed", "git-hook", "index", "gates", "lint", "tool:govulncheck", "tool:golangci-lint"} {
		if m[name].Status != OK {
			t.Errorf("%s: %+v", name, m[name])
		}
	}
	if m["tool:govulncheck"].Message != "govulncheck 1.9.0" {
		t.Errorf("version detection: %q", m["tool:govulncheck"].Message)
	}
	if Worst(cs) != OK {
		t.Errorf("worst %s: %+v", Worst(cs), cs)
	}
}

func TestProblemsAndFix(t *testing.T) {
	t.Parallel()
	inst := instance(t)
	testutil.WriteFile(t, inst.Root, "CLAUDE.md", "# only mine\n")
	if err := os.Remove(inst.Path(".git", "hooks", "pre-commit")); err != nil {
		t.Fatal(err)
	}
	lock := testutil.WriteFile(t, inst.Root, ".strata/garden.lock", "")
	old := now().Add(-3 * time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, inst.Root, "knowledge/x/y/bad.md", "---\nid: bad\n---\n")
	tools := fakeTools(map[string]string{"gofumpt": "v0.10.0"})
	m := byName(Run(context.Background(), inst, Options{Now: now, ToolsRun: tools}))
	for name, status := range map[string]string{"managed": Warn, "git-hook": Warn, "garden-lock": Warn, "lint": Fail, "tool:gofumpt": Warn, "tool:typos": Warn} {
		if m[name].Status != status {
			t.Errorf("%s: want %s, got %+v", name, status, m[name])
		}
	}
	if !strings.Contains(m["tool:gofumpt"].Message, "0.10.0 < pinned 0.12.0") || m["tool:typos"].Fix == "" {
		t.Errorf("tool messages: %+v %+v", m["tool:gofumpt"], m["tool:typos"])
	}
	fixed := byName(Run(context.Background(), inst, Options{Now: now, ToolsRun: tools, Fix: true}))
	for _, name := range []string{"managed", "git-hook", "garden-lock"} {
		if !fixed[name].Fixed {
			t.Errorf("%s not fixed: %+v", name, fixed[name])
		}
	}
	after := byName(Run(context.Background(), inst, Options{Now: now, ToolsRun: tools}))
	if after["managed"].Status != OK || after["git-hook"].Status != OK {
		t.Errorf("after fix: %+v %+v", after["managed"], after["git-hook"])
	}
	if _, ok := after["garden-lock"]; ok {
		t.Error("lock not cleared")
	}
}

func TestGatesAndEngineEdges(t *testing.T) {
	t.Parallel()
	inst := instance(t)
	testutil.WriteFile(t, inst.Root, ".strata/gates.toml", "[[gate]]\nid = \"x\"\ncmd = [\"true\"]\nstages = [\"ci\"]\ndisabled = true\nreason = \"flaky upstream tool\"\n")
	testutil.WriteFile(t, inst.Root, ".strata/tools.toml", "[tools.none]\nversion = \"1.0.0\"\n")
	inst.Config.Engine.MinVersion = "99.0.0"
	m := byName(Run(context.Background(), inst, Options{Now: now, ToolsRun: fakeTools(nil)}))
	if m["engine"].Status != Fail || m["gates-disabled"].Status != Info || m["tool:none"].Status != Info {
		t.Fatalf("%+v", m)
	}
	inst.Config.Engine.MinVersion = "garbage"
	testutil.WriteFile(t, inst.Root, ".strata/gates.toml", "")
	testutil.WriteFile(t, inst.Root, ".strata/tools.toml", "not toml [")
	m = byName(Run(context.Background(), inst, Options{Now: now}))
	if m["engine"].Status != Warn || m["gates"].Status != Warn || m["tools"].Status != Fail {
		t.Fatalf("%+v", m)
	}
	if Worst([]Check{{Status: Info}, {Status: Warn}}) != Warn || Worst([]Check{{Status: Fail, Fixed: true}}) != OK {
		t.Fatal("Worst")
	}
}

func TestResolveArgs(t *testing.T) {
	t.Parallel()
	got := resolveArgs([]string{"go", "version", "-m", "go"})
	if got[3] == "go" || !strings.HasSuffix(got[3], "go") {
		t.Fatalf("not resolved: %v", got)
	}
	if got := resolveArgs([]string{"go", "version", "-m", "no-such-tool-xyz"}); got[3] != "no-such-tool-xyz" {
		t.Fatalf("unknown tool changed: %v", got)
	}
}

func TestPluginCheck(t *testing.T) {
	t.Parallel()
	inst := instance(t)
	dir := t.TempDir()
	check := func(content string) Check {
		t.Helper()
		p := filepath.Join(dir, "installed_plugins.json")
		_ = os.Remove(p)
		if content != "" {
			testutil.WriteFile(t, dir, "installed_plugins.json", content)
		}
		return byName(Run(context.Background(), inst, Options{Now: now, ToolsRun: fakeTools(nil), PluginsFile: p}))[checkPlugin]
	}
	release := strings.TrimSuffix(version.Current(), "-dev")
	if c := check(`{"version":2,"plugins":{"strata@strata":[{"scope":"user","version":"` + release + `"}]}}`); c.Status != OK {
		t.Errorf("matching plugin: %+v", c)
	}
	if c := check(`{"version":2,"plugins":{"strata@strata":[{"version":"0.0.1"}]}}`); c.Status != Warn || !strings.Contains(c.Message, "0.0.1") {
		t.Errorf("old plugin: %+v", c)
	}
	if c := check(`{"version":2,"plugins":{"other@x":[{"version":"1.0.0"}]}}`); c.Status != Warn || c.Fix != pluginInstall {
		t.Errorf("not installed: %+v", c)
	}
	if c := check(""); c.Status != Warn || c.Fix != pluginInstall {
		t.Errorf("no plugins file: %+v", c)
	}
	if c := check("not json"); c.Status != Warn {
		t.Errorf("unreadable: %+v", c)
	}
	if _, ok := byName(Run(context.Background(), inst, Options{Now: now, ToolsRun: fakeTools(nil)}))[checkPlugin]; ok {
		t.Error("empty PluginsFile must skip the check")
	}
}

func TestDefaultPluginsFile(t *testing.T) { //nolint:paralleltest // t.Setenv cannot run in a parallel test
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if got := DefaultPluginsFile(); got != filepath.Join(dir, "plugins", "installed_plugins.json") {
		t.Fatalf("got %q", got)
	}
}
