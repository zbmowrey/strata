package gates

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/finding"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
)

// StageOptions tune RunStage.
type StageOptions struct {
	Now     func() time.Time
	Session string
	Gates   []string
	Fix     bool
	Ratchet bool
	NoCache bool
}

// RunStage loads the instance's gates and runs stage over files
// (repo-relative). Hooks call it: edit with the touched files and Fix, stop
// with Files(SelectChanged) and Fix. Use Format for the blocking message and
// SetHash(report.Blocking) for the loop guard.
func RunStage(ctx context.Context, inst *config.Instance, stage string, files []string, opt StageOptions) (*Report, error) {
	cfg, err := Load(inst)
	if err != nil {
		return nil, err
	}
	tel := telemetry.New(inst, opt.Now)
	if opt.Session != "" {
		tel = tel.WithSession(opt.Session)
	}
	r := &Runner{Inst: inst, Cfg: cfg, Tel: tel, Now: opt.Now, Ratchet: opt.Ratchet, NoCache: opt.NoCache}
	return r.Run(ctx, Request{Stage: stage, Files: files, Gates: opt.Gates, Fix: opt.Fix})
}

// AcceptBaseline runs a gate over every file and records its current
// findings as baselined (the only way a baseline grows). With accept=false
// it reports what would be added without writing. It returns the number of
// new entries.
func AcceptBaseline(ctx context.Context, inst *config.Instance, cfg *Config, id string, accept bool) (int, error) {
	g, ok := cfg.Get(id)
	if !ok {
		return 0, fail.New(fail.Usage, "see `strata gates list`", "unknown gate %q", id)
	}
	if g.Baseline == "" {
		return 0, fail.New(fail.Usage, "add `baseline = \".strata/baselines/"+id+".json\"` to the gate", "gate %q has no baseline file configured", id)
	}
	files, err := Files(ctx, inst.Root, SelectAll, nil)
	if err != nil {
		return 0, err
	}
	saved := g.Baseline
	g.Baseline = "" // run without the baseline to see every current finding
	r := &Runner{Inst: inst, Cfg: cfg, NoCache: true}
	stage := StageCI
	if !g.InStage(StageCI) && len(g.Stages) > 0 {
		stage = g.Stages[len(g.Stages)-1]
	}
	res := r.runGate(ctx, g, Request{Stage: stage, Files: files})
	g.Baseline = saved
	path := inst.Path(filepath.FromSlash(saved))
	b, err := LoadBaseline(path, id)
	if err != nil {
		return 0, err
	}
	fresh, _, _ := b.apply(res.Findings, NewFingerprinter(inst.Root), func(string) bool { return false })
	if !accept || len(fresh) == 0 {
		return len(fresh), nil
	}
	fp := NewFingerprinter(inst.Root)
	for i := range fresh {
		b.Entries = append(b.Entries, BaselineEntry{Rule: fresh[i].Rule, File: fresh[i].File, Fingerprint: fp.Of(&fresh[i])})
	}
	return len(fresh), b.Save(path)
}

// docsURL points at a tool's own documentation for a rule, when known.
func docsURL(parser, rule string) string {
	if rule == "" {
		return ""
	}
	switch parser {
	case ParserGolangci:
		return "https://golangci-lint.run/usage/linters/#" + strings.ToLower(rule)
	case ParserRuff:
		return "https://docs.astral.sh/ruff/rules/#" + rule
	case ParserESLint:
		if !strings.Contains(rule, "/") {
			return "https://eslint.org/docs/latest/rules/" + rule
		}
		return "https://www.npmjs.com/search?q=" + strings.SplitN(rule, "/", 2)[0]
	case ParserSemgrep:
		return "https://semgrep.dev/r/" + rule
	case ParserShellcheck:
		return "https://www.shellcheck.net/wiki/" + rule
	case ParserClippy:
		return "https://rust-lang.github.io/rust-clippy/master/#" + strings.TrimPrefix(rule, "clippy::")
	case ParserPyright:
		return "https://microsoft.github.io/pyright/#/configuration?id=" + rule
	case ParserTSC:
		return "https://typescript.tv/errors/#" + strings.ToLower(rule)
	case ParserGitleaks:
		return "https://github.com/gitleaks/gitleaks (rule " + rule + ")"
	default:
		return ""
	}
}

// Explanation describes a gate (and optionally one of its rules).
type Explanation struct {
	Gate     *Gate  `json:"gate"`
	Rule     string `json:"rule,omitempty"`
	ToolDocs string `json:"tool_docs,omitempty"`
	Command  string `json:"command,omitempty"`
}

// Explain resolves "<gate>[/<rule>]".
func Explain(cfg *Config, ref string) (*Explanation, error) {
	id, rule, _ := strings.Cut(ref, "/")
	g, ok := cfg.Get(id)
	if !ok {
		return nil, fail.New(fail.Usage, "see `strata gates list`", "unknown gate %q", id)
	}
	e := &Explanation{Gate: g, Rule: rule, ToolDocs: docsURL(g.Parser, rule)}
	if g.Builtin != "" {
		e.Command = "built-in: " + g.Builtin
	} else {
		e.Command = strings.Join(g.Cmd, " ")
	}
	return e, nil
}

// Summary counts a report's findings by severity for one-line status.
func Summary(rep *Report) string {
	e, w := finding.Count(append(append([]finding.Finding{}, rep.Blocking...), rep.Advisory...))
	return fmt.Sprintf("%d blocking · %d errors · %d warnings · %d baselined", len(rep.Blocking), e, w, rep.Baselined)
}
