package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func TestStatusCommand(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	out, _, code := run(t, "--root", dir, "status")
	if code != 0 || out != "⛬ 40 · inbox 2 · ? 1\n" {
		t.Fatalf("status: %d %q", code, out)
	}
	testutil.WriteFile(t, dir, ".strata/session/s1.json", `{"last_blocking":3}`)
	testutil.WriteFile(t, dir, ".strata/garden.lock", `{"branch":"garden/x","worktree":"/w","base":"main","base_ref":"main","started":"2026-11-07T12:00:00Z"}`)
	if out, _, code = run(t, "--root", dir, "status", "--session", "s1"); code != 0 || !strings.Contains(out, "gates ✗3 · garden running since") {
		t.Fatalf("status blocked: %d %q", code, out)
	}
	testutil.WriteFile(t, dir, ".strata/session/s2.json", `{"last_blocking":0}`)
	out, _, code = run(t, "--root", dir, "status", "--session", "s2", "--format", "json")
	var rep struct {
		Counts struct {
			Shards int `json:"shards"`
		} `json:"counts"`
		Gates string `json:"gates"`
		V     int    `json:"v"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &rep) != nil || rep.V != 1 || rep.Gates != "ok" || rep.Counts.Shards != 40 {
		t.Fatalf("status json: %d %q", code, out)
	}
}
