package garden

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
)

// The run lease (docs/spec/07 §9.2) lives in RunMarker. Every change to it
// happens under leaseMutex, a portable exclusive-create mutex file (flock is
// a no-op on some platforms).
const (
	leaseVersion    = 2
	leaseMutexFile  = ".strata/garden.lock.mu"
	fenceFileName   = "strata-garden.json" // in the worktree's git directory
	defaultLeaseTTL = 30 * time.Minute
	mutexWait       = 2 * time.Second
	mutexStale      = 10 * time.Second
	mutexPoll       = 20 * time.Millisecond
)

// Owner identifies the holder of the lease. PID is set only for long-lived
// holders (the headless supervisor); a manual /garden run has none.
type Owner struct {
	Host     string `json:"host,omitempty"`
	Launcher string `json:"launcher,omitempty"` // manual | headless
	Model    string `json:"model,omitempty"`
	Session  string `json:"session,omitempty"`
	PID      int    `json:"pid,omitempty"`
}

// fenceFile is what a garden worktree knows about the run it belongs to.
type fenceFile struct {
	Run   string `json:"run"`
	Main  string `json:"main"` // the main instance root
	Fence int    `json:"fence"`
}

// leaseTTL is the configured ttl, or the default.
func leaseTTL(inst *config.Instance) time.Duration {
	if d := inst.Config.Garden.LeaseTTL.D(); d > 0 {
		return d
	}
	return defaultLeaseTTL
}

// Live reports whether the lease is held: its heartbeat (or start) is
// younger than ttl and, on this host, its owner process (if any) is alive.
func (s *Session) Live(now time.Time, ttl time.Duration) bool {
	if s.Owner.PID > 0 && s.Owner.Host == hostname() && !pidAlive(s.Owner.PID) {
		return false
	}
	return now.Sub(s.lastBeat()) < ttl
}

// HeartbeatAge is the time since the last heartbeat (or start).
func (s *Session) HeartbeatAge(now time.Time) time.Duration { return now.Sub(s.lastBeat()) }

func (s *Session) lastBeat() time.Time {
	for _, v := range []string{s.HeartbeatAt, s.Started} {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	}
	return time.Time{}
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// withLeaseMutex runs fn while holding the lease mutex of the main instance.
func withLeaseMutex(mainRoot string, fn func() error) error {
	p := filepath.Join(mainRoot, filepath.FromSlash(leaseMutexFile))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return fmt.Errorf("garden lease: %w", err)
	}
	deadline := time.Now().Add(mutexWait)
	for {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // G304: engine-owned mutex
		if err == nil {
			_ = f.Close()
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("garden lease: %w", err)
		}
		if fi, serr := os.Stat(p); serr == nil && time.Since(fi.ModTime()) > mutexStale {
			_ = os.Remove(p) // debris from a crashed holder
			continue
		}
		if time.Now().After(deadline) {
			return fail.New(fail.Locked, "retry in a moment", "the garden lease is busy (%s)", leaseMutexFile)
		}
		time.Sleep(mutexPoll)
	}
	defer func() { _ = os.Remove(p) }()
	return fn()
}

// readLease loads the lease file; ok is false when there is none.
func readLease(mainRoot string) (*Session, bool, error) {
	raw, err := os.ReadFile(filepath.Join(mainRoot, filepath.FromSlash(RunMarker))) //nolint:gosec // G304: engine-owned lease
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("garden lease: %w", err)
	}
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, true, fail.New(fail.Locked, "`strata garden abort` clears it", "garden lease %s is unreadable", RunMarker)
	}
	return &s, true, nil
}

func writeLease(mainRoot string, s *Session) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("garden lease: %w", err)
	}
	return fsutil.WriteAtomic(filepath.Join(mainRoot, filepath.FromSlash(RunMarker)), append(b, '\n'), 0o600)
}

// acquireLease creates a new lease for run, or explains why it cannot.
func acquireLease(inst *config.Instance, now time.Time, run string, owner Owner) (*Session, error) {
	ttl := leaseTTL(inst)
	var out *Session
	err := withLeaseMutex(inst.Root, func() error {
		cur, exists, err := readLease(inst.Root)
		if err != nil {
			return err
		}
		if exists {
			if cur.Live(now, ttl) {
				return fail.New(fail.Locked, "finish or abort it: strata garden finish | strata garden abort",
					"a garden run is active (%s, heartbeat %s ago)", cur.Run, cur.HeartbeatAge(now).Round(time.Second))
			}
			return fail.New(fail.Locked, "resume it with `strata garden resume`, or `strata garden abort` to discard it",
				"garden run %s lost its lease (no heartbeat for %s)", cur.Run, cur.HeartbeatAge(now).Round(time.Minute))
		}
		if owner.Host == "" {
			owner.Host = hostname()
		}
		stamp := now.UTC().Format(time.RFC3339)
		out = &Session{V: leaseVersion, Run: run, Fence: 1, Attempt: 1, Owner: owner, Started: stamp, HeartbeatAt: stamp, TTL: ttl.String()}
		return writeLease(inst.Root, out)
	})
	return out, err
}

