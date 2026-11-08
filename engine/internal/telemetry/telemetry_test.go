package telemetry

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
)

func TestEmit(t *testing.T) {
	t.Setenv("STRATA_SESSION", "s-env")
	dir := t.TempDir()
	inst := &config.Instance{Root: dir, Config: config.Default()}
	now := func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) }
	w := New(inst, now)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			if err := w.Emit("search", map[string]any{"query": "q", "n": i}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if err := w.WithSession("s-hook").Emit("hook", nil); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, ".strata", "telemetry", "2026-11.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	n, hooks := 0, 0
	for sc.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("corrupt line %q: %v", sc.Text(), err)
		}
		if rec["v"] != float64(1) || rec["ts"] != "2026-11-07T12:00:00Z" {
			t.Fatalf("record %v", rec)
		}
		if rec["event"] == "hook" {
			hooks++
			if rec["session"] != "s-hook" {
				t.Fatal("session override")
			}
		} else if rec["session"] != "s-env" {
			t.Fatal("env session")
		}
		n++
	}
	if n != 51 || hooks != 1 {
		t.Fatalf("lines %d hooks %d", n, hooks)
	}
}

func TestDisabledAndErrors(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Telemetry.Enabled = false
	dir := t.TempDir()
	if err := New(&config.Instance{Root: dir, Config: cfg}, nil).Emit("x", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".strata")); !os.IsNotExist(err) {
		t.Fatal("disabled telemetry wrote files")
	}
	var nilW *Writer
	if nilW.Emit("x", nil) != nil {
		t.Fatal("nil writer")
	}
	blocked := t.TempDir()
	_ = os.WriteFile(filepath.Join(blocked, ".strata"), nil, 0o600)
	if err := New(&config.Instance{Root: blocked, Config: config.Default()}, nil).Emit("x", nil); err == nil {
		t.Fatal("want error")
	}
	if err := New(&config.Instance{Root: dir, Config: config.Default()}, nil).Emit("x", map[string]any{"bad": func() {}}); err == nil {
		t.Fatal("want marshal error")
	}
}
