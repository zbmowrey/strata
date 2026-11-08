package textsim

import "testing"

func TestDiceAndTrigrams(t *testing.T) {
	t.Parallel()
	if Dice(Trigrams("postgres"), Trigrams("postgres")) != 1 || Dice(nil, Trigrams("abc")) != 0 {
		t.Fatal("dice identity/empty")
	}
	if d := Dice(Trigrams("connection pool"), Trigrams("connection pools")); d < 0.8 {
		t.Fatalf("near-identical dice %.2f", d)
	}
	if len(Trigrams("ab")) != 0 {
		t.Fatal("short string has no trigrams")
	}
}

func TestMinHash(t *testing.T) {
	t.Parallel()
	a, _ := MinHash("Session scoped database fixtures leak connections unless they are torn down after the test session ends")
	b, _ := MinHash("Session scoped database fixtures leak connections unless they are torn down after each test session ends")
	c, _ := MinHash("Helm values files are layered from base to environment overrides in a fixed order")
	if j := Jaccard(&a, &b); j < 0.5 {
		t.Fatalf("similar texts jaccard %.2f", j)
	}
	if j := Jaccard(&a, &c); j > 0.2 {
		t.Fatalf("different texts jaccard %.2f", j)
	}
	if Jaccard(&a, &a) != 1 {
		t.Fatal("self jaccard")
	}
	if _, ok := MinHash("  ,, "); ok {
		t.Fatal("empty text")
	}
	s, ok := MinHash("one two")
	if !ok || s == (Signature{}) {
		t.Fatal("short text")
	}
	b1, b2 := Bands(&a, 16), Bands(&a, 16)
	if len(b1) != 16 || b1[3] != b2[3] {
		t.Fatal("bands deterministic")
	}
	if got := Words("Foo-bar, BAZ_9"); len(got) != 4 || got[2] != "baz" {
		t.Fatalf("words %v", got)
	}
}
