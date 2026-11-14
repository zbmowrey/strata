package garden

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func leaseInstance(t *testing.T) *config.Instance {
	t.Helper()
	root := t.TempDir()
	testutil.WriteFile(t, root, ".strata/config.toml", "")
	inst, err := config.Open(root, "")
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

func TestLeaseLiveness(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	beat := func(ago time.Duration) string { return at.Add(-ago).Format(time.RFC3339) }
	for _, tc := range []struct {
		name string
		s    Session
		live bool
	}{
		{"fresh heartbeat", Session{HeartbeatAt: beat(5 * time.Minute)}, true},
		{"heartbeat past ttl", Session{HeartbeatAt: beat(31 * time.Minute)}, false},
		{"v1 marker falls back to start", Session{Started: beat(10 * time.Minute)}, true},
		{"nothing to go on", Session{}, false},
		{"owner process gone on this host", Session{HeartbeatAt: beat(time.Minute), Owner: Owner{Host: hostname(), PID: 1 << 30}}, false},
		{"owner on another host", Session{HeartbeatAt: beat(time.Minute), Owner: Owner{Host: "elsewhere.invalid", PID: 1 << 30}}, true},
		{"owner process alive", Session{HeartbeatAt: beat(time.Minute), Owner: Owner{Host: hostname(), PID: os.Getpid()}}, true},
	} {
		if got := tc.s.Live(at, 30*time.Minute); got != tc.live {
			t.Errorf("%s: live = %v, want %v", tc.name, got, tc.live)
		}
	}
}

func TestAcquireLease(t *testing.T) {
	t.Parallel()
	inst := leaseInstance(t)
	at := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	s, err := acquireLease(inst, at, "r1", Owner{Launcher: "headless", Model: "opus"})
	if err != nil {
		t.Fatal(err)
	}
	if s.V != 2 || s.Fence != 1 || s.Attempt != 1 || s.Owner.Host == "" || s.TTL != "30m0s" {
		t.Fatalf("lease: %+v", s)
	}
	_, err = acquireLease(inst, at.Add(10*time.Minute), "r2", Owner{})
	if fail.CodeOf(err) != fail.Locked || !strings.Contains(err.Error(), "is active") {
		t.Fatalf("live lease: %v", err)
	}
	_, err = acquireLease(inst, at.Add(45*time.Minute), "r2", Owner{})
	if fail.CodeOf(err) != fail.Locked || !strings.Contains(fail.HintOf(err), "garden resume") {
		t.Fatalf("expired lease: %v (hint %q)", err, fail.HintOf(err))
	}
}

func TestLeaseMutex(t *testing.T) {
	t.Parallel()
	inst := leaseInstance(t)
	mu := testutil.WriteFile(t, inst.Root, leaseMutexFile, "")
	// A held mutex makes callers wait and then give up.
	start := time.Now()
	if err := withLeaseMutex(inst.Root, func() error { return nil }); fail.CodeOf(err) != fail.Locked {
		t.Fatalf("held mutex: %v", err)
	}
	if time.Since(start) < mutexWait {
		t.Fatal("gave up before the wait elapsed")
	}
	// Debris from a crashed holder is cleared.
	old := time.Now().Add(-2 * mutexStale)
	if err := os.Chtimes(mu, old, old); err != nil {
		t.Fatal(err)
	}
	ran := false
	if err := withLeaseMutex(inst.Root, func() error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("stale mutex: ran=%v err=%v", ran, err)
	}
	if _, err := os.Stat(mu); !os.IsNotExist(err) {
		t.Fatal("mutex left behind")
	}
}

func TestHeartbeatAndFence(t *testing.T) { //nolint:paralleltest // sets PATH and git identity
	inst, _ := gardenRepo(t)
	ctx := context.Background()
	res, err := Start(ctx, inst, now, Owner{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Run == "" || res.Fence != 1 {
		t.Fatalf("start: %+v", res)
	}
	wt, err := config.Open(res.Worktree, "")
	if err != nil {
		t.Fatal(err)
	}
	// Outside a garden worktree, heartbeats are a no-op.
	if err := Heartbeat(inst, today); err != nil {
		t.Fatal(err)
	}
	later := today.Add(20 * time.Minute)
	if err := Heartbeat(wt, later); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSession(inst)
	if err != nil || s.HeartbeatAt != later.UTC().Format(time.RFC3339) {
		t.Fatalf("heartbeat not recorded: %+v %v", s, err)
	}
	// A takeover bumps the fence: the old worktree is fenced out.
	s.Fence++
	b, _ := json.Marshal(s)
	testutil.WriteFile(t, inst.Root, RunMarker, string(b))
	if err := CheckFence(wt, later); fail.CodeOf(err) != fail.Fenced {
		t.Fatalf("stale fence: %v", err)
	}
	if _, err := Finish(ctx, inst, FinishOptions{Fence: 1}); fail.CodeOf(err) != fail.Fenced {
		t.Fatalf("finish with a stale fence: %v", err)
	}
	// The fence lives in git metadata, never in the worktree's files.
	if out := sh(t, res.Worktree, "git", "status", "--porcelain", "--ignored"); strings.Contains(out, fenceFileName) {
		t.Fatalf("fence file visible to git: %s", out)
	}
	if err := Abort(ctx, inst); err != nil {
		t.Fatal(err)
	}
}

func TestResumable(t *testing.T) { //nolint:paralleltest // sets PATH and git identity
	inst, _ := gardenRepo(t)
	ctx := context.Background()
	res, err := Start(ctx, inst, now, Owner{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := LoadSession(inst)
	if err != nil {
		t.Fatal(err)
	}
	// Start commits the mechanical fixes, so the branch is ahead of base.
	if res.Fixes > 0 != Resumable(ctx, inst, s) {
		t.Fatalf("resumable after start: fixes=%d", res.Fixes)
	}
	if Resumable(ctx, inst, &Session{}) {
		t.Fatal("a lease without a branch is not resumable")
	}
	gone := *s
	gone.BaseRef, gone.Worktree = gone.Branch, filepath.Join(t.TempDir(), "missing")
	if Resumable(ctx, inst, &gone) {
		t.Fatal("no commits and no worktree: not resumable")
	}
	dirty := *s
	dirty.BaseRef = dirty.Branch
	testutil.WriteFile(t, s.Worktree, "knowledge/_inbox/20261107-130000-new.md", "x")
	if !Resumable(ctx, inst, &dirty) {
		t.Fatal("uncommitted worktree changes are resumable")
	}
	if err := Abort(ctx, inst); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeGitDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, ok := worktreeGitDir(dir); ok {
		t.Fatal("no .git: not a worktree")
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, ok := worktreeGitDir(dir); ok {
		t.Fatal(".git directory: a main checkout")
	}
	other := t.TempDir()
	testutil.WriteFile(t, other, ".git", "gitdir: ../meta/worktrees/x\n")
	if got, ok := worktreeGitDir(other); !ok || got != filepath.Join(other, "..", "meta", "worktrees", "x") {
		t.Fatalf("relative gitdir: %q %v", got, ok)
	}
	testutil.WriteFile(t, other, ".git", "not a gitdir line")
	if _, ok := worktreeGitDir(other); ok {
		t.Fatal("malformed .git file")
	}
}

func TestAcquireLeaseRace(t *testing.T) {
	t.Parallel()
	inst := leaseInstance(t)
	at := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	const racers = 8
	errs := make(chan error, racers)
	for i := range racers {
		go func() {
			_, err := acquireLease(inst, at, "r"+string(rune('a'+i)), Owner{})
			errs <- err
		}()
	}
	won := 0
	for range racers {
		switch err := <-errs; {
		case err == nil:
			won++
		case fail.CodeOf(err) != fail.Locked:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("%d racers acquired the lease, want exactly 1", won)
	}
}
