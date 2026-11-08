package bench

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/index"
	"github.com/zbmowrey/strata/engine/internal/search"
)

// corpusDir returns a generated 10k corpus, cached across runs under
// $STRATA_BENCH_CORPUS (or the OS temp dir).
func corpusDir(b *testing.B) string {
	b.Helper()
	dir := os.Getenv("STRATA_BENCH_CORPUS")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "strata-bench-corpus-10k-seed1")
	}
	if _, err := os.Stat(filepath.Join(dir, ".strata", "config.toml")); err != nil {
		if err := GenerateCorpus(dir, DefaultCorpus); err != nil {
			b.Fatal(err)
		}
	}
	return dir
}

func openBench(b *testing.B) (*config.Instance, *index.Index) {
	b.Helper()
	inst, err := config.Open(corpusDir(b), "")
	if err != nil {
		b.Fatal(err)
	}
	ix, _, err := index.Open(context.Background(), inst, index.Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = ix.Close() })
	return inst, ix
}

func BenchmarkFreshness(b *testing.B) {
	_, ix := openBench(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := ix.Refresh(context.Background(), false); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearch(b *testing.B) {
	_, ix := openBench(b)
	q, _ := search.Parse("kalo tenmo")
	s := search.New(ix)
	b.ResetTimer()
	for b.Loop() {
		if _, err := s.Search(context.Background(), search.Request{Query: q, Limit: 10}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkShow10(b *testing.B) {
	inst, ix := openBench(b)
	s := search.New(ix)
	q, _ := search.Parse("kalo")
	res, err := s.Search(context.Background(), search.Request{Query: q, Limit: 10})
	if err != nil {
		b.Fatal(err)
	}
	ids := make([]string, 0, 10)
	for _, h := range res.Hits {
		ids = append(ids, h.ID)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := s.Show(context.Background(), inst.Root, search.ShowRequest{IDs: ids}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRefresh10(b *testing.B) {
	inst, ix := openBench(b)
	files, err := sampleFiles(inst.Root, 10)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	i := 0
	for b.Loop() {
		i++
		for _, f := range files {
			if err := appendLine(f, "\nbench "+string(rune('a'+i%26))+"\n"); err != nil {
				b.Fatal(err)
			}
		}
		if _, err := ix.Refresh(context.Background(), false); err != nil {
			b.Fatal(err)
		}
	}
}
