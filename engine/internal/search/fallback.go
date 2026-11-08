package search

import (
	"cmp"
	"context"
	"encoding/json"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/index"
)

// Fallback answers a search with an in-process scan when the SQLite index is
// unavailable (docs/spec/01-architecture.md §6): the same filters and
// multipliers, with a TF-based lexical score in place of BM25.
func Fallback(_ context.Context, inst *config.Instance, req Request) (*Result, error) {
	start := time.Now()
	if req.Limit <= 0 {
		req.Limit = inst.Config.Search.DefaultLimit
	}
	if req.Scope.Today.IsZero() {
		req.Scope.Today = time.Now()
	}
	if _, err := buildWhere(req.Query.Filters, req.Scope); err != nil {
		return nil, err // same validation as the indexed path
	}
	recs, err := index.Scan(inst)
	if err != nil {
		return nil, err
	}
	s := &Searcher{cfg: inst.Config.Search}
	var hits []Hit
	best := 0.0
	scores := map[string]float64{}
	for i := range recs {
		r := &recs[i]
		if !scopeOK(r, req.Scope, req.Query.Filters) || !filtersOK(r, req.Query.Filters, req.Scope.Today) || !textOK(r, req.Query, req.Synonyms) {
			continue
		}
		sc := tfScore(r, req.Query, req.Synonyms, s.cfg.Weights)
		scores[r.ID] = sc
		best = math.Max(best, sc)
		hits = append(hits, hitOf(r, req.Scope.Today))
	}
	for i := range hits {
		norm := 1.0
		if best > 0 {
			norm = scores[hits[i].ID] / best
		}
		s.score(&hits[i], &candidate{norm: norm}, req.Explain)
	}
	slices.SortFunc(hits, func(a, b Hit) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(b.Updated, a.Updated), cmp.Compare(a.ID, b.ID))
	})
	res := &Result{Query: req.Raw, Total: len(hits)}
	for _, h := range hits {
		if h.Stale {
			res.Stale++
		}
	}
	if len(hits) > req.Limit {
		hits = hits[:req.Limit]
	}
	res.Hits = hits
	res.MS = time.Since(start).Milliseconds()
	return res, nil
}

func hitOf(r *index.Record, today time.Time) Hit {
	h := Hit{ID: r.ID, Kind: r.Kind, Type: r.Type, Title: r.Title, Summary: r.Summary, Path: r.Path, Status: r.Status, Trust: r.Trust, SideEffect: r.SideEffect, Updated: r.Updated}
	h.Stale = recStale(r, today)
	return h
}

func recStale(r *index.Record, today time.Time) bool {
	if r.ReviewAfterDays <= 0 {
		return false
	}
	ref := r.VerifiedAt
	if ref == "" {
		ref = r.Updated
	}
	if len(ref) > 10 {
		ref = ref[:10]
	}
	t, err := time.Parse(time.DateOnly, ref)
	return err == nil && today.After(t.AddDate(0, 0, r.ReviewAfterDays))
}

func scopeOK(r *index.Record, sc Scope, fs []Filter) bool {
	if r.Private && !sc.Private {
		return false
	}
	statusSet := slices.ContainsFunc(fs, func(f Filter) bool { return f.Key == fStatus })
	return r.Status != "archived" || sc.Archived || statusSet
}

// filtersOK evaluates filters in Go (repeated kind/type/status OR together).
func filtersOK(r *index.Record, fs []Filter, today time.Time) bool {
	groups := map[string][]bool{}
	for _, f := range fs {
		ok, grouped := filterOK(r, f, today)
		if grouped {
			groups[f.Key] = append(groups[f.Key], ok)
		} else if !ok {
			return false
		}
	}
	for _, oks := range groups {
		if !slices.Contains(oks, true) {
			return false
		}
	}
	return true
}

