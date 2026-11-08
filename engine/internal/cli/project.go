package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/projects"
	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/schema"
)

func init() {
	register(newProjectCmd)
	register(newResumeCmd)
	register(newGitignoreCmd)
}

// projectStore opens the instance and its project store.
func (a *app) projectStore() (*config.Instance, *projects.Store, error) {
	inst, err := a.instance()
	if err != nil {
		return nil, nil, err
	}
	return inst, projects.New(inst, a.now), nil
}

// syncGitignore regenerates the projects .gitignore block after a change.
func (a *app) syncGitignore(inst *config.Instance) error {
	_, warnings, err := projects.Sync(inst)
	for _, w := range warnings {
		a.warnf("gitignore: %s", w)
	}
	return err
}

// projectCmd is the project command name (also the JSON key of show).
const projectCmd = "project"

func newProjectCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: projectCmd, Short: "Local, multi-session working memory (plan, chronicle, handoff)"}
	cmd.AddCommand(newProjectNewCmd(a), newProjectListCmd(a), newProjectShowCmd(a), newProjectLogCmd(a),
		newProjectHandoffCmd(a), newProjectStatusCmd(a, "pause", projects.StatusPaused, "Pause a project"),
		newProjectStatusCmd(a, "close", projects.StatusDone, "Close a project (the gardener distills it into knowledge)"),
		newProjectStatusCmd(a, "activate", projects.StatusActive, "Mark a paused project active again"),
		newProjectTrackCmd(a), newResumeCmd(a))
	return cmd
}

func newProjectNewCmd(a *app) *cobra.Command {
	var goal string
	var tags []string
	cmd := &cobra.Command{
		Use:   `new <slug> "<title>"`,
		Short: "Create a project",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			inst, st, err := a.projectStore()
			if err != nil {
				return err
			}
			dir, err := st.Create(args[0], args[1], goal, tags)
			if err != nil {
				return err
			}
			if err := a.syncGitignore(inst); err != nil {
				return err
			}
			rel, _ := filepath.Rel(inst.Root, dir)
			return a.printf("%s\n", filepath.ToSlash(rel))
		},
	}
	cmd.Flags().StringVar(&goal, "goal", "", "one-sentence goal (definition of done)")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "tags")
	return cmd
}

func newProjectListCmd(a *app) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   cmdList,
		Short: "List projects (stale ones flagged)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			format, err := a.formatOr("line", "line", fmtJSON)
			if err != nil {
				return err
			}
			_, st, err := a.projectStore()
			if err != nil {
				return err
			}
			sums, err := st.List(all)
			if err != nil {
				return err
			}
			if format == fmtJSON {
				return render.JSON(a.io.Out, map[string]any{"v": 1, "projects": nonNil(sums)})
			}
			return a.projectLines(sums)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include done and archived projects")
	return cmd
}

func (a *app) projectLines(sums []projects.Summary) error {
	w := 8
	for _, s := range sums {
		w = max(w, render.Runes(s.Slug))
	}
	for _, s := range sums {
		status := s.Status
		if s.Archived {
			status = projects.StatusArchived
		}
		flag := ""
		if s.Stale {
			flag = "  ⚠ stale — pause or close?"
		}
		line := fmt.Sprintf("%s  %-8s  %s  %s%s", render.Pad(s.Slug, min(w, 40)), status, s.LastActivity, s.Title, flag)
		if err := a.printf("%s\n", render.Truncate(line, a.width())); err != nil {
			return err
		}
	}
	return nil
}

func newProjectShowCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "show <slug>",
		Short: "Show a project: goal, plan outline, Next and recent log",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			format, err := a.formatOr("md", "md", fmtJSON)
			if err != nil {
				return err
			}
			inst, st, err := a.projectStore()
			if err != nil {
				return err
			}
			sum, err := st.Summary(args[0])
			if err != nil {
				return err
			}
			entries, err := st.Entries(args[0])
			if err != nil {
				return err
			}
			if len(entries) > 5 {
				entries = entries[len(entries)-5:]
			}
			plan := planOutline(filepath.Join(inst.Root, filepath.FromSlash(sum.Dir), projects.PlanFile))
			if format == fmtJSON {
				return render.JSON(a.io.Out, map[string]any{"v": 1, projectCmd: sum, "plan": plan, "recent": nonNil(entries)})
			}
			return a.printf("%s", projectShowMD(sum, plan, entries))
		},
	}
}

// planOutline returns plan.md's headings and numbered steps (≤ 12 lines).
func planOutline(path string) []string {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: path from a loaded project
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(raw), "\n") {
		t := strings.TrimSpace(l)
		if t == "" || t == "1." || (strings.HasPrefix(t, "# ") && len(out) == 0) {
			continue
		}
		out = append(out, t)
		if len(out) == 12 {
			break
		}
	}
	return out
}

