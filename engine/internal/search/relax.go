package search

import (
	"context"
	"strings"
)

// relaxDiscount scales candidates that match only some of the query's
// terms (docs/spec/03-search.md §4 step 3a).
const relaxDiscount = 0.6

// relaxFloor drops relaxed candidates far below the best relaxed match
// (they typically share one common word with the query).
const relaxFloor = 0.3

// stopwords are dropped from the relaxed query: they match almost every
// document and say nothing about the topic.
var stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true, "be": true, "by": true, "can": true,
	"do": true, "does": true, "for": true, "from": true, "how": true, "i": true, "if": true, "in": true, "is": true,
	"it": true, "me": true, "my": true, "of": true, "on": true, "or": true, "should": true, "so": true, "the": true,
	"this": true, "to": true, "we": true, "what": true, "when": true, "where": true, "which": true, "who": true,
	"why": true, "with": true, "you": true, "our": true, "after": true, "before": true, "there": true, "than": true,
}

// relaxedExpr ORs the query's positive term groups (stopwords dropped). It
// is empty when fewer than two meaningful groups remain: a one-term query
// has nothing to relax.
func relaxedExpr(q Query, syn Synonyms) string {
	var parts []string
	for _, g := range q.Groups {
		if g.Neg {
			continue
		}
		if alts := relaxedAlts(g, syn); len(alts) > 0 {
			parts = append(parts, strings.Join(alts, " OR "))
		}
	}
	if len(parts) < 2 {
		return ""
	}
	return strings.Join(parts, " OR ")
}

// relaxedAlts returns a group's FTS terms (with synonyms), minus stopwords.
func relaxedAlts(g Group, syn Synonyms) []string {
	var alts []string
	for _, t := range g.Alts {
		if !t.Phrase && stopwords[strings.ToLower(t.Text)] {
			continue
		}
		ft := ftsTerm(t)
		if ft == "" {
			continue
		}
		alts = append(alts, ft)
		if t.Phrase || t.Prefix {
			continue
		}
		for _, alt := range syn[strings.ToLower(t.Text)] {
			alts = append(alts, ftsTerm(Term{Text: alt}))
		}
	}
	return alts
}

// relax adds documents matching any meaningful query term when the strict
// (all-terms) query found fewer than the limit. BM25 still rewards matching
// more terms; relaxed candidates are discounted below strict ones.
func (s *Searcher) relax(ctx context.Context, req Request, w *where, cands map[int64]*candidate) error {
	expr := relaxedExpr(req.Query, req.Synonyms)
	if expr == "" {
		return nil
	}
	loose := map[int64]*candidate{}
	if _, err := s.lexical(ctx, expr, w, loose); err != nil {
		return err
	}
	for id, c := range loose {
		if _, strict := cands[id]; !strict && c.norm >= relaxFloor {
			c.relaxed = true
			cands[id] = c
		}
	}
	return nil
}
