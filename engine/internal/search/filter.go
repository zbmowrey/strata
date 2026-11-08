package search

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/zbmowrey/strata/engine/internal/corpus"
	"github.com/zbmowrey/strata/engine/internal/fail"
)

// Ordered vocabularies for >=/<= filters.
var (
	trustOrder = []string{"inferred", "observed", fVerified, "user-stated"}
	sideOrder  = []string{"read-only", "writes-local", "network", "destructive"}
)

// where is a SQL predicate over the docs table aliased `d`.
type where struct {
	sql  []string
	args []any
	// pathGlobs are applied precisely in Go after the SQL prefilter.
	pathGlobs []string
	// statusSet reports whether a status filter was given (disables the
	// default archived exclusion).
	statusSet bool
}

func (w *where) add(sql string, args ...any) {
	w.sql = append(w.sql, sql)
	w.args = append(w.args, args...)
}

// SQL returns the predicate (always valid; "1" when empty).
func (w *where) SQL() string {
	if len(w.sql) == 0 {
		return "1"
	}
	return strings.Join(w.sql, " AND ")
}

// Scope holds the default-exclusion switches.
type Scope struct {
	Today    time.Time
	Private  bool
	Archived bool
}

// buildWhere converts filters into a predicate. Repeated kind/type/status
// filters OR together; tags AND together.
func buildWhere(fs []Filter, sc Scope) (*where, error) {
	w := &where{}
	multi := map[string][]string{}
	for _, f := range fs {
		if err := w.filter(f, sc, multi); err != nil {
			return nil, err
		}
	}
	for _, key := range []string{fKind, fType, fStatus, fRuntime, fDomain, fTopic} {
		vals := multi[key]
		if len(vals) == 0 {
			continue
		}
		col := map[string]string{fKind: "d.kind", "type": "d.type", "status": "d.status", "runtime": "d.runtime", "domain": "d.domain", "topic": "d.topic"}[key]
		w.add(col+" IN ("+placeholders(len(vals))+")", anys(vals)...)
	}
	if !sc.Private {
		w.add("d.private = 0")
	}
	if !sc.Archived && !w.statusSet {
		w.add("d.status != 'archived'")
	}
	return w, nil
}

func usage(f Filter, format string, args ...any) error {
	return fail.New(fail.Usage, "see `strata search --help` for filter syntax", "filter %s:%s: %s", f.Key, f.Value, fmt.Sprintf(format, args...))
}

// filter adds one filter's predicate.
func (w *where) filter(f Filter, sc Scope, multi map[string][]string) error {
	if strings.HasPrefix(f.Key, "meta.") {
		w.meta(f)
		return nil
	}
	if f.Neg && f.Key != fTag {
		return usage(f, "only tag filters can be negated")
	}
	switch f.Key {
	case fKind, fStatus, fType, fRuntime, fDomain, fTopic:
		return w.multiValue(f, multi)
	case fTag:
		w.tag(f)
	case fTrust:
		return w.ordered(f, "d.trust", trustOrder, ">=")
	case fSide:
		return w.ordered(f, "d.side_effect", sideOrder, "<=")
	case fUpdated, fCreated, fVerified:
		return w.date(f, sc.Today)
	case fStale:
		return w.stale(f, sc.Today)
	case fPath:
		return w.path(f)
	case fProject:
		w.add("d.kind = 'project' AND d.domain = ?", f.Value)
	}
	return nil
}

// multiValue collects filters whose repeated values OR together.
func (w *where) multiValue(f Filter, multi map[string][]string) error {
	switch f.Key {
	case fKind:
		if !slices.Contains(kindNames(), f.Value) {
			return usage(f, "unknown kind (one of %s)", strings.Join(kindNames(), ", "))
		}
	case fStatus:
		w.statusSet = true
		if f.Value == "any" {
			return nil
		}
	}
	multi[f.Key] = append(multi[f.Key], f.Value)
	return nil
}

func kindNames() []string {
	out := make([]string, 0, len(corpus.Kinds))
	for _, k := range corpus.Kinds {
		out = append(out, string(k))
	}
	return out
}

func (w *where) tag(f Filter) {
	q := "EXISTS (SELECT 1 FROM tags t WHERE t.doc = d.rowid AND t.tag = ?)"
	if f.Neg {
		q = "NOT " + q
	}
	w.add(q, strings.ToLower(f.Value))
}

