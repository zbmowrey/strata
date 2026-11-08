package garden

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/forge"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
)

// RunMarker records the active /garden run (docs/spec/07-gardener.md §3);
// `strata doctor` reports it stale after 2 h.
const RunMarker = ".strata/garden.lock"

// StaleRun is the age after which a run marker is considered abandoned.
const StaleRun = 2 * time.Hour

// Session is the state of one /garden run, stored in the run marker.
type Session struct {
	Started  string `json:"started"`
	Branch   string `json:"branch"`
	Worktree string `json:"worktree"` // absolute
	Base     string `json:"base"`     // default branch name
	BaseRef  string `json:"base_ref"` // what the branch was created from
}

// StartResult is the output of `strata garden start`.
type StartResult struct {
	Counts   map[string]int `json:"counts"`
	Worktree string         `json:"worktree,omitempty"`
	Branch   string         `json:"branch,omitempty"`
	Base     string         `json:"base,omitempty"`
	PlanFile string         `json:"plan_file,omitempty"`
	Skipped  []string       `json:"skipped"`
	OpenPRs  []forge.PR     `json:"open_prs"`
	Tasks    int            `json:"tasks"`
	Fixes    int            `json:"fixes"`
	V        int            `json:"v"`
	Tidy     bool           `json:"tidy"`
}

// git runs git in dir and returns trimmed stdout.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // G204: fixed program, engine-built arguments
	cmd.Dir = dir
	cmd.Env = cleanGitEnv(os.Environ())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// cleanGitEnv drops repository-locating variables a git hook exports
// (GIT_DIR, GIT_INDEX_FILE, …) so commands act on cmd.Dir's repository.
func cleanGitEnv(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "GIT_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE", "GIT_PREFIX", "GIT_OBJECT_DIRECTORY", "GIT_COMMON_DIR":
			continue
		}
		out = append(out, kv)
	}
	return out
}

// claimRun creates the run marker exclusively.
func claimRun(inst *config.Instance, now time.Time) error {
	p := inst.Path(filepath.FromSlash(RunMarker))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return fmt.Errorf("garden marker: %w", err)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // G304: engine-owned marker
	if errors.Is(err, os.ErrExist) {
		hint := "finish or abort it: strata garden finish | strata garden abort"
		if fi, serr := os.Stat(p); serr == nil && now.Sub(fi.ModTime()) > StaleRun {
			hint = "the marker is stale; `strata doctor --fix` or `strata garden abort` clears it"
		}
		return fail.New(fail.Locked, hint, "a /garden run is already active (%s)", RunMarker)
	}
	if err != nil {
		return fmt.Errorf("garden marker: %w", err)
	}
	return f.Close() //nolint:wrapcheck // closing an empty new file
}

func writeSession(inst *config.Instance, s *Session) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("garden marker: %w", err)
	}
	return fsutil.WriteAtomic(inst.Path(filepath.FromSlash(RunMarker)), append(b, '\n'), 0o600)
}

// LoadSession reads the active run (fail.Usage when none).
func LoadSession(inst *config.Instance) (*Session, error) {
	raw, err := os.ReadFile(inst.Path(filepath.FromSlash(RunMarker))) //nolint:gosec // G304: engine-owned marker
	if errors.Is(err, os.ErrNotExist) {
		return nil, fail.New(fail.Usage, "start one with `strata garden start`", "no /garden run is active")
	}
	if err != nil {
		return nil, fmt.Errorf("garden marker: %w", err)
	}
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil || s.Branch == "" {
		return nil, fail.New(fail.Locked, "`strata garden abort` clears it", "garden marker %s is incomplete (a start may have failed)", RunMarker)
	}
	return &s, nil
}

