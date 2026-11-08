package search

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ContextHit is a document whose applies_to globs match a code path.
type ContextHit struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Type        string `json:"type,omitempty"`
	Title       string `json:"title"`
	Summary     string `json:"summary,omitempty"`
	Glob        string `json:"glob"`
	Path        string `json:"matched"`
	SideEffect  string `json:"side_effect,omitempty"`
	Specificity int    `json:"specificity"`
}

// Specificity scores a glob: literal characters count 2, `*`/`?` count 0,
// `**` (with its trailing slash) costs 1, so `tests/conftest.py` beats `tests/**`.
func Specificity(glob string) int {
	score := 0
	for i := 0; i < len(glob); i++ {
		switch glob[i] {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				score--
				i++
				if i+1 < len(glob) && glob[i+1] == '/' {
					i++
				}
			}
		case '?', '[', ']', '{', '}':
		default:
			score += 2
		}
	}
	return score
}

// Context returns active, non-private documents (shards and scripts) whose
// applies_to globs match any of paths, ranked by glob specificity.
func (s *Searcher) Context(ctx context.Context, paths []string, limit int) ([]ContextHit, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT d.id, d.kind, coalesce(d.type, ''), d.title, coalesce(d.summary, ''), coalesce(d.side_effect, ''), a.glob
		FROM applies_to a JOIN docs d ON d.rowid = a.doc
		WHERE d.private = 0 AND d.status NOT IN ('archived', 'superseded', 'deprecated') ORDER BY d.id, a.glob`)
	if err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	defer func() { _ = rows.Close() }()
	best := map[string]ContextHit{}
	for rows.Next() {
		var h ContextHit
		if err := rows.Scan(&h.ID, &h.Kind, &h.Type, &h.Title, &h.Summary, &h.SideEffect, &h.Glob); err != nil {
			return nil, fmt.Errorf("context: %w", err)
		}
		for _, p := range paths {
			if ok, _ := doublestar.Match(h.Glob, strings.TrimPrefix(p, "./")); ok {
				h.Path, h.Specificity = p, Specificity(h.Glob)
				if prev, seen := best[h.ID]; !seen || h.Specificity > prev.Specificity {
					best[h.ID] = h
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	out := make([]ContextHit, 0, len(best))
	for _, h := range best {
		out = append(out, h)
	}
	slices.SortFunc(out, func(a, b ContextHit) int {
		return cmp.Or(cmp.Compare(b.Specificity, a.Specificity), cmp.Compare(a.ID, b.ID))
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// MapNode is a domain or topic in the knowledge map.
type MapNode struct {
	Name     string     `json:"name"`
	Summary  string     `json:"summary,omitempty"`
	Shards   []MapShard `json:"shards,omitempty"`
	Children []MapNode  `json:"topics,omitempty"`
	Count    int        `json:"count"`
}

// MapShard is a leaf in a depth-3 map.
type MapShard struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

// Map builds the domain → topic → shard tree of active, public shards.
// Domains sort by name; topics by count desc then name; shards by updated
// desc then id (matching hubs, docs/spec/02-knowledge.md §6).
func (s *Searcher) Map(ctx context.Context, domain string, depth int) ([]MapNode, error) {
	q := `SELECT d.domain, coalesce(d.topic, ''), d.id, coalesce(d.summary, ''), coalesce(d.updated, '') FROM docs d
		WHERE d.kind = 'shard' AND d.private = 0 AND d.status NOT IN ('archived', 'superseded') AND d.domain != ''`
	args := []any{}
	if domain != "" {
		q += " AND d.domain = ?"
		args = append(args, domain)
	}
	rows, err := s.db().QueryContext(ctx, q+" ORDER BY d.domain, d.topic, d.updated DESC, d.id", args...)
	if err != nil {
		return nil, fmt.Errorf("map: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var domains []MapNode
	for rows.Next() {
		var dom, topic, id, summary, updated string
		if err := rows.Scan(&dom, &topic, &id, &summary, &updated); err != nil {
			return nil, fmt.Errorf("map: %w", err)
		}
		if len(domains) == 0 || domains[len(domains)-1].Name != dom {
			domains = append(domains, MapNode{Name: dom})
		}
		d := &domains[len(domains)-1]
		d.Count++
		if len(d.Children) == 0 || d.Children[len(d.Children)-1].Name != topic {
			d.Children = append(d.Children, MapNode{Name: topic})
		}
		t := &d.Children[len(d.Children)-1]
		t.Count++
		if depth >= 3 {
			t.Shards = append(t.Shards, MapShard{ID: id, Summary: summary})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("map: %w", err)
	}
	return s.finishMap(ctx, domains, depth)
}

func (s *Searcher) finishMap(ctx context.Context, domains []MapNode, depth int) ([]MapNode, error) {
	for i := range domains {
		slices.SortStableFunc(domains[i].Children, func(a, b MapNode) int { return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Name, b.Name)) })
		if depth < 2 {
			domains[i].Children = nil
		}
		var summary string
		err := s.db().QueryRowContext(ctx, "SELECT coalesce(summary, '') FROM docs WHERE kind = 'hub' AND domain = ?", domains[i].Name).Scan(&summary)
		if err == nil {
			domains[i].Summary = summary
		}
	}
	return domains, nil
}
