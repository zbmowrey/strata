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
