package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func TestGardenCommand(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	out, _, code := run(t, "--root", dir, "garden")
	if code != 0 || !strings.Contains(out, "file-inbox") || !strings.Contains(out, "plan: .strata/garden/plan.json") {
		t.Fatalf("garden: %d %q", code, out)
	}
	out, _, code = run(t, "--root", dir, "garden", "--plan", "--format", "json")
	var p struct {
		Counts  map[string]int `json:"counts"`
		V       int            `json:"v"`
		Applied bool           `json:"applied"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &p) != nil || p.V != 1 || !p.Applied || p.Counts["file-inbox"] != 2 {
		t.Fatalf("garden --plan: %d %q", code, out)
	}
	if out, _, code = run(t, "--root", dir, "garden", "--auto"); code != 0 || strings.Contains(out, "task") {
		t.Fatalf("garden --auto: %d %q", code, out)
	}
	if _, _, code = run(t, "--root", dir, "garden", "--auto", "--plan"); code != fail.Usage {
		t.Fatalf("exclusive flags: %d", code)
	}
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, dir, "knowledge/api/http/api-ws.md", "---\nid: api-ws\ntitle: Websocket timeouts\nsummary: Idle websockets close after 60 s.\ntype: fact\ntags: [api]\nstatus: active\ntrust: observed\ncreated: 2026-06-01\nupdated: 2026-06-01\nsources:\n  - { kind: file, ref: \"README.md\", at: 2026-06-01 }\n---\nBody.   \n")
	note, err := gardenAuto(context.Background(), inst, []string{"knowledge/api/http/api-ws.md"})
	if err != nil || !strings.Contains(note, "garden --auto:") {
		t.Fatalf("gardenAuto: %q %v", note, err)
	}
	if note, err = gardenAuto(context.Background(), inst, []string{}); err != nil || note != "" {
		t.Fatalf("gardenAuto no-op: %q %v", note, err)
	}
}

func TestGardenRunCommands(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	out, _, code := run(t, "--root", dir, "garden", "status")
	if code != 0 || !strings.Contains(out, "no garden run") || !strings.Contains(out, "debt: due") {
		t.Fatalf("garden status: %d %q", code, out)
	}
	testutil.WriteFile(t, dir, ".strata/garden.lock", `{"v":2,"run":"r1","fence":2,"attempt":2,"started":"2026-11-07T11:00:00Z","heartbeat_at":"2026-11-07T11:00:00Z","branch":"garden/r1","worktree":"/w","base":"main","base_ref":"main"}`)
	testutil.WriteFile(t, dir, ".strata/garden/runs/r1/progress.json", `{"run":"r1","order":["a","b"],"tasks":{"a":{"status":"done"},"b":{"status":"pending"}}}`)
	testutil.WriteFile(t, dir, ".strata/garden/history.jsonl", `{"run":"r1","attempt":1,"started":"2026-11-07T10:00:00Z","ended":"2026-11-07T10:30:00Z","outcome":"lost"}`+"\n")
	out, _, code = run(t, "--root", dir, "garden", "status")
	if code != 0 || !strings.Contains(out, "run r1 · ") || !strings.Contains(out, "1 pending") || !strings.Contains(out, "r1 a1 lost") {
		t.Fatalf("garden status with a run: %d %q", code, out)
	}
	out, _, code = run(t, "--root", dir, "garden", "status", "--format", "json")
	var st struct {
		Progress map[string]int `json:"progress"`
		Lease    struct {
			Fence int `json:"fence"`
		} `json:"lease"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &st) != nil || st.Lease.Fence != 2 || st.Progress["done"] != 1 {
		t.Fatalf("garden status json: %d %q", code, out)
	}
	if _, _, code = run(t, "--root", dir, "garden", "task", "finish", "a"); code != fail.Usage {
		t.Fatalf("bad task action: %d", code)
	}
	if _, _, code = run(t, "--root", dir, "garden", "task", "done", "a"); code != fail.Usage {
		t.Fatalf("task outside a worktree: %d", code)
	}
	if _, _, code = run(t, "--root", dir, "garden", "start", "--launcher", "robot"); code != fail.Usage {
		t.Fatalf("bad launcher: %d", code)
	}
	// The lease names a branch that does not exist in this (non-git) copy.
	if _, _, code = run(t, "--root", dir, "garden", "resume", "--force"); code == 0 {
		t.Fatal("resume of a run without a branch succeeded")
	}
}
