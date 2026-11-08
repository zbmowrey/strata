// Package graph answers link-graph questions over the index: neighbors,
// BFS neighborhoods, link counts, and tag-IDF relatedness.
package graph

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"math"
	"slices"
	"strings"
)

// Graph queries the links/tags tables of an index.
type Graph struct {
	db *sql.DB
}

// New returns a Graph over db (an open index database).
func New(db *sql.DB) *Graph { return &Graph{db: db} }

// Edge is a directed relation between two document ids.
type Edge struct {
	From, To, Kind string
}

// Out returns the outbound edges of id (targets that resolve to documents).
func (g *Graph) Out(ctx context.Context, id string) ([]Edge, error) {
	return g.edges(ctx, `SELECT s.id, coalesce(t.id, a.id, l.dst_id), l.kind FROM docs s JOIN links l ON l.src = s.rowid
		LEFT JOIN docs t ON t.id = l.dst_id
		LEFT JOIN docs a ON a.rowid = (SELECT doc FROM aliases WHERE alias = lower(l.dst_id))
		WHERE s.id = ? AND (t.id IS NOT NULL OR a.id IS NOT NULL) ORDER BY l.rowid`, id)
}

// In returns the inbound edges of id (links to the id or to its aliases).
func (g *Graph) In(ctx context.Context, id string) ([]Edge, error) {
	names, err := g.aliasesOf(ctx, id)
	if err != nil {
		return nil, err
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
	return g.edges(ctx, `SELECT s.id, ?, l.kind FROM links l JOIN docs s ON s.rowid = l.src
		WHERE l.dst_id IN (`+ph+`) ORDER BY s.id`, append([]any{id}, names...)...)
}

// aliasesOf returns id followed by its aliases.
func (g *Graph) aliasesOf(ctx context.Context, id string) ([]any, error) {
	names := []any{id}
	rows, err := g.db.QueryContext(ctx, "SELECT alias FROM aliases WHERE doc = (SELECT rowid FROM docs WHERE id = ?)", id)
	if err != nil {
		return nil, fmt.Errorf("graph: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, fmt.Errorf("graph: %w", err)
		}
		names = append(names, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("graph: %w", err)
	}
	return names, nil
}

func (g *Graph) edges(ctx context.Context, q string, args ...any) ([]Edge, error) {
	rows, err := g.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("graph: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Edge
	seen := map[string]bool{}
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.From, &e.To, &e.Kind); err != nil {
			return nil, fmt.Errorf("graph: %w", err)
		}
		if k := e.From + "\x00" + e.To; !seen[k] && e.From != e.To {
			seen[k] = true
			out = append(out, e)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("graph: %w", err)
	}
	return out, nil
}

// Counts returns the number of distinct outbound and inbound neighbors.
func (g *Graph) Counts(ctx context.Context, id string) (out, in int, err error) {
	o, err := g.Out(ctx, id)
	if err != nil {
		return 0, 0, err
	}
	i, err := g.In(ctx, id)
	if err != nil {
		return 0, 0, err
	}
	return len(o), len(i), nil
}

// Neighborhood returns documents within depth hops of start (excluding the
// start ids): at each hop outbound neighbors first, then inbound, ordered
// by the number of links to the frontier (desc), then id.
func (g *Graph) Neighborhood(ctx context.Context, start []string, depth int) ([]string, error) {
	seen := map[string]bool{}
	for _, s := range start {
		seen[s] = true
	}
	frontier := slices.Clone(start)
	var out []string
	for hop := 0; hop < depth && len(frontier) > 0; hop++ {
		outCount, inCount := map[string]int{}, map[string]int{}
		for _, id := range frontier {
			if err := g.count(ctx, id, seen, outCount, inCount); err != nil {
				return nil, err
			}
		}
		next := append(byCount(outCount), byCount(inCount)...)
		var added []string
		for _, id := range next {
			if !seen[id] {
				seen[id] = true
				added = append(added, id)
			}
		}
		out = append(out, added...)
		frontier = added
	}
	return out, nil
}

func (g *Graph) count(ctx context.Context, id string, seen map[string]bool, outCount, inCount map[string]int) error {
	outs, err := g.Out(ctx, id)
	if err != nil {
		return err
	}
	for _, e := range outs {
		if !seen[e.To] {
			outCount[e.To]++
		}
	}
	ins, err := g.In(ctx, id)
	if err != nil {
		return err
	}
	for _, e := range ins {
		if !seen[e.From] {
			if _, isOut := outCount[e.From]; !isOut {
				inCount[e.From]++
			}
		}
	}
	return nil
}

func byCount(m map[string]int) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int { return cmp.Or(cmp.Compare(m[b], m[a]), cmp.Compare(a, b)) })
	return ids
}

