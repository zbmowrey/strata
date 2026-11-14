package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/garden"
	"github.com/zbmowrey/strata/engine/internal/gates"
	"github.com/zbmowrey/strata/engine/internal/render"
)

// verifySalvage runs the stop-stage gates (fix mode) over salvaged files.
func verifySalvage(ctx context.Context, wt *config.Instance, files []string) (bool, error) {
	rep, err := gates.RunStage(ctx, wt, "stop", files, gates.StageOptions{Now: time.Now, Fix: true})
	if err != nil {
		return false, err //nolint:wrapcheck // gate errors are descriptive
	}
	return !rep.Blocked(), nil
}

func newGardenResumeCmd(a *app) *cobra.Command {
	var force bool
	var launcher string
	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Take over a lost garden run: new fence, fresh worktree, salvaged work, reset interrupted tasks",
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
			res, err := garden.Resume(cmd.Context(), inst, garden.ResumeOptions{
				Now: a.now, Force: force, Verify: verifySalvage,
				Owner: garden.Owner{Launcher: launcher, Session: sessionFromEnv()},
			})
			if err != nil {
				return err
			}
			if format == fmtJSON {
				return render.JSON(a.io.Out, res)
			}
			line := fmt.Sprintf("%s\n%s · attempt %d · fence %d · %s left · plan: %s\n",
				res.Worktree, res.Branch, res.Attempt, res.Fence, render.Plural(res.Tasks, "task"), res.PlanFile)
			if res.Salvage != "" {
				line += "salvage kept (gates failed or it did not apply): " + res.Salvage + "\n"
			}
			return a.printf("%s", line)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "take over a live lease or a stuck run")
	cmd.Flags().StringVar(&launcher, "launcher", "manual", "who resumes: manual | headless")
	return cmd
}

func newGardenTaskCmd(a *app) *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "task <" + strings.Join(garden.TaskStatuses, "|") + "> <task-id>",
		Short: "Record a garden task transition (run with --root <garden worktree>); heartbeats the lease",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if !slices.Contains(garden.TaskStatuses, args[0]) {
				return fail.New(fail.Usage, "actions: "+strings.Join(garden.TaskStatuses, ", "), "unknown task action %q", args[0])
			}
			inst, err := a.instance()
			if err != nil {
				return err
			}
			p, err := garden.UpdateTask(inst, args[1], args[0], note, a.now())
			if err != nil {
				return err
			}
			c := p.Counts()
			return a.printf("%s %s · %d left\n", args[1], p.Tasks[args[1]].Status, c[garden.TaskPending]+c[garden.TaskInProgress])
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "one line for the PR digest")
	return cmd
}

// gardenStatus is `strata garden status --format json`.
type gardenStatus struct {
	Lease    *garden.Session       `json:"lease,omitempty"`
	Progress map[string]int        `json:"progress,omitempty"`
	History  []garden.HistoryEntry `json:"history"`
	Debt     garden.Debt           `json:"debt"`
	Age      string                `json:"heartbeat_age,omitempty"`
	V        int                   `json:"v"`
	Live     bool                  `json:"live"`
}

func newGardenStatusCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   cmdStatus,
		Short: "The garden lease, the run's progress, recent history and garden debt",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			format, err := a.formatOr("line", "line", fmtJSON)
			if err != nil {
				return err
			}
			inst, err := a.instance()
			if err != nil {
				return err
			}
			now := a.now()
			st := gardenStatus{V: 1, History: []garden.HistoryEntry{}}
			if st.Debt, err = garden.ComputeDebt(inst, now); err != nil {
				return err //nolint:wrapcheck // garden errors are descriptive
			}
			if h, err := garden.ReadHistory(inst); err == nil && len(h) > 0 {
				st.History = h[max(0, len(h)-5):]
			}
			if s, err := garden.LoadSession(inst); err == nil {
				st.Lease, st.Live, st.Age = s, garden.LeaseLive(inst, s, now), s.HeartbeatAge(now).Round(time.Second).String()
				if p, err := garden.LoadProgress(inst.Root, s.Run); err == nil {
					st.Progress = p.Counts()
				}
			}
			if format == fmtJSON {
				return render.JSON(a.io.Out, st)
			}
			return a.printf("%s", gardenStatusText(st))
		},
	}
}

func gardenStatusText(st gardenStatus) string {
	var b strings.Builder
	if st.Lease == nil {
		b.WriteString("no garden run\n")
	} else {
		state := "lost — strata garden resume"
		if st.Live {
			state = "live"
		}
		fmt.Fprintf(&b, "run %s · %s · attempt %d · fence %d · heartbeat %s ago\n", st.Lease.Run, state, st.Lease.Attempt, st.Lease.Fence, st.Age)
		if st.Progress != nil {
			fmt.Fprintf(&b, "tasks: %d pending · %d in progress · %d done · %d skipped · %d escalated\n",
				st.Progress[garden.TaskPending], st.Progress[garden.TaskInProgress], st.Progress[garden.TaskDone],
				st.Progress[garden.TaskSkipped], st.Progress[garden.TaskEscalated])
		}
	}
	due := "not due"
	if st.Debt.Due {
		due = "due (" + strings.Join(st.Debt.Reasons, ", ") + ")"
	}
	fmt.Fprintf(&b, "debt: %s · inbox %d · tasks %d\n", due, st.Debt.Inbox, st.Debt.Tasks)
	for _, e := range st.History {
		fmt.Fprintf(&b, "  %s a%d %s %s\n", e.Run, e.Attempt, e.Outcome, e.Ended)
	}
	return b.String()
}
