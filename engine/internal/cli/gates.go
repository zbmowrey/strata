package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/gates"
	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/search"
)

func init() {
	register(newCheckCmd)
	register(newGatesCmd)
}

type checkFlags struct {
	stage                   string
	only                    []string
	changed, staged, all    bool
	fix, noCache, noRatchet bool
}

func newCheckCmd(a *app) *cobra.Command {
	var f checkFlags
	cmd := &cobra.Command{
		Use:   "check [--changed|--staged|--all|<paths…>]",
		Short: "Run the gates for a stage over changed, staged, all, or given files",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runCheck(cmd.Context(), f, args)
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&f.changed, "changed", false, "working tree vs HEAD plus untracked files (default)")
	fl.BoolVar(&f.staged, "staged", false, "staged files (pre-commit)")
	fl.BoolVar(&f.all, "all", false, "every file in the repository")
	fl.StringVar(&f.stage, "stage", "", "edit|stop|commit|ci (default: stop; commit with --staged; ci with --all)")
	fl.StringSliceVar(&f.only, "gate", nil, "run only these gate ids")
	fl.BoolVar(&f.fix, "fix", false, "let fix-mode gates modify files")
	fl.BoolVar(&f.noCache, "no-cache", false, "ignore cached gate results")
	fl.BoolVar(&f.noRatchet, "no-ratchet", false, "do not shrink baselines")
	return cmd
}

func (f checkFlags) selection(args []string) (gates.Selection, error) {
	n := 0
	for _, b := range []bool{f.changed, f.staged, f.all, len(args) > 0} {
		if b {
			n++
		}
	}
	if n > 1 {
		return 0, fail.New(fail.Usage, "pick one of --changed, --staged, --all or paths", "conflicting file selections")
	}
	switch {
	case f.staged:
		return gates.SelectStaged, nil
	case f.all:
		return gates.SelectAll, nil
	case len(args) > 0:
		return gates.SelectPaths, nil
	default:
		return gates.SelectChanged, nil
	}
}

func (a *app) runCheck(ctx context.Context, f checkFlags, args []string) error {
	format, err := a.formatOr("line", "line", fmtJSON)
	if err != nil {
		return err
	}
	sel, err := f.selection(args)
	if err != nil {
		return err
	}
	stage := f.stage
	if stage == "" {
		stage = gates.DefaultStage(sel)
	}
	if !slices.Contains(gates.Stages, stage) {
		return fail.New(fail.Usage, "stages: "+strings.Join(gates.Stages, ", "), "unknown stage %q", stage)
	}
	inst, err := a.instance()
	if err != nil {
		return err
	}
	if sel == gates.SelectPaths {
		if args, err = relPaths(inst.Root, args); err != nil {
			return err
		}
	}
	files, err := gates.Files(ctx, inst.Root, sel, args)
	if err != nil {
		return err
	}
	rep, err := gates.RunStage(ctx, inst, stage, files, gates.StageOptions{Now: a.now, Gates: f.only, Fix: f.fix, Ratchet: !f.noRatchet, NoCache: f.noCache})
	if err != nil {
		return err
	}
	return a.printCheck(rep, len(files), format)
}

func (a *app) printCheck(rep *gates.Report, nfiles int, format string) error {
	if format == fmtJSON {
		if err := render.JSON(a.io.Out, map[string]any{
			"v": 1, "stage": rep.Stage, "files": nfiles, "blocked": rep.Blocked(), "set_hash": gates.SetHash(rep.Blocking),
			"blocking": nonNil(rep.Blocking), "advisory": nonNil(rep.Advisory), "baselined": rep.Baselined, "gates": nonNil(rep.Gates),
		}); err != nil {
			return err
		}
	} else if err := a.checkLines(rep, nfiles); err != nil {
		return err
	}
	if rep.Blocked() {
		return &fail.Error{
			Code: fail.Blocking, Err: fmt.Errorf("%s", render.Plural(len(rep.Blocking), "blocking finding")),
			Hint: "fix them; `strata gates explain <gate>/<rule>` explains a rule",
		}
	}
	return nil
}

func (a *app) checkLines(rep *gates.Report, nfiles int) error {
	if out := gates.Format(rep); out != "" {
		if err := a.printf("%s", out); err != nil {
			return err
		}
	}
	if len(rep.Advisory) > 0 {
		if err := a.printf("advisory (non-blocking):\n"); err != nil {
			return err
		}
		if err := render.Findings(a.io.Out, rep.Advisory); err != nil {
			return err
		}
	}
	if a.g.quiet {
		return nil
	}
	ran := make([]string, 0, len(rep.Gates))
	for _, g := range rep.Gates {
		if g.Status != gates.StatusSkipped {
			ran = append(ran, g.ID+":"+g.Status)
		}
	}
	mark := "✓"
	if rep.Blocked() {
		mark = "✗"
	}
	_, err := fmt.Fprintf(a.io.Err, "strata check %s %s stage · %s · %s · %s\n", mark, rep.Stage, render.Plural(nfiles, "file"), gates.Summary(rep), strings.Join(ran, " "))
	return err
}

func newGatesCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "gates", Short: "List, explain and baseline gates"}
	cmd.AddCommand(newGatesListCmd(a), newGatesExplainCmd(a), newGatesBaselineCmd(a))
	return cmd
}

func (a *app) loadGates() (*config.Instance, *gates.Config, error) {
	inst, err := a.instance()
	if err != nil {
		return nil, nil, err
	}
	cfg, err := gates.Load(inst)
	if err != nil {
		return nil, nil, err
	}
	return inst, cfg, nil
}

func newGatesListCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured gates",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			format, err := a.formatOr("line", "line", fmtJSON)
			if err != nil {
				return err
			}
			_, cfg, err := a.loadGates()
			if err != nil {
				return err
			}
			if format == fmtJSON {
				return render.JSON(a.io.Out, map[string]any{"v": 1, "gates": nonNil(cfg.Gates)})
			}
			for _, g := range cfg.Gates {
				if err := a.printf("%s\n", render.Truncate(gateLine(g), a.width())); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func gateLine(g gates.Gate) string {
	block := "blocking"
	if !g.IsBlocking() {
		block = "advisory"
	}
	if g.Disabled {
		block = "DISABLED (" + g.Reason + ")"
	}
	tool := g.Builtin
	if tool == "" {
		tool = g.Cmd[0]
	}
	return fmt.Sprintf("%-16s %-5s %-8s %-28s %-12s %s", g.ID, g.Mode, g.Scope, strings.Join(g.Stages, ","), tool, block)
}

func newGatesExplainCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "explain <gate>[/<rule>]",
		Short: "Explain a gate or rule: its rationale shard and the tool's docs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inst, cfg, err := a.loadGates()
			if err != nil {
				return err
			}
			e, err := gates.Explain(cfg, args[0])
			if err != nil {
				return err
			}
			return a.printExplain(cmd.Context(), inst, e)
		},
	}
}

func (a *app) printExplain(ctx context.Context, inst *config.Instance, e *gates.Explanation) error {
	g := e.Gate
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s\n", g.ID, g.Desc)
	if e.Rule != "" {
		fmt.Fprintf(&b, "rule: %s\n", e.Rule)
	}
	fmt.Fprintf(&b, "runs: %s (stages %s, %s scope, %s)\n", e.Command, strings.Join(g.Stages, ","), g.Scope, map[bool]string{true: "blocking", false: "advisory"}[g.IsBlocking()])
	if e.ToolDocs != "" {
		fmt.Fprintf(&b, "tool docs: %s\n", e.ToolDocs)
	}
	if g.Docs != "" {
		b.WriteString(a.rationale(ctx, inst, g.Docs))
	} else {
		b.WriteString("rationale: no linked shard (set `docs = \"<shard-id>\"` on the gate)\n")
	}
	return a.printf("%s", b.String())
}

// rationale returns the brief of the gate's docs shard via the index.
func (a *app) rationale(ctx context.Context, inst *config.Instance, id string) string {
	_, ix, err := a.openIndex(ctx)
	if err != nil {
		return "rationale: " + id + " (index unavailable)\n"
	}
	defer func() { _ = ix.Close() }()
	res, err := search.New(ix).Show(ctx, inst.Root, search.ShowRequest{IDs: []string{id}, Mode: search.ShowBrief})
	if err != nil || len(res.Docs) == 0 {
		return "rationale: shard " + id + " not found (strata new shard … to write it)\n"
	}
	return "rationale [" + id + "]:\n" + strings.TrimRight(res.Docs[0].Content, "\n") + "\n"
}

func newGatesBaselineCmd(a *app) *cobra.Command {
	var accept bool
	cmd := &cobra.Command{
		Use:   "baseline <gate>",
		Short: "Record a gate's current findings as baselined (requires --accept to write)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inst, cfg, err := a.loadGates()
			if err != nil {
				return err
			}
			n, err := gates.AcceptBaseline(cmd.Context(), inst, cfg, args[0], accept)
			if err != nil {
				return err
			}
			g, _ := cfg.Get(args[0])
			switch {
			case n == 0:
				return a.printf("%s: nothing to add; baseline %s is current\n", args[0], g.Baseline)
			case accept:
				return a.printf("%s: added %s to %s\n", args[0], render.Plural(n, "finding"), g.Baseline)
			default:
				return a.printf("%s: %s would be added to %s (re-run with --accept to write; baselines only shrink otherwise)\n", args[0], render.Plural(n, "finding"), g.Baseline)
			}
		},
	}
	cmd.Flags().BoolVar(&accept, "accept", false, "write the new entries (a deliberate human/gardener-PR act)")
	return cmd
}
