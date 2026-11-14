package garden

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/knowledge"
)

const (
	poisonTries        = 2 // a task interrupted this many times is skipped and escalated
	defaultMaxAttempts = 3
	terminateWait      = 10 * time.Second
)

// Verify runs the edit/stop-stage gates over files in a worktree and
// reports whether they are clean. The CLI wires it to internal/gates.
type Verify func(ctx context.Context, wt *config.Instance, files []string) (bool, error)

// ResumeOptions configure Resume.
type ResumeOptions struct {
	Now    func() time.Time
	Verify Verify
	Owner  Owner
	Force  bool // take over a live lease, or a stuck run
}

// Resume takes over a lost run and prepares a fresh worktree for it
// (docs/spec/07 §9.3): new fence and attempt, the old owner stopped, the
// old worktree's uncommitted work salvaged, interrupted tasks reset and
// poison tasks escalated.
func Resume(ctx context.Context, inst *config.Instance, opt ResumeOptions) (*StartResult, error) {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	now := opt.Now()
	cur, err := LoadSession(inst)
	if err != nil {
		return nil, err
	}
	prog, err := LoadProgress(inst.Root, cur.Run)
	if err != nil {
		return nil, err
	}
	if cur.Attempt >= maxAttempts(inst) && !opt.Force {
		record(inst.Root, cur, OutcomeStuck, now, nil)
		writeLastFailure(inst.Root, cur, prog, "max_attempts reached")
		return nil, fail.New(fail.Locked, "see .strata/garden/last-failure; `strata garden resume --force` or `strata garden abort`",
			"garden run %s is stuck after %d attempts", cur.Run, cur.Attempt)
	}
	if _, err := git(ctx, inst.Root, "rev-parse", "--verify", "--quiet", "refs/heads/"+cur.Branch); err != nil {
		return nil, fail.New(fail.Usage, "nothing to resume; `strata garden abort` clears the lease", "garden branch %s is gone", cur.Branch)
	}
	old, next, err := takeOver(inst, cur, now, opt)
	if err != nil {
		return nil, err
	}
	stopOwner(old)
	record(inst.Root, old, OutcomeLost, now, nil)
	res := &StartResult{
		V: 2, Run: next.Run, Fence: next.Fence, Branch: next.Branch, Base: next.Base, Resumed: true, Attempt: next.Attempt,
		PlanFile: filepath.Join(RunDir(inst.Root, next.Run), runPlanFile), Skipped: []string{}, Counts: map[string]int{},
	}
	patch, err := salvagePatch(ctx, old.Worktree)
	if err != nil {
		return nil, err
	}
	if res.Worktree, err = rebuildWorktree(ctx, inst, old, next); err != nil {
		return nil, err
	}
	wt, err := config.Open(res.Worktree, "")
	if err != nil {
		return nil, fail.Wrap(fail.NotInstance, "the garden branch lost .strata/config.toml", err)
	}
	if res.Salvage, err = applySalvage(ctx, inst.Root, wt, next, patch, opt.Verify); err != nil {
		return nil, err
	}
	if err := resetInterrupted(ctx, inst, wt, prog, opt.Now); err != nil {
		return nil, err
	}
	record(inst.Root, next, OutcomeStarted, now, nil)
	res.Remaining = prog.Remaining()
	res.Tasks = len(res.Remaining)
	res.Counts = prog.Counts()
	return res, nil
}

func maxAttempts(inst *config.Instance) int {
	if n := inst.Config.Garden.MaxAttempts; n > 0 {
		return n
	}
	return defaultMaxAttempts
}

// takeOver bumps fence and attempt under the mutex, re-checking that the
// lease is still the one Resume inspected and is not live (unless forced).
func takeOver(inst *config.Instance, seen *Session, now time.Time, opt ResumeOptions) (old, next *Session, err error) {
	ttl := leaseTTL(inst)
	err = withLeaseMutex(inst.Root, func() error {
		cur, exists, err := readLease(inst.Root)
		if err != nil {
			return err
		}
		if !exists || cur.Run != seen.Run || cur.Fence != seen.Fence {
			return fail.New(fail.Locked, "another session resumed it first", "garden run %s changed hands", seen.Run)
		}
		if cur.Live(now, ttl) && !opt.Force {
			return fail.New(fail.Locked, "`strata garden resume --force` takes it over anyway",
				"garden run %s is live (heartbeat %s ago)", cur.Run, cur.HeartbeatAge(now).Round(time.Second))
		}
		o := *cur
		n := *cur
		n.Fence, n.Attempt, n.Owner = cur.Fence+1, cur.Attempt+1, opt.Owner
		if n.Owner.Host == "" {
			n.Owner.Host = hostname()
		}
		if n.Owner.Launcher == "" {
			n.Owner.Launcher = "manual"
		}
		n.HeartbeatAt = now.UTC().Format(time.RFC3339)
		old, next = &o, &n
		return writeLease(inst.Root, &n)
	})
	return old, next, err
}

