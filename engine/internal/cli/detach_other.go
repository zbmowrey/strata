//go:build !unix

package cli

import (
	"os/exec"
	"syscall"
)

// groupAttr has no session or group control here.
func groupAttr() *syscall.SysProcAttr { return nil }

// killGroup stops the child process.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
