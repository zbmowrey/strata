package bench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/index"
	"github.com/zbmowrey/strata/engine/internal/search"
)

// Retrieval bench files (docs/spec/10-evals.md §1).
const (
	RetrievalFile = ".strata/bench/retrieval.yaml"
	BaselineFile  = ".strata/bench/baseline.json"
)

// GoldenQuery is one bench query with graded relevance (3 best).
type GoldenQuery struct {
	Relevant map[string]int `yaml:"relevant" json:"relevant"`
	Q        string         `yaml:"q" json:"q"`
	Filters  string         `yaml:"filters" json:"filters,omitempty"`
}

// Metrics are the retrieval bench's aggregate measures.
type Metrics struct {
	Recall1  float64 `json:"recall_at_1"`
	Recall3  float64 `json:"recall_at_3"`
	Recall5  float64 `json:"recall_at_5"`
	Recall10 float64 `json:"recall_at_10"`
	MRR      float64 `json:"mrr"`
	NDCG10   float64 `json:"ndcg_at_10"`
	LatP50   float64 `json:"latency_p50_ms"`
	LatP95   float64 `json:"latency_p95_ms"`
	BytesP50 float64 `json:"bytes_p50"`
	BytesP90 float64 `json:"bytes_p90"`
	Queries  int     `json:"queries"`
}

// QueryResult is one query's outcome.
type QueryResult struct {
	Q        string   `json:"q"`
	Top      []string `json:"top"`
	FirstHit int      `json:"first_relevant_rank"` // 0 = not in the top 10
	Recall1  float64  `json:"-"`
	Recall3  float64  `json:"-"`
	Recall5  float64  `json:"recall_at_5"`
	Recall10 float64  `json:"-"`
	NDCG10   float64  `json:"ndcg_at_10"`
}

// RetrievalReport is the output of `strata bench retrieval`.
type RetrievalReport struct {
	Baseline    *Metrics      `json:"baseline,omitempty"`
	Metrics     Metrics       `json:"metrics"`
	Regressions []string      `json:"regressions"`
	Queries     []QueryResult `json:"queries"`
	V           int           `json:"v"`
}

// Tolerances for regressions (docs/spec/10-evals.md §1).
const (
	MRRTolerance = 0.02
	CostGrowth   = 1.2 // latency and bytes
)

// LoadGolden reads the golden queries.
func LoadGolden(inst *config.Instance) ([]GoldenQuery, error) {
	raw, err := os.ReadFile(inst.Path(RetrievalFile)) //nolint:gosec // G304: instance bench file
	if errors.Is(err, os.ErrNotExist) {
		return nil, fail.New(fail.Usage, "add golden queries to "+RetrievalFile, "no retrieval bench")
	}
	if err != nil {
		return nil, fmt.Errorf("retrieval bench: %w", err)
	}
	var qs []GoldenQuery
	if err := yaml.Unmarshal(raw, &qs); err != nil {
		return nil, fail.Wrap(fail.Usage, "fix "+RetrievalFile, fmt.Errorf("retrieval bench: %w", err))
	}
	if len(qs) == 0 {
		return nil, fail.New(fail.Usage, "add golden queries to "+RetrievalFile, "retrieval bench has no queries")
	}
	return qs, nil
}

// RunRetrieval runs every golden query through the real search path.
// render returns the line-format output of a result (for output bytes).
func RunRetrieval(ctx context.Context, inst *config.Instance, ix *index.Index, today time.Time, render func(*search.Result) []byte) (*RetrievalReport, error) {
	qs, err := LoadGolden(inst)
	if err != nil {
		return nil, err
	}
	syn, err := search.LoadSynonyms(inst.Path(inst.Config.Search.Synonyms))
	if err != nil {
		return nil, fmt.Errorf("synonyms: %w", err)
	}
	s := search.New(ix)
	rep := &RetrievalReport{V: 1, Regressions: []string{}}
	var lat, size []float64
	for _, g := range qs {
		raw := strings.TrimSpace(g.Q + " " + g.Filters)
		q, err := search.Parse(raw)
		if err != nil {
			return nil, fail.Wrap(fail.Usage, "fix the query in "+RetrievalFile, fmt.Errorf("query %q: %w", g.Q, err))
		}
		start := time.Now()
		res, err := s.Search(ctx, search.Request{Query: q, Raw: raw, Limit: 10, Synonyms: syn, Scope: search.Scope{Today: today}})
		if err != nil {
			return nil, err
		}
		lat = append(lat, float64(time.Since(start).Microseconds())/1000)
		size = append(size, float64(len(render(res))))
		rep.Queries = append(rep.Queries, scoreQuery(g, res))
	}
	rep.Metrics = aggregate(rep.Queries, lat, size)
	return rep, nil
}

func scoreQuery(g GoldenQuery, res *search.Result) QueryResult {
	qr := QueryResult{Q: g.Q, Top: []string{}}
	for _, h := range res.Hits {
		qr.Top = append(qr.Top, h.ID)
	}
	for i, id := range qr.Top {
		if g.Relevant[id] > 0 {
			qr.FirstHit = i + 1
			break
		}
	}
	qr.Recall1, qr.Recall3 = recallAt(g, qr.Top, 1), recallAt(g, qr.Top, 3)
	qr.Recall5, qr.Recall10 = recallAt(g, qr.Top, 5), recallAt(g, qr.Top, 10)
	qr.NDCG10 = ndcg10(g, qr.Top)
	return qr
}

