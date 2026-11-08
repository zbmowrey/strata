package cli

import (
	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/search"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
)

// tel returns the telemetry writer for inst (events never fail a command).
func (a *app) tel(inst *config.Instance) *telemetry.Writer {
	return telemetry.New(inst, a.now)
}

// emitSearch records a search event (docs/spec/01-architecture.md §8).
func (a *app) emitSearch(inst *config.Instance, raw string, q search.Query, res *search.Result) {
	top := make([]string, 0, 5)
	for i, h := range res.Hits {
		if i == 5 {
			break
		}
		top = append(top, h.ID)
	}
	filters := make([]string, 0, len(q.Filters))
	for _, f := range q.Filters {
		filters = append(filters, f.Key+":"+f.Value)
	}
	fields := map[string]any{"filters": filters, "n_results": res.Total, "top_ids": top, "zero": len(res.Hits) == 0, "ms": res.MS}
	if inst.Config.Telemetry.StoreQueries {
		fields["query"] = raw
	}
	_ = a.tel(inst).Emit("search", fields)
}

// emitShow records a show event.
func (a *app) emitShow(inst *config.Instance, res *search.ShowResult) {
	ids := make([]string, 0, len(res.Docs))
	n := 0
	for _, d := range res.Docs {
		if !d.Linked {
			ids = append(ids, d.ID)
		}
		n += len(d.Content)
	}
	missing := make([]string, 0, len(res.Missing))
	for _, m := range res.Missing {
		missing = append(missing, m.ID)
	}
	_ = a.tel(inst).Emit("show", map[string]any{"ids": ids, "missing": missing, "bytes": n})
}
