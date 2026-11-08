package telemetry

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
)

func TestReadAndPrune(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inst := &config.Instance{Root: dir, Config: config.Default()}
	at := func(s string) func() time.Time {
		return func() time.Time { tm, _ := time.Parse(time.RFC3339, s); return tm }
	}
	for _, e := range []struct{ ts, name string }{
		{"2026-01-10T00:00:00Z", "search"},
		{"2026-05-03T00:00:00Z", "bash"},
		{"2026-05-20T00:00:00Z", "search"},
		{"2026-11-01T00:00:00Z", "show"},
	} {
		w := New(inst, at(e.ts)).WithSession("s1")
		if err := w.Emit(e.name, map[string]any{"q": "x", "n": 3, "ids": []string{"a", "b"}}); err != nil {
			t.Fatal(err)
		}
	}
	junk := filepath.Join(dir, filepath.FromSlash(Dir), "2026-11.jsonl")
	f, err := os.OpenFile(junk, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("not json\n{\"event\":\"x\"}\n")
	_ = f.Close()
	all, err := Read(inst, time.Time{})
	if err != nil || len(all) != 4 {
		t.Fatalf("read all: %v %d", err, len(all))
	}
	r := all[0]
	if r.Name != "search" || r.Session != "s1" || r.Str("q") != "x" || r.Int("n") != 3 || len(r.Strs("ids")) != 2 || r.Str("missing") != "" {
		t.Fatalf("record %+v", r)
	}
	since, _ := time.Parse(time.DateOnly, "2026-05-10")
	recent, err := Read(inst, since, "search")
	if err != nil || len(recent) != 1 {
		t.Fatalf("since+name: %v %d", err, len(recent))
	}
	inst.Config.Telemetry.Retention = config.Days(180)
	n, err := Prune(inst, at("2026-11-08T00:00:00Z")())
	if err != nil || n != 2 {
		t.Fatalf("prune: %v %d", err, n)
	}
	left, _ := Read(inst, time.Time{})
	if len(left) != 2 {
		t.Fatalf("after prune: %d", len(left))
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(Dir), "2026-01.jsonl")); !os.IsNotExist(err) {
		t.Fatal("emptied month not deleted")
	}
	inst.Config.Telemetry.Retention = config.Duration{}
	if n, err := Prune(inst, time.Now()); n != 0 || err != nil {
		t.Fatal("zero retention should be a no-op")
	}
}
