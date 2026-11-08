package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
)

func init() { register(newTelemetryCmd) }

func newTelemetryCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "telemetry", Short: "Local telemetry (never transmitted): stats and prune"}
	cmd.AddCommand(newTelemetryStatsCmd(a), newTelemetryPruneCmd(a))
	return cmd
}

func newTelemetryStatsCmd(a *app) *cobra.Command {
	var since string
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Summarize local telemetry: events, bash programs, zero-result searches, hook latency, gate results",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			format, err := a.formatOr("line", "line", fmtJSON)
			if err != nil {
				return err
			}
			d, err := config.ParseDuration(since)
			if err != nil {
				return fail.Wrap(fail.Usage, "use a duration such as 30d, 12h or 90m", err)
			}
			inst, err := a.instance()
			if err != nil {
				return err
			}
			from := a.now().Add(-d.D())
			recs, err := telemetry.Read(inst, from)
			if err != nil {
				return err
			}
			st := telemetry.Summarize(recs)
			if format == fmtJSON {
				return render.JSON(a.io.Out, map[string]any{"v": 1, "since": from.UTC().Format(time.RFC3339), "stats": st})
			}
			return a.printTelemetryStats(st, since)
		},
	}
	cmd.Flags().StringVar(&since, "since", "30d", "window to summarize")
	return cmd
}

func (a *app) printTelemetryStats(st telemetry.Stats, since string) error {
	lines := []string{fmt.Sprintf("telemetry (last %s): %s · %s", since, render.Plural(st.Total, "event"), render.Plural(st.Sessions, "session"))}
	if len(st.Events) > 0 {
		lines = append(lines, "events      "+joinCounts(st.Events))
	}
	if st.Searches > 0 {
		lines = append(lines, fmt.Sprintf("search      %d searches · %d zero-result (%.1f%%)", st.Searches, st.ZeroResults, st.ZeroRate*100))
	}
	if len(st.BashTop) > 0 {
		lines = append(lines, "bash top    "+joinCounts(st.BashTop))
	}
	for _, h := range st.Hooks {
		lines = append(lines, fmt.Sprintf("hook        %-14s n=%-5d p50 %.0f ms · p95 %.0f ms", h.Hook, h.N, h.P50, h.P95))
	}
	for _, g := range st.Gates {
		lines = append(lines, fmt.Sprintf("gate        %-14s pass %d · fail %d · other %d", g.Gate, g.Pass, g.Fail, g.Other))
	}
	w := a.width()
	for _, l := range lines {
		if err := a.printf("%s\n", render.Truncate(l, w)); err != nil {
			return err
		}
	}
	return nil
}

func joinCounts(cs []telemetry.Count) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, fmt.Sprintf("%s %d", c.Name, c.N))
	}
	return strings.Join(parts, " · ")
}

func newTelemetryPruneCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "prune",
		Short: "Delete telemetry older than [telemetry] retention",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			inst, err := a.instance()
			if err != nil {
				return err
			}
			n, err := telemetry.Prune(inst, a.now())
			if err != nil {
				return err
			}
			if !a.g.quiet {
				_, _ = fmt.Fprintf(a.io.Err, "strata: pruned %s\n", render.Plural(n, "telemetry event"))
			}
			return nil
		},
	}
}
