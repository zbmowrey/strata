//go:build unix

package garden

import (
	"errors"
	"syscall"
)

// pidAlive reports whether a process with pid exists (signal 0 probes it).
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// terminate asks pid to exit (SIGTERM to the process group, then the pid).
func terminate(pid int) {
	if syscall.Kill(-pid, syscall.SIGTERM) != nil {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
}
