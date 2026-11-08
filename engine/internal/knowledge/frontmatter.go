// Package knowledge implements the knowledge write paths: shard scaffolding,
// capture, rename (mv), supersede, archive, hubs and questions
// (docs/spec/02-knowledge.md). Every write is atomic and content-preserving.
package knowledge

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/schema"
)

var topKeyRE = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*):`)

// Value renders a YAML scalar or flow list for frontmatter.
func Value(v any) string {
	switch x := v.(type) {
	case []string:
		parts := make([]string, len(x))
		for i, s := range x {
			parts[i] = quote(s)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case string:
		return quote(x)
	default:
		return fmt.Sprint(x)
	}
}

// quote returns s, double-quoted only when YAML would misread it.
func quote(s string) string {
	if s == "" || strings.ContainsAny(s, ":#[]{},&*!|>'\"%@`") || strings.TrimSpace(s) != s ||
		slices.Contains([]string{"true", "false", "null", "yes", "no", "~"}, strings.ToLower(s)) {
		return fmt.Sprintf("%q", s)
	}
	return s
}

// SetKey sets a top-level frontmatter key to an already-rendered YAML value,
// replacing the key's whole block (including continuation lines) or
// inserting it in canonical shard-key order. Other bytes are preserved.
func SetKey(raw []byte, key, rendered string) ([]byte, error) {
	d, err := doc.Parse("", raw)
	if err != nil {
		return nil, fmt.Errorf("set %s: %w", key, err)
	}
	if d.Frontmatter == nil {
		return nil, fmt.Errorf("set %s: no frontmatter", key)
	}
	start := bytes.Index(raw, d.Frontmatter)
	lines := strings.SplitAfter(string(d.Frontmatter), "\n")
	line := key + ": " + rendered + "\n"
	from, to := blockOf(lines, key)
	if from >= 0 {
		lines = slices.Replace(lines, from, to, line)
	} else {
		lines = slices.Insert(lines, insertAt(lines, key), line)
	}
	out := raw[:start:start]
	out = append(out, strings.Join(lines, "")...)
	return append(out, raw[start+len(d.Frontmatter):]...), nil
}

// RemoveKey deletes a top-level key's block (no-op when absent).
func RemoveKey(raw []byte, key string) ([]byte, error) {
	d, err := doc.Parse("", raw)
	if err != nil {
		return nil, fmt.Errorf("remove %s: %w", key, err)
	}
	if d.Frontmatter == nil {
		return raw, nil
	}
	start := bytes.Index(raw, d.Frontmatter)
	lines := strings.SplitAfter(string(d.Frontmatter), "\n")
	from, to := blockOf(lines, key)
	if from < 0 {
		return raw, nil
	}
	lines = slices.Delete(lines, from, to)
	out := raw[:start:start]
	out = append(out, strings.Join(lines, "")...)
	return append(out, raw[start+len(d.Frontmatter):]...), nil
}

// blockOf returns the [from, to) line range of key's block, or -1.
func blockOf(lines []string, key string) (from, to int) {
	from = -1
	for i, l := range lines {
		m := topKeyRE.FindStringSubmatch(l)
		switch {
		case len(m) > 1 && m[1] == key:
			from = i
		case from >= 0 && (m != nil || strings.HasPrefix(l, "#")):
			return from, i
		}
	}
	if from >= 0 {
		return from, len(lines)
	}
	return -1, -1
}

// insertAt finds the canonical insertion line for a new key: before the
// first existing key that sorts after it in schema.ShardKeys, else at end.
func insertAt(lines []string, key string) int {
	rank := slices.Index(schema.ShardKeys, key)
	if rank < 0 {
		return len(lines)
	}
	for i, l := range lines {
		if m := topKeyRE.FindStringSubmatch(l); m != nil {
			if r := slices.Index(schema.ShardKeys, m[1]); r > rank {
				return i
			}
		}
	}
	return len(lines)
}
