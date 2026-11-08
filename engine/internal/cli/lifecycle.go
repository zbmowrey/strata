package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/perms"
	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/setup"
	"github.com/zbmowrey/strata/engine/internal/version"
)

func init() {
	register(newInitCmd)
	register(newUpgradeCmd)
	register(newPermsCmd)
}

func newInitCmd(a *app) *cobra.Command {
	var opt setup.Options
	var profiles string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create or complete a Strata instance in this repository (idempotent)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := a.formatOr("line", "line", fmtJSON)
			if err != nil {
				return err
			}
			root := a.g.root
			if root == "" {
				if root, err = os.Getwd(); err != nil {
					return fmt.Errorf("working directory: %w", err)
				}
			}
			if profiles != "" {
				opt.Profiles = strings.Split(profiles, ",")
			}
			opt.Now, opt.Version = a.now, version.Version
			res, err := setup.Init(cmd.Context(), root, opt)
			if res != nil {
				if perr := a.printSetup(res, format, opt.DryRun); perr != nil && err == nil {
					err = perr
				}
			}
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&profiles, "profile", "", "gate profiles, comma-separated (default: detected): go,python,ts,rust,shell,docs,security")
	f.BoolVar(&opt.CI, "ci", false, "also write .github/workflows/strata.yml")
	f.BoolVar(&opt.Scripts, "scripts", false, "also create the scripts modules and shared lib starters")
	f.BoolVar(&opt.NoHooks, "no-hooks", false, "do not wire hooks or the allowlist into .claude/settings.json")
	f.BoolVar(&opt.DryRun, "dry-run", false, "print the full change list without writing")
	return cmd
}

func newUpgradeCmd(a *app) *cobra.Command {
	var dry bool
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Refresh managed blocks, hook wiring, allowlist, hubs and catalogs",
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
			res, err := setup.Upgrade(cmd.Context(), inst, setup.Options{Now: a.now, Version: version.Version, DryRun: dry})
			if res != nil {
				if perr := a.printSetup(res, format, dry); perr != nil && err == nil {
					err = perr
				}
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&dry, "dry-run", false, "print the change list without writing")
	return cmd
}

// printSetup renders a change list: the full list on dry runs, else the
// changed paths on stdout and skips on stderr.
func (a *app) printSetup(res *setup.Result, format string, dry bool) error {
	for _, w := range res.Warnings {
		a.warnf("%s", w)
	}
	if format == fmtJSON {
		return render.JSON(a.io.Out, map[string]any{"v": 1, "dry_run": dry, "changes": nonNil(res.Changes), "warnings": nonNil(res.Warnings)})
	}
	if dry {
		for _, c := range res.Changes {
			if err := a.printf("%-7s %s%s\n", c.Action, c.Path, noteSuffix(c.Note)); err != nil {
				return err
			}
		}
		return nil
	}
	skipped := 0
	for _, c := range res.Changes {
		if c.Action == setup.Skip && c.Note != "exists" && !strings.HasPrefix(c.Note, "a pre-commit") {
			continue // managed piece already current
		}
		if c.Action == setup.Skip {
			skipped++
			if a.g.verbose || c.Note != "exists" {
				a.warnf("skipped %s%s", c.Path, noteSuffix(c.Note))
			}
		}
	}
	if skipped > 0 && !a.g.verbose {
		a.warnf("kept %s you own (-v lists them)", render.Plural(skipped, "existing file"))
	}
	return a.printPaths(slices.Compact(res.Changed())...)
}

func noteSuffix(n string) string {
	if n == "" {
		return ""
	}
	return " (" + n + ")"
}

func newPermsCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "perms", Short: "Claude Code permission allowlist"}
	var dry bool
	sync := &cobra.Command{
		Use:   "sync",
		Short: "Regenerate the Strata-owned allowlist entries in .claude/settings.json",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			inst, err := a.instance()
			if err != nil {
				return err
			}
			changed, err := perms.Sync(inst, dry)
			if err != nil || !changed {
				return err
			}
			return a.printPaths(perms.SettingsFile)
		},
	}
	sync.Flags().BoolVar(&dry, "dry-run", false, "report whether the allowlist would change")
	cmd.AddCommand(sync)
	return cmd
}
