package search

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/index"
)

// MaxLimit caps --limit; CandidatePool is the BM25 candidate set size used
// for normalization (docs/spec/03-search.md §4).
const (
	MaxLimit      = 200
	CandidatePool = 200
	fuzzyDiscount = 0.5
	synonymWeight = 0.7
	fuzzyMinSim   = 0.6
	staleFactor   = 0.85
)

// Request is a search request.
type Request struct {
	Scope    Scope
	Synonyms Synonyms
	Query    Query
	Raw      string
	Limit    int
	Explain  bool
}

// Explain is a hit's score breakdown.
type Explain struct {
	BM25     float64 `json:"bm25,omitempty"`
	Norm     float64 `json:"norm"`
	Synonym  float64 `json:"synonym,omitempty"`
	Fuzzy    float64 `json:"fuzzy,omitempty"`
	Relaxed  float64 `json:"relaxed,omitempty"`
	Status   float64 `json:"status"`
	Trust    float64 `json:"trust"`
	Stale    float64 `json:"stale"`
	KindMult float64 `json:"kind"`
	Exact    bool    `json:"exact,omitempty"`
}

// Hit is one ranked result.
type Hit struct {
	Explain    *Explain `json:"explain,omitempty"`
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Type       string   `json:"type,omitempty"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary,omitempty"`
	Path       string   `json:"path"`
	Status     string   `json:"status,omitempty"`
	Trust      string   `json:"trust,omitempty"`
	SideEffect string   `json:"side_effect,omitempty"`
	Updated    string   `json:"updated,omitempty"`
	Score      float64  `json:"score"`
	Stale      bool     `json:"stale"`
	Proposed   bool     `json:"proposed,omitempty"`
	Exact      bool     `json:"exact,omitempty"`
	rowid      int64
}

// Result is a search response.
type Result struct {
	Query       string   `json:"query"`
	Hits        []Hit    `json:"results"`
	Suggestions []string `json:"suggestions,omitempty"`
	Total       int      `json:"total"`
	MS          int64    `json:"ms"`
	Stale       int      `json:"stale"`
}

// Searcher runs queries against one index.
type Searcher struct {
	ix  *index.Index
	cfg config.Search
}

// New returns a Searcher over ix.
func New(ix *index.Index) *Searcher {
	return &Searcher{ix: ix, cfg: ix.Instance().Config.Search}
}

// db returns the index connection (looked up per call: a rebuild replaces it).
func (s *Searcher) db() *sql.DB { return s.ix.DB() }

// Search executes req.
func (s *Searcher) Search(ctx context.Context, req Request) (*Result, error) {
	start := time.Now()
	if req.Limit <= 0 {
		req.Limit = s.cfg.DefaultLimit
	}
	if req.Limit > MaxLimit {
		return nil, fail.New(fail.Usage, fmt.Sprintf("use --limit ≤ %d", MaxLimit), "limit %d too large", req.Limit)
	}
	if req.Scope.Today.IsZero() {
		req.Scope.Today = time.Now()
	}
	w, err := buildWhere(req.Query.Filters, req.Scope)
	if err != nil {
		return nil, err
	}
	if _, _, neg := ftsExpr(req.Query, req.Synonyms); neg != "" {
		// Exclusions apply to every candidate source (lexical, exact, fuzzy).
		w.add("d.rowid NOT IN (SELECT rowid FROM docs_fts WHERE docs_fts MATCH ?)", neg)
	}
	cands, total, err := s.collect(ctx, req, w)
	if err != nil {
		return nil, err
	}
	hits, err := s.hydrate(ctx, cands, w, req)
	if err != nil {
		return nil, err
	}
	res := &Result{Query: req.Raw, Total: max(total, len(hits))}
	for _, h := range hits {
		if h.Stale {
			res.Stale++
		}
	}
	if len(hits) > req.Limit {
		hits = hits[:req.Limit]
	}
	res.Hits = hits
	if len(hits) == 0 {
		res.Suggestions = s.suggest(ctx, req)
	}
	res.MS = time.Since(start).Milliseconds()
	return res, nil
}

// candidate is a document with its raw signals.
type candidate struct {
	bm25    float64
	norm    float64
	fuzzy   float64
	synonym float64
	relaxed bool // matched only the relaxed (any-term) query
	exact   bool
}

// collect gathers exact, lexical and fuzzy candidates keyed by rowid.
func (s *Searcher) collect(ctx context.Context, req Request, w *where) (map[int64]*candidate, int, error) {
	cands := map[int64]*candidate{}
	expr, orig, _ := ftsExpr(req.Query, req.Synonyms)
	total, err := s.lexical(ctx, expr, w, cands)
	if err != nil {
		return nil, 0, err
	}
	if expr != orig && len(cands) > 0 {
		if err := s.discountSynonyms(ctx, orig, cands); err != nil {
			return nil, 0, err
		}
	}
	if len(cands) < req.Limit {
		if err := s.relax(ctx, req, w, cands); err != nil {
			return nil, 0, err
		}
	}
	if err := s.exact(ctx, req.Query, w, cands); err != nil {
		return nil, 0, err
	}
	if expr != "" && (len(cands) < req.Limit || s.anyTokenMissing(ctx, req.Query)) {
		if err := s.fuzzy(ctx, req.Query, w, cands); err != nil {
			return nil, 0, err
		}
	}
	return cands, total, nil
}