// updateLease rewrites the lease under the mutex when its run and fence
// still match; otherwise the caller has been fenced out.
func updateLease(mainRoot, run string, fence int, mutate func(*Session)) error {
	return withLeaseMutex(mainRoot, func() error {
		cur, exists, err := readLease(mainRoot)
		if err != nil {
			return err
		}
		if !exists || cur.Run != run || cur.Fence != fence {
			return fenced(run, fence, cur)
		}
		mutate(cur)
		return writeLease(mainRoot, cur)
	})
}

func fenced(run string, fence int, cur *Session) error {
	held := "no run holds the lease"
	if cur != nil {
		held = fmt.Sprintf("the lease is %s fence %d", cur.Run, cur.Fence)
	}
	return fail.New(fail.Fenced, "this gardener lost its lease; stop writing (a newer attempt owns the run)",
		"fenced: %s fence %d is stale (%s)", run, fence, held)
}

// worktreeFence reads the fence file of the garden worktree rooted at root;
// ok is false when root is not a garden worktree.
func worktreeFence(root string) (*fenceFile, bool) {
	gitdir, ok := worktreeGitDir(root)
	if !ok {
		return nil, false
	}
	raw, err := os.ReadFile(filepath.Join(gitdir, fenceFileName)) //nolint:gosec // G304: engine-owned file in git metadata
	if err != nil {
		return nil, false
	}
	var f fenceFile
	if json.Unmarshal(raw, &f) != nil || f.Run == "" || f.Main == "" {
		return nil, false
	}
	return &f, true
}

// worktreeGitDir resolves a linked worktree's git directory from its `.git`
// file ("gitdir: <path>"); ok is false for a main checkout or a non-repo.
func worktreeGitDir(root string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(root, ".git")) //nolint:gosec // G304: the repository's own .git file
	if err != nil {
		return "", false // missing, or a directory (main checkout)
	}
	dir, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:")
	if !ok {
		return "", false
	}
	dir = strings.TrimSpace(dir)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	return dir, true
}

// writeFence records the run and fence in the worktree's git directory.
func writeFence(worktree, mainRoot string, s *Session) error {
	gitdir, ok := worktreeGitDir(worktree)
	if !ok {
		return fmt.Errorf("garden fence: %s is not a linked worktree", worktree)
	}
	b, err := json.Marshal(fenceFile{Run: s.Run, Main: mainRoot, Fence: s.Fence})
	if err != nil {
		return fmt.Errorf("garden fence: %w", err)
	}
	return fsutil.WriteAtomic(filepath.Join(gitdir, fenceFileName), append(b, '\n'), 0o600)
}

// Heartbeat records progress for the garden run that owns the worktree at
// inst.Root. It is a no-op outside garden worktrees. A stale fence returns a
// fail.Fenced error; read-only callers may ignore it, writers must stop.
func Heartbeat(inst *config.Instance, now time.Time) error {
	f, ok := worktreeFence(inst.Root)
	if !ok {
		return nil
	}
	return updateLease(f.Main, f.Run, f.Fence, func(s *Session) {
		s.HeartbeatAt = now.UTC().Format(time.RFC3339)
	})
}

// CheckFence is Heartbeat for writers: it fails with fail.Fenced when the
// caller's worktree no longer owns the run.
func CheckFence(inst *config.Instance, now time.Time) error { return Heartbeat(inst, now) }

// Resumable reports whether a lost run has work worth finishing: commits on
// its branch beyond the base, or uncommitted changes in its worktree.
func Resumable(ctx context.Context, inst *config.Instance, s *Session) bool {
	if s.Branch == "" {
		return false
	}
	if n, err := git(ctx, inst.Root, "rev-list", "--count", s.BaseRef+".."+s.Branch); err == nil && n != "0" {
		return true
	}
	if _, err := os.Stat(s.Worktree); err != nil {
		return false
	}
	out, err := git(ctx, s.Worktree, "status", "--porcelain")
	return err == nil && out != ""
}
