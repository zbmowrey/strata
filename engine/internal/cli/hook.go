package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/hooks"
)

func init() { register(newHookCmd) }

// hookGates, hookGardenAuto and hookGardenLine are wired by the gates and
// garden commands.
var (
	hookGates      hooks.Gates
	hookGardenAuto hooks.GardenAuto
	hookGardenLine hooks.GardenLine
)

func newHookCmd(a *app) *cobra.Command {
	var subagent bool
	cmd := &cobra.Command{
		Use:       "hook <event>",
		Short:     "Claude Code hook adapter (reads the hook payload on stdin)",
		Long:      "Events: " + strings.Join(hooks.Events, ", ") + ". Installed by `strata init` into .claude/settings.json.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: hooks.Events,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !slices.Contains(hooks.Events, args[0]) {
				return fail.New(fail.Usage, "events: "+strings.Join(hooks.Events, ", "), "unknown hook event %q", args[0])
			}
			code := hooks.Run(cmd.Context(), args[0], a.io.In, hooks.Env{
				Now: a.now, Out: a.io.Out, Err: a.io.Err, Root: a.g.root, Subagent: subagent,
				Gates: hookGates, GardenAuto: hookGardenAuto, GardenLine: hookGardenLine,
			})
			return fail.Silent(code)
		},
	}
	cmd.Flags().BoolVar(&subagent, "subagent", false, "SubagentStop: same gates, no knowledge nudge")
	return cmd
}