func relevantCount(g GoldenQuery) int {
	n := 0
	for _, grade := range g.Relevant {
		if grade > 0 {
			n++
		}
	}
	return n
}

func recallAt(g GoldenQuery, top []string, k int) float64 {
	total := relevantCount(g)
	if total == 0 {
		return 0
	}
	found := 0
	for _, id := range top[:min(k, len(top))] {
		if g.Relevant[id] > 0 {
			found++
		}
	}
	return float64(found) / float64(total)
}

// ndcg10 is nDCG@10 with gain 2^grade − 1.
func ndcg10(g GoldenQuery, top []string) float64 {
	const k = 10
	dcg := 0.0
	for i, id := range top[:min(k, len(top))] {
		dcg += (math.Pow(2, float64(g.Relevant[id])) - 1) / math.Log2(float64(i)+2)
	}
	grades := make([]int, 0, len(g.Relevant))
	for _, v := range g.Relevant {
		grades = append(grades, v)
	}
	slices.Sort(grades)
	slices.Reverse(grades)
	ideal := 0.0
	for i, v := range grades[:min(k, len(grades))] {
		ideal += (math.Pow(2, float64(v)) - 1) / math.Log2(float64(i)+2)
	}
	if ideal == 0 {
		return 0
	}
	return dcg / ideal
}

// aggregate averages per-query measures and takes cost percentiles.
func aggregate(qs []QueryResult, lat, size []float64) Metrics {
	m := Metrics{Queries: len(qs)}
	for _, q := range qs {
		if q.FirstHit > 0 {
			m.MRR += 1 / float64(q.FirstHit)
		}
		m.NDCG10 += q.NDCG10
		m.Recall1 += q.Recall1
		m.Recall3 += q.Recall3
		m.Recall5 += q.Recall5
		m.Recall10 += q.Recall10
	}
	n := float64(max(1, len(qs)))
	m.MRR, m.NDCG10, m.Recall5 = r3(m.MRR/n), r3(m.NDCG10/n), r3(m.Recall5/n)
	m.Recall1, m.Recall3, m.Recall10 = r3(m.Recall1/n), r3(m.Recall3/n), r3(m.Recall10/n)
	m.LatP50, m.LatP95 = pct(lat, 50), pct(lat, 95)
	m.BytesP50, m.BytesP90 = pct(size, 50), pct(size, 90)
	return m
}

func r3(f float64) float64 { return math.Round(f*1000) / 1000 }

func pct(xs []float64, p int) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	return round1(s[min(len(s)-1, (len(s)*p+99)/100-1)])
}

// Compare fills Regressions against a baseline. Latency and bytes regress
// only beyond 20% and a small absolute floor (sub-millisecond jitter).
func (r *RetrievalReport) Compare(base *Metrics) {
	if base == nil {
		return
	}
	r.Baseline = base
	m := r.Metrics
	if m.MRR < base.MRR-MRRTolerance {
		r.Regressions = append(r.Regressions, fmt.Sprintf("MRR %.3f < baseline %.3f − %.2f", m.MRR, base.MRR, MRRTolerance))
	}
	if m.Recall5 < base.Recall5 {
		r.Regressions = append(r.Regressions, fmt.Sprintf("recall@5 %.3f < baseline %.3f", m.Recall5, base.Recall5))
	}
	if m.LatP95 > base.LatP95*CostGrowth && m.LatP95-base.LatP95 > 2 {
		r.Regressions = append(r.Regressions, fmt.Sprintf("latency p95 %.1f ms > baseline %.1f ms + 20%%", m.LatP95, base.LatP95))
	}
	if m.BytesP90 > base.BytesP90*CostGrowth {
		r.Regressions = append(r.Regressions, fmt.Sprintf("output bytes p90 %.0f > baseline %.0f + 20%%", m.BytesP90, base.BytesP90))
	}
}

// LoadRetrievalBaseline reads the accepted metrics (nil when absent).
func LoadRetrievalBaseline(inst *config.Instance) (*Metrics, error) {
	raw, err := os.ReadFile(inst.Path(BaselineFile)) //nolint:gosec // G304: instance bench file
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil //nolint:nilnil // no baseline yet
	}
	if err != nil {
		return nil, fmt.Errorf("bench baseline: %w", err)
	}
	var b struct {
		Metrics Metrics `json:"metrics"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("bench baseline: %w", err)
	}
	return &b.Metrics, nil
}

// SaveRetrievalBaseline accepts the current metrics.
func SaveRetrievalBaseline(inst *config.Instance, m Metrics, now time.Time) error {
	b, err := json.MarshalIndent(map[string]any{"v": 1, "date": now.Format(time.DateOnly), "metrics": m}, "", "  ")
	if err != nil {
		return fmt.Errorf("bench baseline: %w", err)
	}
	return fsutil.WriteAtomic(inst.Path(BaselineFile), append(b, '\n'), 0o644)
}
