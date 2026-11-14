package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/garden"
	"github.com/zbmowrey/strata/engine/internal/perms"
)

// Headless garden runs (docs/spec/07 §9.4).
const (
	envGarden       = "STRATA_GARDEN"
	envGardenRun    = "STRATA_GARDEN_RUN"
	envNoAutolaunch = "STRATA_NO_AUTOLAUNCH"
	superviseLog    = ".strata/garden/supervise.log"
	watchEvery      = 30 * time.Second
)

// gardenerTools are what a headless gardener may run without a prompt, on
// top of perms.Allowlist. Edits need no rule: acceptEdits approves them
// inside the working directory (the worktree) only. Nothing here pushes,
// opens a PR, merges or aborts: that is the supervisor's job.
var gardenerTools = []string{
	"Read", "Glob", "Grep",
	"Bash(strata new:*)", "Bash(strata questions ask:*)", "Bash(strata questions show:*)",
	"Bash(strata garden task:*)", "Bash(strata scripts list:*)", "Bash(strata gates explain:*)",
	"Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)",
	"Bash(git add:*)", "Bash(git commit:*)", "Bash(git mv:*)", "Bash(git rm:*)",
}

// launcher is the process plumbing, swappable in tests.
type launcher struct {
	self   func() (string, error) // the strata binary
	claude string                 // the Claude Code CLI
	exec   func(ctx context.Context, dir string, env []string, out io.Writer, name string, args ...string) (*exec.Cmd, error)
	verify func(ctx context.Context, self, worktree string) (map[string]string, bool)
}

var defaultLauncher = launcher{self: os.Executable, claude: "claude", exec: startProcess, verify: verifyRun}

func startProcess(ctx context.Context, dir string, env []string, out io.Writer, name string, args ...string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: engine-built argv for strata/claude
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, env, out, out
	cmd.SysProcAttr = groupAttr()
	cmd.Cancel = func() error { killGroup(cmd); return nil }
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	return cmd, nil
}

// verifyRun is §3 step 6 through the CLI itself: every blocking gate and the
// retrieval bench, in the worktree. It returns output tails per check.
func verifyRun(ctx context.Context, self, worktree string) (map[string]string, bool) {
	out, ok := map[string]string{}, true
	for name, args := range map[string][]string{
		"check": {"--root", worktree, "check", "--all"},
		"bench": {"--root", worktree, "bench", "retrieval"},
	} {
		cmd := exec.CommandContext(ctx, self, args...) //nolint:gosec // G204: the strata binary itself
		cmd.Dir = worktree
		b, err := cmd.CombinedOutput()
		if err != nil {
			ok = false
			out[name] = tail(string(b), 40)
		}
	}
	return out, ok
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

func newGardenLaunchCmd(a *app) *cobra.Command {
	var resume, dryRun bool
	cmd := &cobra.Command{
		Use:   "launch",
		Short: "Start a detached headless garden run (a supervisor running claude -p) and return",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			inst, err := a.instance()
			if err != nil {
				return err
			}
			pid, argv, err := defaultLauncher.launch(cmd.Context(), inst, resume, dryRun)
			if err != nil {
				return err
			}
			if dryRun {
				return a.printf("%s\n", strings.Join(argv, " "))
			}
			return a.printf("garden ▸ supervisor started (pid %d) — strata garden status\n", pid)
		},
	}
	cmd.Flags().BoolVar(&resume, "resume", false, "resume the lost run instead of starting one")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the supervisor command without starting it")
	return cmd
}

