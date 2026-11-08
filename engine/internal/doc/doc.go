// Package doc scans markdown documents: YAML frontmatter, ATX headings with
// Obsidian-compatible slugs, wikilinks, code fences, and size counts. It is a
// purpose-built scanner, not a CommonMark parser (docs/spec/01-architecture.md §12).
package doc

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// Heading is an ATX heading outside code fences.
type Heading struct {
	Text  string
	Slug  string
	Line  int // 1-based line in the file
	Start int // byte offset of the heading line in the file
	End   int // byte offset where the section ends (next heading of same or higher level, or EOF)
	Level int
}

// Link is a wikilink `[[target#anchor|label]]` (or an embed `![[...]]`).
type Link struct {
	Target string // stem of the target (path and .md stripped); empty for a self-link
	Anchor string
	Label  string
	Raw    string // the full `[[...]]` text
	Line   int
	Col    int // 1-based byte column of `[[`
	Offset int // byte offset of `[[` in the file
	Embed  bool
}

// Doc is a scanned markdown document.
type Doc struct {
	Path        string
	Raw         []byte
	Frontmatter []byte // YAML between the delimiters (nil when absent)
	Meta        *Meta  // decoded frontmatter (empty when absent)
	Headings    []Heading
	Links       []Link
	BodyStart   int // byte offset of the body
	BodyLine    int // 1-based line where the body starts
	BodyBytes   int
	BodyLines   float64 // non-blank lines; lines inside code fences count 0.5
	CodeFences  int
}

// ErrFrontmatter is wrapped by frontmatter syntax errors.
var ErrFrontmatter = errors.New("invalid frontmatter")

// PosError is an error with a 1-based line and column in the file.
type PosError struct {
	Err  error
	Line int
	Col  int
}

func (e *PosError) Error() string { return fmt.Sprintf("%d:%d: %v", e.Line, e.Col, e.Err) }

// Unwrap returns the underlying error.
func (e *PosError) Unwrap() error { return e.Err }

// Parse scans raw. Frontmatter YAML errors are returned as *PosError
// wrapping ErrFrontmatter, together with a Doc whose body fields are still
// populated (so lint can keep reporting other problems).
func Parse(path string, raw []byte) (*Doc, error) {
	d := &Doc{Path: path, Raw: raw, Meta: &Meta{}}
	fm, bodyStart, bodyLine, ok := splitFrontmatter(raw)
	d.BodyStart, d.BodyLine = bodyStart, bodyLine
	var fmErr error
	if ok {
		d.Frontmatter = fm
		m, err := decodeMeta(fm, 1) // frontmatter content begins on line 2
		if err != nil {
			fmErr = err
		} else {
			d.Meta = m
		}
	}
	d.scanBody()
	return d, fmErr
}

// Body returns the body bytes (everything after the frontmatter).
func (d *Doc) Body() []byte { return d.Raw[d.BodyStart:] }

// splitFrontmatter finds a leading `---` block. It returns the YAML bytes,
// the body offset, the body's 1-based line, and whether a block was found.
// An unterminated block is treated as no frontmatter.
func splitFrontmatter(raw []byte) (fm []byte, bodyStart, bodyLine int, ok bool) {
	start := 0
	if bytes.HasPrefix(raw, []byte("\xef\xbb\xbf")) {
		start = 3
	}
	first, next := lineAt(raw, start)
	if strings.TrimRight(string(first), " \t\r") != "---" {
		return nil, 0, 1, false
	}
	line := 2
	for pos := next; pos < len(raw); line++ {
		l, n := lineAt(raw, pos)
		t := strings.TrimRight(string(l), " \t\r")
		if t == "---" || t == "..." {
			return raw[next:pos], n, line + 1, true
		}
		pos = n
	}
	return nil, 0, 1, false
}

// lineAt returns the line starting at pos (without its newline) and the
// offset of the following line.
func lineAt(raw []byte, pos int) (line []byte, next int) {
	i := bytes.IndexByte(raw[pos:], '\n')
	if i < 0 {
		return raw[pos:], len(raw)
	}
	return raw[pos : pos+i], pos + i + 1
}

// fence tracks an open code fence.
type fence struct {
	char byte
	n    int
}

// fenceOpen reports whether line opens a fence (``` or ~~~, ≤ 3 spaces indent).
func fenceOpen(line string) (fence, bool) {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 || len(t) < 3 {
		return fence{}, false
	}
	c := t[0]
	if c != '`' && c != '~' {
		return fence{}, false
	}
	n := 0
	for n < len(t) && t[n] == c {
		n++
	}
	if n < 3 || (c == '`' && strings.IndexByte(t[n:], '`') >= 0) {
		return fence{}, false
	}
	return fence{char: c, n: n}, true
}

// closes reports whether line closes f.
func (f fence) closes(line string) bool {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 {
		return false
	}
	n := 0
	for n < len(t) && t[n] == f.char {
		n++
	}
	return n >= f.n && strings.TrimSpace(t[n:]) == ""
}

