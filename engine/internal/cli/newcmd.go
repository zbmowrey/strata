package cli

import "github.com/spf13/cobra"

func init() { register(newNewCmd) }

// newSubcommands are the `strata new <kind>` constructors (script, shard,
// playbook); each kind's file appends to it from init via registerNew.
var newSubcommands []func(*app) *cobra.Command

func registerNew(f func(*app) *cobra.Command) { newSubcommands = append(newSubcommands, f) }

func newNewCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "new", Short: "Scaffold a shard, script or playbook"}
	for _, f := range newSubcommands {
		cmd.AddCommand(f(a))
	}
	return cmd
}
