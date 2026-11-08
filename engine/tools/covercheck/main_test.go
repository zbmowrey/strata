package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseAndReport(t *testing.T) {
	t.Parallel()
	profile := `mode: atomic
example.com/x/internal/a/a.go:1.1,2.2 3 1
example.com/x/internal/a/a.go:3.1,4.2 1 0
example.com/x/internal/a/a.go:3.1,4.2 1 0
example.com/x/internal/b/b.go:1.1,2.2 1 0
example.com/x/internal/b/b.go:1.1,2.2 1 2
example.com/x/cmd/c.go:1.1,2.2 5 0
`
	pkgs, err := parse(strings.NewReader(profile))
	if err != nil {
		t.Fatal(err)
	}
	if got := pkgs["example.com/x/internal/a"]; got.total != 4 || got.covered != 3 {
		t.Fatalf("a = %+v", got)
	}
	var buf bytes.Buffer
	failed := report(&buf, pkgs, "example.com/x/internal/", 80)
	if failed != 1 {
		t.Fatalf("failed = %d, output:\n%s", failed, buf.String())
	}
	want := "FAIL   75.0%  a\nok    100.0%  b\n"
	if buf.String() != want {
		t.Fatalf("got %q want %q", buf.String(), want)
	}
}

func TestParseMalformed(t *testing.T) {
	t.Parallel()
	if _, err := parse(strings.NewReader("mode: set\nbad line\n")); err == nil {
		t.Fatal("want error")
	}
	if _, err := parse(strings.NewReader("mode: set\nf.go:1 x 1\n")); err == nil {
		t.Fatal("want error")
	}
}
