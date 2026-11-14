package cli

import (
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/garden"
	"github.com/zbmowrey/strata/engine/internal/hooks"
	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
)

func init() { register(newStatusCmd) }

const cmdStatus = "status"

// statusReport is the cheap snapshot behind the plugin status line.
type statusReport struct {
	Garden *garden.Session `json:"garden_run,omitempty"`
	Debt   *garden.Debt    `json:"garden_debt,omitempty"`
	// GardenLive is false when the run lost its lease (07 §9.2).
	GardenLive bool         `json:"garden_live,omitempty"`
	Counts     hooks.Counts `json:"counts"`
	Session    string       `json:"session,omitempty"`
	Gates      string       `json:"gates"` // "ok" | "blocked" | "unknown"
	Blocking   int          `json:"blocking"`
	V          int          `json:"v"`
}

func newStatusCmd(a *app) *cobra.Command {
	var session string
	cmd := &cobra.Command{
		Use:   cmdStatus,
		Short: "Instance snapshot for status lines: counts, last gate result, active /garden run",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := a.formatOr("line", "line", fmtJSON)
			if err != nil {
				return err
			}
			inst, ix, err := a.openIndex(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = ix.Close() }()
			counts, err := hooks.CountIndex(cmd.Context(), ix, a.now())
			if err != nil {
				return err
			}
			if session == "" {
				session = sessionFromEnv()
			}
			rep := statusReport{V: 1, Counts: counts, Session: session, Gates: "unknown"}
			if session != "" {
				if _, err := os.Stat(inst.Path(".strata", "session")); err == nil {
					st := hooks.LoadState(inst.Root, session)
					rep.Blocking = st.LastBlocking
					rep.Gates = map[bool]string{true: "blocked", false: "ok"}[st.LastBlocking > 0]
				}
			}
			if s, err := garden.LoadSession(inst); err == nil {
				rep.Garden, rep.GardenLive = s, garden.LeaseLive(inst, s, a.now())
			}
			if d, err := garden.ComputeDebt(inst, a.now()); err == nil {
				rep.Debt = &d
			}
			if format == fmtJSON {
				return render.JSON(a.io.Out, rep)
			}
			return a.printf("%s\n", statusLine(rep))
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Claude Code session id (default: from the environment)")
	return cmd
}

func sessionFromEnv() string {
	for _, k := range telemetry.SessionEnv {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// statusLine renders `⛬ 1,204 · inbox 3 · ? 1 · gates ✓` (docs/spec/08 §5.3).
func statusLine(r statusReport) string {
	line := "⛬ " + hooks.Thousands(r.Counts.Shards) + " · inbox " + strconv.Itoa(r.Counts.Inbox)
	if r.Counts.Questions > 0 {
		line += " · ? " + strconv.Itoa(r.Counts.Questions)
	}
	switch r.Gates {
	case "ok":
		line += " · gates ✓"
	case "blocked":
		line += " · gates ✗" + strconv.Itoa(r.Blocking)
	}
	switch {
	case r.Garden != nil && r.GardenLive:
		line += " · garden running since " + shortTime(r.Garden.Started)
	case r.Garden != nil:
		line += " · garden lost (strata garden resume)"
	case r.Debt != nil && r.Debt.Due:
		line += " · garden due"
	}
	return line
}

func shortTime(rfc string) string {
	t, err := time.Parse(time.RFC3339, rfc)
	if err != nil {
		return rfc
	}
	return t.Local().Format("15:04")
}