func projectShowMD(s projects.Summary, plan []string, entries []projects.Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "═══ %s · project/%s · %s/\n%s\n", s.Slug, s.Status, s.Dir, s.Title)
	if s.Goal != "" {
		fmt.Fprintf(&b, "goal: %s\n", s.Goal)
	}
	fmt.Fprintf(&b, "created %s · updated %s · last activity %s · %s\n", s.Created, s.Updated, s.LastActivity, render.Plural(s.Entries, "log entry"))
	if len(s.Track) > 0 {
		fmt.Fprintf(&b, "tracked: %s\n", strings.Join(s.Track, ", "))
	}
	if len(plan) > 0 {
		b.WriteString("\n## Plan\n" + strings.Join(plan, "\n") + "\n")
	}
	if s.Next != "" {
		b.WriteString("\n## Next\n" + s.Next + "\n")
	}
	if len(entries) > 0 {
		b.WriteString("\n## Recent log\n")
		for i := len(entries) - 1; i >= 0; i-- {
			b.WriteString("- " + entries[i].Headline() + "  [" + entries[i].ID + "]\n")
		}
	}
	return b.String()
}

func newProjectLogCmd(a *app) *cobra.Command {
	texts := map[string]*string{}
	cmd := &cobra.Command{
		Use:   `log [<slug>] --tried|--failed|--worked|--learned|--decided|--blocked|--next|--note "<text>"`,
		Short: "Append a chronicle line (no slug: the _adhoc project)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var prefix, text string
			for _, p := range schema.LogPrefixes {
				if *texts[p] == "" {
					continue
				}
				if prefix != "" {
					return fail.New(fail.Usage, "one line per call", "pass exactly one of --%s", strings.Join(schema.LogPrefixes, "|--"))
				}
				prefix, text = p, *texts[p]
			}
			if prefix == "" {
				return fail.New(fail.Usage, `e.g. strata project log ci-flake --tried "…"`, "pass one of --%s", strings.Join(schema.LogPrefixes, "|--"))
			}
			slug := ""
			if len(args) == 1 {
				slug = args[0]
			}
			_, st, err := a.projectStore()
			if err != nil {
				return err
			}
			path, err := st.Log(slug, prefix, text)
			if err != nil {
				return err
			}
			return a.printf("%s\n", path)
		},
	}
	for _, p := range schema.LogPrefixes {
		texts[p] = new(string)
		cmd.Flags().StringVar(texts[p], p, "", p+": line")
	}
	return cmd
}

func newProjectHandoffCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "handoff <slug>",
		Short: "Rewrite handoff.md, pre-filling Don't redo from failed: log lines",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			_, st, err := a.projectStore()
			if err != nil {
				return err
			}
			path, err := st.Handoff(args[0])
			if err != nil {
				return err
			}
			return a.printf("%s\n", path)
		},
	}
}

func newProjectStatusCmd(a *app, use, status, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <slug>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			inst, st, err := a.projectStore()
			if err != nil {
				return err
			}
			if args[0] == projects.Adhoc {
				return fail.New(fail.Usage, "the _adhoc project is always active", "cannot %s %s", use, projects.Adhoc)
			}
			if err := st.SetStatus(args[0], status); err != nil {
				return err
			}
			if err := a.syncGitignore(inst); err != nil {
				return err
			}
			return a.printf("%s\n", args[0])
		},
	}
}

func newProjectTrackCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "track <slug> <glob>…",
		Short: "Opt project files into version control (rare; see docs/spec/05-projects.md §5)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			inst, st, err := a.projectStore()
			if err != nil {
				return err
			}
			if err := st.Track(args[0], args[1:]); err != nil {
				return err
			}
			if err := a.syncGitignore(inst); err != nil {
				return err
			}
			return a.printf("%s\n", ".gitignore")
		},
	}
}

func newResumeCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "resume [<slug>]",
		Short: "Where active work stands: each project's handoff Next",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			format, err := a.formatOr("md", "md", fmtJSON)
			if err != nil {
				return err
			}
			inst, st, err := a.projectStore()
			if err != nil {
				return err
			}
			var sums []projects.Summary
			if len(args) == 1 {
				sum, err := st.Summary(args[0])
				if err != nil {
					return err
				}
				sums = []projects.Summary{sum}
			} else if sums, err = projects.Active(inst, a.now); err != nil {
				return err
			}
			if format == fmtJSON {
				return render.JSON(a.io.Out, map[string]any{"v": 1, "projects": nonNil(sums)})
			}
			return a.printf("%s", projects.RenderResume(sums))
		},
	}
}

func newGitignoreCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "gitignore", Short: "Managed .gitignore blocks"}
	cmd.AddCommand(&cobra.Command{
		Use:   "sync",
		Short: "Regenerate the projects block from each project's track: globs",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			inst, err := a.instance()
			if err != nil {
				return err
			}
			changed, warnings, err := projects.Sync(inst)
			if err != nil {
				return err
			}
			for _, w := range warnings {
				a.warnf("gitignore: %s", w)
			}
			if changed {
				return a.printf(".gitignore\n")
			}
			return nil
		},
	})
	return cmd
}
