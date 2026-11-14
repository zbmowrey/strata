package cli

import (
	"fmt"
	"os"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/garden"
)

// instance opens the instance selected by --root or discovered from cwd.
func (a *app) instance() (*config.Instance, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("working directory: %w", err)
	}
	inst, err := config.Open(a.g.root, cwd)
	if err != nil {
		return nil, err
	}
	if a.g.verbose {
		for _, w := range inst.Warnings {
			a.warnf("%s", w)
		}
	}
	// Any command run in a garden worktree is progress for its run. A stale
	// fence is reported to writers by garden.CheckFence, not here.
	if err := garden.Heartbeat(inst, a.now()); err != nil && a.g.verbose {
		a.warnf("garden heartbeat: %v", err)
	}
	return inst, nil
}

// warnf prints a non-essential warning to stderr unless --quiet.
func (a *app) warnf(format string, args ...any) {
	if a.g.quiet {
		return
	}
	_, _ = a.io.Err.Write([]byte("strata: warning: " + sprintf(format, args...) + "\n"))
}
