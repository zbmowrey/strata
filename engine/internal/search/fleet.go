package search

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/index"
)

// ExpandHome expands a leading ~/ in p.
func ExpandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}

// Fleet searches the local index and every federation member in parallel
// (each refreshed first), prefixes member ids with `<name>:` and merges by
// normalized score (docs/spec/01-architecture.md §10). Federation is
// read-only: member indexes are refreshed, never their content.
func Fleet(ctx context.Context, inst *config.Instance, local *index.Index, req Request) (*Result, error) {
	members := inst.Config.Federation.Members
	results := make([]*Result, len(members)+1)
	errs := make([]error, len(members)+1)
	var wg sync.WaitGroup
	wg.Go(func() { results[0], errs[0] = New(local).Search(ctx, req) })
	for i, m := range members {
		wg.Go(func() {
			results[i+1], errs[i+1] = searchMember(ctx, m, req)
		})
	}
	wg.Wait()
	if errs[0] != nil {
		return nil, errs[0]
	}
	merged := &Result{Query: req.Raw}
	for i, r := range results {
		if r == nil {
			if i > 0 {
				merged.Suggestions = append(merged.Suggestions, fmt.Sprintf("member %s unavailable: %v", members[i-1].Name, errs[i]))
			}
			continue
		}
		for _, h := range r.Hits {
			if i > 0 {
				h.ID = members[i-1].Name + ":" + h.ID
			}
			merged.Hits = append(merged.Hits, h)
		}
		merged.Total += r.Total
		merged.Stale += r.Stale
		merged.MS = max(merged.MS, r.MS)
	}
	slices.SortStableFunc(merged.Hits, func(a, b Hit) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(b.Updated, a.Updated), cmp.Compare(a.ID, b.ID))
	})
	if len(merged.Hits) > req.Limit && req.Limit > 0 {
		merged.Hits = merged.Hits[:req.Limit]
	}
	return merged, nil
}

func searchMember(ctx context.Context, m config.Member, req Request) (*Result, error) {
	inst, err := config.Open(ExpandHome(m.Path), "")
	if err != nil {
		return nil, err
	}
	ix, _, err := index.Open(ctx, inst, index.Options{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = ix.Close() }()
	return New(ix).Search(ctx, req)
}
