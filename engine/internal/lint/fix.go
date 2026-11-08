package lint

import (
	"bytes"
	"regexp"
	"slices"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/corpus"
	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/finding"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/schema"
)

// applyFixes rewrites selected markdown files whose normalized form differs.
// Fixes are content-preserving: alias links → ids, canonical frontmatter key
// order, lowercase de-duplicated tags, trailing whitespace, final newline.
func applyFixes(items []*item, _ []finding.Finding, _ config.Config, opt Options) ([]string, error) {
	tbl := buildTable(items)
	var fixed []string
	for _, it := range items {
		if !selected(it.file, opt) || it.file.Kind == corpus.Script || it.doc == nil || it.doc.Raw == nil || it.err != nil {
			continue
		}
		out := RewriteLinks(it.doc, func(target string) (string, bool) {
			if _, ok := tbl.target(target); ok {
				return "", false
			}
			id, ok := tbl.aliases[target]
			return id, ok
		})
		if it.file.Kind == corpus.Shard {
			out = NormalizeFrontmatter(out, schema.ShardKeys)
		}
		out = trimTrailing(out)
		if bytes.Equal(out, it.doc.Raw) {
			continue
		}
		if err := fsutil.WriteAtomic(it.file.Abs, out, 0o644); err != nil {
			return fixed, err
		}
		fixed = append(fixed, it.file.Rel)
	}
	return fixed, nil
}

// NormalizeFile applies the per-file content-preserving fixes (canonical
// shard frontmatter, trailing whitespace, final newline) without the
// corpus-wide link table — the incremental path of `garden --auto`.
func NormalizeFile(raw []byte, shard bool) []byte {
	if shard {
		raw = NormalizeFrontmatter(raw, schema.ShardKeys)
	}
	return trimTrailing(raw)
}

// RewriteLinks replaces link targets for which resolve returns a new id,
// keeping anchors and labels. It returns the new file content.
func RewriteLinks(d *doc.Doc, resolve func(target string) (string, bool)) []byte {
	var b bytes.Buffer
	last := 0
	for _, l := range d.Links {
		id, ok := resolve(l.Target)
		if !ok || l.Target == "" {
			continue
		}
		inner := id
		if l.Anchor != "" {
			inner += "#" + l.Anchor
		}
		if l.Label != "" {
			inner += "|" + l.Label
		}
		b.Write(d.Raw[last:l.Offset])
		b.WriteString("[[" + inner + "]]")
		last = l.Offset + len(l.Raw)
	}
	if last == 0 {
		return d.Raw
	}
	b.Write(d.Raw[last:])
	return b.Bytes()
}

var (
	topKey   = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*):`)
	flowTags = regexp.MustCompile(`^(tags:\s*)\[(.*)\](\s*(#.*)?)$`)
)

// NormalizeFrontmatter reorders top-level frontmatter keys into order
// (unknown keys keep their relative order after the known ones) and
// lowercases/de-duplicates a single-line flow `tags:` list. Comments and
// values are preserved verbatim. Frontmatter it cannot split cleanly is
// returned unchanged.
func NormalizeFrontmatter(raw []byte, order []string) []byte {
	d, err := doc.Parse("", raw)
	if err != nil || d.Frontmatter == nil || d.Meta.Len() == 0 {
		return raw
	}
	fmStart := bytes.Index(raw, d.Frontmatter)
	blocks, ok := splitBlocks(string(d.Frontmatter))
	if !ok || len(blocks) != d.Meta.Len() {
		return raw
	}
	rank := func(key string) int {
		if i := slices.Index(order, key); i >= 0 {
			return i
		}
		return len(order)
	}
	slices.SortStableFunc(blocks, func(a, b block) int { return rank(a.key) - rank(b.key) })
	var b strings.Builder
	for _, blk := range blocks {
		text := blk.text
		if blk.key == "tags" {
			text = normalizeTags(text)
		}
		b.WriteString(text)
	}
	out := make([]byte, 0, len(raw))
	out = append(out, raw[:fmStart]...)
	out = append(out, b.String()...)
	return append(out, raw[fmStart+len(d.Frontmatter):]...)
}

type block struct{ key, text string }

// splitBlocks splits frontmatter text into top-level key blocks. Leading
// comment lines attach to the following key; anything before the first key
// other than comments/blank lines makes the split fail.
func splitBlocks(fm string) ([]block, bool) {
	lines := strings.SplitAfter(fm, "\n")
	var blocks []block
	pending := ""
	for _, l := range lines {
		if l == "" {
			continue
		}
		if m := topKey.FindStringSubmatch(l); m != nil {
			blocks = append(blocks, block{key: m[1], text: pending + l})
			pending = ""
			continue
		}
		trimmed := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "#"):
			pending += l
		case len(blocks) == 0 && trimmed != "":
			return nil, false
		case len(blocks) == 0:
			pending += l
		default:
			blocks[len(blocks)-1].text += pending + l
			pending = ""
		}
	}
	if pending != "" {
		if len(blocks) == 0 {
			return nil, false
		}
		blocks[len(blocks)-1].text += pending
	}
	for i := range blocks {
		if !strings.HasSuffix(blocks[i].text, "\n") {
			blocks[i].text += "\n"
		}
	}
	return blocks, true
}

func normalizeTags(text string) string {
	line := strings.TrimRight(text, "\n")
	if strings.Contains(line, "\n") {
		return text
	}
	m := flowTags.FindStringSubmatch(line)
	if len(m) < 4 || strings.ContainsAny(m[2], "[]{}\"'") {
		return text
	}
	var tags []string
	for _, t := range strings.Split(m[2], ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	return m[1] + "[" + strings.Join(tags, ", ") + "]" + m[3] + "\n"
}

// trimTrailing removes trailing spaces/tabs on every line (outside nothing:
// markdown hard breaks via two spaces are not used in shards) and ensures a
// single final newline.
func trimTrailing(raw []byte) []byte {
	lines := bytes.Split(raw, []byte("\n"))
	for i, l := range lines {
		lines[i] = bytes.TrimRight(l, " \t")
	}
	out := bytes.Join(lines, []byte("\n"))
	out = bytes.TrimRight(out, "\n")
	if len(out) == 0 {
		return out
	}
	return append(out, '\n')
}
