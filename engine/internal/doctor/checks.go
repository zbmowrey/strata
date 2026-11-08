package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/zbmowrey/strata/engine/internal/finding"
	"github.com/zbmowrey/strata/engine/internal/gates"
	"github.com/zbmowrey/strata/engine/internal/index"
	"github.com/zbmowrey/strata/engine/internal/knowledge"
	"github.com/zbmowrey/strata/engine/internal/lint"
	"github.com/zbmowrey/strata/engine/internal/scaffold"
	"github.com/zbmowrey/strata/engine/internal/setup"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
	"github.com/zbmowrey/strata/engine/internal/version"
)

// managed reports stale managed pieces (CLAUDE.md block, .gitignore blocks,
// hook wiring, allowlist); --fix regenerates them via upgrade.
func (d *doctor) managed() []Check {
	res, err := setup.Upgrade(d.ctx, d.inst, setup.Options{Now: d.opt.Now, Version: version.Version, DryRun: true})
	if err != nil {
		return []Check{{Name: "managed", Status: Fail, Message: err.Error()}}
	}
	var stale []string
	for _, c := range res.Changes {
		if c.Action != setup.Skip {
			stale = append(stale, c.Path+" ("+c.Note+")")
		}
	}
	if len(stale) == 0 {
		return []Check{{Name: "managed", Status: OK, Message: "CLAUDE.md block, .gitignore blocks, hooks and allowlist current"}}
	}
	c := Check{Name: "managed", Status: Warn, Message: "stale: " + strings.Join(stale, ", "), Fix: "strata upgrade"}
	if d.opt.Fix {
		if _, err := setup.Upgrade(d.ctx, d.inst, setup.Options{Now: d.opt.Now, Version: version.Version}); err == nil {
			c.Fixed, c.Message = true, c.Message+" — regenerated"
		}
	}
	return []Check{c}
}

func (d *doctor) gitHook() []Check {
	res, err := setup.InstallGitHook(d.ctx, d.inst, true)
	if err != nil {
		return []Check{{Name: checkGitHook, Status: Warn, Message: err.Error()}}
	}
	for _, w := range res.Warnings {
		return []Check{{Name: checkGitHook, Status: Info, Message: w}}
	}
	for _, c := range res.Changes {
		switch {
		case c.Action == setup.Create:
			out := Check{Name: checkGitHook, Status: Warn, Message: "pre-commit hook missing", Fix: "strata doctor --fix installs it"}
			if d.opt.Fix {
				if _, err := setup.InstallGitHook(d.ctx, d.inst, false); err == nil {
					out.Fixed, out.Message = true, out.Message+" — installed"
				}
			}
			return []Check{out}
		case c.Note != "installed":
			return []Check{{Name: checkGitHook, Status: Warn, Message: c.Note}}
		}
	}
	return []Check{{Name: checkGitHook, Status: OK, Message: "pre-commit runs strata check --staged"}}
}

func (d *doctor) indexHealth() []Check {
	ix, st, err := index.Open(d.ctx, d.inst, index.Options{})
	if err != nil {
		return []Check{{Name: "index", Status: Fail, Message: err.Error(), Fix: "strata reindex"}}
	}
	defer func() { _ = ix.Close() }()
	var res string
	if err := ix.DB().QueryRowContext(d.ctx, "PRAGMA quick_check").Scan(&res); err != nil || res != "ok" {
		return []Check{{Name: "index", Status: Fail, Message: fmt.Sprintf("quick_check: %s %v", res, err), Fix: "strata reindex"}}
	}
	return []Check{{Name: "index", Status: OK, Message: plural(st.Files, "file") + " indexed"}}
}

func (d *doctor) gatesConfig() []Check {
	cfg, err := gates.Load(d.inst)
	if err != nil {
		return []Check{{Name: "gates", Status: Fail, Message: err.Error()}}
	}
	if len(cfg.Gates) == 0 {
		return []Check{{Name: "gates", Status: Warn, Message: "no gates configured", Fix: "strata init (writes .strata/gates.toml for detected languages)"}}
	}
	var disabled []string
	for _, g := range cfg.Gates {
		if g.Disabled {
			disabled = append(disabled, g.ID+" ("+g.Reason+")")
		}
	}
	out := []Check{{Name: "gates", Status: OK, Message: plural(len(cfg.Gates), "gate") + " configured"}}
	if len(disabled) > 0 {
		out = append(out, Check{Name: "gates-disabled", Status: Info, Message: "disabled: " + strings.Join(disabled, "; ")})
	}
	return out
}

