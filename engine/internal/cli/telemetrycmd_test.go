package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

// telemetryInstance writes an instance with a fixed telemetry history
// (TestMain pins STRATA_NOW to 2026-11-07).
func telemetryInstance(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	testutil.WriteFile(t, dir, ".strata/config.toml", "")
	lines := []string{
		`{"ts":"2026-03-01T10:00:00Z","v":1,"session":"old","event":"bash","first_token":"make"}`,
		`{"ts":"2026-11-01T10:00:00Z","v":1,"session":"s1","event":"bash","first_token":"go","normalized_hash":"a","length":10}`,
		`{"ts":"2026-11-01T10:01:00Z","v":1,"session":"s1","event":"bash","first_token":"go","normalized_hash":"a","length":10}`,
		`{"ts":"2026-11-01T10:02:00Z","v":1,"session":"s2","event":"bash","first_token":"git","normalized_hash":"b","length":7}`,
		`{"ts":"2026-11-02T10:00:00Z","v":1,"session":"s2","event":"search","n_results":0,"zero":true}`,
		`{"ts":"2026-11-02T10:00:01Z","v":1,"session":"s2","event":"search","n_results":4,"zero":false}`,
		`{"ts":"2026-11-02T10:00:02Z","v":1,"session":"s2","event":"hook","hook":"post-edit","ms":8,"outcome":"clean"}`,
		`{"ts":"2026-11-02T10:00:03Z","v":1,"session":"s2","event":"hook","hook":"post-edit","ms":30,"outcome":"blocked"}`,
		`{"ts":"2026-11-02T10:00:04Z","v":1,"session":"s2","event":"gate","gate":"go-lint","stage":"edit","result":"fail"}`,
		`{"ts":"2026-11-02T10:00:05Z","v":1,"session":"s2","event":"gate","gate":"go-lint","stage":"stop","result":"pass"}`,
		`{"ts":"2026-11-02T10:00:06Z","v":1,"session":"s2","event":"gate","stage":"stop","result":"abandoned"}`,
	}
	testutil.WriteFile(t, dir, ".strata/telemetry/2026-03.jsonl", lines[0]+"\n")
	testutil.WriteFile(t, dir, ".strata/telemetry/2026-11.jsonl", strings.Join(lines[1:], "\n")+"\n")
	return dir
}

func TestTelemetryStats(t *testing.T) {
	t.Parallel()
	dir := telemetryInstance(t)
	out, _, code := run(t, "--root", dir, "telemetry", "stats", "--format", "json")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	testutil.Golden(t, filepath.Join("testdata", "golden", "telemetry-stats.json.golden"), []byte(out))
	out, _, code = run(t, "--root", dir, "telemetry", "stats", "--since", "365d")
	if code != 0 || !strings.Contains(out, "11 events · 3 sessions") || !strings.Contains(out, "bash top    go 2 · git 1 · make 1") ||
		!strings.Contains(out, "1 zero-result (50.0%)") || !strings.Contains(out, "p95 30 ms") {
		t.Fatalf("line: %d\n%s", code, out)
	}
	if _, _, code := run(t, "--root", dir, "telemetry", "stats", "--since", "soon"); code != fail.Usage {
		t.Fatalf("bad --since: %d", code)
	}
	if _, _, code := run(t, "--root", t.TempDir(), "telemetry", "stats"); code != fail.NotInstance {
		t.Fatalf("not an instance: %d", code)
	}
}

func TestTelemetryPrune(t *testing.T) {
	t.Parallel()
	dir := telemetryInstance(t)
	out, errOut, code := run(t, "--root", dir, "telemetry", "prune")
	if code != 0 || out != "" || !strings.Contains(errOut, "pruned 1 telemetry event") {
		t.Fatalf("prune: %d %q %q", code, out, errOut)
	}
	if _, errOut, _ := run(t, "--root", dir, "-q", "telemetry", "prune"); errOut != "" {
		t.Fatalf("quiet prune: %q", errOut)
	}
	if _, _, code := run(t, "--root", t.TempDir(), "telemetry", "prune"); code != fail.NotInstance {
		t.Fatalf("not an instance: %d", code)
	}
}