// Rel is one related document.
type Rel struct {
	ID       string  `json:"id"`
	Relation string  `json:"relation"` // →, ←, #tag[,#tag…], or a 2-hop path
	Score    float64 `json:"score"`
}

// Related ranks neighbors of id: links (weight 1 each direction) plus shared
// tags weighted by IDF; depth 2 adds neighbors-of-neighbors at half weight.
func (g *Graph) Related(ctx context.Context, id string, depth, limit int) ([]Rel, error) {
	scores := relScores{self: id, m: map[string]*Rel{}}
	bump := scores.bump
	if err := g.linkRelations(ctx, id, 1, bump); err != nil {
		return nil, err
	}
	if err := g.tagRelations(ctx, id, bump); err != nil {
		return nil, err
	}
	if depth >= 2 {
		if err := g.secondHop(ctx, &scores); err != nil {
			return nil, err
		}
	}
	out := make([]Rel, 0, len(scores.m))
	for _, r := range scores.m {
		r.Score = math.Round(r.Score*100) / 100
		out = append(out, *r)
	}
	slices.SortFunc(out, func(a, b Rel) int { return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(a.ID, b.ID)) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type relScores struct {
	m    map[string]*Rel
	self string
}

func (s *relScores) bump(other, rel string, w float64) {
	if other == s.self {
		return
	}
	r := s.m[other]
	if r == nil {
		r = &Rel{ID: other}
		s.m[other] = r
	}
	r.Score += w
	if r.Relation == "" {
		r.Relation = rel
	} else if !strings.Contains(r.Relation, rel) {
		r.Relation += " " + rel
	}
}

// secondHop adds neighbors of linked neighbors at half weight.
func (g *Graph) secondHop(ctx context.Context, s *relScores) error {
	first := make([]string, 0, len(s.m))
	for other, r := range s.m {
		if strings.ContainsAny(r.Relation, "→←") {
			first = append(first, other)
		}
	}
	slices.Sort(first)
	for _, f := range first {
		if err := g.linkRelations(ctx, f, 0.5, func(other, _ string, w float64) { s.bump(other, "via "+f, w) }); err != nil {
			return err
		}
	}
	return nil
}

func (g *Graph) linkRelations(ctx context.Context, id string, w float64, bump func(string, string, float64)) error {
	outs, err := g.Out(ctx, id)
	if err != nil {
		return err
	}
	for _, e := range outs {
		bump(e.To, "→", w)
	}
	ins, err := g.In(ctx, id)
	if err != nil {
		return err
	}
	for _, e := range ins {
		bump(e.From, "←", w)
	}
	return nil
}

func (g *Graph) tagRelations(ctx context.Context, id string, bump func(string, string, float64)) error {
	var n float64
	if err := g.db.QueryRowContext(ctx, "SELECT count(*) FROM docs WHERE status != 'archived' AND private = 0").Scan(&n); err != nil {
		return fmt.Errorf("graph tags: %w", err)
	}
	rows, err := g.db.QueryContext(ctx, `SELECT o.id, t2.tag, (SELECT count(*) FROM tags x WHERE x.tag = t2.tag) FROM docs s
		JOIN tags t1 ON t1.doc = s.rowid JOIN tags t2 ON t2.tag = t1.tag AND t2.doc != s.rowid
		JOIN docs o ON o.rowid = t2.doc WHERE s.id = ? AND o.status != 'archived' AND o.private = 0 ORDER BY o.id, t2.tag`, id)
	if err != nil {
		return fmt.Errorf("graph tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var other, tag string
		var df float64
		if err := rows.Scan(&other, &tag, &df); err != nil {
			return fmt.Errorf("graph tags: %w", err)
		}
		bump(other, "#"+tag, math.Log((n+1)/(df+1))/2)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("graph tags: %w", err)
	}
	return nil
}
