package cli

import (
	"bytes"

	"github.com/spf13/cobra"

	"github.com/zbmowrey/strata/engine/internal/bench"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/search"
)

func init() { benchSubcommands = append(benchSubcommands, newBenchRetrievalCmd) }

func newBenchRetrievalCmd(a *app) *cobra.Command {
	var update bool
	cmd := &cobra.Command{
		Use:   "retrieval",
		Short: "Run the golden queries (.strata/bench/retrieval.yaml) and compare with the baseline",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := a.formatOr("line", "line", fmtJSON)
			if err != nil {
				return err
			}
			inst, ix, err := a.openIndex(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = ix.Close() }()
			rep, err := bench.RunRetrieval(cmd.Context(), inst, ix, a.now(), a.lineBytes)
			if err != nil {
				return err
			}
			base, err := bench.LoadRetrievalBaseline(inst)
			if err != nil {
				return err
			}
			if update {
				if err := bench.SaveRetrievalBaseline(inst, rep.Metrics, a.now()); err != nil {
					return err
				}
			} else {
				rep.Compare(base)
			}
			if err := a.printRetrieval(rep, format); err != nil {
				return err
			}
			if len(rep.Regressions) > 0 {
				return fail.New(fail.Blocking, "investigate, or accept with --update-baseline", "%s", render.Plural(len(rep.Regressions), "regression"))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&update, "update-baseline", false, "accept the current metrics as .strata/bench/baseline.json")
	return cmd
}

// lineBytes renders a result in the default line format (output bytes).
func (a *app) lineBytes(res *search.Result) []byte {
	var buf bytes.Buffer
	b := &app{io: Streams{Out: &buf, Err: &bytes.Buffer{}}, g: a.g}
	_ = b.searchLines(res, false, false)
	return buf.Bytes()
}

func (a *app) printRetrieval(rep *bench.RetrievalReport, format string) error {
	if format == fmtJSON {
		return render.JSON(a.io.Out, rep)
	}
	m := rep.Metrics
	line := func(name string, v float64, base func(*bench.Metrics) float64) error {
		if rep.Baseline == nil {
			return a.printf("%-14s %8.3f\n", name, v)
		}
		return a.printf("%-14s %8.3f  (baseline %.3f)\n", name, v, base(rep.Baseline))
	}
	rows := []struct {
		name string
		v    float64
		f    func(*bench.Metrics) float64
	}{
		{"MRR", m.MRR, func(b *bench.Metrics) float64 { return b.MRR }},
		{"recall@1", m.Recall1, func(b *bench.Metrics) float64 { return b.Recall1 }},
		{"recall@3", m.Recall3, func(b *bench.Metrics) float64 { return b.Recall3 }},
		{"recall@5", m.Recall5, func(b *bench.Metrics) float64 { return b.Recall5 }},
		{"recall@10", m.Recall10, func(b *bench.Metrics) float64 { return b.Recall10 }},
		{"nDCG@10", m.NDCG10, func(b *bench.Metrics) float64 { return b.NDCG10 }},
		{"latency p95 ms", m.LatP95, func(b *bench.Metrics) float64 { return b.LatP95 }},
		{"bytes p90", m.BytesP90, func(b *bench.Metrics) float64 { return b.BytesP90 }},
	}
	if err := a.printf("%s\n", render.Plural(m.Queries, "query")); err != nil {
		return err
	}
	for _, r := range rows {
		if err := line(r.name, r.v, r.f); err != nil {
			return err
		}
	}
	for _, q := range rep.Queries {
		if q.FirstHit == 0 {
			if err := a.printf("miss: %q\n", q.Q); err != nil {
				return err
			}
		}
	}
	for _, r := range rep.Regressions {
		a.warnf("regression: %s", r)
	}
	return nil
}
