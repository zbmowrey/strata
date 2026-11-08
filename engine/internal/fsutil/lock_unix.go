//go:build unix

package fsutil

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func flock(f *os.File, wait bool) error {
	how := syscall.LOCK_EX
	if !wait {
		how |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil { //nolint:gosec // G115: fds fit in int
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return ErrLocked
		}
		return fmt.Errorf("flock %s: %w", f.Name(), err)
	}
	return nil
}

func funlock(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil { //nolint:gosec // G115: fds fit in int
		return fmt.Errorf("unlock %s: %w", f.Name(), err)
	}
	return nil
}
