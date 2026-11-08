package search

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want Query
	}{
		{"fixture teardown", Query{Groups: []Group{{Alts: []Term{{Text: "fixture"}}}, {Alts: []Term{{Text: "teardown"}}}}}},
		{`"connection leak" OR hang  domain:testing  trust:>=verified`, Query{
			Groups:  []Group{{Alts: []Term{{Text: "connection leak", Phrase: true}, {Text: "hang"}}}},
			Filters: []Filter{{Key: "domain", Value: "testing"}, {Key: "trust", Value: ">=verified"}},
		}},
		{"deploy* -legacy -tag:old tag:\"two words\" meta.owner:me", Query{
			Groups:  []Group{{Alts: []Term{{Text: "deploy", Prefix: true}}}, {Alts: []Term{{Text: "legacy"}}, Neg: true}},
			Filters: []Filter{{Key: "tag", Value: "old", Neg: true}, {Key: "tag", Value: "two words"}, {Key: "meta.owner", Value: "me"}},
		}},
		{"a OR b OR c", Query{Groups: []Group{{Alts: []Term{{Text: "a"}, {Text: "b"}, {Text: "c"}}}}}},
		{"http:timeout foo:bar", Query{Groups: []Group{{Alts: []Term{{Text: "http:timeout"}}}, {Alts: []Term{{Text: "foo:bar"}}}}}},
		{"or - x", Query{Groups: []Group{{Alts: []Term{{Text: "or"}}}, {Alts: []Term{{Text: "-"}}}, {Alts: []Term{{Text: "x"}}}}}},
		{"", Query{}},
	}
	for _, tc := range tests {
		got, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.in, err)
			continue
		}
		if diff := cmp.Diff(tc.want, got); diff != "" {
			t.Errorf("Parse(%q) (-want +got):\n%s", tc.in, diff)
		}
		again, err := Parse(got.String())
		if err != nil || !cmp.Equal(again, got) {
			t.Errorf("round trip of %q via %q: %v", tc.in, got.String(), err)
		}
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()
	for in, pos := range map[string]int{
		`"unterminated`: 0,
		"OR a":          0,
		"a OR":          2,
		"a OR OR b":     5,
		"tag:":          4,
		"a OR tag:x":    5,
		"tag:x OR a":    6,
		"a OR -b":       5,
		"-a OR b":       3,
		`x ""`:          2,
		"***":           0,
		`tag:"open`:     4,
		"meta.:x":       -1,
	} {
		_, err := Parse(in)
		if pos < 0 {
			if err != nil {
				t.Errorf("Parse(%q) = %v, want success", in, err)
			}
			continue
		}
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("Parse(%q): want ParseError, got %v", in, err)
			continue
		}
		if pe.Pos != pos || !strings.Contains(pe.Error(), "^") {
			t.Errorf("Parse(%q) pos = %d want %d: %v", in, pe.Pos, pos, pe)
		}
	}
}

func TestQueryHelpers(t *testing.T) {
	t.Parallel()
	q, _ := Parse(`alpha "beta gamma" -delta x OR y`)
	if q.Text() != "alpha beta gamma" || len(q.Positive()) != 3 {
		t.Fatalf("text %q positive %d", q.Text(), len(q.Positive()))
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"a b", `"x y" OR z`, "-tag:x kind:shard", "meta.k:v", "a* -b", `tag:"a b"`, "OR", `""`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		q, err := Parse(s)
		if err != nil {
			return
		}
		again, err := Parse(q.String())
		if err != nil {
			t.Fatalf("String() of %q = %q does not parse: %v", s, q.String(), err)
		}
		if !cmp.Equal(q, again) {
			t.Fatalf("round trip mismatch for %q:\n%#v\n%#v", s, q, again)
		}
	})
}
