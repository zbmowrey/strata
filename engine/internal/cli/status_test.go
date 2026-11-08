package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestQuestionsApply(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	if _, _, code := run(t, "--root", dir, "questions", "apply", "q-20261103-coverage-floor"); code != 4 {
		t.Fatalf("apply open question: %d", code)
	}
	p := "knowledge/_questions/q-20261103-coverage-floor.md"
	testutil.WriteFile(t, dir, p, strings.Replace(readFile(t, dir, p), "status: open", "status: answered", 1))
	out, _, code := run(t, "--root", dir, "questions", "apply", "q-20261103-coverage-floor")
	if code != 0 || out != p+"\n" || !strings.Contains(readFile(t, dir, p), "status: applied") {
		t.Fatalf("apply: %d %q", code, out)
	}
	if _, _, code := run(t, "--root", dir, "lint", p); code != 0 {
		t.Fatalf("applied question fails lint: %d", code)
	}
}

func readFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
