package bench

import (
	"context"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/index"
	"github.com/zbmowrey/strata/engine/internal/testutil"

	"github.com/zbmowrey/strata/engine/internal/search"
)

func TestRetrievalMetrics(t *testing.T) {
	t.Parallel()
	g := GoldenQuery{Q: "q", Relevant: map[string]int{"a": 3, "b": 1, "z": 0}}
	q := scoreQuery(g, &search.Result{})
	if q.FirstHit != 0 || q.Recall5 != 0 || q.NDCG10 != 0 {
		t.Fatalf("empty result: %+v", q)
	}
	top := []string{"x", "b", "a"}
	if r := recallAt(g, top, 1); r != 0 {
		t.Fatalf("recall@1 %v", r)
	}
	if r := recallAt(g, top, 3); r != 1 {
		t.Fatalf("recall@3 %v", r)
	}
	if n := ndcg10(g, []string{"a", "b"}); n != 1 {
		t.Fatalf("ideal ndcg %v", n)
	}
	if n := ndcg10(g, top); n <= 0 || n >= 1 {
		t.Fatalf("ndcg %v", n)
	}
	if recallAt(GoldenQuery{}, top, 5) != 0 || ndcg10(GoldenQuery{}, top) != 0 {
		t.Fatal("no relevant docs")
	}
	m := aggregate([]QueryResult{{FirstHit: 2, Recall5: 1, NDCG10: 0.5}, {}}, []float64{1, 3}, []float64{100, 300})
	if m.MRR != 0.25 || m.Recall5 != 0.5 || m.LatP95 != 3 || m.BytesP50 != 100 {
		t.Fatalf("aggregate %+v", m)
	}
	if pct(nil, 50) != 0 {
		t.Fatal("pct empty")
	}
	rep := &RetrievalReport{Metrics: Metrics{MRR: 0.5, Recall5: 1, LatP95: 10, BytesP90: 100}}
	rep.Compare(nil)
	rep.Compare(&Metrics{MRR: 0.51, Recall5: 1, LatP95: 9.5, BytesP90: 100})
	if len(rep.Regressions) != 0 {
		t.Fatalf("within tolerance: %v", rep.Regressions)
	}
	rep.Compare(&Metrics{MRR: 0.6, Recall5: 1, LatP95: 2, BytesP90: 50})
	if len(rep.Regressions) != 3 {
		t.Fatalf("regressions: %v", rep.Regressions)
	}
}

func TestRunRetrievalAndBaseline(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGolden(inst); fail.CodeOf(err) != fail.Usage {
		t.Fatalf("missing bench: %v", err)
	}
	testutil.WriteFile(t, dir, RetrievalFile, "- q: \"tests hang at exit\"\n  relevant: { pytest-fixture-teardown: 3 }\n")
	ix, _, err := index.Open(context.Background(), inst, index.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ix.Close() }()
	today := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	rep, err := RunRetrieval(context.Background(), inst, ix, today, func(*search.Result) []byte { return []byte("xx") })
	if err != nil || rep.Metrics.Queries != 1 || rep.Queries[0].FirstHit == 0 || rep.Metrics.BytesP50 != 2 {
		t.Fatalf("run: %v %+v", err, rep)
	}
	if b, err := LoadRetrievalBaseline(inst); b != nil || err != nil {
		t.Fatal("baseline should be absent")
	}
	if err := SaveRetrievalBaseline(inst, rep.Metrics, today); err != nil {
		t.Fatal(err)
	}
	b, err := LoadRetrievalBaseline(inst)
	if err != nil || b.MRR != rep.Metrics.MRR {
		t.Fatalf("baseline round trip: %v %+v", err, b)
	}
	testutil.WriteFile(t, dir, BaselineFile, "{")
	if _, err := LoadRetrievalBaseline(inst); err == nil {
		t.Fatal("corrupt baseline accepted")
	}
	testutil.WriteFile(t, dir, RetrievalFile, "not: [yaml")
	if _, err := LoadGolden(inst); fail.CodeOf(err) != fail.Usage {
		t.Fatalf("bad yaml: %v", err)
	}
}
