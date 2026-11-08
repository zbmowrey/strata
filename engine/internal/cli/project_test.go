package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func TestProjectLifecycle(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	cmd := func(want int, args ...string) string {
		t.Helper()
		out, errOut, code := run(t, append([]string{"--root", dir}, args...)...)
		if code != want {
			t.Fatalf("%v: code=%d want %d stderr=%s", args, code, want, errOut)
		}
		return out
	}
	if out := cmd(0, "project", "new", "auth-rework", "Rework the auth flow", "--goal", "Refresh tokens rotate", "--tags", "auth,api"); out != "projects/auth-rework\n" {
		t.Fatalf("new: %q", out)
	}
	cmd(0, "project", "log", "auth-rework", "--tried", "rotating tokens on every request")
	cmd(0, "project", "log", "auth-rework", "--failed", "zanzibarquux rotation broke mobile clients")
	cmd(0, "project", "log", "--note", "unrelated observation")
	// The new log entry is searchable immediately (indexed as its own document).
	if out := cmd(0, "search", "zanzibarquux"); !strings.HasPrefix(out, "auth-rework:log#2026-11-07-00-00") {
		t.Fatalf("search: %q", out)
	}
	if out := cmd(0, "project", "handoff", "auth-rework"); out != "projects/auth-rework/handoff.md\n" {
		t.Fatalf("handoff: %q", out)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "projects", "auth-rework", "handoff.md"))
	if !strings.Contains(string(raw), "- zanzibarquux rotation broke mobile clients (log 2026-11-07 00:00)") {
		t.Fatalf("handoff did not prefill Don't redo:\n%s", raw)
	}
	cmd(0, "project", "track", "auth-rework", "out/summary.md")
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(gi), "!/projects/auth-rework/out/summary.md") {
		t.Fatalf("gitignore:\n%s", gi)
	}
	testutil.Golden(t, filepath.Join("testdata", "golden", "resume.golden"), []byte(cmd(0, "resume")))
	testutil.Golden(t, filepath.Join("testdata", "golden", "project-show.golden"), []byte(cmd(0, "project", "show", "auth-rework")))
	testutil.Golden(t, filepath.Join("testdata", "golden", "project-list.golden"), []byte(cmd(0, "project", "list")))
	var list struct {
		Projects []map[string]any `json:"projects"`
	}
	if err := json.Unmarshal([]byte(cmd(0, "--format", "json", "project", "list", "--all")), &list); err != nil || len(list.Projects) != 3 {
		t.Fatalf("json list %v %v", list, err)
	}
	cmd(0, "project", "pause", "ci-flake")
	cmd(0, "project", "close", "auth-rework")
	if out := cmd(0, "project", "resume"); !strings.HasPrefix(out, "No active projects") {
		t.Fatalf("resume after close: %q", out)
	}
	if out := cmd(0, "resume", "auth-rework", "--format", "json"); !strings.Contains(out, `"status":"done"`) {
		t.Fatalf("resume slug: %q", out)
	}
	if out := cmd(0, "--format", "json", "project", "show", "ci-flake"); !strings.Contains(out, `"recent"`) {
		t.Fatalf("show json: %q", out)
	}
	cmd(0, "project", "activate", "ci-flake")
	if out := cmd(0, "gitignore", "sync"); out != "" {
		t.Fatalf("sync should be a no-op: %q", out)
	}
	cmd(0, "lint")
}

func TestProjectErrors(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	for _, args := range [][]string{
		{"project", "new", "Bad Slug", "x"},
		{"project", "new", "ci-flake", "dup"},
		{"project", "log", "ci-flake"},
		{"project", "log", "ci-flake", "--tried", "a", "--failed", "b"},
		{"project", "log", "nope-nope", "--note", "x"},
		{"project", "show", "nope-nope"},
		{"project", "pause", "_adhoc"},
		{"project", "track", "ci-flake", "scratch/x"},
		{"project", "handoff", "nope-nope"},
		{"resume", "nope-nope"},
		{"--format", "table", "project", "list"},
	} {
		_, errOut, code := run(t, append([]string{"--root", dir}, args...)...)
		if code != fail.Usage {
			t.Errorf("%v: code=%d stderr=%s", args, code, errOut)
		}
	}
	if _, _, code := run(t, "--root", t.TempDir(), "gitignore", "sync"); code != fail.NotInstance {
		t.Errorf("not instance: %d", code)
	}
}
