package bench

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
)

var small = CorpusSpec{Shards: 60, Scripts: 4, Playbooks: 2, Links: 30, Seed: 7}

func TestGenerateCorpusDeterministic(t *testing.T) {
	t.Parallel()
	a, b := t.TempDir(), t.TempDir()
	if err := GenerateCorpus(a, small); err != nil {
		t.Fatal(err)
	}
	if err := GenerateCorpus(b, small); err != nil {
		t.Fatal(err)
	}
	var n int
	err := filepath.WalkDir(a, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(a, p)
		x, _ := os.ReadFile(p)
		y, err := os.ReadFile(filepath.Join(b, rel))
		if err != nil || !bytes.Equal(x, y) {
			t.Errorf("%s differs between runs", rel)
		}
		n++
		return nil
	})
	if err != nil || n != 60+4+2+1 {
		t.Fatalf("files %d err %v", n, err)
	}
	if _, err := config.Open(a, ""); err != nil {
		t.Fatal(err)
	}
}

func TestRunPerfAndBaseline(t *testing.T) {
	t.Parallel()
	corpus := t.TempDir()
	if err := GenerateCorpus(corpus, small); err != nil {
		t.Fatal(err)
	}
	extra := Op{Name: "hook-post-edit", Run: func(context.Context) (time.Duration, error) { return 30 * time.Millisecond, nil }}
	base := &PerfBaseline{Machine: Machine(), P95: map[string]float64{"version": 0.001, "search": 1e9}}
	rs, err := RunPerf(context.Background(), PerfOptions{Binary: "/usr/bin/true", Corpus: corpus, N: 3, Extra: []Op{extra}}, base)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]PerfResult{}
	for _, r := range rs {
		got[r.Name] = r
	}
	for _, name := range []string{"version", "freshness", "search", "show-10", "show-links-1", "refresh-10-changed", "rebuild-10k", "hook-post-edit"} {
		if _, ok := got[name]; !ok {
			t.Errorf("missing op %s", name)
		}
	}
	if !got["hook-post-edit"].Over || got["search"].Regress || got["rebuild-10k"].P95 <= 0 {
		t.Errorf("budget/regression flags wrong: %+v", got)
	}
	if got["version"].Baseline == 0 {
		t.Error("baseline not attached")
	}
	nb := NewBaseline(rs, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	if nb.Date != "2026-01-02" || nb.P95["search"] != got["search"].P95 {
		t.Fatalf("baseline %+v", nb)
	}
}

func TestLoadBaseline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if b, err := LoadBaseline(filepath.Join(dir, "none.json")); b != nil || err != nil {
		t.Fatal("missing baseline should be nil, nil")
	}
	p := filepath.Join(dir, "b.json")
	_ = os.WriteFile(p, []byte(`{"machine":"m","p95_ms":{"x":1}}`), 0o600)
	b, err := LoadBaseline(p)
	if err != nil || b.P95["x"] != 1 {
		t.Fatalf("%+v %v", b, err)
	}
	_ = os.WriteFile(p, []byte(`{`), 0o600)
	if _, err := LoadBaseline(p); err == nil {
		t.Fatal("want parse error")
	}
	if _, err := LoadBaseline(dir); err == nil {
		t.Fatal("want read error")
	}
	if !strings.Contains(Machine(), "/") {
		t.Fatal("machine")
	}
}

func TestPerfErrors(t *testing.T) {
	t.Parallel()
	if _, err := RunPerf(context.Background(), PerfOptions{Binary: "/usr/bin/true", Corpus: "/nonexistent"}, nil); err == nil {
		t.Fatal("want corpus error")
	}
	empty := t.TempDir()
	_ = os.MkdirAll(filepath.Join(empty, ".strata"), 0o750)
	_ = os.WriteFile(filepath.Join(empty, ".strata", "config.toml"), nil, 0o600)
	if _, err := RunPerf(context.Background(), PerfOptions{Binary: "/usr/bin/true", Corpus: empty}, nil); err == nil {
		t.Fatal("want no-shards error")
	}
	if err := appendLine("/nonexistent/x", "y"); err == nil {
		t.Fatal("append to missing file")
	}
	failing := Op{Name: "x", Run: func(context.Context) (time.Duration, error) { return 0, os.ErrClosed }}
	if _, err := measure(context.Background(), failing, 1); err == nil {
		t.Fatal("op error not propagated")
	}
}
