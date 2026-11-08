package search

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/zbmowrey/strata/engine/internal/textsim"
)

// fuzzyTokens are the positive non-phrase tokens long enough for trigrams.
func fuzzyTokens(q Query) []string {
	var out []string
	for _, g := range q.Positive() {
		for _, t := range g.Alts {
			if !t.Phrase && len([]rune(t.Text)) >= 3 {
				out = append(out, strings.ToLower(t.Text))
			}
		}
	}
	return out
}

// anyTokenMissing reports whether some positive token has no FTS hit.
func (s *Searcher) anyTokenMissing(ctx context.Context, q Query) bool {
	for _, g := range q.Positive() {
		for _, t := range g.Alts {
			ft := ftsTerm(t)
			if ft == "" {
				continue
			}
			var one int
			if err := s.db().QueryRowContext(ctx, "SELECT 1 FROM docs_fts WHERE docs_fts MATCH ? LIMIT 1", ft).Scan(&one); err != nil {
				return true
			}
		}
	}
	return false
}

// trigrams and dice are the shared similarity primitives.
func trigrams(s string) map[string]bool { return textsim.Trigrams(s) }

func dice(a, b map[string]bool) float64 { return textsim.Dice(a, b) }

// nameWords splits a name into lowercase words (plus the whole name).
func nameWords(name string) []string {
	lower := strings.ToLower(name)
	words := strings.FieldsFunc(lower, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	return append(words, lower)
}

// similarity is the mean, over tokens, of each token's best word match.
func similarity(tokens, names []string) float64 {
	if len(tokens) == 0 {
		return 0
	}
	var words []string
	for _, n := range names {
		words = append(words, nameWords(n)...)
	}
	total := 0.0
	for _, tok := range tokens {
		tt := trigrams(tok)
		best := 0.0
		for _, w := range words {
			best = max(best, dice(tt, trigrams(w)), editSimilarity(tok, w))
		}
		total += best
	}
	return total / float64(len(tokens))
}

// fuzzyCandidates returns docs whose names share trigrams with the tokens,
// scored by similarity (≥ threshold), honoring the where clause.
func (s *Searcher) fuzzyCandidates(ctx context.Context, tokens []string, w *where, threshold float64) (map[int64]float64, map[int64]string, error) {
	var tris []string
	seen := map[string]bool{}
	for _, tok := range tokens {
		for t := range trigrams(tok) {
			if !seen[t] {
				seen[t] = true
				tris = append(tris, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
			}
		}
	}
	if len(tris) == 0 {
		return nil, nil, nil
	}
	slices.Sort(tris)
	q := `SELECT d.rowid, d.id, d.title, coalesce((SELECT group_concat(alias, ' ') FROM aliases a WHERE a.doc = d.rowid), '')
		FROM names_tri JOIN docs d ON d.rowid = names_tri.rowid WHERE names_tri MATCH ? AND ` + w.SQL() + ` LIMIT 400`
	rows, err := s.db().QueryContext(ctx, q, append([]any{strings.Join(tris, " OR ")}, w.args...)...)
	if err != nil {
		return nil, nil, fmt.Errorf("search fuzzy: %w", err)
	}
	defer func() { _ = rows.Close() }()
	sims, ids := map[int64]float64{}, map[int64]string{}
	for rows.Next() {
		var rowid int64
		var id, title, aliases string
		if err := rows.Scan(&rowid, &id, &title, &aliases); err != nil {
			return nil, nil, fmt.Errorf("search fuzzy: %w", err)
		}
		if sim := similarity(tokens, []string{id, title, aliases}); sim >= threshold {
			sims[rowid], ids[rowid] = sim, id
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("search fuzzy: %w", err)
	}
	return sims, ids, nil
}

// fuzzy merges trigram matches into the candidate set.
func (s *Searcher) fuzzy(ctx context.Context, q Query, w *where, cands map[int64]*candidate) error {
	sims, _, err := s.fuzzyCandidates(ctx, fuzzyTokens(q), w, fuzzyMinSim)
	if err != nil {
		return err
	}
	for rowid, sim := range sims {
		c := cands[rowid]
		if c == nil {
			c = &candidate{}
			cands[rowid] = c
		}
		c.fuzzy = sim
	}
	return nil
}

// suggest proposes nearest ids and filters to drop after a zero-result search.
func (s *Searcher) suggest(ctx context.Context, req Request) []string {
	return append(s.nearestIDs(ctx, req), s.droppableFilters(ctx, req)...)
}

func (s *Searcher) nearestIDs(ctx context.Context, req Request) []string {
	tokens := fuzzyTokens(req.Query)
	if len(tokens) == 0 {
		tokens = []string{strings.ToLower(req.Query.Text())}
	}
	all, err := buildWhere(nil, req.Scope)
	if err != nil {
		return nil
	}
	sims, ids, err := s.fuzzyCandidates(ctx, tokens, all, 0.3)
	if err != nil {
		return nil
	}
	var out []string
	for i, id := range rankBySim(sims, ids) {
		if i == 3 {
			break
		}
		out = append(out, "did you mean "+id+"?")
	}
	return out
}

// rankBySim orders ids by similarity desc, then id.
func rankBySim(sims map[int64]float64, ids map[int64]string) []string {
	type pair struct {
		id  string
		sim float64
	}
	ps := make([]pair, 0, len(sims))
	for rowid, sim := range sims {
		ps = append(ps, pair{ids[rowid], sim})
	}
	slices.SortFunc(ps, func(a, b pair) int { return cmp.Or(cmp.Compare(b.sim, a.sim), cmp.Compare(a.id, b.id)) })
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.id
	}
	return out
}

// droppableFilters names filters whose removal would yield results.
func (s *Searcher) droppableFilters(ctx context.Context, req Request) []string {
	var out []string
	for i, f := range req.Query.Filters {
		rest := slices.Delete(slices.Clone(req.Query.Filters), i, i+1)
		w, err := buildWhere(rest, req.Scope)
		if err != nil {
			continue
		}
		expr, _, neg := ftsExpr(Query{Groups: req.Query.Groups, Filters: rest}, req.Synonyms)
		if neg != "" {
			w.add("d.rowid NOT IN (SELECT rowid FROM docs_fts WHERE docs_fts MATCH ?)", neg)
		}
		if n, err := s.lexical(ctx, expr, w, map[int64]*candidate{}); err == nil && n > 0 {
			name := f.Key + ":" + f.Value
			if f.Neg {
				name = "-" + name
			}
			out = append(out, fmt.Sprintf("try removing filter %s (%d results)", name, n))
		}
	}
	return out
}

// editSimilarity is 1 - DL(a,b)/max(len) using the optimal-string-alignment
// Damerau-Levenshtein distance; it catches transpositions trigrams miss.
func editSimilarity(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	n := max(len(ra), len(rb))
	if n == 0 || absDiff(len(ra), len(rb)) > 2 {
		return 0
	}
	return 1 - float64(osaDistance(ra, rb))/float64(n)
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// osaDistance computes the optimal string alignment distance.
func osaDistance(a, b []rune) int {
	prev2 := make([]int, len(b)+1)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(b)]
}
