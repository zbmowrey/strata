package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/knowledge"
	"github.com/zbmowrey/strata/engine/internal/perms"
	"github.com/zbmowrey/strata/engine/internal/projects"
	"github.com/zbmowrey/strata/engine/internal/scaffold"
	"github.com/zbmowrey/strata/engine/internal/scripts"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
)

// PreCommitMarker identifies the git pre-commit hook Strata installs.
const PreCommitMarker = "# strata:pre-commit"

// PreCommitHook is the installed hook script.
const PreCommitHook = "#!/bin/sh\n" + PreCommitMarker + " — installed by `strata init`; runs commit-stage gates.\nexec strata check --staged\n"

// scaffoldFiles creates the user-owned starter files and directories.
func (r *run) scaffoldFiles() error {
	v := scaffold.VarsFor(r.inst.Config, r.opt.Version)
	files := []struct{ tmpl, rel string }{
		{"synonyms.txt", ".strata/synonyms.txt"},
		{"retrieval.yaml", ".strata/bench/retrieval.yaml"},
	}
	if r.opt.CI {
		files = append(files, struct{ tmpl, rel string }{"ci.yml", ".github/workflows/strata.yml"})
	}
	for _, f := range files {
		body, err := scaffold.Render(f.tmpl, v)
		if err != nil {
			return err
		}
		if err := r.createFile(f.rel, []byte(body)); err != nil {
			return err
		}
	}
	p := r.inst.Config.Paths
	for _, dir := range []string{p.Knowledge + "/_inbox", p.Knowledge + "/_questions", p.Scripts} {
		if err := r.createFile(dir+"/.gitkeep", nil); err != nil {
			return err
		}
	}
	return nil
}

// managed refreshes every Strata-owned block and generated file.
func (r *run) managed() error {
	v := scaffold.VarsFor(r.inst.Config, r.opt.Version)
	block, err := scaffold.ClaudeBlock(v)
	if err != nil {
		return err
	}
	if err := r.ensureBlock("CLAUDE.md", "protocol block", scaffold.ClaudeMarkers, block); err != nil {
		return err
	}
	if err := r.gitignore(); err != nil {
		return err
	}
	if !r.opt.NoHooks {
		if err := r.settings(); err != nil {
			return err
		}
	}
	return r.generated()
}

// gitignore refreshes the local-state and projects blocks, replacing a
// legacy bare projects ignore line that would defeat track re-includes.
func (r *run) gitignore() error {
	if err := r.dropLegacyProjectsLine(); err != nil {
		return err
	}
	if err := r.ensureBlock(".gitignore", "local-state block", scaffold.LocalMarkers, scaffold.LocalIgnores(r.inst.Config.Paths)); err != nil {
		return err
	}
	ps, err := projects.New(r.inst, r.opt.Now).All()
	if err != nil {
		return err
	}
	body, warnings := projects.GitignoreBlock(r.inst.Config.Paths.Projects, ps)
	r.res.Warnings = append(r.res.Warnings, warnings...)
	return r.ensureBlock(".gitignore", "projects block", projects.GitignoreMarkers, body)
}

func (r *run) dropLegacyProjectsLine() error {
	p := r.inst.Path(".gitignore")
	raw, err := os.ReadFile(p) //nolint:gosec // G304: the instance's .gitignore
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read .gitignore: %w", err)
	}
	if _, _, ok := fsutil.FindBlock(raw, projects.GitignoreMarkers); ok {
		return nil
	}
	legacy := map[string]bool{"/" + r.inst.Config.Paths.Projects + "/": true, r.inst.Config.Paths.Projects + "/": true, "/" + r.inst.Config.Paths.Projects: true}
	lines := strings.SplitAfter(string(raw), "\n")
	kept := lines[:0]
	for _, l := range lines {
		if !legacy[strings.TrimSpace(l)] {
			kept = append(kept, l)
		}
	}
	if len(kept) == len(lines) {
		return nil
	}
	r.note(".gitignore", Update, "replaced bare projects ignore with the managed projects block")
	if r.opt.DryRun {
		return nil
	}
	return fsutil.WriteAtomic(p, []byte(strings.Join(kept, "")), 0o644)
}

