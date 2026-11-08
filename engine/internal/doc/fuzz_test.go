package doc

import (
	"strings"
	"testing"
)

func FuzzParse(f *testing.F) {
	for _, s := range []string{sample, "", "---\n---\n", "---\nid: [\n---\n", "[[a#b|c]]", "```\n[[x]]\n", "# h\n## i\n### j", "---\na: &x 1\nb: *x\n---\n"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		d, _ := Parse("f.md", raw)
		if d.BodyStart < 0 || d.BodyStart > len(raw) {
			t.Fatalf("body start %d", d.BodyStart)
		}
		for _, h := range d.Headings {
			if h.Start > h.End || h.End > len(raw) || h.Level < 1 || h.Level > 6 {
				t.Fatalf("bad heading %+v", h)
			}
		}
		for _, l := range d.Links {
			if l.Offset < 0 || l.Offset >= len(raw) || !strings.HasPrefix(string(raw[l.Offset:]), "[[") {
				t.Fatalf("bad link %+v", l)
			}
		}
		_ = d.FirstParagraph()
		_, _ = d.Section("x")
	})
}