// lexical runs the BM25 query (or, with no positive terms, a filter-only
// listing) and returns the total match count.
func (s *Searcher) lexical(ctx context.Context, expr string, w *where, cands map[int64]*candidate) (int, error) {
	weights := s.weights()
	args := append([]any{}, w.args...)
	var q, count string
	switch {
	case expr != "":
		q = "SELECT d.rowid, bm25(docs_fts, " + weights + ") AS s FROM docs_fts JOIN docs d ON d.rowid = docs_fts.rowid WHERE docs_fts MATCH ? AND " + w.SQL() + " ORDER BY s, d.id LIMIT " + strconv.Itoa(CandidatePool)
		count = "SELECT count(*) FROM docs_fts JOIN docs d ON d.rowid = docs_fts.rowid WHERE docs_fts MATCH ? AND " + w.SQL()
		args = append([]any{expr}, args...)
	default:
		q = "SELECT d.rowid, 0 FROM docs d WHERE " + w.SQL() + " ORDER BY d.updated DESC, d.id LIMIT " + strconv.Itoa(CandidatePool)
		count = "SELECT count(*) FROM docs d WHERE " + w.SQL()
	}
	rows, err := s.db().QueryContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("search: %w", err)
	}
	defer func() { _ = rows.Close() }()
	best := 0.0
	var order []int64
	for rows.Next() {
		var id int64
		var score float64
		if err := rows.Scan(&id, &score); err != nil {
			return 0, fmt.Errorf("search: %w", err)
		}
		cands[id] = &candidate{bm25: score, norm: 1}
		order = append(order, id)
		best = math.Min(best, score)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("search: %w", err)
	}
	if best < 0 {
		for _, id := range order {
			cands[id].norm = cands[id].bm25 / best
		}
	}
	var total int
	if err := s.db().QueryRowContext(ctx, count, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("search count: %w", err)
	}
	return total, nil
}

func (s *Searcher) weights() string {
	ws := make([]string, 0, len(index.FTSColumns))
	for _, c := range index.FTSColumns {
		v, ok := s.cfg.Weights[c]
		if !ok {
			v = config.Default().Search.Weights[c]
		}
		ws = append(ws, strconv.FormatFloat(v, 'f', -1, 64))
	}
	return strings.Join(ws, ", ")
}

// discountSynonyms marks candidates that only match through a synonym.
func (s *Searcher) discountSynonyms(ctx context.Context, orig string, cands map[int64]*candidate) error {
	ids := keys(cands)
	matched := map[int64]bool{}
	if orig != "" {
		rows, err := s.db().QueryContext(ctx, "SELECT rowid FROM docs_fts WHERE docs_fts MATCH ? AND rowid IN ("+int64List(ids)+")", orig)
		if err != nil {
			return fmt.Errorf("search synonyms: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return fmt.Errorf("search synonyms: %w", err)
			}
			matched[id] = true
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("search synonyms: %w", err)
		}
	}
	for _, id := range ids {
		if !matched[id] {
			cands[id].synonym = synonymWeight
		}
	}
	return nil
}

// exact applies the id/alias short-circuit.
func (s *Searcher) exact(ctx context.Context, q Query, w *where, cands map[int64]*candidate) error {
	pos := q.Positive()
	if len(pos) != 1 || len(pos[0].Alts) != 1 || len(q.Groups) != 1 {
		return nil
	}
	text := strings.ToLower(pos[0].Alts[0].Text)
	args := append([]any{text, text}, w.args...)
	rows, err := s.db().QueryContext(ctx, "SELECT d.rowid FROM docs d WHERE (lower(d.id) = ? OR d.rowid IN (SELECT doc FROM aliases WHERE alias = ?)) AND "+w.SQL(), args...)
	if err != nil {
		return fmt.Errorf("search exact: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("search exact: %w", err)
		}
		c := cands[id]
		if c == nil {
			c = &candidate{norm: 1}
			cands[id] = c
		}
		c.exact = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("search exact: %w", err)
	}
	return nil
}

