package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/scaffold"
)

// ConfigFile is the instance configuration path.
const ConfigFile = ".strata/config.toml"

// Init creates or completes an instance at root. It is idempotent: existing
// user-owned files are skipped, managed blocks are refreshed.
func Init(ctx context.Context, root string, opt Options) (*Result, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("init: %w", err)
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	r := &run{ctx: ctx, opt: opt, res: &Result{}, inst: &config.Instance{Root: abs, Config: config.Default()}}
	cfgBody, err := scaffold.Render("config.toml", scaffold.VarsFor(r.inst.Config, opt.Version))
	if err != nil {
		return nil, err
	}
	if err := r.createFile(ConfigFile, []byte(cfgBody)); err != nil {
		return nil, err
	}
	if _, err := os.Stat(r.inst.Path(ConfigFile)); err == nil {
		if r.inst, err = config.Open(abs, ""); err != nil {
			return nil, err
		}
		r.res.Warnings = append(r.res.Warnings, r.inst.Warnings...)
	}
	for _, step := range []func() error{r.scaffoldFiles, r.gateFiles, r.managed, r.gitHook} {
		if err := step(); err != nil {
			return r.res, err
		}
	}
	return r.res, nil
}

// Upgrade refreshes Strata's managed pieces of an existing instance: the
// CLAUDE.md block, .gitignore blocks, hook wiring, allowlist, hubs and
// catalogs. It never creates or edits user-owned files.
func Upgrade(ctx context.Context, inst *config.Instance, opt Options) (*Result, error) {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	r := &run{ctx: ctx, inst: inst, opt: opt, res: &Result{}}
	if err := r.managed(); err != nil {
		return r.res, err
	}
	return r.res, nil
}

// InstallGitHook installs the git pre-commit hook (doctor --fix); with
// dryRun it only reports what it would do.
func InstallGitHook(ctx context.Context, inst *config.Instance, dryRun bool) (*Result, error) {
	r := &run{ctx: ctx, inst: inst, opt: Options{DryRun: dryRun, Now: time.Now}, res: &Result{}}
	return r.res, r.gitHook()
}
