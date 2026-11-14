// Package doctor implements `strata doctor` (docs/spec/09-cli.md §4):
// health checks over an instance, with safe fixes.
package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/garden"
	"github.com/zbmowrey/strata/engine/internal/version"
)

// Statuses, most severe first.
const (
	Fail = "fail"
	Warn = "warn"
	OK   = "ok"
	Info = "info"
)

// Check is one doctor result.
type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"` // command or action that resolves it
	Fixed   bool   `json:"fixed,omitempty"`
}

// Options configure a doctor run.
type Options struct {
	Now      func() time.Time
	ToolsRun func(ctx context.Context, argv []string) (string, error) // runs a tool's version command
	Fix      bool
	// PluginsFile is Claude Code's installed_plugins.json (DefaultPluginsFile);
	// empty skips the plugin check.
	PluginsFile string
}

// Check names referenced more than once.
const (
	checkGitHook = "git-hook"
	checkLint    = "lint"
)

// Run executes every check (and safe fixes with Fix).
func Run(ctx context.Context, inst *config.Instance, opt Options) []Check {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.ToolsRun == nil {
		opt.ToolsRun = runTool
	}
	d := &doctor{ctx: ctx, inst: inst, opt: opt}
	out := make([]Check, 0, 16)
	for _, f := range []func() []Check{
		d.engine, d.plugin, d.managed, d.gitHook, d.indexHealth, d.gatesConfig, d.tools, d.lintSummary, d.questions, d.gardenLock,
	} {
		out = append(out, f()...)
	}
	return out
}

// Worst returns the most severe status among checks.
func Worst(cs []Check) string {
	worst := OK
	for _, c := range cs {
		if c.Fixed {
			continue
		}
		switch c.Status {
		case Fail:
			return Fail
		case Warn:
			worst = Warn
		}
	}
	return worst
}

type doctor struct {
	ctx  context.Context
	inst *config.Instance
	opt  Options
}

func (d *doctor) engine() []Check {
	want := d.inst.Config.Engine.MinVersion
	// A development build counts as its release for this check.
	ok, err := version.AtLeast(strings.TrimSuffix(version.Current(), "-dev"), want)
	switch {
	case err != nil:
		return []Check{{Name: "engine", Status: Warn, Message: fmt.Sprintf("cannot compare %s with min_version %q: %v", version.Current(), want, err)}}
	case !ok:
		return []Check{{Name: "engine", Status: Fail, Message: fmt.Sprintf("strata %s < min_version %s", version.Current(), want), Fix: "go install github.com/zbmowrey/strata/engine/cmd/strata@latest"}}
	}
	return []Check{{Name: "engine", Status: OK, Message: "strata " + version.Current() + " ≥ min_version " + want}}
}

func (d *doctor) gardenLock() []Check {
	p := d.inst.Path(filepath.FromSlash(garden.RunMarker))
	if _, err := os.Stat(p); err != nil {
		return nil
	}
	now := d.opt.Now()
	s, err := garden.LoadSession(d.inst)
	if err == nil && garden.LeaseLive(d.inst, s, now) {
		return []Check{{Name: "garden-lock", Status: Info, Message: fmt.Sprintf("garden run %s holds the lease (heartbeat %s ago)", s.Run, s.HeartbeatAge(now).Round(time.Minute))}}
	}
	if err == nil && garden.Resumable(d.ctx, d.inst, s) {
		// Never discard work a resume can finish (docs/spec/07 §9.3).
		return []Check{{
			Name: "garden-lock", Status: Warn, Fix: "strata garden resume (or strata garden abort to discard it)",
			Message: fmt.Sprintf("garden run %s lost its lease and is resumable", s.Run),
		}}
	}
	c := Check{Name: "garden-lock", Status: Warn, Message: "garden lease is stale with nothing to salvage", Fix: "strata doctor --fix clears it"}
	if d.opt.Fix {
		// Abort also removes the run's worktree and unpushed branch.
		if garden.Abort(d.ctx, d.inst) == nil || os.Remove(p) == nil {
			c.Fixed, c.Message = true, c.Message+" — cleared"
		}
	}
	return []Check{c}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, strings.TrimSuffix(word, "s"))
}

func readFile(p string) ([]byte, error) {
	b, err := os.ReadFile(p) //nolint:gosec // G304: instance-owned file
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	return b, nil
}
