package cli

import (
	"fmt"
	"os"

	"github.com/zbmowrey/strata/engine/internal/config"
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
	return inst, nil
}

// warnf prints a non-essential warning to stderr unless --quiet.
func (a *app) warnf(format string, args ...any) {
	if a.g.quiet {
		return
	}
	_, _ = a.io.Err.Write([]byte("strata: warning: " + sprintf(format, args...) + "\n"))
}