// launch forks `strata garden supervise` into its own session, detached
// from the caller, and returns its pid.
func (l launcher) launch(ctx context.Context, inst *config.Instance, resume, dryRun bool) (int, []string, error) {
	self, err := l.self()
	if err != nil {
		return 0, nil, fmt.Errorf("locate strata: %w", err)
	}
	argv := []string{self, "--root", inst.Root, "garden", "supervise"}
	if resume {
		argv = append(argv, "--resume")
	}
	if dryRun {
		return 0, argv, nil
	}
	logPath := inst.Path(filepath.FromSlash(superviseLog))
	if err := os.MkdirAll(filepath.Dir(logPath), 0o750); err != nil {
		return 0, nil, fmt.Errorf("garden launch: %w", err)
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // G304: engine-owned log
	if err != nil {
		return 0, nil, fmt.Errorf("garden launch: %w", err)
	}
	defer func() { _ = logf.Close() }()
	env := append(os.Environ(), envNoAutolaunch+"=1")
	// Detached: not tied to the caller's context, which ends with the hook.
	cmd, err := l.exec(context.WithoutCancel(ctx), inst.Root, env, logf, argv[0], argv[1:]...)
	if err != nil {
		return 0, nil, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, argv, nil
}

func newGardenSuperviseCmd(a *app) *cobra.Command {
	var resume bool
	cmd := &cobra.Command{
		Use:    "supervise",
		Short:  "Run one headless garden attempt to completion (started by `garden launch`)",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			inst, err := a.instance()
			if err != nil {
				return err
			}
			msg, err := defaultLauncher.supervise(cmd.Context(), inst, resume, a.now)
			if msg != "" {
				_ = a.printf("%s\n", msg)
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&resume, "resume", false, "resume the lost run")
	return cmd
}

// supervise runs one attempt: start or resume, the agent under a watchdog,
// then verification and the PR (docs/spec/07 §9.4).
func (l launcher) supervise(ctx context.Context, inst *config.Instance, resume bool, now func() time.Time) (string, error) {
	cfg := inst.Config.Garden
	if !resume {
		if prs := garden.OpenGardenPRs(ctx, inst); len(prs) >= max(1, cfg.MaxOpenPRs) {
			garden.RecordDeferred(inst, now(), fmt.Sprintf("%d garden PRs open", len(prs)))
			return fmt.Sprintf("garden ▸ deferred: %d garden PRs await review", len(prs)), nil
		}
	}
	owner := garden.Owner{Launcher: "headless", Model: cfg.Model, PID: os.Getpid()}
	var res *garden.StartResult
	var err error
	if resume {
		res, err = garden.Resume(ctx, inst, garden.ResumeOptions{Now: now, Owner: owner, Verify: verifySalvage})
	} else {
		res, err = garden.Start(ctx, inst, now, owner)
	}
	if err != nil {
		return "", err //nolint:wrapcheck // garden errors are descriptive
	}
	if res.Tidy {
		return "garden ▸ tidy", nil
	}
	runDir := garden.RunDir(inst.Root, res.Run)
	logf, err := os.OpenFile(filepath.Join(runDir, "run.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // G304: engine-owned log
	if err != nil {
		return "", fmt.Errorf("garden run log: %w", err)
	}
	defer func() { _ = logf.Close() }()
	wt, err := config.Open(res.Worktree, "")
	if err != nil {
		return "", err //nolint:wrapcheck // config errors are descriptive
	}
	args, err := claudeArgs(wt, cfg, gardenerTask(res, runDir))
	if err != nil {
		return "", err
	}
	env := append(os.Environ(), envGarden+"=1", envGardenRun+"="+res.Run, envNoAutolaunch+"=1")
	outcome := l.runAgent(ctx, inst, res, env, logf, args, now)
	if outcome != "" {
		garden.RecordOutcome(inst, outcome, now(), 0)
		return "garden ▸ run " + res.Run + " " + outcome + " — the next session resumes it", nil
	}
	return l.publish(ctx, inst, res, runDir, now)
}

// runAgent runs claude in the worktree and kills it when the lease stops
// getting heartbeats (hung) or the run exceeds max_run. It returns "" on a
// clean exit with every task settled, else the failure outcome.
func (l launcher) runAgent(ctx context.Context, inst *config.Instance, res *garden.StartResult, env []string, out io.Writer, args []string, now func() time.Time) string {
	maxRun := inst.Config.Garden.MaxRun.D()
	runCtx, cancel := context.WithTimeout(ctx, maxRun)
	defer cancel()
	cmd, err := l.exec(runCtx, res.Worktree, env, out, l.claude, args...)
	if err != nil {
		_, _ = fmt.Fprintf(out, "garden: %v\n", err)
		return garden.OutcomeFailed
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(watchEvery)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			switch {
			case runCtx.Err() != nil:
				return garden.OutcomeTimeout
			case err != nil:
				return garden.OutcomeFailed
			}
			if p, perr := garden.LoadProgress(inst.Root, res.Run); perr != nil || len(p.Remaining()) > 0 {
				return garden.OutcomeFailed // stopped with work left
			}
			return ""
		case <-ticker.C:
			if s, err := garden.LoadSession(inst); err == nil && !garden.LeaseLive(inst, s, now()) {
				killGroup(cmd) // hung: no strata activity within lease_ttl
				if err := <-done; err != nil {
					_, _ = fmt.Fprintf(out, "garden: agent stopped (no heartbeat within lease_ttl): %v\n", err)
				}
				return garden.OutcomeTimeout
			}
		}
	}
}

// publish verifies the branch and opens the PR, or records why not.
func (l launcher) publish(ctx context.Context, inst *config.Instance, res *garden.StartResult, runDir string, now func() time.Time) (string, error) {
	self, err := l.self()
	if err != nil {
		return "", fmt.Errorf("locate strata: %w", err)
	}
	failures, ok := l.verify(ctx, self, res.Worktree)
	if !ok {
		b, _ := json.MarshalIndent(failures, "", "  ")
		_ = fsutil.WriteAtomic(filepath.Join(runDir, "verify.json"), append(b, '\n'), 0o600)
		garden.RecordOutcome(inst, garden.OutcomeFailedVerify, now(), 0)
		return "garden ▸ run " + res.Run + " failed verification — the next session resumes it with the failures", nil
	}
	_ = os.Remove(filepath.Join(runDir, "verify.json"))
	fr, err := garden.Finish(ctx, inst, garden.FinishOptions{Fence: res.Fence})
	if err != nil {
		return "", err //nolint:wrapcheck // garden errors are descriptive
	}
	switch {
	case fr.PR != nil:
		return "garden ▸ PR " + fr.PR.URL, nil
	case fr.CompareURL != "":
		return "garden ▸ pushed; open the PR: " + fr.CompareURL, nil
	}
	return "garden ▸ " + fr.Message, nil
}

// claudeArgs builds the headless invocation (flags verified against Claude
// Code 2.1.325: -p, --model, --effort, --permission-mode, --allowedTools,
// --agent).
func claudeArgs(wt *config.Instance, cfg config.Garden, prompt string) ([]string, error) {
	allow, err := perms.Allowlist(wt)
	if err != nil {
		return nil, err //nolint:wrapcheck // perms errors are descriptive
	}
	for _, t := range gardenerTools {
		if !slices.Contains(allow, t) {
			allow = append(allow, t)
		}
	}
	args := []string{"-p", prompt, "--model", cfg.Model, "--effort", cfg.Effort, "--permission-mode", "acceptEdits"}
	if cfg.Agent != "" {
		args = append(args, "--agent", cfg.Agent)
	}
	return append(args, "--allowedTools", strings.Join(allow, ",")), nil
}

// gardenerTask is the headless prompt: the run, its plan and the rules
// that differ from an interactive /garden.
func gardenerTask(res *garden.StartResult, runDir string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are the Strata gardener, running headless (attempt %d of run %s). ", max(1, res.Attempt), res.Run)
	fmt.Fprintf(&b, "Your working directory is the garden worktree %s on branch %s: run strata and git commands plainly there (no --root, no -C, no cd).\n\n", res.Worktree, res.Branch)
	fmt.Fprintf(&b, "Work the plan in %s under the autonomy matrix (the strata-garden skill is your manual). ", res.PlanFile)
	b.WriteString("For every task: `strata garden task start <id>` before you begin, then `strata garden task done|skip|escalate <id> --note \"<one line for the PR>\"`. ")
	b.WriteString("Commit per task category (garden(knowledge): …, garden(scripts): …). Write a short summary for the PR to .strata/garden-summary.md.\n\n")
	if len(res.Remaining) > 0 {
		fmt.Fprintf(&b, "This run was resumed: work only these remaining tasks: %s. Check each target's current state first; earlier attempts may have done part of it.\n\n", strings.Join(res.Remaining, ", "))
	}
	if raw, err := os.ReadFile(filepath.Join(runDir, "verify.json")); err == nil { //nolint:gosec // G304: engine-owned run state
		fmt.Fprintf(&b, "The previous attempt failed verification. Fix these first, or revert the offending commit and say so in the summary:\n%s\n\n", raw)
	}
	b.WriteString("Do not push, open or merge PRs, run `strata garden finish|abort|resume`, or touch files outside the worktree: the supervisor verifies and publishes when you exit. Exit when every task is settled.")
	return b.String()
}
