package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/bench"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/render"
)

func init() { register(newBenchCmd) }

func newBenchCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "bench", Short: "Retrieval bench, protocol evals and performance benchmarks"}
	cmd.AddCommand(newBenchPerfCmd(a))
	return cmd
}

func newBenchPerfCmd(a *app) *cobra.Command {
	var corpus, baseline string
	var n int
	var update bool
	var budgets string
	cmd := &cobra.Command{
		Use:   "perf",
		Short: "Measure the §7 performance budgets on the synthetic corpus (engine repo)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := a.formatOr("line", "line", fmtJSON)
			if err != nil {
				return err
			}
			if budgets != "strict" && budgets != "report" {
				return fail.New(fail.Usage, "use --budgets strict|report", "bad --budgets %q", budgets)
			}
			bin, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locate strata binary: %w", err)
			}
			if corpus, err = ensureCorpus(corpus); err != nil {
				return err
			}
			base, err := bench.LoadBaseline(baseline)
			if err != nil {
				return err
			}
			rs, err := bench.RunPerf(cmd.Context(), bench.PerfOptions{Binary: bin, Corpus: corpus, N: n, Extra: perfExtras(bin)}, base)
			if err != nil {
				return err
			}
			if update {
				b, _ := json.MarshalIndent(bench.NewBaseline(rs, a.now()), "", "  ")
				if err := fsutil.WriteAtomic(baseline, append(b, '\n'), 0o644); err != nil {
					return err
				}
			}
			return a.printPerf(rs, format, base, budgets != "report")
		},
	}
	cmd.Flags().StringVar(&corpus, "corpus", "", "corpus directory (generated if missing)")
	cmd.Flags().StringVar(&baseline, "baseline", filepath.Join("engine", "testdata", "perf-baseline.json"), "baseline file")
	cmd.Flags().IntVar(&n, "n", 40, "samples per operation")
	cmd.Flags().StringVar(&budgets, "budgets", "strict", "strict = fail on budget misses/regressions; report = print only (other hardware, e.g. CI)")
	cmd.Flags().BoolVar(&update, "update-baseline", false, "write the results as the new baseline")
	return cmd
}

// ensureCorpus returns the corpus directory, generating the default corpus
// when it does not exist yet.
func ensureCorpus(dir string) (string, error) {
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "strata-bench-corpus-10k-seed1")
	}
	if _, err := os.Stat(filepath.Join(dir, ".strata", "config.toml")); err == nil {
		return dir, nil
	}
	return dir, bench.GenerateCorpus(dir, bench.DefaultCorpus)
}

// perfExtras is extended by later phases (hooks, garden).
var perfExtras = func(string) []bench.Op { return nil }

func (a *app) printPerf(rs []bench.PerfResult, format string, base *bench.PerfBaseline, strict bool) error {
	bad := 0
	for _, r := range rs {
		if r.Over || r.Regress {
			bad++
		}
	}
	var err error
	if format == fmtJSON {
		err = render.JSON(a.io.Out, map[string]any{"v": 1, "machine": bench.Machine(), "results": rs})
	} else {
		err = a.perfTable(rs, base)
	}
	if err != nil {
		return err
	}
	if bad > 0 && strict {
		return fail.New(fail.Blocking, "investigate before merging (docs/spec/01-architecture.md §7)", "%s over budget or regressed", render.Plural(bad, "operation"))
	}
	return nil
}

func (a *app) perfTable(rs []bench.PerfResult, base *bench.PerfBaseline) error {
	if base != nil && base.Machine != bench.Machine() {
		a.warnf("baseline recorded on %s; relative regression check skipped on %s", base.Machine, bench.Machine())
	}
	if err := a.printf("%-20s %9s %9s %9s %9s\n", "OPERATION", "P50 ms", "P95 ms", "BUDGET", "BASELINE"); err != nil {
		return err
	}
	for _, r := range rs {
		mark := "ok"
		switch {
		case r.Over:
			mark = "OVER BUDGET"
		case r.Regress:
			mark = "REGRESSED >20%"
		}
		if err := a.printf("%-20s %9.1f %9.1f %9.0f %9.1f  %s\n", r.Name, r.P50, r.P95, r.Budget, r.Baseline, mark); err != nil {
			return err
		}
	}
	return nil
}
