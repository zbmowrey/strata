package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrLocked is returned by TryLock when another process holds the lock.
var ErrLocked = errors.New("lock held by another process")

// Lock is an advisory exclusive file lock.
type Lock struct {
	f *os.File
}

// TryLock acquires an exclusive lock on path without waiting.
func TryLock(path string) (*Lock, error) {
	f, err := openLockFile(path)
	if err != nil {
		return nil, err
	}
	if err := flock(f, false); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}

// LockWait acquires the lock, polling until timeout elapses.
func LockWait(path string, timeout time.Duration) (*Lock, error) {
	deadline := time.Now().Add(timeout)
	for {
		l, err := TryLock(path)
		if !errors.Is(err, ErrLocked) || time.Now().After(deadline) {
			return l, err
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Unlock releases the lock. It is safe to call on a nil Lock.
func (l *Lock) Unlock() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := funlock(l.f)
	cerr := l.f.Close()
	l.f = nil
	return errors.Join(err, cerr)
}

func openLockFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: lock paths are engine-chosen under .strata/
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	return f, nil
}
