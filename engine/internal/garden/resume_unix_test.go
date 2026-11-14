//go:build unix

package garden

import (
	"context"
	"encoding/json"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func TestResumeStopsAHungSupervisor(t *testing.T) { //nolint:paralleltest // sets PATH and git identity
	inst, res, _ := startRun(t)
	hung := exec.CommandContext(context.Background(), "sleep", "60")
	hung.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := hung.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- hung.Wait() }()
	s := mustSession(t, inst)
	s.Owner = Owner{Host: hostname(), Launcher: "headless", PID: hung.Process.Pid}
	b, _ := json.Marshal(s)
	testutil.WriteFile(t, inst.Root, RunMarker, string(b))
	// Its heartbeat is stale, so the lease is lost even though it runs.
	if _, err := Resume(context.Background(), inst, ResumeOptions{Now: at(time.Hour), Verify: clean}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the hung supervisor exited cleanly instead of being stopped")
		}
	case <-time.After(5 * time.Second):
		_ = hung.Process.Kill()
		t.Fatal("the hung supervisor was not stopped")
	}
	if res.Run == "" {
		t.Fatal("no run")
	}
}
