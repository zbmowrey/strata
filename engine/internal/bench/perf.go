package bench

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/index"
)

// Budgets are the p95 budgets of docs/spec/01-architecture.md §7, in ms.
var Budgets = map[string]float64{
	"version":            10,
	"freshness":          15,
	"search":             50,
	"show-10":            30,
	"show-links-1":       60,
	"refresh-10-changed": 80,
	"rebuild-10k":        3000,
	"hook-post-edit":     25,
	"hook-session-start": 300,
	"garden-plan":        5000,
}

// PerfResult is one measured operation.
type PerfResult struct {
	Name     string  `json:"name"`
	P50      float64 `json:"p50_ms"`
	P95      float64 `json:"p95_ms"`
	Budget   float64 `json:"budget_ms"`
	Baseline float64 `json:"baseline_p95_ms,omitempty"`
	N        int     `json:"n"`
	Over     bool    `json:"over_budget"`
	Regress  bool    `json:"regressed"`
}

// PerfBaseline is engine/testdata/perf-baseline.json.
type PerfBaseline struct {
	Machine string             `json:"machine"`
	Date    string             `json:"date"`
	P95     map[string]float64 `json:"p95_ms"`
}

// Machine identifies where a baseline was recorded; relative regression
// checks only apply on the same kind of machine.
func Machine() string {
	return fmt.Sprintf("%s/%s/%dcpu", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
}

// PerfOptions configure a perf run.
type PerfOptions struct {
	Binary string // strata executable used for process-level timings
	Corpus string // generated corpus directory (copied before mutation)
	Extra  []Op   // operations contributed by later phases (hooks, garden)
	N      int
}

// Op is one timed operation; Run returns the elapsed time of one sample.
type Op struct {
	Run  func(ctx context.Context) (time.Duration, error)
	Name string
}

// RunPerf measures every operation and compares with budgets and baseline.
func RunPerf(ctx context.Context, opt PerfOptions, base *PerfBaseline) ([]PerfResult, error) {
	if opt.N <= 0 {
		opt.N = 20
	}
	ops, cleanup, err := standardOps(ctx, opt)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	ops = append(ops, opt.Extra...)
	var out []PerfResult
	for _, op := range ops {
		r, err := measure(ctx, op, opt.N)
		if err != nil {
			return nil, fmt.Errorf("perf %s: %w", op.Name, err)
		}
		r.Budget = Budgets[op.Name]
		r.Over = r.Budget > 0 && r.P95 > r.Budget
		if base != nil && base.Machine == Machine() {
			if b, ok := base.P95[op.Name]; ok {
				r.Baseline = b
				r.Regress = r.P95 > b*1.2 && r.P95-b > 2 // ignore sub-2ms jitter
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func measure(ctx context.Context, op Op, n int) (PerfResult, error) {
	samples := make([]float64, 0, n)
	for range n {
		d, err := op.Run(ctx)
		if err != nil {
			return PerfResult{}, err
		}
		samples = append(samples, float64(d.Microseconds())/1000)
	}
	slices.Sort(samples)
	p95 := samples[min(len(samples)-1, (len(samples)*95+99)/100-1)]
	return PerfResult{Name: op.Name, N: n, P50: round1(samples[len(samples)/2]), P95: round1(p95)}, nil
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

// execOp times one invocation of the binary.
func execOp(name, bin, dir string, args ...string) Op {
	return Op{Name: name, Run: func(ctx context.Context) (time.Duration, error) {
		cmd := exec.CommandContext(ctx, bin, append([]string{"--root", dir}, args...)...) //nolint:gosec // G204: benchmarking our own binary
		start := time.Now()
		err := cmd.Run()
		d := time.Since(start)
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 3 { //nolint:errorlint // ExitError is returned unwrapped by Run
			err = nil
		}
		return d, err
	}}
}

// standardOps builds the retrieval and index operations over a private copy
// of the corpus.
func standardOps(ctx context.Context, opt PerfOptions) ([]Op, func(), error) {
	dir, err := os.MkdirTemp("", "strata-perf-*")
	if err != nil {
		return nil, nil, fmt.Errorf("perf: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if err := copyTree(opt.Corpus, dir); err != nil {
		cleanup()
		return nil, nil, err
	}
	inst, err := config.Open(dir, "")
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	ix, _, err := index.Open(ctx, inst, index.Options{})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	ids, err := sampleIDs(ctx, ix, 10)
	if err != nil {
		_ = ix.Close()
		cleanup()
		return nil, nil, err
	}
	files, err := sampleFiles(dir, 10)
	if err != nil {
		_ = ix.Close()
		cleanup()
		return nil, nil, err
	}
	ops := []Op{
		execOp("version", opt.Binary, dir, "version"),
		{Name: "freshness", Run: func(ctx context.Context) (time.Duration, error) {
			st, err := ix.Refresh(ctx, false)
			return st.Elapsed, err
		}},
		execOp("search", opt.Binary, dir, "search", "kalo tenmo"),
		execOp("show-10", opt.Binary, dir, append([]string{"show"}, ids...)...),
		execOp("show-links-1", opt.Binary, dir, "show", ids[0], "--links", "1"),
		{Name: "refresh-10-changed", Run: func(ctx context.Context) (time.Duration, error) {
			for i, f := range files {
				if err := appendLine(f, fmt.Sprintf("\nperf edit %d %d\n", i, time.Now().UnixNano())); err != nil {
					return 0, err
				}
			}
			st, err := ix.Refresh(ctx, false)
			return st.Elapsed, err
		}},
		{Name: "rebuild-10k", Run: func(ctx context.Context) (time.Duration, error) {
			st, err := ix.Rebuild(ctx)
			return st.Elapsed, err
		}},
	}
	return ops, func() { _ = ix.Close(); cleanup() }, nil
}

func sampleIDs(ctx context.Context, ix *index.Index, n int) ([]string, error) {
	rows, err := ix.DB().QueryContext(ctx, "SELECT id FROM docs WHERE kind = 'shard' ORDER BY id LIMIT ?", n)
	if err != nil {
		return nil, fmt.Errorf("perf ids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("perf ids: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("perf ids: %w", err)
	}
	if len(ids) == 0 {
		return nil, errors.New("perf: corpus has no shards")
	}
	return ids, nil
}

func sampleFiles(dir string, n int) ([]string, error) {
	var out []string
	err := filepath.WalkDir(filepath.Join(dir, "knowledge"), func(p string, d os.DirEntry, err error) error {
		if err != nil || len(out) >= n {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("perf files: %w", err)
	}
	return out, nil
}

func appendLine(path, s string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // G304: perf corpus copy
	if err != nil {
		return fmt.Errorf("perf edit: %w", err)
	}
	_, werr := f.WriteString(s)
	if err := cmp.Or(werr, f.Close()); err != nil {
		return fmt.Errorf("perf edit: %w", err)
	}
	return nil
}

func copyTree(src, dst string) error {
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if strings.HasPrefix(rel, filepath.Join(".strata", "index")) {
			return nil
		}
		t := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(t, 0o750)
		}
		b, err := os.ReadFile(p) //nolint:gosec // G304: copying the perf corpus
		if err != nil {
			return err //nolint:wrapcheck // wrapped once after WalkDir
		}
		return os.WriteFile(t, b, 0o600) //nolint:gosec // G703: destination is our own temp copy
	})
	if err != nil {
		return fmt.Errorf("copy corpus: %w", err)
	}
	return nil
}

// LoadBaseline reads a perf baseline (nil, nil when absent).
func LoadBaseline(path string) (*PerfBaseline, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: baseline path is engine-defined
	if os.IsNotExist(err) {
		return nil, nil //nolint:nilnil // absence is not an error
	}
	if err != nil {
		return nil, fmt.Errorf("perf baseline: %w", err)
	}
	var b PerfBaseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("perf baseline %s: %w", path, err)
	}
	return &b, nil
}

// NewBaseline captures results as a baseline.
func NewBaseline(rs []PerfResult, now time.Time) PerfBaseline {
	b := PerfBaseline{Machine: Machine(), Date: now.Format(time.DateOnly), P95: map[string]float64{}}
	for _, r := range rs {
		b.P95[r.Name] = r.P95
	}
	return b
}
