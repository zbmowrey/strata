package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/search"
)

func init() { register(newSearchCmd) }

type searchFlags struct {
	kinds, types, tags, notTags, metas       []string
	domain, topic, status, trust, side       string
	runtime, updated, created, verified, pth string
	project, stale, scope                    string
	limit                                    int
	explain, private, archived               bool
}

func newSearchCmd(a *app) *cobra.Command {
	var f searchFlags
	cmd := &cobra.Command{
		Use:   "search <query…>",
		Short: "Ranked one-line results across knowledge, scripts, playbooks and projects",
		Long: `Search the instance. Query syntax: whitespace = AND, OR between terms, "phrases",
-term excludes, term* prefix. Inline filters: kind: type: tag: -tag: domain: topic: status:
trust:(>=x) side:(<=x) runtime: updated:/created:/verified:(>DATE, <DATE, A..B, >30d)
stale: path:(glob) project: meta.<key>:`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runSearch(cmd.Context(), strings.Join(args, " "), f)
		},
	}
	fl := cmd.Flags()
	fl.StringSliceVar(&f.kinds, "kind", nil, "kind filter (repeatable = OR)")
	fl.StringSliceVar(&f.types, "type", nil, "shard type filter")
	fl.StringSliceVar(&f.tags, "tag", nil, "tag filter (repeatable = AND)")
	fl.StringSliceVar(&f.notTags, "not-tag", nil, "exclude tag")
	fl.StringSliceVar(&f.metas, "meta", nil, "frontmatter key=value filter")
	for name, p := range map[string]*string{
		"domain": &f.domain, "topic": &f.topic, "status": &f.status, "trust": &f.trust, "side": &f.side,
		"runtime": &f.runtime, "updated": &f.updated, "created": &f.created, "verified": &f.verified,
		"path": &f.pth, "project": &f.project, "stale": &f.stale,
	} {
		fl.StringVar(p, name, "", name+" filter (same syntax as the inline "+name+": filter)")
	}
	fl.StringVar(&f.scope, "scope", "local", "local | fleet")
	fl.IntVar(&f.limit, "limit", 0, "maximum results (default from config, max 200)")
	fl.BoolVar(&f.explain, "explain", false, "print the score breakdown per hit")
	fl.BoolVar(&f.private, "private", false, "include the private tier")
	fl.BoolVar(&f.archived, "archived", false, "include archived documents")
	return cmd
}

// flagFilters converts flags into inline-equivalent filters.
func (f searchFlags) filters() ([]search.Filter, error) {
	var out []search.Filter
	add := func(key string, vals ...string) {
		for _, v := range vals {
			if v != "" {
				out = append(out, search.Filter{Key: key, Value: v})
			}
		}
	}
	add("kind", f.kinds...)
	add("type", f.types...)
	add("tag", f.tags...)
	for _, t := range f.notTags {
		out = append(out, search.Filter{Key: "tag", Value: t, Neg: true})
	}
	for k, v := range map[string]string{
		"domain": f.domain, "topic": f.topic, "status": f.status, "trust": f.trust, "side": f.side, "runtime": f.runtime,
		"updated": f.updated, "created": f.created, "verified": f.verified, "path": f.pth, "project": f.project, "stale": f.stale,
	} {
		add(k, v)
	}
	for _, m := range f.metas {
		k, v, ok := strings.Cut(m, "=")
		if !ok || k == "" {
			return nil, fail.New(fail.Usage, "use --meta key=value", "bad --meta %q", m)
		}
		out = append(out, search.Filter{Key: "meta." + k, Value: v})
	}
	return out, nil
}

func (a *app) runSearch(ctx context.Context, raw string, f searchFlags) error {
	format, err := a.formatOr("line", "line", "table", fmtJSON, fmtJSONL, "md")
	if err != nil {
		return err
	}
	q, err := search.Parse(raw)
	if err != nil {
		return fail.Wrap(fail.Usage, "see `strata search --help` for query syntax", err)
	}
	extra, err := f.filters()
	if err != nil {
		return err
	}
	q.Filters = append(q.Filters, sortFilters(extra)...)
	if len(q.Groups) == 0 && len(q.Filters) == 0 {
		return fail.New(fail.Usage, "pass search terms or filters", "empty query")
	}
	if f.scope != "local" && f.scope != "fleet" {
		return fail.New(fail.Usage, "use --scope local|fleet", "unknown scope %q", f.scope)
	}
	res, inst, err := a.execSearch(ctx, raw, q, f)
	if err != nil {
		return err
	}
	a.emitSearch(inst, raw, q, res)
	return a.printSearch(res, format, f.explain)
}

func sortFilters(fs []search.Filter) []search.Filter {
	// Map iteration above is unordered; sort for deterministic behavior.
	for i := 1; i < len(fs); i++ {
		for j := i; j > 0 && fs[j].Key < fs[j-1].Key; j-- {
			fs[j], fs[j-1] = fs[j-1], fs[j]
		}
	}
	return fs
}