// scanBody walks body lines collecting headings, links and counts.
func (d *Doc) scanBody() {
	raw := d.Raw
	d.BodyBytes = len(raw) - d.BodyStart
	var open *fence
	line := d.BodyLine
	for pos := d.BodyStart; pos < len(raw); line++ {
		l, next := lineAt(raw, pos)
		s := strings.TrimRight(string(l), "\r")
		switch {
		case open != nil:
			d.BodyLines += 0.5
			if open.closes(s) {
				open = nil
			}
		default:
			if f, ok := fenceOpen(s); ok {
				open = &f
				d.CodeFences++
				d.BodyLines += 0.5
				break
			}
			if strings.TrimSpace(s) != "" {
				d.BodyLines++
			}
			if h, ok := parseHeading(s); ok {
				h.Line, h.Start = line, pos
				d.Headings = append(d.Headings, h)
			}
			d.Links = append(d.Links, scanLinks(s, line, pos)...)
		}
		pos = next
	}
	d.closeSections()
}

// closeSections sets each heading's End offset.
func (d *Doc) closeSections() {
	for i := range d.Headings {
		d.Headings[i].End = len(d.Raw)
		for j := i + 1; j < len(d.Headings); j++ {
			if d.Headings[j].Level <= d.Headings[i].Level {
				d.Headings[i].End = d.Headings[j].Start
				break
			}
		}
	}
}

// parseHeading parses an ATX heading line.
func parseHeading(s string) (Heading, bool) {
	t := strings.TrimLeft(s, " ")
	if len(s)-len(t) > 3 {
		return Heading{}, false
	}
	n := 0
	for n < len(t) && n < 7 && t[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || (n < len(t) && t[n] != ' ' && t[n] != '\t') {
		return Heading{}, false
	}
	text := strings.TrimSpace(t[n:])
	// Strip a closing sequence of #s preceded by a space.
	if trimmed := strings.TrimRight(text, "#"); trimmed != text && (trimmed == "" || strings.HasSuffix(trimmed, " ")) {
		text = strings.TrimSpace(trimmed)
	}
	return Heading{Level: n, Text: text, Slug: Slug(text)}, true
}

// Slug converts heading text to an anchor slug: lowercase, runs of
// non-alphanumerics collapsed to '-', trimmed. `[[id#Some Heading]]` and
// `id#some-heading` resolve to the same section.
func Slug(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	dash := false
	for _, r := range strings.ToLower(text) {
		alnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127
		if alnum {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	return b.String()
}

// scanLinks finds wikilinks in one line, skipping inline code spans.
func scanLinks(s string, line, lineOff int) []Link {
	if !strings.Contains(s, "[[") {
		return nil
	}
	var out []Link
	for i := 0; i < len(s); {
		if s[i] == '`' {
			i = skipCodeSpan(s, i)
			continue
		}
		if !strings.HasPrefix(s[i:], "[[") {
			i++
			continue
		}
		end := strings.Index(s[i+2:], "]]")
		if end < 0 {
			break
		}
		inner := s[i+2 : i+2+end]
		if strings.Contains(inner, "[[") || strings.TrimSpace(inner) == "" {
			i += 2
			continue
		}
		l := parseLinkInner(inner)
		l.Embed = i > 0 && s[i-1] == '!'
		l.Raw, l.Line, l.Col, l.Offset = s[i:i+4+end], line, i+1, lineOff+i
		out = append(out, l)
		i += 4 + end
	}
	return out
}

// skipCodeSpan returns the index after the code span starting at s[i] (a
// backtick run), or i+run when it is unterminated.
func skipCodeSpan(s string, i int) int {
	n := 0
	for i+n < len(s) && s[i+n] == '`' {
		n++
	}
	closer := strings.Repeat("`", n)
	j := strings.Index(s[i+n:], closer)
	if j < 0 {
		return i + n
	}
	return i + n + j + n
}

// parseLinkInner splits `target#anchor|label`.
func parseLinkInner(inner string) Link {
	var l Link
	target, label, hasLabel := strings.Cut(inner, "|")
	if hasLabel {
		l.Label = strings.TrimSpace(label)
	}
	target, anchor, _ := strings.Cut(target, "#")
	l.Anchor = strings.TrimSpace(anchor)
	target = strings.TrimSpace(target)
	if i := strings.LastIndexByte(target, '/'); i >= 0 {
		target = target[i+1:]
	}
	l.Target = strings.TrimSuffix(target, ".md")
	return l
}

// Section returns the heading whose slug equals Slug(anchor), if any.
func (d *Doc) Section(anchor string) (Heading, bool) {
	want := Slug(anchor)
	for _, h := range d.Headings {
		if h.Slug == want {
			return h, true
		}
	}
	return Heading{}, false
}

// FirstParagraph returns the first paragraph of the body that is not a
// heading or code fence, joined into one line.
func (d *Doc) FirstParagraph() string {
	var parts []string
	var open *fence
	for pos := d.BodyStart; pos < len(d.Raw); {
		l, next := lineAt(d.Raw, pos)
		pos = next
		s := strings.TrimSpace(strings.TrimRight(string(l), "\r"))
		if open != nil {
			if open.closes(s) {
				open = nil
			}
			continue
		}
		if f, ok := fenceOpen(s); ok {
			if len(parts) > 0 {
				break
			}
			open = &f
			continue
		}
		_, isHeading := parseHeading(s)
		if s == "" || isHeading || strings.HasPrefix(s, "<!--") {
			if len(parts) > 0 {
				break
			}
			continue
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

// LineCol converts a byte offset in Raw to a 1-based line and column.
func (d *Doc) LineCol(off int) (line, col int) {
	if off > len(d.Raw) {
		off = len(d.Raw)
	}
	line = 1 + bytes.Count(d.Raw[:off], []byte("\n"))
	col = off - bytes.LastIndexByte(d.Raw[:off], '\n')
	return line, col
}