// defaultBranch resolves the default branch and the ref to branch from:
// origin's HEAD (fetched) when there is a remote, else the local branch.
func defaultBranch(ctx context.Context, root string) (base, ref string) {
	if head, err := git(ctx, root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		base = strings.TrimPrefix(head, "origin/")
	}
	if base == "" {
		for _, b := range []string{"main", "master"} {
			if _, err := git(ctx, root, "rev-parse", "--verify", "--quiet", "refs/heads/"+b); err == nil {
				base = b
				break
			}
		}
	}
	if base == "" {
		base, _ = git(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	}
	if _, err := git(ctx, root, "fetch", "--quiet", "origin", base); err == nil {
		return base, "origin/" + base
	}
	return base, base
}

// Start begins a /garden run: marker, worktree and branch, the plan inside
// the worktree (minus tasks touching open garden PRs) and a commit of the
// mechanical fixes. A tidy garden cleans up after itself.
func Start(ctx context.Context, inst *config.Instance, now func() time.Time) (*StartResult, error) {
	if now == nil {
		now = time.Now
	}
	t := now()
	if err := claimRun(inst, t); err != nil {
		return nil, err
	}
	s, err := createWorktree(ctx, inst, t)
	if err != nil {
		_ = os.Remove(inst.Path(filepath.FromSlash(RunMarker)))
		return nil, err
	}
	res, err := planInWorktree(ctx, inst, s, now)
	if err != nil {
		_ = Abort(ctx, inst)
		return nil, err
	}
	if res.Tidy {
		if err := Abort(ctx, inst); err != nil {
			return nil, err
		}
		return &StartResult{V: 1, Tidy: true, Counts: map[string]int{}, Skipped: []string{}, OpenPRs: res.OpenPRs}, nil
	}
	return res, nil
}

func createWorktree(ctx context.Context, inst *config.Instance, t time.Time) (*Session, error) {
	base, ref := defaultBranch(ctx, inst.Root)
	stamp := t.Format("20060102-1504")
	prefix := inst.Config.Garden.BranchPrefix
	if prefix == "" {
		prefix = "garden/"
	}
	s := &Session{
		Started: t.UTC().Format(time.RFC3339), Branch: prefix + stamp, Base: base, BaseRef: ref,
		Worktree: inst.Path(".strata", "worktrees", "garden-"+stamp),
	}
	if _, err := git(ctx, inst.Root, "worktree", "add", "--quiet", "-b", s.Branch, s.Worktree, ref); err != nil {
		return nil, err
	}
	return s, writeSession(inst, s)
}

// planInWorktree runs the deterministic phase in the worktree, drops tasks
// that open garden PRs already touch, and commits the mechanical fixes.
func planInWorktree(ctx context.Context, inst *config.Instance, s *Session, now func() time.Time) (*StartResult, error) {
	wt, err := config.Open(s.Worktree, "")
	if err != nil {
		return nil, fail.Wrap(fail.NotInstance, "commit .strata/config.toml on the default branch first", err)
	}
	p, err := Run(ctx, wt, Options{Now: now, Apply: true, Plan: true})
	if err != nil {
		return nil, err
	}
	res := &StartResult{V: 1, Worktree: s.Worktree, Branch: s.Branch, Base: s.Base, PlanFile: wt.Path(filepath.FromSlash(PlanFile)), Skipped: []string{}}
	res.OpenPRs = openGardenPRs(ctx, inst)
	if touched := touchedFiles(res.OpenPRs); len(touched) > 0 {
		res.Skipped = p.dropTouching(touched)
		p.finish(wt.Config.Garden.Escalate)
		if err := p.write(res.PlanFile); err != nil {
			return nil, err
		}
	}
	committed, err := commitFixes(ctx, s.Worktree, p)
	if err != nil {
		return nil, err
	}
	res.Counts, res.Tasks = p.Counts, len(p.Tasks)
	if committed {
		res.Fixes = len(p.Fixes)
	}
	res.Tidy = res.Tasks == 0 && !committed
	return res, nil
}

func openGardenPRs(ctx context.Context, inst *config.Instance) []forge.PR {
	f, err := forge.Open(ctx, inst.Root, inst.Config.Garden.Forge)
	if err != nil {
		return []forge.PR{}
	}
	prs, err := f.FindOpen(ctx, inst.Config.Garden.BranchPrefix)
	if err != nil || prs == nil {
		return []forge.PR{}
	}
	return prs
}

func touchedFiles(prs []forge.PR) map[string]bool {
	out := map[string]bool{}
	for _, pr := range prs {
		for _, f := range pr.Files {
			out[f] = true
		}
	}
	return out
}

// dropTouching removes tasks whose evidence paths are in touched and
// returns their ids.
func (p *Plan) dropTouching(touched map[string]bool) []string {
	var skipped []string
	p.Tasks = slices.DeleteFunc(p.Tasks, func(t Task) bool {
		for _, path := range taskPaths(t) {
			if touched[path] {
				skipped = append(skipped, t.ID)
				return true
			}
		}
		return false
	})
	return nonNil(skipped)
}

func taskPaths(t Task) []string {
	var out []string
	if s, ok := t.Evidence[evPath].(string); ok && s != "" {
		out = append(out, s)
	}
	if ps, ok := t.Evidence["paths"].([]string); ok {
		out = append(out, ps...)
	}
	return out
}

// commitFixes commits the worktree's mechanical fixes (if any).
func commitFixes(ctx context.Context, wt string, p *Plan) (bool, error) {
	if _, err := git(ctx, wt, "add", "-A"); err != nil {
		return false, err
	}
	if _, err := git(ctx, wt, "diff", "--cached", "--quiet"); err == nil {
		return false, nil // nothing staged
	}
	var kinds []string
	for _, f := range p.Fixes {
		if f.Kind != "index" {
			kinds = append(kinds, f.Kind)
		}
	}
	msg := "garden(fixes): mechanical fixes\n\n" + strings.Join(kinds, ", ") + "\n"
	if _, err := git(ctx, wt, "commit", "--quiet", "-m", msg); err != nil {
		return false, err
	}
	return true, nil
}

// FinishOptions configure Finish.
type FinishOptions struct {
	Title   string
	Body    string
	NoReady bool
}

// FinishResult is the output of `strata garden finish`.
type FinishResult struct {
	PR         *forge.PR `json:"pr,omitempty"`
	CompareURL string    `json:"compare_url,omitempty"`
	Message    string    `json:"message,omitempty"`
	V          int       `json:"v"`
	Ready      bool      `json:"ready"`
}

// Finish pushes the garden branch, opens a draft PR with the digest, marks
// it ready (unless NoReady) and cleans up. The gardener never merges.
func Finish(ctx context.Context, inst *config.Instance, opt FinishOptions) (*FinishResult, error) {
	s, err := LoadSession(inst)
	if err != nil {
		return nil, err
	}
	ahead, err := git(ctx, inst.Root, "rev-list", "--count", s.BaseRef+".."+s.Branch)
	if err != nil {
		return nil, err
	}
	if n, _ := strconv.Atoi(ahead); n == 0 {
		return &FinishResult{V: 1, Message: "nothing to propose"}, Abort(ctx, inst)
	}
	if _, err := git(ctx, inst.Root, "push", "--quiet", "-u", "origin", s.Branch); err != nil {
		return nil, fail.Wrap(fail.Runtime, "the branch and worktree are kept; fix the remote and rerun `strata garden finish`", err)
	}
	res := &FinishResult{V: 1}
	if err := openPR(ctx, inst, s, opt, res); err != nil {
		return nil, err
	}
	return res, cleanup(ctx, inst, s, false)
}

func openPR(ctx context.Context, inst *config.Instance, s *Session, opt FinishOptions, res *FinishResult) error {
	title := opt.Title
	if title == "" {
		title = "garden: " + strings.TrimPrefix(s.Branch, inst.Config.Garden.BranchPrefix)
	}
	f, err := forge.Open(ctx, inst.Root, inst.Config.Garden.Forge)
	if errors.Is(err, forge.ErrNoCLI) {
		remote, rerr := git(ctx, inst.Root, "remote", "get-url", "origin")
		if rerr != nil {
			return rerr
		}
		res.CompareURL, err = forge.CompareURL(remote, s.Base, s.Branch)
		res.Message = "forge CLI not found; open the PR from the compare URL"
		return err //nolint:wrapcheck // CompareURL errors are descriptive
	}
	if err != nil {
		return err //nolint:wrapcheck // forge errors are descriptive
	}
	pr, err := f.CreateDraft(ctx, s.Branch, s.Base, title, opt.Body)
	if err != nil {
		return err //nolint:wrapcheck // forge errors are descriptive
	}
	res.PR = &pr
	if opt.NoReady {
		return nil
	}
	if err := f.MarkReady(ctx, strconv.Itoa(pr.Number)); err != nil {
		return err //nolint:wrapcheck // forge errors are descriptive
	}
	res.Ready = true
	return nil
}

// Abort removes the worktree, the unpushed local branch and the marker.
func Abort(ctx context.Context, inst *config.Instance) error {
	s, err := LoadSession(inst)
	if fail.CodeOf(err) == fail.Locked {
		return os.Remove(inst.Path(filepath.FromSlash(RunMarker))) //nolint:wrapcheck // os errors carry the path
	}
	if err != nil {
		return err
	}
	return cleanup(ctx, inst, s, true)
}

func cleanup(ctx context.Context, inst *config.Instance, s *Session, dropBranch bool) error {
	if _, err := os.Stat(s.Worktree); err == nil {
		if _, err := git(ctx, inst.Root, "worktree", "remove", "--force", s.Worktree); err != nil {
			return err
		}
	}
	if dropBranch {
		_, _ = git(ctx, inst.Root, "branch", "-D", s.Branch)
	}
	if err := os.Remove(inst.Path(filepath.FromSlash(RunMarker))); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("garden marker: %w", err)
	}
	return nil
}
