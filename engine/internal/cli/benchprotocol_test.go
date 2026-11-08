package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func TestBenchProtocol(t *testing.T) { //nolint:paralleltest // swaps the package-level session runner
	dir := testutil.CopyFixture(t, "instance-basic")
	out, _, code := run(t, "--root", dir, "bench", "protocol", "--list")
	if code != 0 || strings.Count(out, "\n") != 12 || !strings.Contains(out, "kb-first") {
		t.Fatalf("list: %d %q", code, out)
	}
	if _, _, code := run(t, "--root", dir, "bench", "protocol", "--scenario", "nope"); code != fail.Usage {
		t.Fatalf("unknown scenario: %d", code)
	}
	if _, _, code := run(t, "--root", dir, "bench", "protocol", "--suite", "nope"); code != fail.Usage {
		t.Fatalf("unknown suite: %d", code)
	}
	protocolClaude = func(_ context.Context, _ string, _, _ []string) ([]byte, error) {
		return []byte(`{"type":"system","subtype":"init","model":"claude-test"}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"strata search fixtures"}}]}}
{"type":"result","subtype":"success","result":"done","total_cost_usd":0.01,"num_turns":2}
`), nil
	}
	defer func() { protocolClaude = nil }()
	out, _, code = run(t, "--root", dir, "bench", "protocol", "--scenario", "capture-correction", "--runs", "1", "--plugin-dir", "/none")
	if code != fail.Blocking || !strings.Contains(out, "✗ capture-correction") || !strings.Contains(out, "run 1: no Bash call matching") {
		t.Fatalf("failing run: %d %q", code, out)
	}
	out, _, code = run(t, "--root", dir, "bench", "protocol", "--scenario", "capture-correction", "--runs", "1", "--format", "json")
	var rep struct {
		Model     string `json:"model"`
		Total     int    `json:"total"`
		Scenarios []struct {
			ID string `json:"id"`
		} `json:"scenarios"`
	}
	if code != fail.Blocking || json.NewDecoder(strings.NewReader(out)).Decode(&rep) != nil || rep.Model != "claude-test" || rep.Total != 1 {
		t.Fatalf("json: %d %q", code, out)
	}
	if saved := readFile(t, dir, ".strata/bench/protocol.json"); !strings.Contains(saved, `"id": "capture-correction"`) {
		t.Fatalf("protocol.json: %s", saved)
	}
	if findPluginDir(t.TempDir()) != "" {
		t.Fatal("plugin dir found in an empty tree")
	}
}
