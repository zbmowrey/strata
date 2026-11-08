package fsutil

import (
	"encoding/json"
	"testing"
)

func TestObjectPreservesOrder(t *testing.T) {
	t.Parallel()
	o, err := ParseObject([]byte(`{"z": 1, "a": {"y": [1,2], "b": "x"}, "m": null}`))
	if err != nil {
		t.Fatal(err)
	}
	inner, err := o.Object("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := inner.SetValue("c", []string{"q"}); err != nil {
		t.Fatal(err)
	}
	inner.Delete("y")
	inner.Delete("absent")
	if err := o.SetObject("a", inner); err != nil {
		t.Fatal(err)
	}
	if err := o.SetValue("new", true); err != nil {
		t.Fatal(err)
	}
	got, err := o.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"z":1,"a":{"b":"x","c":["q"]},"m":null,"new":true}` {
		t.Fatalf("got %s", got)
	}
	pretty, _ := o.Pretty()
	var check map[string]any
	if json.Unmarshal(pretty, &check) != nil || pretty[len(pretty)-1] != '\n' {
		t.Fatal("pretty output invalid")
	}
	if v, ok := o.Get("z"); !ok || string(v) != "1" || len(o.Keys()) != 4 {
		t.Fatal("Get/Keys")
	}
	if missing, err := o.Object("nope"); err != nil || len(missing.Keys()) != 0 {
		t.Fatal("missing nested object")
	}
}

func TestParseObjectErrors(t *testing.T) {
	t.Parallel()
	if o, err := ParseObject(nil); err != nil || len(o.Keys()) != 0 {
		t.Fatal("empty input")
	}
	for _, bad := range []string{`[1]`, `{"a":`, `{"a":1} x`, `{`, `{"a" 1}`, `"s"`} {
		if _, err := ParseObject([]byte(bad)); err == nil {
			t.Errorf("ParseObject(%q) succeeded", bad)
		}
	}
	if err := NewObject().SetValue("f", func() {}); err == nil {
		t.Fatal("marshal error")
	}
}
