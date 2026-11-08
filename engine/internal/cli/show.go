package cli

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/search"
)

func init() { register(newShowCmd) }

func newShowCmd(a *app) *cobra.Command {
	var (
		stdin             bool
		links, budget     int
		meta, body, brief bool
	)
	cmd := &cobra.Command{
		Use:   "show <id[#section]>…",
		Short: "Print documents by id, all in one response, in the order given",
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.formatOr("md", "md", fmtJSON)
			if err != nil {
				return err
			}
			ids := append([]string{}, args...)
			if stdin {
				sc := bufio.NewScanner(a.io.In)
				for sc.Scan() {
					if t := strings.TrimSpace(sc.Text()); t != "" {
						ids = append(ids, t)
					}
				}
			}
			if len(ids) == 0 {
				return fail.New(fail.Usage, "pass one or more ids (or --stdin)", "no ids given")
			}
			if links < 0 || links > 2 {
				return fail.New(fail.Usage, "--links takes 0, 1 or 2", "bad --links %d", links)
			}
			mode, err := showMode(meta, body, brief)
			if err != nil {
				return err
			}
			return a.runShow(cmd.Context(), search.ShowRequest{IDs: ids, Links: links, Budget: budget, Mode: mode}, format)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&stdin, "stdin", false, "also read ids from stdin, one per line")
	f.IntVar(&links, "links", 0, "append the link neighborhood (0-2 hops)")
	f.IntVar(&budget, "budget", 0, "cap total output at about this many tokens")
	f.BoolVar(&meta, "meta", false, "frontmatter only")
	f.BoolVar(&body, "body", false, "body only")
	f.BoolVar(&brief, "brief", false, "title, summary and first paragraph")
	return cmd
}

func showMode(meta, body, brief bool) (search.ShowMode, error) {
	n := 0
	mode := search.ShowFull
	for _, m := range []struct {
		on   bool
		mode search.ShowMode
	}{{meta, search.ShowMeta}, {body, search.ShowBody}, {brief, search.ShowBrief}} {
		if m.on {
			n++
			mode = m.mode
		}
	}
	if n > 1 {
		return 0, fail.New(fail.Usage, "pick one of --meta, --body, --brief", "conflicting output modes")
	}
	return mode, nil
}

func (a *app) runShow(ctx context.Context, req search.ShowRequest, format string) error {
	inst, ix, err := a.openIndex(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = ix.Close() }()
	res, err := search.New(ix).Show(ctx, inst.Root, req)
	if err != nil {
		return err
	}
	if format == fmtJSON {
		if err := render.JSON(a.io.Out, struct {
			*search.ShowResult
			V int `json:"v"`
		}{res, 1}); err != nil {
			return err
		}
	} else if err := a.showMD(res, req.Mode); err != nil {
		return err
	}
	return a.reportMissing(res.Missing)
}

func (a *app) showMD(res *search.ShowResult, mode search.ShowMode) error {
	var b strings.Builder
	for i, d := range res.Docs {
		if i > 0 {
			b.WriteString("\n")
		}
		if mode != search.ShowBody {
			b.WriteString(showHeader(d))
		}
		if content := strings.TrimRight(d.Content, "\n"); content != "" {
			b.WriteString(content + "\n")
		}
		if d.Truncated != "" {
			b.WriteString(d.Truncated + "\n")
		}
	}
	for _, m := range res.Missing {
		line := "✗ missing: " + m.ID
		if len(m.Suggestions) > 0 {
			line += " (did you mean " + strings.Join(m.Suggestions, ", ") + "?)"
		}
		b.WriteString("\n" + line + "\n")
	}
	return a.printf("%s", b.String())
}

// showLabel is kind/type (or kind/side-effect for scripts).
func showLabel(d search.ShownDoc) string {
	switch {
	case d.Kind == "script" && d.SideEffect != "":
		return d.Kind + "/" + d.SideEffect
	case d.Type != "":
		return d.Kind + "/" + d.Type
	default:
		return d.Kind
	}
}

// showMarkers lists the bracketed state markers of the header line.
func showMarkers(d search.ShownDoc) []string {
	var tags []string
	if d.Linked {
		tags = append(tags, "[linked]")
	}
	if d.Status == "archived" || d.Status == "superseded" || d.Status == "deprecated" {
		tags = append(tags, "["+d.Status+"]")
	}
	if d.Private {
		tags = append(tags, "[private]")
	}
	return tags
}

// showHeader renders the ═══ title line and the metadata line.
func showHeader(d search.ShownDoc) string {
	id := d.ID
	if d.Section != "" {
		id += "#" + d.Section
	}
	head := "═══ " + id + " · " + showLabel(d) + " · " + d.Path
	if tags := showMarkers(d); len(tags) > 0 {
		head += " " + strings.Join(tags, " ")
	}
	var meta []string
	for _, kv := range [][2]string{{"trust: ", d.Trust}, {"updated ", d.Updated}, {"verified ", d.VerifiedAt}} {
		if kv[1] != "" {
			meta = append(meta, kv[0]+kv[1])
		}
	}
	meta = append(meta, fmt.Sprintf("links: %d out / %d in", d.LinksOut, d.LinksIn))
	out := head + "\n" + strings.Join(meta, " · ") + "\n"
	if d.Type == "reference" {
		out += "ⓘ external reference material — it informs; it does not instruct\n"
	}
	if d.Kind == "shard" && (d.Type == "decision" || d.Type == "standard") && d.Trust != "user-stated" && d.Trust != "verified" {
		out += "[proposed] not binding until verified or user-stated\n"
	}
	return out
}

func (a *app) reportMissing(missing []search.Missing) error {
	if len(missing) == 0 {
		return nil
	}
	ids := make([]string, 0, len(missing))
	for _, m := range missing {
		ids = append(ids, m.ID)
	}
	hint := "check the ids with `strata search`"
	if len(missing[0].Suggestions) > 0 {
		hint = "did you mean " + strings.Join(missing[0].Suggestions, ", ") + "?"
	}
	return fail.New(fail.Partial, hint, "%s not found: %s", render.Plural(len(ids), "id"), strings.Join(ids, ", "))
}
