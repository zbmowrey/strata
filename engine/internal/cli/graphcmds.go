package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/graph"
	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/search"
)

func init() {
	register(newRelatedCmd)
	register(newContextCmd)
	register(newMapCmd)
}

func newRelatedCmd(a *app) *cobra.Command {
	var depth, limit int
	cmd := &cobra.Command{
		Use:   "related <id>",
		Short: "Neighbors in the link graph and by shared tags",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.formatOr("line", "line", fmtJSON, fmtJSONL)
			if err != nil {
				return err
			}
			if depth < 1 || depth > 2 {
				return fail.New(fail.Usage, "--depth takes 1 or 2", "bad --depth %d", depth)
			}
			_, ix, err := a.openIndex(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = ix.Close() }()
			var exists int
			if err := ix.DB().QueryRowContext(cmd.Context(), "SELECT count(*) FROM docs WHERE id = ?", args[0]).Scan(&exists); err != nil || exists == 0 {
				return fail.New(fail.Partial, "check the id with `strata search`", "id not found: %s", args[0])
			}
			rels, err := graph.New(ix.DB()).Related(cmd.Context(), args[0], depth, limit)
			if err != nil {
				return err
			}
			return a.printRelated(args[0], rels, format)
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 1, "1 or 2 hops")
	cmd.Flags().IntVar(&limit, "limit", 15, "maximum results")
	return cmd
}

func (a *app) printRelated(id string, rels []graph.Rel, format string) error {
	switch format {
	case fmtJSON:
		return render.JSON(a.io.Out, map[string]any{"v": 1, "id": id, "related": nonNil(rels)})
	case fmtJSONL:
		return render.JSONL(a.io.Out, rels)
	}
	w := 8
	for _, r := range rels {
		w = max(w, render.Runes(r.ID))
	}
	for _, r := range rels {
		line := fmt.Sprintf("%s  %5.2f  %s", render.Pad(r.ID, min(w, 40)), r.Score, r.Relation)
		if err := a.printf("%s\n", render.Truncate(line, a.width())); err != nil {
			return err
		}
	}
	return nil
}

func newContextCmd(a *app) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "context <path>…",
		Short: "Knowledge and scripts whose applies_to globs match code paths",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.formatOr("line", "line", fmtJSON, fmtJSONL)
			if err != nil {
				return err
			}
			inst, ix, err := a.openIndex(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = ix.Close() }()
			paths, err := relPaths(inst.Root, args)
			if err != nil {
				return err
			}
			hits, err := search.New(ix).Context(cmd.Context(), paths, limit)
			if err != nil {
				return err
			}
			switch format {
			case fmtJSON:
				return render.JSON(a.io.Out, map[string]any{"v": 1, "paths": paths, "results": nonNil(hits)})
			case fmtJSONL:
				return render.JSONL(a.io.Out, hits)
			}
			return a.contextLines(hits)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 10, "maximum results")
	return cmd
}

func (a *app) contextLines(hits []search.ContextHit) error {
	idW := 8
	for _, h := range hits {
		idW = max(idW, render.Runes(h.ID))
	}
	for _, h := range hits {
		label := h.Kind
		if h.Type != "" {
			label += "/" + h.Type
		}
		text := h.Title
		if h.Summary != "" && h.Title != h.ID {
			text += " — " + h.Summary
		} else if h.Summary != "" {
			text = h.Summary
		}
		prefix := fmt.Sprintf("%s  %s  ", render.Pad(h.ID, min(idW, 40)), render.Pad(label, 14))
		if err := a.printf("%s\n", strings.TrimRight(prefix+render.Truncate(render.OneLine(text)+"  ["+h.Glob+"]", a.width()-render.Runes(prefix)), " ")); err != nil {
			return err
		}
	}
	return nil
}

func newMapCmd(a *app) *cobra.Command {
	var depth int
	cmd := &cobra.Command{
		Use:   "map [domain]",
		Short: "Tree of domains → topics → counts (→ shards at depth 3)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.formatOr("line", "line", fmtJSON)
			if err != nil {
				return err
			}
			if depth < 1 || depth > 3 {
				return fail.New(fail.Usage, "--depth takes 1-3", "bad --depth %d", depth)
			}
			domain := ""
			if len(args) == 1 {
				domain = args[0]
			}
			_, ix, err := a.openIndex(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = ix.Close() }()
			nodes, err := search.New(ix).Map(cmd.Context(), domain, depth)
			if err != nil {
				return err
			}
			if format == fmtJSON {
				return render.JSON(a.io.Out, map[string]any{"v": 1, "domains": nonNil(nodes)})
			}
			if len(nodes) == 0 && domain != "" {
				return fail.New(fail.Partial, "see `strata map` for domains", "no shards in domain %q", domain)
			}
			return a.mapLines(nodes)
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 2, "1 = domains, 2 = topics, 3 = shards")
	return cmd
}

func (a *app) mapLines(nodes []search.MapNode) error {
	width := a.width()
	for _, d := range nodes {
		line := fmt.Sprintf("%s (%d)", d.Name, d.Count)
		if d.Summary != "" {
			line += " — " + d.Summary
		}
		if err := a.printf("%s\n", render.Truncate(line, width)); err != nil {
			return err
		}
		for _, t := range d.Children {
			if err := a.printf("  %s (%d)\n", t.Name, t.Count); err != nil {
				return err
			}
			for _, s := range t.Shards {
				if err := a.printf("%s\n", render.Truncate("    "+s.ID+" — "+render.OneLine(s.Summary), width)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
