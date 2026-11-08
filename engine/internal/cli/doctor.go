package cli

import (
	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/doctor"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/render"
)

func init() { register(newDoctorCmd) }

var doctorMarks = map[string]string{doctor.OK: "✓", doctor.Warn: "⚠", doctor.Fail: "✗", doctor.Info: "ℹ"}

func newDoctorCmd(a *app) *cobra.Command {
	var fix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the instance's health (--fix applies safe fixes)",
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
			checks := doctor.Run(cmd.Context(), inst, doctor.Options{Now: a.now, Fix: fix, PluginsFile: doctor.DefaultPluginsFile()})
			if err := a.printDoctor(checks, format); err != nil {
				return err
			}
			if doctor.Worst(checks) == doctor.Fail {
				return fail.Silent(fail.Blocking)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "regenerate managed blocks, install the git hook, clear a stale garden lock")
	return cmd
}

func (a *app) printDoctor(checks []doctor.Check, format string) error {
	if format == fmtJSON {
		return render.JSON(a.io.Out, map[string]any{"v": 1, "status": doctor.Worst(checks), "checks": checks})
	}
	w := a.width()
	for _, c := range checks {
		mark := doctorMarks[c.Status]
		if c.Fixed {
			mark = "✓"
		}
		line := mark + " " + render.Pad(c.Name, 18) + " " + c.Message
		if c.Fix != "" && !c.Fixed {
			line += " → " + c.Fix
		}
		if err := a.printf("%s\n", render.Truncate(line, w)); err != nil {
			return err
		}
	}
	return nil
}