// filterOK reports whether r passes f, and whether f ORs with same-key filters.
func filterOK(r *index.Record, f Filter, today time.Time) (ok, grouped bool) {
	if strings.HasPrefix(f.Key, "meta.") {
		v, _ := json.Marshal(r.Meta[strings.TrimPrefix(f.Key, "meta.")])
		hit := strings.Trim(string(v), `"`) == f.Value || strings.Contains(string(v), `"`+f.Value+`"`)
		return hit != f.Neg, false
	}
	switch f.Key {
	case fKind, fType, fStatus, fRuntime, fDomain, fTopic:
		v := map[string]string{fKind: r.Kind, "type": r.Type, "status": r.Status, "runtime": r.Runtime, "domain": r.Domain, "topic": r.Topic}[f.Key]
		return f.Value == "any" || v == f.Value, true
	case fTag:
		return slices.Contains(r.Tags, strings.ToLower(f.Value)) != f.Neg, false
	case fTrust:
		return orderedOK(r.Trust, f.Value, trustOrder, ">="), false
	case fSide:
		return orderedOK(r.SideEffect, f.Value, sideOrder, "<="), false
	case fPath:
		ok, _ := doublestar.Match(f.Value, strings.TrimSuffix(r.Path, "/"))
		return ok, false
	case fProject:
		return r.Kind == fProject && r.Domain == f.Value, false
	case fStale:
		return recStale(r, today) == (f.Value == "true"), false
	default: // dates
		return dateOK(r, f, today), false
	}
}

func orderedOK(have, want string, order []string, op string) bool {
	v := strings.TrimPrefix(want, op)
	if v == want {
		return have == v
	}
	hi, wi := slices.Index(order, have), slices.Index(order, v)
	if hi < 0 {
		return false
	}
	if op == ">=" {
		return hi >= wi
	}
	return hi <= wi
}

func dateOK(r *index.Record, f Filter, today time.Time) bool {
	v := map[string]string{fUpdated: r.Updated, fCreated: r.Created, fVerified: r.VerifiedAt}[f.Key]
	if len(v) > 10 {
		v = v[:10]
	}
	if v == "" {
		return false
	}
	w := &where{}
	if err := w.date(f, today); err != nil || len(w.sql) == 0 {
		return false
	}
	// Reuse the SQL predicate's operator and argument.
	expr, args := w.sql[0], w.args
	switch {
	case strings.Contains(expr, "BETWEEN"):
		return v >= args[0].(string) && v <= args[1].(string) //nolint:forcetypeassert // date() always binds strings
	default:
		op := strings.Fields(expr[strings.LastIndex(expr, ")")+1:])[0]
		arg := args[0].(string) //nolint:forcetypeassert // date() always binds strings
		return map[string]bool{"=": v == arg, ">": v > arg, ">=": v >= arg, "<": v < arg, "<=": v <= arg}[op]
	}
}

// words tokenizes text into lowercase alphanumeric words.
func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func matchesTerm(text string, t Term, syn Synonyms) bool {
	lower := strings.ToLower(text)
	if t.Phrase {
		return strings.Contains(strings.Join(words(lower), " "), strings.Join(words(t.Text), " "))
	}
	alts := append([]string{strings.ToLower(t.Text)}, syn[strings.ToLower(t.Text)]...)
	for _, w := range words(lower) {
		for _, a := range alts {
			if w == a || (t.Prefix && strings.HasPrefix(w, a)) || (!t.Prefix && len(a) >= 4 && strings.HasPrefix(w, a)) {
				return true
			}
		}
	}
	return false
}

func allText(r *index.Record) string {
	heads := make([]string, 0, len(r.Headings))
	for _, h := range r.Headings {
		heads = append(heads, h.Text)
	}
	return strings.Join([]string{r.Title, r.ID, strings.Join(r.Aliases, " "), r.Summary, strings.Join(r.Tags, " "), strings.Join(heads, " "), r.Body}, "\n")
}

func textOK(r *index.Record, q Query, syn Synonyms) bool {
	text := allText(r)
	for _, g := range q.Groups {
		matched := slices.ContainsFunc(g.Alts, func(t Term) bool { return matchesTerm(text, t, syn) })
		if matched == g.Neg {
			return false
		}
	}
	return true
}

func tfScore(r *index.Record, q Query, syn Synonyms, weights map[string]float64) float64 {
	cols := map[string]string{"title": r.Title, "aliases": r.ID + " " + strings.Join(r.Aliases, " "), "summary": r.Summary, "tags": strings.Join(r.Tags, " "), "body": r.Body}
	total := 0.0
	for _, g := range q.Positive() {
		for _, t := range g.Alts {
			for col, text := range cols {
				if matchesTerm(text, t, syn) {
					total += weights[col]
				}
			}
		}
	}
	return total
}
