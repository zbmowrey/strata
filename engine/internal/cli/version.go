package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/version"
)

func init() { register(newVersionCmd) }

func newVersionCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build information",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			format, err := a.formatOr("line", "line", fmtJSON)
			if err != nil {
				return err
			}
			info := version.Get()
			if format == fmtJSON {
				return json.NewEncoder(a.io.Out).Encode(struct {
					V int `json:"v"`
					version.Info
				}{1, info})
			}
			commit := info.Commit
			if len(commit) > 12 {
				commit = commit[:12]
			}
			if commit == "" {
				commit = "unknown"
			}
			_, err = fmt.Fprintf(a.io.Out, "strata %s (%s, %s, %s/%s)\n", info.Version, commit, info.Go, info.OS, info.Arch)
			return err
		},
	}
}
