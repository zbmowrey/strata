package garden

import (
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func TestRenderDigestHeading(t *testing.T) {
	t.Parallel()
	root, wt := t.TempDir(), t.TempDir()
	testutil.WriteFile(t, root, ".strata/garden/runs/r1/progress.json", `{"run":"r1","order":["a"],"tasks":{"a":{"status":"done","note":"filed"}}}`)
	s := &Session{Run: "r1", Worktree: wt, Attempt: 1}
	if d := RenderDigest(root, s); !strings.HasPrefix(d, "## Garden r1\n") || !strings.Contains(d, "- `a` — filed") {
		t.Fatalf("no summary:\n%s", d)
	}
	testutil.WriteFile(t, wt, summaryFile, "## Garden 2026-11-14 14:09\n\nFiled one item.\n")
	if d := RenderDigest(root, s); strings.Count(d, "## Garden") != 1 || !strings.HasPrefix(d, "## Garden 2026-11-14") {
		t.Fatalf("duplicate heading:\n%s", d)
	}
}
