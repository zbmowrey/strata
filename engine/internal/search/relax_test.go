package search

import "testing"

func TestRelaxedExpr(t *testing.T) {
	t.Parallel()
	syn := Synonyms{"pg": {"postgres"}}
	cases := map[string]string{
		"keep claude working after stop": `"keep" OR "claude" OR "working" OR "stop"`,
		"how is the pg pool sized":       `"pg" OR "postgres" OR "pool" OR "sized"`,
		"teardown":                       "",
		"how to teardown":                "",
		`"exact phrase" hang -flaky`:     `"exact phrase" OR "hang"`,
	}
	for in, want := range cases {
		q, err := Parse(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := relaxedExpr(q, syn); got != want {
			t.Errorf("relaxedExpr(%q) = %q, want %q", in, got, want)
		}
	}
}