func (d *doctor) lintSummary() []Check {
	r, err := lint.Run(d.inst, lint.Options{})
	if err != nil {
		return []Check{{Name: checkLint, Status: Fail, Message: err.Error()}}
	}
	counts := map[string]int{}
	for _, f := range r.Findings {
		counts[f.Severity]++
	}
	msg := fmt.Sprintf("%s checked: %d errors, %d warnings", plural(r.Checked, "file"), counts[finding.Error], counts[finding.Warning])
	switch {
	case counts[finding.Error] > 0:
		return []Check{{Name: checkLint, Status: Fail, Message: msg, Fix: "strata lint"}}
	case counts[finding.Warning] > 0:
		return []Check{{Name: checkLint, Status: Warn, Message: msg, Fix: "strata lint"}}
	}
	return []Check{{Name: checkLint, Status: OK, Message: msg}}
}

func (d *doctor) questions() []Check {
	qs, err := knowledge.New(d.inst, d.opt.Now, telemetry.New(d.inst, d.opt.Now)).Questions(false)
	if err != nil || len(qs) == 0 {
		return nil
	}
	return []Check{{Name: "questions", Status: Info, Message: plural(len(qs), "open question") + " (strata questions list)"}}
}

// tools compares .strata/tools.toml pins with installed versions.
func (d *doctor) tools() []Check {
	raw, err := readFile(d.inst.Path(".strata", "tools.toml"))
	if err != nil {
		return nil
	}
	var file struct {
		Tools map[string]scaffold.ToolPin `toml:"tools"`
	}
	if err := toml.Unmarshal(raw, &file); err != nil {
		return []Check{{Name: "tools", Status: Fail, Message: ".strata/tools.toml: " + err.Error()}}
	}
	names := make([]string, 0, len(file.Tools))
	for n := range file.Tools {
		names = append(names, n)
	}
	slices.Sort(names)
	out := make([]Check, len(names))
	var wg sync.WaitGroup
	for i, n := range names {
		wg.Go(func() { out[i] = d.tool(n, file.Tools[n]) })
	}
	wg.Wait()
	return out
}

// reSemver finds a version token: `@v1.2.3` first (Go module stamps), else
// a standalone `v1.2.3`/`1.2` (not glued to a word such as `go1.27.3`).
var (
	reModVersion = regexp.MustCompile(`@v(\d+\.\d+(?:\.\d+)?)`)
	reSemver     = regexp.MustCompile(`(?:^|[\s(:=,])v?(\d+\.\d+(?:\.\d+)?)`)
)

func versionIn(out string) string {
	for _, re := range []*regexp.Regexp{reModVersion, reSemver} {
		if m := re.FindStringSubmatch(out); m != nil {
			return m[1]
		}
	}
	return ""
}

func (d *doctor) tool(name string, pin scaffold.ToolPin) Check {
	c := Check{Name: "tool:" + name}
	if len(pin.Check) == 0 {
		c.Status, c.Message = Info, "no version check configured"
		return c
	}
	out, err := d.opt.ToolsRun(d.ctx, pin.Check)
	if err != nil {
		c.Status, c.Message, c.Fix = Warn, name+" not found", pin.Install
		return c
	}
	have := versionIn(out)
	if have == "" {
		c.Status, c.Message = Info, "installed; version not detected"
		return c
	}
	if strings.Count(have, ".") == 1 {
		have += ".0"
	}
	ok, err := version.AtLeast(have, pin.Version)
	if err != nil || !ok {
		c.Status, c.Message, c.Fix = Warn, fmt.Sprintf("%s %s < pinned %s", name, have, pin.Version), pin.Install
		return c
	}
	c.Status, c.Message = OK, name+" "+have
	return c
}

// resolveArgs turns `go version -m <tool>` into `go version -m <path>`
// (go needs a binary path; tools.toml names the tool).
func resolveArgs(argv []string) []string {
	out := slices.Clone(argv)
	for i := 1; i < len(out); i++ {
		if out[i-1] == "-m" && !strings.ContainsRune(out[i], '/') {
			if p, err := exec.LookPath(out[i]); err == nil {
				out[i] = p
			}
		}
	}
	return out
}

func runTool(ctx context.Context, argv []string) (string, error) {
	argv = resolveArgs(argv)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput() //nolint:gosec // G204: argv from the instance's tools.toml
	if err != nil {
		return "", fmt.Errorf("%s: %w", argv[0], err)
	}
	return string(out), nil
}
