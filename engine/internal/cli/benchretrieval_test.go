package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func TestBenchRetrieval(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	if _, _, code := run(t, "--root", dir, "bench", "retrieval"); code != fail.Usage {
		t.Fatalf("no bench file: %d", code)
	}
	testutil.WriteFile(t, dir, ".strata/bench/retrieval.yaml", `- q: "tests hang at exit"
  relevant: { pytest-fixture-teardown: 3 }
- q: "connection pool size"
  relevant: { postgres-connection-pool: 3, test-database-per-worker: 1 }
- q: "zzzz nothing matches this"
  relevant: { ci-pipeline-stages: 3 }
- q: "xdist"
  filters: "kind:shard"
  relevant: { pytest-parallel-xdist: 3 }
`)
	out, _, code := run(t, "--root", dir, "bench", "retrieval", "--update-baseline")
	if code != 0 || !strings.Contains(out, "4 queries") || !strings.Contains(out, `miss: "zzzz nothing matches this"`) {
		t.Fatalf("bench: %d %q", code, out)
	}
	// Keep quality from the accepted baseline but relax its cost: latency
	// under a loaded, parallel test run must not read as a regression.
	base := readFile(t, dir, ".strata/bench/baseline.json")
	base = regexp.MustCompile(`"latency_p95_ms": [0-9.]+`).ReplaceAllString(base, `"latency_p95_ms": 100000`)
	testutil.WriteFile(t, dir, ".strata/bench/baseline.json", base)
	out, _, code = run(t, "--root", dir, "bench", "retrieval", "--format", "json")
	var rep struct {
		Baseline *struct {
			MRR float64 `json:"mrr"`
		} `json:"baseline"`
		Metrics struct {
			MRR     float64 `json:"mrr"`
			Recall5 float64 `json:"recall_at_5"`
			NDCG    float64 `json:"ndcg_at_10"`
		} `json:"metrics"`
		Regressions []string `json:"regressions"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &rep) != nil || rep.Baseline == nil || rep.Metrics.MRR < 0.5 || rep.Metrics.MRR > 0.76 || len(rep.Regressions) != 0 {
		t.Fatalf("bench json: %d %q", code, out)
	}
	// A worse baseline-relative result is a blocking regression.
	testutil.WriteFile(t, dir, ".strata/bench/baseline.json", `{"metrics":{"mrr":0.99,"recall_at_5":1,"latency_p95_ms":0.001,"bytes_p90":1}}`)
	out, errOut, code := run(t, "--root", dir, "bench", "retrieval")
	if code != fail.Blocking || !strings.Contains(errOut, "regression: MRR") || !strings.Contains(errOut, "recall@5") || !strings.Contains(out, "(baseline 0.990)") {
		t.Fatalf("regression: %d %q %q", code, out, errOut)
	}
	testutil.WriteFile(t, dir, ".strata/bench/retrieval.yaml", "- q: \"x\"\n  filters: \"kind:\"\n  relevant: {}\n")
	if _, _, code := run(t, "--root", dir, "bench", "retrieval"); code != fail.Usage {
		t.Fatalf("bad filter: %d", code)
	}
	testutil.WriteFile(t, dir, ".strata/bench/retrieval.yaml", "[]\n")
	if _, _, code := run(t, "--root", dir, "bench", "retrieval"); code != fail.Usage {
		t.Fatalf("empty bench: %d", code)
	}
}