// settings wires hooks and the allowlist into .claude/settings.json.
func (r *run) settings() error {
	changed, err := perms.SyncHooks(r.inst.Root, false, r.opt.DryRun)
	if err != nil {
		return err
	}
	r.noteChanged(perms.SettingsFile, changed, "hooks")
	changed, err = perms.Sync(r.inst, r.opt.DryRun)
	if err != nil {
		return err
	}
	r.noteChanged(perms.SettingsFile, changed, "permission allowlist")
	return nil
}

func (r *run) noteChanged(rel string, changed bool, what string) {
	if changed {
		r.note(rel, Update, what)
	} else {
		r.note(rel, Skip, what+" current")
	}
}

// generated regenerates hubs and script catalogs (skipped on dry runs:
// they are derived files whose content is reported by their own commands).
func (r *run) generated() error {
	if r.opt.DryRun {
		r.note(r.inst.Config.Paths.Knowledge+"/_hubs/", Skip, "hubs and catalogs regenerate on a real run")
		return nil
	}
	hubs, err := knowledge.New(r.inst, r.opt.Now, telemetry.New(r.inst, r.opt.Now)).Hubs()
	if err != nil {
		return err
	}
	for _, h := range hubs {
		r.note(h, Update, "hub")
	}
	cats, err := scripts.WriteCatalog(r.inst)
	if err != nil {
		return err
	}
	for _, c := range cats {
		r.note(c, Update, "catalog")
	}
	return nil
}

// gitHook installs the pre-commit hook unless a foreign one exists.
func (r *run) gitHook() error {
	gitDir := r.inst.Path(".git")
	if !isDir(gitDir) {
		r.res.Warnings = append(r.res.Warnings, "not a git checkout (or a worktree): pre-commit hook not installed")
		return nil
	}
	rel := ".git/hooks/pre-commit"
	p := filepath.Join(gitDir, "hooks", "pre-commit")
	raw, err := os.ReadFile(p) //nolint:gosec // G304: the repository's own git hook
	switch {
	case err == nil && hasPrefixLine(raw, PreCommitMarker):
		r.note(rel, Skip, "installed")
		return nil
	case err == nil:
		r.note(rel, Skip, "a pre-commit hook exists; add `strata check --staged` to it")
		return nil
	}
	r.note(rel, Create, "runs strata check --staged")
	if r.opt.DryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return fmt.Errorf("git hook: %w", err)
	}
	return fsutil.WriteAtomic(p, []byte(PreCommitHook), 0o755) //nolint:gosec // G302: git hooks must be executable
}

// gateFiles writes gates.toml and tools.toml for the selected profiles
// (user-owned once written: never overwritten).
func (r *run) gateFiles() error {
	profiles := r.opt.Profiles
	if len(profiles) == 0 {
		profiles = scaffold.DetectProfiles(r.inst.Root)
	}
	g, err := scaffold.RenderGates(profiles)
	if err != nil {
		return fail.Wrap(fail.Usage, "profiles: "+strings.Join(scaffold.Profiles, ", "), err)
	}
	if err := r.createFile(".strata/gates.toml", g); err != nil {
		return err
	}
	t, err := scaffold.RenderTools(profiles)
	if err != nil {
		return err
	}
	if err := r.createFile(".strata/tools.toml", t); err != nil {
		return err
	}
	if r.opt.Scripts {
		return r.scriptsLib(profiles)
	}
	return nil
}

// scriptsLib creates the scripts modules and shared lib starters.
func (r *run) scriptsLib(profiles []string) error {
	var runtimes []string
	for _, p := range profiles {
		switch p {
		case "go":
			runtimes = append(runtimes, scripts.RuntimeGo)
		case "python":
			runtimes = append(runtimes, scripts.RuntimePython)
		}
	}
	if len(runtimes) == 0 {
		runtimes = []string{r.inst.Config.Scripts.DefaultRuntime}
	}
	if r.opt.DryRun {
		r.note(r.inst.Config.Paths.Scripts+"/lib/", Create, "shared lib starters for "+strings.Join(runtimes, ", ")+" (missing files only)")
		return nil
	}
	written, err := scripts.WriteLib(r.ctx, r.inst, runtimes)
	if err != nil {
		return err
	}
	for _, w := range written {
		r.note(w, Create, "")
	}
	return nil
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