func (w *where) meta(f Filter) {
	path := "$." + strings.TrimPrefix(f.Key, "meta.")
	q := "(json_extract(d.meta_json, ?) = ? OR EXISTS (SELECT 1 FROM json_each(d.meta_json, ?) WHERE value = ?))"
	if f.Neg {
		q = "NOT " + q
	}
	w.add(q, path, f.Value, path, f.Value)
}

// ordered handles trust:>=x / side:<=x (and exact values).
func (w *where) ordered(f Filter, col string, order []string, op string) error {
	v := strings.TrimPrefix(f.Value, op)
	i := slices.Index(order, v)
	if i < 0 {
		return usage(f, "unknown value (one of %s)", strings.Join(order, ", "))
	}
	if v == f.Value {
		w.add(col+" = ?", v)
		return nil
	}
	var allowed []string
	if op == ">=" {
		allowed = order[i:]
	} else {
		allowed = order[:i+1]
	}
	w.add(col+" IN ("+placeholders(len(allowed))+")", anys(allowed)...)
	return nil
}

// date handles >DATE, <DATE, DATE..DATE, DATE, >30d (newer than), <30d (older than).
func (w *where) date(f Filter, today time.Time) error {
	col := map[string]string{fUpdated: "d.updated", fCreated: "d.created", fVerified: "d.verified_at"}[f.Key]
	expr := "substr(" + col + ", 1, 10)"
	v := f.Value
	if lo, hi, ok := strings.Cut(v, ".."); ok {
		a, err1 := parseDate(lo, today)
		b, err2 := parseDate(hi, today)
		if err1 != nil || err2 != nil {
			return usage(f, "want DATE..DATE")
		}
		w.add(expr+" BETWEEN ? AND ?", a, b)
		return nil
	}
	op := "="
	for _, p := range []string{">=", "<=", ">", "<"} {
		if strings.HasPrefix(v, p) {
			op, v = p, strings.TrimPrefix(v, p)
			break
		}
	}
	d, err := parseDate(v, today)
	if err != nil {
		return usage(f, "want YYYY-MM-DD or a relative age like 30d")
	}
	if isRelative(v) { // ">30d" means newer than 30 days ago
		op = map[string]string{">": ">=", ">=": ">=", "<": "<", "<=": "<", "=": ">="}[op]
	}
	w.add(col+" != '' AND "+expr+" "+op+" ?", d)
	return nil
}

func isRelative(v string) bool { return strings.HasSuffix(v, "d") || strings.HasSuffix(v, "w") }

func parseDate(v string, today time.Time) (string, error) {
	if isRelative(v) {
		n, err := strconv.Atoi(strings.TrimRight(v, "dw"))
		if err != nil || n < 0 {
			return "", fmt.Errorf("bad age %q", v)
		}
		if strings.HasSuffix(v, "w") {
			n *= 7
		}
		return today.AddDate(0, 0, -n).Format(time.DateOnly), nil
	}
	t, err := time.Parse(time.DateOnly, v)
	if err != nil {
		return "", fmt.Errorf("bad date %q: %w", v, err)
	}
	return t.Format(time.DateOnly), nil
}

// StaleSQL is the staleness predicate (docs/spec/02-knowledge.md §7).
const StaleSQL = "(d.review_after_days > 0 AND julianday(?) > julianday(substr(coalesce(nullif(d.verified_at, ''), d.updated), 1, 10)) + d.review_after_days)"

func (w *where) stale(f Filter, today time.Time) error {
	switch f.Value {
	case "true":
		w.add(StaleSQL, today.Format(time.DateOnly))
	case "false":
		w.add("NOT "+StaleSQL, today.Format(time.DateOnly))
	default:
		return usage(f, "want true or false")
	}
	return nil
}

func (w *where) path(f Filter) error {
	if !doublestar.ValidatePattern(f.Value) {
		return usage(f, "invalid glob")
	}
	like := strings.NewReplacer("**", "*").Replace(f.Value)
	w.add("d.path GLOB ?", like)
	w.pathGlobs = append(w.pathGlobs, f.Value)
	return nil
}

// matchPath applies path globs precisely.
func (w *where) matchPath(p string) bool {
	for _, g := range w.pathGlobs {
		ok, _ := doublestar.Match(g, strings.TrimSuffix(p, "/"))
		if !ok {
			ok, _ = doublestar.Match(g, p)
		}
		if !ok {
			return false
		}
	}
	return true
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func anys(vals []string) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = v
	}
	return out
}
