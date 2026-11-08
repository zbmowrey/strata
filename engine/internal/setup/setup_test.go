package setup

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/perms"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

var now = func() time.Time { return time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC) }

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "hooks"), 0o750); err != nil {
		t.Fatal(err)
	}
	return dir
}

func actions(r *Result) map[string]string {
	m := map[string]string{}
	for _, c := range r.Changes {
		m[c.Path+"|"+c.Note] = c.Action
	}
	return m
}

func TestInitFreshDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	dir := gitRepo(t)
	res, err := Init(context.Background(), dir, Options{Now: now, DryRun: true, Profiles: []string{"go"}, CI: true, Scripts: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".strata/config.toml", ".strata/gates.toml", "CLAUDE.md", ".github/workflows/strata.yml", ".git/hooks/pre-commit", "scripts/lib/"} {
		if !slices.Contains(res.Changed(), want) {
			t.Errorf("dry run missing %s: %v", want, res.Changed())
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 { // only .git
		t.Fatalf("dry run wrote files: %v", entries)
	}
}

func TestInitIdempotent(t *testing.T) {
	t.Parallel()
	dir := gitRepo(t)
	testutil.WriteFile(t, dir, "go.mod", "module example.com/x\n\ngo 1.26\n")
	testutil.WriteFile(t, dir, "CLAUDE.md", "# Mine\n\nkeep me\n")
	testutil.WriteFile(t, dir, ".gitignore", "node_modules\n/projects/\n")
	testutil.WriteFile(t, dir, ".claude/settings.json", `{"model":"x","permissions":{"allow":["Bash(make:*)"]}}`)
	res, err := Init(context.Background(), dir, Options{Now: now, Scripts: true, Version: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if a := actions(res); a[".gitignore|replaced bare projects ignore with the managed projects block"] != Update {
		t.Errorf("legacy projects line not replaced: %v", a)
	}
	claude := read(t, dir, "CLAUDE.md")
	if !strings.HasPrefix(claude, "# Mine\n\nkeep me\n") || !strings.Contains(claude, "<!-- strata:begin v1") {
		t.Fatalf("CLAUDE.md:\n%s", claude)
	}
	gi := read(t, dir, ".gitignore")
	if strings.Contains(gi, "\n/projects/\n") || !strings.Contains(gi, "# >>> strata:projects") || !strings.Contains(gi, "/.strata/index.db*") {
		t.Fatalf(".gitignore:\n%s", gi)
	}
	settings := read(t, dir, ".claude/settings.json")
	for _, want := range []string{`"model": "x"`, "Bash(make:*)", "strata hook post-edit"} {
		if !strings.Contains(settings, want) {
			t.Errorf("settings missing %s:\n%s", want, settings)
		}
	}
	if !strings.Contains(read(t, dir, ".strata/gates.toml"), `id = "go-lint"`) || !exists(filepath.Join(dir, "scripts", "go.mod")) {
		t.Error("go profile not detected or scripts lib missing")
	}
	if fi, err := os.Stat(filepath.Join(dir, ".git", "hooks", "pre-commit")); err != nil || fi.Mode()&0o100 == 0 {
		t.Fatalf("pre-commit hook: %v", err)
	}
	again, err := Init(context.Background(), dir, Options{Now: now, Scripts: true, Version: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	var changed []string
	for _, c := range again.Changes {
		if c.Action != Skip && !strings.HasPrefix(c.Path, "knowledge/_hubs") && c.Path != "scripts/CATALOG.md" {
			changed = append(changed, c.Path+" "+c.Note)
		}
	}
	if len(changed) > 0 {
		t.Fatalf("second init changed: %v", changed)
	}
}

func TestInitForeignPreCommitAndNoHooks(t *testing.T) {
	t.Parallel()
	dir := gitRepo(t)
	testutil.WriteFile(t, dir, ".git/hooks/pre-commit", "#!/bin/sh\nmake lint\n")
	res, err := Init(context.Background(), dir, Options{Now: now, NoHooks: true})
	if err != nil {
		t.Fatal(err)
	}
	if a := actions(res); a[".git/hooks/pre-commit|a pre-commit hook exists; add `strata check --staged` to it"] != Skip {
		t.Fatalf("foreign hook: %v", a)
	}
	if exists(filepath.Join(dir, perms.SettingsFile)) {
		t.Fatal("--no-hooks wrote settings.json")
	}
}

func TestInitNotGitAndBadProfile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	res, err := Init(context.Background(), dir, Options{Now: now, NoHooks: true})
	if err != nil || len(res.Warnings) == 0 {
		t.Fatalf("err %v warnings %v", err, res.Warnings)
	}
	if _, err := Init(context.Background(), t.TempDir(), Options{Now: now, Profiles: []string{"cobol"}}); err == nil {
		t.Fatal("unknown profile accepted")
	}
}

func TestUpgradeRefreshesBlock(t *testing.T) {
	t.Parallel()
	dir := gitRepo(t)
	if _, err := Init(context.Background(), dir, Options{Now: now}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "CLAUDE.md")
	raw := read(t, dir, "CLAUDE.md")
	if err := os.WriteFile(p, []byte(strings.Replace(raw, "Search before you act", "Search later", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Upgrade(context.Background(), inst, Options{DryRun: true})
	if err != nil || !slices.Contains(res.Changed(), "CLAUDE.md") {
		t.Fatalf("dry upgrade: %v %v", err, res.Changed())
	}
	if _, err := Upgrade(context.Background(), inst, Options{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, dir, "CLAUDE.md"), "Search before you act") {
		t.Fatal("block not restored")
	}
	if res, err := InstallGitHook(context.Background(), inst, false); err != nil || len(res.Changed()) != 0 {
		t.Fatalf("hook reinstall: %v %v", err, res.Changed())
	}
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
