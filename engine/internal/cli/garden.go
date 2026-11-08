package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/garden"
	"github.com/zbmowrey/strata/engine/internal/render"
)

func init() {
	register(newGardenCmd)
	hookGardenAuto = gardenAuto
}

// gardenAuto is the session-start safe subset: mechanical fixes only.
func gardenAuto(ctx context.Context, inst *config.Instance, changed []string) (string, error) {
	p, err := garden.Run(ctx, inst, garden.Options{Apply: true, Changed: changed})
	if err != nil {
		return "", err
	}
	n := 0
	for _, f := range p.Fixes {
		if f.Kind != "index" {
			n += max(1, len(f.Paths))
		}
	}
	if n == 0 {
		return "", nil
	}
	return "garden --auto: " + render.Plural(n, "mechanical fix") + " applied", nil
}

func newGardenCmd(a *app) *cobra.Command {
	var plan, auto bool
	cmd := &cobra.Command{
		Use:   "garden",
		Short: "Deterministic gardener: mechanical fixes and the judgment plan (.strata/garden/plan.json)",
		Long: "Without flags: analyze and print the plan; nothing is changed.\n" +
			"--auto: apply the mechanical fixes only (the SessionStart subset).\n" +
			"--plan: apply the mechanical fixes and write the plan (what /garden runs in its worktree).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := a.formatOr("line", "line", fmtJSON)
			if err != nil {
				return err
			}
			inst, err := a.instance()
			if err != nil {
				return err
			}
			opt := garden.Options{Now: a.now, Apply: plan || auto, Plan: plan || !auto}
			p, err := garden.Run(cmd.Context(), inst, opt)
			if err != nil {
				return err
			}
			for _, w := range p.Warnings {
				a.warnf("%s", w)
			}
			if format == fmtJSON {
				return render.JSON(a.io.Out, p)
			}
			return a.printGarden(p, opt)
		},
	}
	cmd.Flags().BoolVar(&plan, "plan", false, "apply mechanical fixes and write .strata/garden/plan.json")
	cmd.Flags().BoolVar(&auto, "auto", false, "apply mechanical fixes only")
	cmd.MarkFlagsMutuallyExclusive("plan", "auto")
	return cmd
}

func (a *app) printGarden(p *garden.Plan, opt garden.Options) error {
	for _, f := range p.Fixes {
		line := "fixed  " + f.Kind
		if f.Note != "" {
			line += ": " + f.Note
		}
		if len(f.Paths) > 0 {
			line += fmt.Sprintf(" (%s)", render.Plural(len(f.Paths), "file"))
		}
		if err := a.printf("%s\n", line); err != nil {
			return err
		}
	}
	if !opt.Plan {
		return nil
	}
	if len(p.Tasks) == 0 {
		return a.printf("garden is tidy\n")
	}
	w := a.width()
	for _, t := range p.Tasks {
		if err := a.printf("%s\n", render.Truncate(fmt.Sprintf("%-6s %-26s %s", t.Risk, t.Type, t.ID), w)); err != nil {
			return err
		}
	}
	return a.printf("%s · plan: %s\n", render.Plural(len(p.Tasks), "task"), garden.PlanFile)
}
