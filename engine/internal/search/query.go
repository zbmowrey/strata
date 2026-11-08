// Package search implements the query language, ranking and result assembly
// for `strata search` (docs/spec/03-search.md §4–§6).
package search

import (
	"fmt"
	"strings"
	"unicode"
)

// Term is one free-text term.
type Term struct {
	Text   string
	Phrase bool // "quoted phrase"
	Prefix bool // term*
}

// Group is a set of alternatives joined by OR. A negated group excludes
// documents matching any alternative.
type Group struct {
	Alts []Term
	Neg  bool
}

// Filter is an inline filter `key:value` (or `-key:value`).
type Filter struct {
	Key   string
	Value string
	Neg   bool
}

// Query is a parsed search query: groups combine with AND; filters combine with AND
// (repeated kind filters OR together, see Filters()).
type Query struct {
	Groups  []Group
	Filters []Filter
}

// Filter keys.
const (
	fKind     = "kind"
	fType     = "type"
	fTag      = "tag"
	fDomain   = "domain"
	fTopic    = "topic"
	fStatus   = "status"
	fTrust    = "trust"
	fSide     = "side"
	fRuntime  = "runtime"
	fUpdated  = "updated"
	fCreated  = "created"
	fVerified = "verified"
	fStale    = "stale"
	fPath     = "path"
	fProject  = "project"
)

// FilterKeys are the recognized inline filter keys (plus `meta.<key>`).
var FilterKeys = map[string]bool{
	fKind: true, fType: true, fTag: true, fDomain: true, fTopic: true, fStatus: true, fTrust: true,
	fSide: true, fRuntime: true, fUpdated: true, fCreated: true, fVerified: true, fStale: true,
	fPath: true, fProject: true,
}

// ParseError is a query syntax error with a 0-based byte position.
type ParseError struct {
	Query string
	Msg   string
	Pos   int
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("query parse error at %d: %s\n  %s\n  %s^", e.Pos+1, e.Msg, e.Query, strings.Repeat(" ", e.Pos))
}

// token is a lexical unit of the query.
type token struct {
	text   string // unquoted text (for filters: the value)
	key    string // filter key, if any
	pos    int
	neg    bool
	quoted bool
	or     bool // the bare OR operator
}

// Parse parses a query string.
func Parse(s string) (Query, error) {
	toks, err := lex(s)
	if err != nil {
		return Query{}, err
	}
	var q Query
	pendingOr := false
	for i, t := range toks {
		switch {
		case t.or:
			if i == 0 || i == len(toks)-1 || pendingOr || toks[i-1].key != "" || toks[i-1].neg {
				return Query{}, &ParseError{Query: s, Pos: t.pos, Msg: "OR must join two positive terms"}
			}
			pendingOr = true
		case t.key != "":
			if pendingOr {
				return Query{}, &ParseError{Query: s, Pos: t.pos, Msg: "OR cannot join filters"}
			}
			q.Filters = append(q.Filters, Filter{Key: t.key, Value: t.text, Neg: t.neg})
		default:
			term := termOf(t)
			if pendingOr {
				if t.neg {
					return Query{}, &ParseError{Query: s, Pos: t.pos, Msg: "OR cannot join a negated term"}
				}
				last := &q.Groups[len(q.Groups)-1]
				last.Alts = append(last.Alts, term)
				pendingOr = false
				continue
			}
			q.Groups = append(q.Groups, Group{Alts: []Term{term}, Neg: t.neg})
		}
	}
	return q, nil
}

func termOf(t token) Term {
	if t.quoted {
		return Term{Text: t.text, Phrase: true}
	}
	if strings.HasSuffix(t.text, "*") && len(t.text) > 1 {
		return Term{Text: strings.TrimRight(t.text, "*"), Prefix: true}
	}
	return Term{Text: t.text}
}