// hydrate loads candidate rows, applies multipliers and sorts.
func (s *Searcher) hydrate(ctx context.Context, cands map[int64]*candidate, w *where, req Request) ([]Hit, error) {
	if len(cands) == 0 {
		return nil, nil
	}
	today := req.Scope.Today.Format(time.DateOnly)
	q := "SELECT d.rowid, d.id, d.kind, d.type, d.status, d.trust, d.title, d.summary, d.path, d.updated, d.side_effect, " + StaleSQL +
		" FROM docs d WHERE d.rowid IN (" + int64List(keys(cands)) + ") AND " + w.SQL()
	rows, err := s.db().QueryContext(ctx, q, append([]any{today}, w.args...)...)
	if err != nil {
		return nil, fmt.Errorf("search hydrate: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var hits []Hit
	for rows.Next() {
		var h Hit
		var typ, status, trust, summary, updated, side sql.NullString
		if err := rows.Scan(&h.rowid, &h.ID, &h.Kind, &typ, &status, &trust, &h.Title, &summary, &h.Path, &updated, &side, &h.Stale); err != nil {
			return nil, fmt.Errorf("search hydrate: %w", err)
		}
		h.Type, h.Status, h.Trust, h.Summary, h.Updated, h.SideEffect = typ.String, status.String, trust.String, summary.String, updated.String, side.String
		if !w.matchPath(h.Path) {
			continue
		}
		s.score(&h, cands[h.rowid], req.Explain)
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search hydrate: %w", err)
	}
	slices.SortFunc(hits, func(a, b Hit) int {
		return cmp.Or(boolCmp(b.Exact, a.Exact), cmp.Compare(b.Score, a.Score), cmp.Compare(b.Updated, a.Updated), cmp.Compare(a.ID, b.ID))
	})
	return hits, nil
}

func boolCmp(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	default:
		return -1
	}
}

var (
	statusMult = map[string]float64{"active": 1.0, "draft": 0.8, "deprecated": 0.5, "superseded": 0.3}
	trustMult  = map[string]float64{"user-stated": 1.15, "verified": 1.1, "observed": 1.0, "inferred": 0.9}
)

// score combines the lexical/fuzzy signal with the multipliers.
func (s *Searcher) score(h *Hit, c *candidate, explain bool) {
	e := &Explain{BM25: round(c.bm25, 4), Norm: round(c.norm, 4), Status: mult(statusMult, h.Status), Trust: mult(trustMult, h.Trust), Stale: 1, KindMult: 1, Exact: c.exact}
	if v, ok := s.cfg.KindBoost[h.Kind]; ok {
		e.KindMult = v
	} else if v, ok := config.Default().Search.KindBoost[h.Kind]; ok {
		e.KindMult = v
	}
	if h.Stale {
		e.Stale = staleFactor
	}
	base := c.norm
	if c.bm25 == 0 && c.fuzzy > 0 {
		base = 0
	}
	if c.synonym > 0 {
		e.Synonym = c.synonym
		base *= c.synonym
	}
	if c.relaxed {
		e.Relaxed = relaxDiscount
		base *= relaxDiscount
	}
	if c.fuzzy > 0 {
		e.Fuzzy = round(c.fuzzy, 4)
		base = math.Max(base, fuzzyDiscount*c.fuzzy)
	}
	h.Score = round(base*e.Status*e.Trust*e.Stale*e.KindMult, 2)
	h.Exact = c.exact
	if c.exact {
		h.Score = 1
	}
	h.Proposed = (h.Type == "decision" || h.Type == "standard") && h.Kind == "shard" && h.Trust != "user-stated" && h.Trust != fVerified
	if explain {
		h.Explain = e
	}
}

func mult(m map[string]float64, k string) float64 {
	if v, ok := m[k]; ok {
		return v
	}
	return 1
}

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

func keys(m map[int64]*candidate) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func int64List(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	if len(parts) == 0 {
		return "NULL"
	}
	return strings.Join(parts, ",")
}

// ftsExpr builds the FTS5 MATCH expressions: expanded (with synonyms),
// original (without), and the negated part.
func ftsExpr(q Query, syn Synonyms) (expanded, original, negated string) {
	var exp, orig, neg []string
	for _, g := range q.Groups {
		var e, o []string
		for _, t := range g.Alts {
			ft := ftsTerm(t)
			if ft == "" {
				continue
			}
			o = append(o, ft)
			e = append(e, ft)
			if !t.Phrase && !t.Prefix {
				for _, alt := range syn[strings.ToLower(t.Text)] {
					e = append(e, ftsTerm(Term{Text: alt}))
				}
			}
		}
		if len(o) == 0 {
			continue
		}
		if g.Neg {
			neg = append(neg, "("+strings.Join(o, " OR ")+")")
			continue
		}
		exp = append(exp, "("+strings.Join(e, " OR ")+")")
		orig = append(orig, "("+strings.Join(o, " OR ")+")")
	}
	return strings.Join(exp, " AND "), strings.Join(orig, " AND "), strings.Join(neg, " OR ")
}

// ftsTerm quotes a term for FTS5 ("" for terms with no indexable characters).
func ftsTerm(t Term) string {
	if strings.IndexFunc(t.Text, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) < 0 {
		return ""
	}
	q := `"` + strings.ReplaceAll(t.Text, `"`, `""`) + `"`
	if t.Prefix {
		q += "*"
	}
	return q
}
