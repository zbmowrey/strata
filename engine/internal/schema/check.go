// Package schema validates each corpus kind against its compiled schema
// (docs/spec/02-knowledge.md §2, 04-scriptorium.md §3, 05-projects.md §3),
// reporting findings with precise positions.
package schema

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/finding"
)

// IDPattern is the id format shared by shards and scripts.
var IDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Shared frontmatter keys.
const (
	KeyAliases  = "aliases"
	KeyTags     = "tags"
	SideNetwork = "network"
)

// Rule ids.
const (
	RuleSyntax       = "frontmatter/syntax"
	RuleNoFM         = "frontmatter/missing"
	RuleRequired     = "schema/required"
	RuleType         = "schema/type"
	RuleEnum         = "schema/enum"
	RuleDate         = "schema/date"
	RuleDuration     = "schema/duration"
	RuleGlob         = "schema/glob"
	RuleUnknown      = "schema/unknown-key"
	RuleIDFormat     = "id/format"
	RuleIDMismatch   = "id/mismatch"
	RuleLayout       = "layout/path"
	RuleTrustSources = "trust/sources"
	RuleTrustEvid    = "trust/evidence"
	RuleSuperseded   = "status/superseded-by"
	RuleSensitive    = "sensitive/location"
	RuleBodyLines    = "size/body-lines"
	RuleBodyBytes    = "size/body-bytes"
	RuleSummary      = "size/summary"
	RuleTitle        = "size/title"
	RuleSections     = "size/sections"
	RuleLinks        = "size/links"
)

// checker accumulates findings for one file.
type checker struct {
	d    *doc.Doc
	meta *doc.Meta
	file string
	out  []finding.Finding
}

func newChecker(file string, d *doc.Doc, meta *doc.Meta) *checker {
	if meta == nil {
		meta = &doc.Meta{}
	}
	return &checker{file: file, d: d, meta: meta}
}

func (c *checker) add(sev, rule string, line, col int, hint, format string, args ...any) {
	if line == 0 {
		line, col = 1, 1
	}
	c.out = append(c.out, finding.Finding{Rule: rule, Severity: sev, File: c.file, Line: line, Col: col, Message: fmt.Sprintf(format, args...), FixHint: hint})
}

// at reports at key's position (or line 1 when absent).
func (c *checker) at(sev, rule, key, hint, format string, args ...any) {
	l, col := c.meta.Pos(key)
	c.add(sev, rule, l, col, hint, format, args...)
}

func (c *checker) required(keys ...string) {
	for _, k := range keys {
		if v, ok := c.meta.Get(k); !ok || v == nil || doc.Scalar(v) == "" && !isList(v) {
			c.add(finding.Error, RuleRequired, 1, 1, "add `"+k+":` to the frontmatter", "missing required key %q", k)
		}
	}
}

func isList(v any) bool { _, ok := v.([]any); return ok }

// str checks key is a scalar string when present and returns it.
func (c *checker) str(key string) string {
	v, ok := c.meta.Get(key)
	if !ok || v == nil {
		return ""
	}
	switch v.(type) {
	case []any, map[string]any:
		c.at(finding.Error, RuleType, key, "", "%s must be a single value", key)
		return ""
	}
	return doc.Scalar(v)
}

// list checks key is a list of scalars when present and returns it.
func (c *checker) list(key string) []string {
	v, ok := c.meta.Get(key)
	if !ok || v == nil {
		return nil
	}
	items, isL := v.([]any)
	if !isL {
		c.at(finding.Error, RuleType, key, "use a YAML list: "+key+": [a, b]", "%s must be a list", key)
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		switch it.(type) {
		case []any, map[string]any, nil:
			c.at(finding.Error, RuleType, key, "", "%s items must be plain values", key)
			return nil
		}
		out = append(out, doc.Scalar(it))
	}
	return out
}

func (c *checker) enum(key string, allowed ...string) string {
	v := c.str(key)
	if v != "" && !slices.Contains(allowed, v) {
		c.at(finding.Error, RuleEnum, key, "one of: "+strings.Join(allowed, ", "), "%s %q is not allowed", key, v)
	}
	return v
}

// date checks an ISO date (YYYY-MM-DD; a full RFC 3339 timestamp is accepted).
func (c *checker) date(key string) {
	v := c.str(key)
	if v == "" {
		return
	}
	if _, err := time.Parse(time.DateOnly, v); err == nil {
		return
	}
	if _, err := time.Parse(time.RFC3339, v); err == nil {
		return
	}
	c.at(finding.Error, RuleDate, key, "use YYYY-MM-DD", "%s %q is not an ISO date", key, v)
}

func (c *checker) duration(key string) {
	if v := c.str(key); v != "" {
		if _, err := config.ParseDuration(v); err != nil {
			c.at(finding.Error, RuleDuration, key, "e.g. 180d, 2w, never", "%s: %v", key, err)
		}
	}
}

func (c *checker) globs(key string) {
	for _, g := range c.list(key) {
		if !doublestar.ValidatePattern(g) || strings.HasPrefix(g, "/") {
			c.at(finding.Error, RuleGlob, key, "use a relative doublestar glob", "%s: invalid glob %q", key, g)
		}
	}
}

func (c *checker) unknown(known map[string]bool) {
	for _, k := range c.meta.Keys() {
		if !known[k] {
			c.at(finding.Warning, RuleUnknown, k, "", "unknown key %q", k)
		}
	}
}

func (c *checker) idFormat(id, key string) {
	if id == "" {
		return
	}
	if !IDPattern.MatchString(id) || len(id) < 3 || len(id) > 64 {
		c.at(finding.Error, RuleIDFormat, key, "lowercase kebab-case, 3–64 chars", "id %q is not lowercase kebab-case of 3–64 chars", id)
	}
}

func set(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

// frontmatter reports a missing or unparsable frontmatter block. It returns
// false when the remaining schema checks cannot run.
func frontmatter(c *checker, parseErr error, required bool) bool {
	if parseErr != nil {
		line, col := 1, 1
		if pe, ok := parseErr.(*doc.PosError); ok { //nolint:errorlint // doc.Parse returns *PosError unwrapped
			line, col = pe.Line, pe.Col
		}
		c.add(finding.Error, RuleSyntax, line, col, "fix the YAML between the --- lines", "%v", unwrapPos(parseErr))
		return false
	}
	if c.d != nil && c.d.Frontmatter == nil && required {
		c.add(finding.Error, RuleNoFM, 1, 1, "start the file with a --- frontmatter block", "missing frontmatter")
		return false
	}
	return true
}

func unwrapPos(err error) error {
	if pe, ok := err.(*doc.PosError); ok { //nolint:errorlint // see frontmatter
		return pe.Err
	}
	return err
}
