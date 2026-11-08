package finding

import (
	"strings"
	"testing"
)

func TestSortAndCount(t *testing.T) {
	t.Parallel()
	fs := []Finding{
		{File: "b", Line: 1, Severity: Warning, Rule: "x"},
		{File: "a", Line: 2, Severity: Info, Rule: "y"},
		{File: "a", Line: 2, Severity: Error, Rule: "z"},
		{File: "a", Line: 1, Severity: Warning, Rule: "w"},
	}
	Sort(fs)
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.File + f.Rule)
	}
	got := b.String()
	if got != "awazaybx" {
		t.Fatalf("order %s", got)
	}
	if e, w := Count(fs); e != 1 || w != 2 {
		t.Fatalf("count %d %d", e, w)
	}
	if Rank("other") != 2 {
		t.Fatal("rank")
	}
}
