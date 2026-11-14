package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/garden"
	"github.com/zbmowrey/strata/engine/internal/render"
)

func init() {
	register(newGardenCmd)
	hookGardenAuto = gardenAuto
	hookGardenDebt = gardenDebtLine
}

// gardenDebtLine is the session-start garden line: empty unless debt is due.
func gardenDebtLine(inst *config.Instance, now time.Time) (string, error) {
	d, err := garden.ComputeDebt(inst, now)
	if err != nil || !d.Due {
		return "", err
	}
	return "garden ▸ debt due (" + strings.Join(d.Reasons, ", ") + ") — run /garden", nil
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
			if opt.Apply {
				// In a garden worktree, a writer must still own the run.
				if err := garden.CheckFence(inst, a.now()); err != nil {
					return err
				}
			}
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
	cmd.AddCommand(newGardenStartCmd(a), newGardenFinishCmd(a), newGardenAbortCmd(a),
		newGardenResumeCmd(a), newGardenTaskCmd(a), newGardenStatusCmd(a))
	return cmd
}

func newGardenStartCmd(a *app) *cobra.Command {
	var launcher string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Begin a garden run: lease, worktree + branch, plan, mechanical-fix commit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := a.formatOr("line", "line", fmtJSON)
			if err != nil {
				return err
			}
			inst, err := a.instance()
			if err != nil {
				return err
			}
			if launcher != "manual" && launcher != "headless" {
				return fail.New(fail.Usage, "use manual or headless", "unknown launcher %q", launcher)
			}
			res, err := garden.Start(cmd.Context(), inst, a.now, garden.Owner{Launcher: launcher, Session: sessionFromEnv()})
			if err != nil {
				return err
			}
			if format == fmtJSON {
				return render.JSON(a.io.Out, res)
			}
			if res.Tidy {
				return a.printf("garden is tidy\n")
			}
			return a.printf("%s\n%s · fence %d · %s · plan: %s\n", res.Worktree, res.Branch, res.Fence, render.Plural(res.Tasks, "task"), res.PlanFile)
		},
	}
	cmd.Flags().StringVar(&launcher, "launcher", "manual", "who started the run: manual | headless")
	return cmd
}

func newGardenFinishCmd(a *app) *cobra.Command {
	var opt garden.FinishOptions
	var bodyFile string
	cmd := &cobra.Command{
		Use:   "finish",
		Short: "Push the garden branch, open a draft PR with the digest, mark it ready, clean up",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := a.formatOr("line", "line", fmtJSON)
			if err != nil {
				return err
			}
			inst, err := a.instance()
			if err != nil {
				return err
			}
			if bodyFile != "" {
				b, err := os.ReadFile(bodyFile) //nolint:gosec // G304: the digest file the caller names
				if err != nil {
					return fail.Wrap(fail.Usage, "pass the digest file written by the gardener", err)
				}
				opt.Body = string(b)
			}
			res, err := garden.Finish(cmd.Context(), inst, opt)
			if err != nil {
				return err
			}
			if format == fmtJSON {
				return render.JSON(a.io.Out, res)
			}
			switch {
			case res.PR != nil:
				return a.printf("%s\n", res.PR.URL)
			case res.CompareURL != "":
				return a.printf("%s\n", res.CompareURL)
			}
			return a.printf("%s\n", res.Message)
		},
	}
	cmd.Flags().StringVar(&opt.Title, "title", "", "PR title (default: garden: <stamp>)")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "the digest (docs/spec/07-gardener.md §6) used as the PR body")
	cmd.Flags().BoolVar(&opt.NoReady, "no-ready", false, "leave the PR as a draft")
	cmd.Flags().IntVar(&opt.Fence, "fence", 0, "the fence from `garden start`; refused (exit 7) when the run was taken over")
	return cmd
}

func newGardenAbortCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "abort",
		Short: "Abandon the active /garden run: remove its worktree, local branch and marker",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			inst, err := a.instance()
			if err != nil {
				return err
			}
			return garden.Abort(cmd.Context(), inst)
		},
	}
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