// lex splits the query into tokens, honoring quotes.
func lex(s string) ([]token, error) {
	var toks []token
	for i := 0; i < len(s); {
		if isSpace(s[i]) {
			i++
			continue
		}
		t, next, err := lexOne(s, i)
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		i = next
	}
	return toks, nil
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// lexOne reads one token starting at i.
func lexOne(s string, i int) (token, int, error) {
	t := token{pos: i}
	if s[i] == '-' && i+1 < len(s) && !isSpace(s[i+1]) {
		t.neg = true
		i++
	}
	if s[i] == '"' {
		text, next, err := quoted(s, i)
		if err != nil {
			return t, 0, err
		}
		t.text, t.quoted = text, true
		return t, next, nil
	}
	end := i
	for end < len(s) && !isSpace(s[end]) && s[end] != '"' {
		end++
	}
	word := s[i:end]
	if key, val, ok := strings.Cut(word, ":"); ok && isFilterKey(key) {
		return lexFilter(s, t, key, val, end)
	}
	if word == "OR" && !t.neg {
		t.or = true
		return t, end, nil
	}
	if word == "" || strings.Trim(word, "*") == "" {
		return t, 0, &ParseError{Query: s, Pos: i, Msg: "expected a term"}
	}
	t.text = word
	return t, end, nil
}

// lexFilter completes a `key:value` token (value possibly quoted).
func lexFilter(s string, t token, key, val string, end int) (token, int, error) {
	t.key = key
	if val != "" {
		t.text = val
		return t, end, nil
	}
	if end < len(s) && s[end] == '"' {
		text, next, err := quoted(s, end)
		if err != nil {
			return t, 0, err
		}
		t.text = text
		return t, next, nil
	}
	return t, 0, &ParseError{Query: s, Pos: end, Msg: "filter " + key + ": needs a value"}
}

func isFilterKey(k string) bool {
	if FilterKeys[k] {
		return true
	}
	rest, ok := strings.CutPrefix(k, "meta.")
	return ok && rest != "" && strings.IndexFunc(rest, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' }) < 0
}

// quoted reads a "…" string starting at s[i]=='"'; `""` inside is not
// special (no escapes): the phrase ends at the next quote.
func quoted(s string, i int) (string, int, error) {
	j := strings.IndexByte(s[i+1:], '"')
	if j < 0 {
		return "", 0, &ParseError{Query: s, Pos: i, Msg: "unterminated quote"}
	}
	text := s[i+1 : i+1+j]
	if strings.TrimSpace(text) == "" {
		return "", 0, &ParseError{Query: s, Pos: i, Msg: "empty phrase"}
	}
	return text, i + j + 2, nil
}

// String renders the query canonically; Parse(q.String()) yields q.
func (q Query) String() string {
	parts := make([]string, 0, len(q.Groups)+len(q.Filters))
	for _, g := range q.Groups {
		alts := make([]string, 0, len(g.Alts))
		for _, a := range g.Alts {
			alts = append(alts, a.String())
		}
		s := strings.Join(alts, " OR ")
		if g.Neg {
			s = "-" + s
		}
		parts = append(parts, s)
	}
	for _, f := range q.Filters {
		s := f.Key + ":" + quoteIfNeeded(f.Value)
		if f.Neg {
			s = "-" + s
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

func (t Term) String() string {
	switch {
	case t.Phrase:
		return `"` + t.Text + `"`
	case t.Prefix:
		return t.Text + "*"
	default:
		return t.Text
	}
}

func quoteIfNeeded(v string) string {
	if strings.ContainsAny(v, " \t\r\n") {
		return `"` + v + `"`
	}
	return v
}

// Text returns the free-text part of the query (used for the exact-id
// short-circuit and suggestions).
func (q Query) Text() string {
	var parts []string
	for _, g := range q.Groups {
		if !g.Neg && len(g.Alts) == 1 {
			parts = append(parts, g.Alts[0].Text)
		}
	}
	return strings.Join(parts, " ")
}

// Positive returns the non-negated groups.
func (q Query) Positive() []Group {
	var out []Group
	for _, g := range q.Groups {
		if !g.Neg {
			out = append(out, g)
		}
	}
	return out
}