// stopOwner terminates the previous holder when it is a live process on
// this host (a hung supervisor), waiting briefly for it to exit.
func stopOwner(s *Session) {
	if s.Owner.PID <= 0 || s.Owner.Host != hostname() || !pidAlive(s.Owner.PID) {
		return
	}
	terminate(s.Owner.PID)
	deadline := time.Now().Add(terminateWait)
	for pidAlive(s.Owner.PID) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
}

// salvagePatch captures every uncommitted change (untracked files too) in
// the old worktree as a binary patch; nil when there is none or no worktree.
func salvagePatch(ctx context.Context, worktree string) ([]byte, error) {
	if _, err := os.Stat(worktree); err != nil {
		return nil, nil //nolint:nilerr // a missing worktree simply has nothing to salvage
	}
	if _, err := git(ctx, worktree, "add", "-A"); err != nil {
		return nil, err
	}
	return gitRaw(ctx, worktree, "diff", "--cached", "--binary", "HEAD")
}

// rebuildWorktree removes the old worktree and checks the branch out in a
// fresh one named for the attempt, then writes the new fence into it.
func rebuildWorktree(ctx context.Context, inst *config.Instance, old, next *Session) (string, error) {
	if _, err := os.Stat(old.Worktree); err == nil {
		if _, err := git(ctx, inst.Root, "worktree", "remove", "--force", old.Worktree); err != nil {
			return "", err
		}
	}
	_, _ = git(ctx, inst.Root, "worktree", "prune")
	path := inst.Path(".strata", "worktrees", "garden-"+next.Run+"-a"+strconv.Itoa(next.Attempt))
	if _, err := git(ctx, inst.Root, "worktree", "add", "--quiet", path, next.Branch); err != nil {
		return "", err
	}
	err := updateLease(inst.Root, next.Run, next.Fence, func(s *Session) { s.Worktree = path })
	if err != nil {
		return "", err
	}
	next.Worktree = path
	return path, writeFence(path, inst.Root, next)
}

// applySalvage replays the patch in the new worktree. Clean under the gates
// → committed; otherwise (or when it does not apply) the patch is kept in
// the run directory and its path returned for the digest.
func applySalvage(ctx context.Context, mainRoot string, wt *config.Instance, s *Session, patch []byte, verify Verify) (string, error) {
	if len(bytes.TrimSpace(patch)) == 0 {
		return "", nil
	}
	keep := filepath.Join(RunDir(mainRoot, s.Run), "salvage-a"+strconv.Itoa(s.Attempt)+".patch")
	if err := os.WriteFile(keep, patch, 0o600); err != nil {
		return "", fmt.Errorf("garden salvage: %w", err)
	}
	if _, err := git(ctx, wt.Root, "apply", "--index", keep); err != nil {
		return keep, nil //nolint:nilerr // an unappliable patch is kept for the human, not fatal
	}
	names, err := git(ctx, wt.Root, "diff", "--cached", "--name-only")
	if err != nil {
		return "", err
	}
	files := strings.Fields(names)
	clean := false
	if verify != nil {
		if clean, err = verify(ctx, wt, files); err != nil {
			clean = false
		}
	}
	if !clean {
		_, _ = git(ctx, wt.Root, "reset", "--hard", "--quiet", "HEAD")
		return keep, nil
	}
	if _, err := git(ctx, wt.Root, "commit", "--quiet", "--no-verify", "-m",
		fmt.Sprintf("garden(resume): salvage %d files from attempt %d", len(files), s.Attempt-1)); err != nil {
		return "", err
	}
	_ = os.Remove(keep)
	return "", nil
}

// resetInterrupted returns in-progress tasks to pending (counting the
// interruption) and escalates tasks interrupted poisonTries times.
func resetInterrupted(ctx context.Context, inst, wt *config.Instance, p *Progress, now func() time.Time) error {
	var poison []string
	for _, id := range p.Order {
		t := p.Tasks[id]
		if t.Status != TaskInProgress {
			continue
		}
		t.Tries++
		t.Status = TaskPending
		if t.Tries >= poisonTries {
			t.Status, t.Note = TaskEscalated, fmt.Sprintf("poison: interrupted %d times; raised as a question", t.Tries)
			poison = append(poison, id)
		}
	}
	if len(poison) > 0 {
		store := knowledge.New(wt, now, nil)
		for _, id := range poison {
			q := fmt.Sprintf("Garden task %s was interrupted %d times in run %s. Should it be done by hand, changed, or dropped?", id, poisonTries, p.Run)
			if _, err := store.Ask(q, nil, "gardener"); err != nil {
				return err //nolint:wrapcheck // knowledge errors are descriptive
			}
		}
		if _, err := git(ctx, wt.Root, "add", "-A"); err != nil {
			return err
		}
		if _, err := git(ctx, wt.Root, "commit", "--quiet", "--no-verify", "-m",
			"garden(questions): escalate "+strconv.Itoa(len(poison))+" interrupted tasks"); err != nil {
			return err
		}
	}
	return writeProgress(inst.Root, p)
}

// gitRaw is git returning untrimmed stdout (patches must keep their bytes).
func gitRaw(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // G204: fixed program, engine-built arguments
	cmd.Dir = dir
	cmd.Env = cleanGitEnv(os.Environ())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
