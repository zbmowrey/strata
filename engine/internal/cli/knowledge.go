package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/knowledge"
	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
)

func init() {
	registerNew(newShardCmd)
	registerNew(newPlaybookCmd)
	register(newCaptureCmd)
	register(newMvCmd)
	register(newSupersedeCmd)
	register(newArchiveCmd)
	register(newHubsCmd)
	register(newQuestionsCmd)
}

// store opens the instance's knowledge store.
func (a *app) store() (*knowledge.Store, error) {
	inst, err := a.instance()
	if err != nil {
		return nil, err
	}
	return knowledge.New(inst, a.now, a.tel(inst)), nil
}

// printPaths prints one path per line (the mutating-command contract).
func (a *app) printPaths(paths ...string) error {
	for _, p := range paths {
		if err := a.printf("%s\n", p); err != nil {
			return err
		}
	}
	return nil
}

func newShardCmd(a *app) *cobra.Command {
	var in knowledge.NewShardInput
	cmd := &cobra.Command{
		Use:   "shard <domain>/<topic>/<id>",
		Short: "Scaffold a shard (one idea per shard)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			st, err := a.store()
			if err != nil {
				return err
			}
			in.Path = args[0]
			rel, err := st.NewShard(in)
			if err != nil {
				return err
			}
			return a.printPaths(rel)
		},
	}
	cmd.Flags().StringVar(&in.Type, "type", "", "shard type (required): fact|gotcha|howto|decision|standard|reference|glossary|correction")
	cmd.Flags().StringVar(&in.Title, "title", "", "title (≤ 80 chars)")
	cmd.Flags().StringVar(&in.Summary, "summary", "", "one-line summary (≤ 160 chars)")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

func newPlaybookCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "playbook <name>",
		Short: "Scaffold a playbook (a Claude Code skill indexed as kind:playbook)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			st, err := a.store()
			if err != nil {
				return err
			}
			rel, err := st.NewPlaybook(args[0])
			if err != nil {
				return err
			}
			return a.printPaths(rel)
		},
	}
}

func newCaptureCmd(a *app) *cobra.Command {
	var in knowledge.CaptureInput
	cmd := &cobra.Command{
		Use:   `capture "<text>"`,
		Short: "Capture something non-obvious into the inbox (searchable immediately)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			st, err := a.store()
			if err != nil {
				return err
			}
			in.Text = strings.Join(args, " ")
			in.Session = sessionID()
			rel, err := st.Capture(in)
			if err != nil {
				return err
			}
			return a.printPaths(rel)
		},
	}
	f := cmd.Flags()
	f.StringVar(&in.Type, "type", "fact", "fact|gotcha|howto|decision|standard|reference|glossary|correction")
	f.StringSliceVar(&in.Tags, "tags", nil, "comma-separated tags")
	f.StringVar(&in.Source, "source", "", "evidence as kind:ref (user, url, file, commit, session, script, doc)")
	f.StringVar(&in.Project, "project", "", "project slug (default _adhoc)")
	f.StringVar(&in.Shard, "shard", "", "with --type correction: the shard being corrected (increments occurrences)")
	return cmd
}

// sessionID returns the current Claude session id from the environment.
func sessionID() string {
	for _, k := range telemetry.SessionEnv {
		if v := getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func newMvCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "mv <old-id> <new-id>",
		Short: "Rename a shard id, rewriting every inbound link (old id becomes an alias)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			st, err := a.store()
			if err != nil {
				return err
			}
			changed, err := st.Move(args[0], args[1])
			if err != nil {
				return err
			}
			return a.printPaths(changed...)
		},
	}
}

func newSupersedeCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "supersede <old-id> <new-id>",
		Short: "Mark old as superseded by new (linked both ways)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			st, err := a.store()
			if err != nil {
				return err
			}
			changed, err := st.Supersede(args[0], args[1])
			if err != nil {
				return err
			}
			return a.printPaths(changed...)
		},
	}
}

func newArchiveCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "archive <id>…",
		Short: "Move shards to knowledge/_archive/ (ids and links keep working)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			st, err := a.store()
			if err != nil {
				return err
			}
			moved, err := st.Archive(args)
			if err != nil {
				return err
			}
			return a.printPaths(moved...)
		},
	}
}

func newHubsCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "hubs",
		Short: "Regenerate knowledge/_hubs/ (hand-written intros are preserved)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := a.store()
			if err != nil {
				return err
			}
			written, err := st.Hubs(cmd.Context())
			if err != nil {
				return err
			}
			return a.printPaths(written...)
		},
	}
}

func newQuestionsCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "questions", Short: "Escalations awaiting a human answer"}
	cmd.AddCommand(questionsListCmd(a), questionsShowCmd(a), questionsAskCmd(a), questionsAnswerCmd(a))
	return cmd
}

func questionsListCmd(a *app) *cobra.Command {
	var all bool
	list := &cobra.Command{
		Use: cmdList, Short: "List open questions", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			format, err := a.formatOr("line", "line", fmtJSON)
			if err != nil {
				return err
			}
			st, err := a.store()
			if err != nil {
				return err
			}
			qs, err := st.Questions(all)
			if err != nil {
				return err
			}
			if format == fmtJSON {
				return render.JSON(a.io.Out, map[string]any{"v": 1, "questions": nonNil(qs)})
			}
			for _, q := range qs {
				line := fmt.Sprintf("%s  %-8s  %s", q.ID, q.Status, render.OneLine(q.Question))
				if err := a.printf("%s\n", render.Truncate(line, a.width())); err != nil {
					return err
				}
			}
			return nil
		},
	}
	list.Flags().BoolVar(&all, "all", false, "include answered and withdrawn questions")
	return list
}

func questionsShowCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "show <id>", Short: "Print a question", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			st, err := a.store()
			if err != nil {
				return err
			}
			text, err := st.ShowQuestion(args[0])
			if err != nil {
				return err
			}
			return a.printf("%s", text)
		},
	}
}

func questionsAskCmd(a *app) *cobra.Command {
	var blocking []string
	ask := &cobra.Command{
		Use: `ask "<question>"`, Short: "Raise a question for the human", Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			st, err := a.store()
			if err != nil {
				return err
			}
			rel, err := st.Ask(strings.Join(args, " "), blocking, "claude")
			if err != nil {
				return err
			}
			return a.printPaths(rel)
		},
	}
	ask.Flags().StringSliceVar(&blocking, "blocking", nil, "ids waiting on the answer")
	return ask
}

func questionsAnswerCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "answer <id>", Short: "Mark a question answered (after writing under ## Answer)", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			st, err := a.store()
			if err != nil {
				return err
			}
			rel, err := st.Answer(args[0])
			if err != nil {
				return err
			}
			return a.printPaths(rel)
		},
	}
}
