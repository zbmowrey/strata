package version

import "testing"

func TestParseCompare(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.0", 0},
		{"v0.2.0", "0.1.9", 1},
		{"1.0.0", "1.0.1", -1},
		{"0.1.0-dev", "0.1.0", -1},
		{"0.1.0", "0.1.0-rc1", 1},
		{"0.1.0-a", "0.1.0-b", -1},
		{"1.2.3+meta", "1.2.3", 0},
	}
	for _, tc := range tests {
		a, err := Parse(tc.a)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Parse(tc.b)
		if err != nil {
			t.Fatal(err)
		}
		if got := Compare(a, b); got != tc.want {
			t.Errorf("Compare(%s,%s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
	for _, bad := range []string{"", "1.2", "a.b.c", "1.-1.0"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) succeeded", bad)
		}
	}
}

func TestAtLeast(t *testing.T) {
	t.Parallel()
	ok, err := AtLeast("0.1.0-dev", "0.1.0")
	if err != nil || !ok {
		t.Fatalf("dev build should satisfy own minimum: %v %v", ok, err)
	}
	ok, err = AtLeast("0.1.0", "0.2.0")
	if err != nil || ok {
		t.Fatalf("0.1.0 >= 0.2.0: %v %v", ok, err)
	}
	if _, err := AtLeast("x", "0.1.0"); err == nil {
		t.Fatal("want error")
	}
	if _, err := AtLeast("0.1.0", "x"); err == nil {
		t.Fatal("want error")
	}
}

func TestGet(t *testing.T) {
	t.Parallel()
	info := Get()
	if info.Version == "" || info.Go == "" || info.OS == "" {
		t.Fatalf("incomplete info %+v", info)
	}
}
