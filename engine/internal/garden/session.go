package garden

import (
	"bytes"
	"context"
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
)

// RunMarker is the run lease of the active garden run (docs/spec/07 §9.2).
const RunMarker = ".strata/garden.lock"

// Session is the run lease: the state of one garden run and who holds it.
// v1 markers (no run, fence or heartbeat) still load; their start time
// stands in for the heartbeat.
type Session struct {
	Owner       Owner  `json:"owner"`
	Run         string `json:"run,omitempty"`
	Started     string `json:"started"`
	HeartbeatAt string `json:"heartbeat_at,omitempty"`
	TTL         string `json:"ttl,omitempty"`
	Branch      string `json:"branch"`
	Worktree    string `json:"worktree"` // absolute
	Base        string `json:"base"`     // default branch name
	BaseRef     string `json:"base_ref"` // what the branch was created from
	V           int    `json:"v"`
	Fence       int    `json:"fence,omitempty"`
	Attempt     int    `json:"attempt,omitempty"`
}

// StartResult is the output of `strata garden start`.
type StartResult struct {
	Counts   map[string]int `json:"counts"`
	Run      string         `json:"run,omitempty"`
	Worktree string         `json:"worktree,omitempty"`
	Branch   string         `json:"branch,omitempty"`
	Base     string         `json:"base,omitempty"`
	PlanFile string         `json:"plan_file,omitempty"`
	Skipped  []string       `json:"skipped"`
	OpenPRs  []forge.PR     `json:"open_prs"`
	Tasks    int            `json:"tasks"`
	Fixes    int            `json:"fixes"`
	Fence    int            `json:"fence,omitempty"`
	Attempt  int            `json:"attempt,omitempty"`
	// Resume only: the run continues; Remaining lists its open tasks and
	// Salvage names a patch of uncommitted work that could not be committed.
	Remaining []string `json:"remaining,omitempty"`
	Salvage   string   `json:"salvage,omitempty"`
	Resumed   bool     `json:"resumed,omitempty"`
	V         int      `json:"v"`
	Tidy      bool     `json:"tidy"`
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

// LoadSession reads the run lease (fail.Usage when none).
func LoadSession(inst *config.Instance) (*Session, error) {
	s, exists, err := readLease(inst.Root)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fail.New(fail.Usage, "start one with `strata garden start`", "no /garden run is active")
	}
	if s.Branch == "" {
		return nil, fail.New(fail.Locked, "`strata garden abort` clears it", "garden lease %s is incomplete (a start may have failed)", RunMarker)
	}
	return s, nil
}

// LeaseLive reports whether s is held right now under inst's ttl.
func LeaseLive(inst *config.Instance, s *Session, now time.Time) bool {
	return s.Live(now, leaseTTL(inst))
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
func Start(ctx context.Context, inst *config.Instance, now func() time.Time, owner Owner) (*StartResult, error) {
	if now == nil {
		now = time.Now
	}
	t := now()
	if owner.Launcher == "" {
		owner.Launcher = "manual"
	}
	lease, err := acquireLease(inst, t, t.Format("20060102-1504"), owner)
	if err != nil {
		return nil, err
	}
	s, err := createWorktree(ctx, inst, t, lease)
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
		if err := abortWith(ctx, inst, OutcomeTidy, t); err != nil {
			return nil, err
		}
		return &StartResult{V: 2, Tidy: true, Counts: map[string]int{}, Skipped: []string{}, OpenPRs: res.OpenPRs}, nil
	}
	return res, nil
}