func (a *app) execSearch(ctx context.Context, raw string, q search.Query, f searchFlags) (*search.Result, *config.Instance, error) {
	inst, err := a.instance()
	if err != nil {
		return nil, nil, err
	}
	syn, err := search.LoadSynonyms(inst.Path(inst.Config.Search.Synonyms))
	if err != nil {
		return nil, nil, fmt.Errorf("synonyms: %w", err)
	}
	req := search.Request{
		Query: q, Raw: raw, Limit: f.limit, Explain: f.explain, Synonyms: syn,
		Scope: search.Scope{Today: a.now(), Private: f.private, Archived: f.archived},
	}
	_, ix, err := a.openIndex(ctx)
	if err != nil {
		if fail.CodeOf(err) == fail.Locked {
			return nil, nil, err
		}
		a.warnf("index unavailable (%v); falling back to a full scan without BM25 statistics", err)
		res, err := search.Fallback(ctx, inst, req)
		return res, inst, err
	}
	defer func() { _ = ix.Close() }()
	if f.scope == "fleet" {
		res, err := search.Fleet(ctx, inst, ix, req)
		return res, inst, err
	}
	res, err := search.New(ix).Search(ctx, req)
	return res, inst, err
}

func (a *app) printSearch(res *search.Result, format string, explain bool) error {
	switch format {
	case fmtJSON:
		return render.JSON(a.io.Out, struct {
			*search.Result
			V int `json:"v"`
		}{res, 1})
	case fmtJSONL:
		return render.JSONL(a.io.Out, res.Hits)
	case "md":
		return a.searchMD(res)
	default:
		return a.searchLines(res, format == "table", explain)
	}
}

// kindLabel is the second column: shard/gotcha, script/ro, playbook…
func kindLabel(h search.Hit) string {
	switch h.Kind {
	case "shard", "inbox":
		if h.Type != "" {
			return h.Kind + "/" + h.Type
		}
	case "script":
		abbr := map[string]string{"read-only": "ro", "writes-local": "wl", "network": "net", "destructive": "DESTR"}[h.SideEffect]
		if abbr != "" {
			return "script/" + abbr
		}
	}
	return h.Kind
}

// hitText is the descriptive column.
func hitText(h search.Hit) string {
	text := h.Title
	if h.Summary != "" && h.Title != h.Summary {
		if h.Title == "" || h.Title == h.ID {
			text = h.Summary
		} else {
			text = h.Title + " — " + h.Summary
		}
	}
	if h.Proposed {
		text = "[proposed] " + text
	}
	if h.Stale {
		text = "⚠ " + text
	}
	if h.Status == "superseded" || h.Status == "deprecated" || h.Status == "archived" {
		text = "(" + h.Status + ") " + text
	}
	return render.OneLine(text)
}

func (a *app) searchLines(res *search.Result, table, explain bool) error {
	width := a.width()
	idW, kindW := 8, 8
	for _, h := range res.Hits {
		idW, kindW = max(idW, render.Runes(h.ID)), max(kindW, render.Runes(kindLabel(h)))
	}
	idW = min(idW, 40)
	if table && len(res.Hits) > 0 {
		if err := a.printf("%s  %s  %s  %s\n", render.Pad("ID", idW), render.Pad("KIND", kindW), "SCORE", "TITLE"); err != nil {
			return err
		}
	}
	for _, h := range res.Hits {
		prefix := fmt.Sprintf("%s  %s  %4.2f  ", render.Pad(h.ID, idW), render.Pad(kindLabel(h), kindW), h.Score)
		line := prefix + render.Truncate(hitText(h), width-render.Runes(prefix))
		if err := a.printf("%s\n", strings.TrimRight(line, " ")); err != nil {
			return err
		}
		if explain && h.Explain != nil {
			e := h.Explain
			if err := a.printf("    bm25=%.4f norm=%.4f syn=%.2f fuzzy=%.2f × status %.2f × trust %.2f × stale %.2f × kind %.2f exact=%v\n",
				e.BM25, e.Norm, e.Synonym, e.Fuzzy, e.Status, e.Trust, e.Stale, e.KindMult, e.Exact); err != nil {
				return err
			}
		}
	}
	return a.searchFooter(res)
}

func (a *app) searchFooter(res *search.Result) error {
	var parts []string
	if res.Stale > 0 {
		parts = append(parts, fmt.Sprintf("⚠ %d stale", res.Stale))
	}
	if more := res.Total - len(res.Hits); more > 0 && len(res.Hits) > 0 {
		parts = append(parts, fmt.Sprintf("%d more results (use --limit)", more))
	}
	if len(res.Hits) == 0 {
		parts = append(parts, "no results")
	}
	if len(parts) > 0 {
		if err := a.printf("%s\n", strings.Join(parts, " · ")); err != nil {
			return err
		}
	}
	for _, s := range res.Suggestions {
		if err := a.printf("  %s\n", s); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) searchMD(res *search.Result) error {
	for _, h := range res.Hits {
		if err := a.printf("- **%s** (%s, %.2f) — %s\n", h.ID, kindLabel(h), h.Score, hitText(h)); err != nil {
			return err
		}
	}
	return a.searchFooter(res)
}
