package render

import (
	"bytes"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/finding"
)

func TestFindingsAndJSON(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	err := Findings(&b, []finding.Finding{
		{File: "a.go", Line: 1, Col: 2, Severity: "error", Gate: "go-lint", Rule: "errcheck", Message: "m", FixHint: "h"},
		{File: "b.md", Line: 3, Col: 4, Severity: "warning", Rule: "link/broken", Message: "n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "a.go:1:2  error    go-lint/errcheck  m (hint: h)\nb.md:3:4  warning  link/broken  n\n"
	if b.String() != want {
		t.Fatalf("got %q", b.String())
	}
	b.Reset()
	if err := JSON(&b, map[string]string{"a": "<b>"}); err != nil || b.String() != "{\"a\":\"<b>\"}\n" {
		t.Fatalf("json %q %v", b.String(), err)
	}
	b.Reset()
	if err := JSONL(&b, []int{1, 2}); err != nil || b.String() != "1\n2\n" {
		t.Fatalf("jsonl %q", b.String())
	}
}

func TestPlural(t *testing.T) {
	t.Parallel()
	for in, want := range map[int]string{0: "0 errors", 1: "1 error", 2: "2 errors"} {
		if got := Plural(in, "error"); got != want {
			t.Errorf("%d: %s", in, got)
		}
	}
	if Plural(2, "query") != "2 queries" || Plural(2, "key") != "2 keys" {
		t.Error("y plural")
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, bytes.ErrTooLarge }

func TestWriteErrors(t *testing.T) {
	t.Parallel()
	if Findings(errWriter{}, []finding.Finding{{}}) == nil || JSONL(errWriter{}, []int{1}) == nil {
		t.Fatal("want write errors")
	}
}

func TestTextHelpers(t *testing.T) {
	t.Setenv("COLUMNS", "")
	if Width(0, false) != DefaultWidth {
		t.Fatal("default width")
	}
	t.Setenv("COLUMNS", "99")
	if Width(0, true) != 99 {
		t.Fatal("COLUMNS width")
	}
	t.Setenv("COLUMNS", "10")
	if w := Width(^uintptr(0), true); w != DefaultWidth {
		t.Fatalf("bad fd width %d", w)
	}
	if Truncate("héllo", 3) != "hé…" || Truncate("hi", 5) != "hi" || Truncate("x", 0) != "" {
		t.Fatal("Truncate")
	}
	if Pad("ab", 4) != "ab  " || Pad("abcdef", 3) != "ab…" {
		t.Fatal("Pad")
	}
	if OneLine(" a \n b\t c ") != "a b c" || Runes("héllo") != 5 {
		t.Fatal("OneLine/Runes")
	}
}
