package doc

import "testing"

var order = []string{"id", "title", "summary", "type", "tags", "status", "trust", "sensitive"}

func TestSetRemoveKey(t *testing.T) {
	t.Parallel()
	raw := []byte("---\nid: x\ntitle: T\ntags:\n  - a\n  - b\n# comment for status\nstatus: active\nmystery: 1\n---\nbody\n")
	out, err := SetKey(raw, "tags", Value([]string{"c", "d e"}), order)
	if err != nil {
		t.Fatal(err)
	}
	out, _ = SetKey(out, "trust", "inferred", order)
	out, _ = SetKey(out, "sensitive", "false", order)
	out, _ = SetKey(out, "zzz", "1", order)
	out, _ = RemoveKey(out, "mystery")
	out, _ = RemoveKey(out, "absent")
	want := "---\nid: x\ntitle: T\ntags: [c, d e]\n# comment for status\nstatus: active\ntrust: inferred\nsensitive: false\nzzz: 1\n---\nbody\n"
	if string(out) != want {
		t.Fatalf("got:\n%s", out)
	}
	if _, err := SetKey([]byte("no frontmatter"), "a", "b", order); err == nil {
		t.Fatal("want error")
	}
	if _, err := SetKey([]byte("---\na: [\n---\n"), "a", "b", order); err == nil {
		t.Fatal("want parse error")
	}
	if _, err := RemoveKey([]byte("---\na: [\n---\n"), "a"); err == nil {
		t.Fatal("want parse error")
	}
	if got, _ := RemoveKey([]byte("plain"), "a"); string(got) != "plain" {
		t.Fatal("remove on plain")
	}
	for in, want := range map[any]string{"plain": "plain", "a: b": `"a: b"`, "": `""`, "yes": `"yes"`, 3: "3", " pad": `" pad"`} {
		if got := Value(in); got != want {
			t.Errorf("Value(%v) = %s", in, got)
		}
	}
}