// createWorktree adds the run's worktree and branch, records them in the
// lease and writes the fence into the worktree's git directory.
func createWorktree(ctx context.Context, inst *config.Instance, t time.Time, lease *Session) (*Session, error) {
	base, ref := defaultBranch(ctx, inst.Root)
	prefix := inst.Config.Garden.BranchPrefix
	if prefix == "" {
		prefix = "garden/"
	}
	branch, worktree := prefix+lease.Run, inst.Path(".strata", "worktrees", "garden-"+lease.Run)
	if _, err := git(ctx, inst.Root, "worktree", "add", "--quiet", "-b", branch, worktree, ref); err != nil {
		return nil, err
	}
	var out Session
	err := updateLease(inst.Root, lease.Run, lease.Fence, func(s *Session) {
		s.Branch, s.Worktree, s.Base, s.BaseRef = branch, worktree, base, ref
		s.HeartbeatAt = t.UTC().Format(time.RFC3339)
		out = *s
	})
	if err != nil {
		return nil, err
	}
	return &out, writeFence(worktree, inst.Root, &out)
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
	res := &StartResult{
		V: 2, Run: s.Run, Fence: s.Fence, Worktree: s.Worktree, Branch: s.Branch, Base: s.Base,
		PlanFile: wt.Path(filepath.FromSlash(PlanFile)), Skipped: []string{},
	}
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
	if res.Tidy {
		return res, nil
	}
	// The run's plan and ledger live in the main tree (07 §9.3).
	if err := initRun(inst.Root, s.Run, p); err != nil {
		return nil, err
	}
	res.PlanFile = filepath.Join(RunDir(inst.Root, s.Run), runPlanFile)
	record(inst.Root, s, OutcomeStarted, now(), nil)
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
	Fence   int // when set, must be the lease's current fence (else fail.Fenced)
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
	if opt.Fence > 0 && opt.Fence != s.Fence {
		return nil, fenced(s.Run, opt.Fence, s)
	}
	ahead, err := git(ctx, inst.Root, "rev-list", "--count", s.BaseRef+".."+s.Branch)
	if err != nil {
		return nil, err
	}
	if n, _ := strconv.Atoi(ahead); n == 0 {
		return &FinishResult{V: 1, Message: "nothing to propose"}, abortWith(ctx, inst, OutcomeTidy, time.Now())
	}
	if opt.Body == "" {
		opt.Body = RenderDigest(inst.Root, s)
	}
	if _, err := git(ctx, inst.Root, "push", "--quiet", "-u", "origin", s.Branch); err != nil {
		return nil, fail.Wrap(fail.Runtime, "the branch and worktree are kept; fix the remote and rerun `strata garden finish`", err)
	}
	res := &FinishResult{V: 1}
	if err := openPR(ctx, inst, s, opt, res); err != nil {
		return nil, err
	}
	record(inst.Root, s, OutcomePR, time.Now(), func(e *HistoryEntry) {
		if res.PR != nil {
			e.PR = res.PR.Number
		}
	})
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
	// A retried finish reuses the PR an earlier attempt already opened.
	pr, found := existingPR(ctx, f, s.Branch)
	if !found {
		if pr, err = f.CreateDraft(ctx, s.Branch, s.Base, title, opt.Body); err != nil {
			return err //nolint:wrapcheck // forge errors are descriptive
		}
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

// existingPR finds an open PR whose head is exactly branch.
func existingPR(ctx context.Context, f forge.Forge, branch string) (forge.PR, bool) {
	prs, err := f.FindOpen(ctx, branch)
	if err != nil {
		return forge.PR{}, false
	}
	for _, pr := range prs {
		if pr.Branch == branch {
			return pr, true
		}
	}
	return forge.PR{}, false
}

// Abort removes the worktree, the unpushed local branch and the lease, and
// records the run as aborted.
func Abort(ctx context.Context, inst *config.Instance) error {
	return abortWith(ctx, inst, OutcomeAborted, time.Now())
}

func abortWith(ctx context.Context, inst *config.Instance, outcome string, now time.Time) error {
	s, err := LoadSession(inst)
	if fail.CodeOf(err) == fail.Locked {
		return os.Remove(inst.Path(filepath.FromSlash(RunMarker))) //nolint:wrapcheck // os errors carry the path
	}
	if err != nil {
		return err
	}
	if s.Run != "" {
		record(inst.Root, s, outcome, now, nil)
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
