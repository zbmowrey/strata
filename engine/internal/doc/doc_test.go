package doc

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const sample = `---
id: pytest-fixture-teardown
title: DB fixtures need explicit teardown
tags: [testing, pytest]
created: 2026-11-07
count: 3
ok: true
ratio: 0.5
neg: -2
empty:
---
Session fixtures leak connections
unless torn down.

## Symptom
Tests hang. See [[debug-hanging-tests]] and ![[diagram.png]].

` + "```python" + `
# not a heading [[not-a-link]]
x = 1
` + "```" + `

## Fix ##
Use [[pytest-fixture-teardown#Symptom|the symptom]] and [[folder/other.md]] but not ` + "`[[code]]`" + `.
### Deep
[[#Fix]]
`

func TestParseSample(t *testing.T) {
	t.Parallel()
	d, err := Parse("k.md", []byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Meta.String("id"); got != "pytest-fixture-teardown" {
		t.Errorf("id = %q", got)
	}
	if got := d.Meta.Strings("tags"); !cmp.Equal(got, []string{"testing", "pytest"}) {
		t.Errorf("tags = %v", got)
	}
	for k, want := range map[string]string{"created": "2026-11-07", "count": "3", "ok": "true", "ratio": "0.5", "neg": "-2", "empty": "", "missing": ""} {
		if got := d.Meta.String(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if l, c := d.Meta.Pos("title"); l != 3 || c != 1 {
		t.Errorf("title pos = %d:%d", l, c)
	}
	if d.BodyLine != 12 || !strings.HasPrefix(string(d.Body()), "Session") {
		t.Errorf("body line %d body %q", d.BodyLine, d.Body()[:10])
	}
	var heads []string
	for _, h := range d.Headings {
		heads = append(heads, h.Slug)
	}
	if !cmp.Equal(heads, []string{"symptom", "fix", "deep"}) {
		t.Errorf("headings = %v", heads)
	}
	var targets []string
	for _, l := range d.Links {
		targets = append(targets, l.Target+"#"+l.Anchor+"|"+l.Label)
	}
	want := []string{"debug-hanging-tests#|", "diagram.png#|", "pytest-fixture-teardown#Symptom|the symptom", "other#|", "#Fix|"}
	if diff := cmp.Diff(want, targets); diff != "" {
		t.Errorf("links (-want +got):\n%s", diff)
	}
	if !d.Links[1].Embed || d.Links[0].Embed {
		t.Error("embed detection")
	}
	for _, l := range d.Links {
		if !strings.HasPrefix(string(d.Raw[l.Offset:]), "[[") {
			t.Errorf("offset of %s wrong", l.Raw)
		}
		if line, col := d.LineCol(l.Offset); line != l.Line || col != l.Col {
			t.Errorf("%s at %d:%d, LineCol %d:%d", l.Raw, l.Line, l.Col, line, col)
		}
	}
	if d.CodeFences != 1 {
		t.Errorf("fences = %d", d.CodeFences)
	}
	// 2 para + 2 symptom + 4 fence lines*0.5 + 2 fix + 2 deep
	if d.BodyLines != 10 {
		t.Errorf("body lines = %v", d.BodyLines)
	}
	if got := d.FirstParagraph(); got != "Session fixtures leak connections unless torn down." {
		t.Errorf("first paragraph = %q", got)
	}
	sec, ok := d.Section("Fix")
	if !ok || !strings.HasPrefix(string(d.Raw[sec.Start:sec.End]), "## Fix ##") || !strings.Contains(string(d.Raw[sec.Start:sec.End]), "### Deep") {
		t.Errorf("section fix = %q", d.Raw[sec.Start:sec.End])
	}
	sym, _ := d.Section("symptom")
	if strings.Contains(string(d.Raw[sym.Start:sym.End]), "## Fix") {
		t.Error("symptom section overruns")
	}
	if _, ok := d.Section("nope"); ok {
		t.Error("found missing section")
	}
}

func TestSlug(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"How to apply":        "how-to-apply",
		"  Why?  ":            "why",
		"C++ & Go (2026)":     "c-go-2026",
		"Ünïcode Heading":     "ünïcode-heading",
		"---":                 "",
		"already-slugged-123": "already-slugged-123",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHeadingEdgeCases(t *testing.T) {
	t.Parallel()
	cases := map[string]int{
		"# a":        1,
		"####### no": 0,
		"#no":        0,
		"    # code": 0,
		"   ### ok":  3,
		"## ":        2,
		"#":          1,
	}
	for in, lvl := range cases {
		h, ok := parseHeading(in)
		if (lvl == 0) == ok || (ok && h.Level != lvl) {
			t.Errorf("parseHeading(%q) = %+v %v", in, h, ok)
		}
	}
}

func TestFrontmatterVariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, src string
		wantFM    bool
		wantErr   bool
		bodyLine  int
	}{
		{"none", "# hi\n", false, false, 1},
		{"bom", "\xef\xbb\xbf---\nid: x\n---\nbody\n", true, false, 4},
		{"dots", "---\nid: x\n...\nbody\n", true, false, 4},
		{"unterminated", "---\nid: x\nbody\n", false, false, 1},
		{"empty", "---\n---\nbody", true, false, 3},
		{"comment only", "---\n# c\n---\n", true, false, 4},
		{"crlf", "---\r\nid: x\r\n---\r\nbody\r\n", true, false, 4},
		{"syntax", "---\nid: [\n---\n", true, true, 4},
		{"not mapping", "---\n- a\n- b\n---\n", true, true, 5},
		{"scalar", "---\nhello\n---\n", true, true, 4},
		{"dup key", "---\na: 1\na: 2\n---\n", true, true, 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, err := Parse("x.md", []byte(tc.src))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if err != nil {
				var pe *PosError
				if !errors.As(err, &pe) || !errors.Is(err, ErrFrontmatter) || pe.Line < 2 {
					t.Fatalf("error not positioned: %#v", err)
				}
				if !strings.Contains(pe.Error(), ":") || pe.Unwrap() == nil {
					t.Fatal("PosError formatting")
				}
			}
			if (d.Frontmatter != nil) != tc.wantFM || d.BodyLine != tc.bodyLine {
				t.Fatalf("fm=%v bodyLine=%d", d.Frontmatter != nil, d.BodyLine)
			}
		})
	}
}

func TestLinksEdgeCases(t *testing.T) {
	t.Parallel()
	src := "a [[ ]] b [[x [[y]] c [[unterminated `[[in code]]` ``[[x`` [[ok|label]] [[z]]\n"
	d, _ := Parse("x.md", []byte(src))
	var got []string
	for _, l := range d.Links {
		got = append(got, l.Target)
	}
	if !cmp.Equal(got, []string{"y", "ok", "z"}) {
		t.Errorf("links = %v", got)
	}
}

func TestFenceRules(t *testing.T) {
	t.Parallel()
	src := "````\n```\n## not heading\n````\n~~~ go\n[[no]]\n~~~~\n``` x ` y\n## heading\n    ```\n## also heading\n"
	d, _ := Parse("x.md", []byte(src))
	if len(d.Links) != 0 {
		t.Errorf("links in fences: %v", d.Links)
	}
	if len(d.Headings) != 2 {
		t.Errorf("headings = %+v", d.Headings)
	}
}

func TestFirstParagraphSkipsFenceAndComment(t *testing.T) {
	t.Parallel()
	d, _ := Parse("x.md", []byte("<!-- c -->\n```\ncode\n```\n# H\n\nfirst line\nsecond\n\nnext"))
	if got := d.FirstParagraph(); got != "first line second" {
		t.Errorf("got %q", got)
	}
	d, _ = Parse("x.md", []byte("para\n```\ncode\n```\n"))
	if got := d.FirstParagraph(); got != "para" {
		t.Errorf("got %q", got)
	}
}

func TestMetaHelpers(t *testing.T) {
	t.Parallel()
	m, err := ParseYAML([]byte("b: 1\na: [x, 2, true]\nc: s\nd: {k: v}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cmp.Equal(m.Keys(), []string{"b", "a", "c", "d"}) || !cmp.Equal(m.SortedKeys(), []string{"a", "b", "c", "d"}) || m.Len() != 4 {
		t.Error("key order")
	}
	if !cmp.Equal(m.Strings("a"), []string{"x", "2", "true"}) || !cmp.Equal(m.Strings("c"), []string{"s"}) || m.Strings("zz") != nil {
		t.Error("Strings")
	}
	if !m.Has("d") || m.Has("zz") || len(m.Map()) != 4 {
		t.Error("Has/Map")
	}
	if v, ok := m.Get("d"); !ok || Scalar(v) == "" {
		t.Error("Get")
	}
	if Scalar(int64(-1)) != "-1" || Scalar(1) != "1" || Scalar(nil) != "" {
		t.Error("Scalar")
	}
	if empty, err := ParseYAML(nil); err != nil || empty.Len() != 0 {
		t.Error("empty yaml")
	}
}

func TestLineColClamp(t *testing.T) {
	t.Parallel()
	d, _ := Parse("x", []byte("a\nbc"))
	if l, c := d.LineCol(100); l != 2 || c != 3 {
		t.Errorf("%d:%d", l, c)
	}
}
