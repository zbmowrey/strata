package cli

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/garden"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

var launchNow = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func shell(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

// gitInstance is the fixture instance in a git repo with a bare origin and
// a fake gh whose `pr list` prints prList.
func gitInstance(t *testing.T, prList string) *config.Instance {
	t.Helper()
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com"} {
		t.Setenv(k, v)
	}
	bin := t.TempDir()
	fake := "#!/bin/sh\ncase \"$1 $2\" in\n\"pr create\") echo https://github.com/o/r/pull/7 ;;\n\"pr list\") echo '" + prList + "' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fake), 0o755); err != nil { //nolint:gosec // G306: test fake must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	bare := t.TempDir()
	shell(t, bare, "git", "init", "--quiet", "--bare", "-b", "main")
	root := testutil.CopyFixture(t, "instance-basic")
	cfg, _ := os.ReadFile(filepath.Join(root, ".strata", "config.toml"))
	testutil.WriteFile(t, root, ".strata/config.toml", string(cfg)+"\n[garden]\nforge = \"github\"\n")
	testutil.WriteFile(t, root, ".gitignore", "/.strata/index.db*\n/.strata/*.lock\n/.strata/garden/\n/.strata/worktrees/\n/.strata/telemetry/\n/.strata/session/\n/.strata/garden.lock*\n")
	shell(t, root, "git", "init", "--quiet", "-b", "main")
	shell(t, root, "git", "add", "-A")
	shell(t, root, "git", "commit", "--quiet", "-m", "init")
	shell(t, root, "git", "remote", "add", "origin", bare)
	shell(t, root, "git", "push", "--quiet", "-u", "origin", "main")
	shell(t, root, "git", "remote", "set-head", "origin", "main")
	inst, err := config.Open(root, "")
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

// fakeLauncher runs `program` instead of claude after calling agent, and
// stubs verification.
func fakeLauncher(program string, agent func(worktree string), verifyOK bool) launcher {
	return launcher{
		self:   func() (string, error) { return "/bin/strata", nil },
		claude: program,
		exec: func(ctx context.Context, dir string, env []string, out io.Writer, name string, _ ...string) (*exec.Cmd, error) {
			if agent != nil {
				agent(dir)
			}
			return startProcess(ctx, dir, env, out, name)
		},
		verify: func(context.Context, string, string) (map[string]string, bool) {
			return map[string]string{"check": "1 blocking finding"}, verifyOK
		},
	}
}

// settleAll marks every task of the worktree's run done, as an agent would.
func settleAll(t *testing.T) func(string) {
	t.Helper()
	return func(worktree string) {
		wt, err := config.Open(worktree, "")
		if err != nil {
			t.Error(err)
			return
		}
		s, err := garden.LoadSession(mustMain(t, worktree))
		if err != nil {
			t.Error(err)
			return
		}
		p, _ := garden.LoadProgress(mustMain(t, worktree).Root, s.Run)
		for _, id := range p.Order {
			if _, err := garden.UpdateTask(wt, id, "done", "ok", launchNow); err != nil {
				t.Error(err)
			}
		}
		testutil.WriteFile(t, worktree, "knowledge/_inbox/20261107-130000-x.md", "---\ntype: fact\ncaptured: 2026-11-07T13:00:00Z\n---\nx\n")
		shell(t, worktree, "git", "add", "-A")
		shell(t, worktree, "git", "commit", "--quiet", "--no-verify", "-m", "garden(knowledge): x")
	}
}

func mustMain(t *testing.T, worktree string) *config.Instance {
	t.Helper()
	inst, err := config.Open(filepath.Dir(filepath.Dir(filepath.Dir(worktree))), "")
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

func lastOutcome(t *testing.T, inst *config.Instance) garden.HistoryEntry {
	t.Helper()
	h, err := garden.ReadHistory(inst)
	if err != nil || len(h) == 0 {
		t.Fatalf("history: %v", err)
	}
	return h[len(h)-1]
}

func TestSuperviseOpensPR(t *testing.T) { //nolint:paralleltest // sets PATH and git identity
	inst := gitInstance(t, "[]")
	msg, err := fakeLauncher("true", settleAll(t), true).supervise(context.Background(), inst, false, func() time.Time { return launchNow })
	if err != nil || !strings.Contains(msg, "pull/7") {
		t.Fatalf("supervise: %q %v", msg, err)
	}
	if e := lastOutcome(t, inst); e.Outcome != garden.OutcomePR || e.PR != 7 || e.Launcher != "headless" || e.Model != "opus" {
		t.Fatalf("history: %+v", e)
	}
	if _, err := garden.LoadSession(inst); err == nil {
		t.Fatal("lease kept after the PR")
	}
}

func TestSuperviseFailures(t *testing.T) { //nolint:paralleltest // sets PATH and git identity
	inst := gitInstance(t, "[]")
	clock := func() time.Time { return launchNow }
	// The agent crashes: the run is left for the next session to resume.
	msg, err := fakeLauncher("false", nil, true).supervise(context.Background(), inst, false, clock)
	if err != nil || !strings.Contains(msg, "failed") || lastOutcome(t, inst).Outcome != garden.OutcomeFailed {
		t.Fatalf("crash: %q %v", msg, err)
	}
	s, err := garden.LoadSession(inst)
	if err != nil {
		t.Fatal("lease dropped after a crash")
	}
	// Resumed, settled, but verification fails: nothing is published.
	msg, err = fakeLauncher("true", settleAll(t), false).supervise(context.Background(), inst, true, func() time.Time { return launchNow.Add(time.Hour) })
	if err != nil || !strings.Contains(msg, "failed verification") || lastOutcome(t, inst).Outcome != garden.OutcomeFailedVerify {
		t.Fatalf("verify: %q %v", msg, err)
	}
	b, err := os.ReadFile(filepath.Join(garden.RunDir(inst.Root, s.Run), "verify.json"))
	if err != nil || !strings.Contains(string(b), "blocking finding") {
		t.Fatalf("verify.json: %s %v", b, err)
	}
	// The next prompt carries the failures.
	res := &garden.StartResult{Run: s.Run, Attempt: 3, Worktree: "/w", Branch: "garden/x", PlanFile: "/p.json", Remaining: []string{"a"}}
	prompt := gardenerTask(res, garden.RunDir(inst.Root, s.Run))
	for _, want := range []string{"failed verification", "blocking finding", "remaining tasks: a", "attempt 3", "/p.json", "strata garden task start"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestSuperviseDefersWhilePRsAwaitReview(t *testing.T) { //nolint:paralleltest // sets PATH and git identity
	inst := gitInstance(t, `[{"number":3,"url":"u","headRefName":"garden/old","files":[]}]`)
	msg, err := fakeLauncher("true", nil, true).supervise(context.Background(), inst, false, func() time.Time { return launchNow })
	if err != nil || !strings.Contains(msg, "deferred") || !strings.HasPrefix(lastOutcome(t, inst).Outcome, garden.OutcomeDeferred) {
		t.Fatalf("defer: %q %v", msg, err)
	}
}

func TestClaudeArgs(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	args, err := claudeArgs(inst, inst.Config.Garden, "do it")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"-p do it", "--model opus", "--effort medium", "--permission-mode acceptEdits", "--agent strata:gardener", "Bash(strata garden task:*)", "Bash(git commit:*)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %s", want, joined)
		}
	}
	allow := strings.Split(args[len(args)-1], ",")
	for _, never := range []string{"Edit", "Write", "Bash(git push:*)", "Bash(strata garden:*)"} {
		if slices.Contains(allow, never) {
			t.Errorf("allowlist must not contain %q", never)
		}
	}
	cfg := inst.Config.Garden
	cfg.Agent = ""
	if args, _ = claudeArgs(inst, cfg, "x"); slices.Contains(args, "--agent") {
		t.Error("--agent passed with no agent configured")
	}
}

func TestLaunchDryRun(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	out, _, code := run(t, "--root", dir, "garden", "launch", "--resume", "--dry-run")
	if code != 0 || !strings.Contains(out, "garden supervise --resume") || !strings.Contains(out, "--root "+dir) {
		t.Fatalf("dry run: %d %q", code, out)
	}
}
