//go:build unix

package cli

import (
	"os/exec"
	"syscall"
)

// groupAttr starts a child in its own session and process group, so it
// outlives the caller and can be stopped as a group.
func groupAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

// killGroup stops a child's whole process group.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) != nil {
		_ = cmd.Process.Kill()
	}
}
