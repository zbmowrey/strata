package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func TestScriptsCommands(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	out, _, code := run(t, "--root", dir, "scripts", "list", "--side", "writes-local")
	if code != 0 || !strings.HasPrefix(out, "render-release-notes  writes-local") || strings.Count(out, "\n") != 1 {
		t.Fatalf("list: %d %q", code, out)
	}
	out, _, code = run(t, "--root", dir, "--format", "json", "scripts", "list", "--domain", "infra")
	var list struct {
		Scripts []map[string]string `json:"scripts"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || len(list.Scripts) != 2 {
		t.Fatalf("list json: %d %q", code, out)
	}
	if out, _, code = run(t, "--root", dir, "--format", "jsonl", "scripts", "list"); code != 0 || strings.Count(out, "\n") != 5 {
		t.Fatalf("list jsonl: %d %q", code, out)
	}
	if out, _, code = run(t, "--root", dir, "run", "normalize-csv", "--", "--help"); code != 0 || !strings.Contains(out, "usage: normalize-csv") {
		t.Fatalf("run: %d %q", code, out)
	}
	if out, _, code = run(t, "--root", dir, "scripts", "build", "normalize-csv", "check-migrations"); code != 0 || out != "normalize-csv\tcached\ncheck-migrations\tbuilt\n" {
		t.Fatalf("build: %d %q", code, out)
	}
	if out, _, code = run(t, "--root", dir, "scripts", "build", "--all", "--force"); code != 0 || strings.Count(out, "built") != 4 {
		t.Fatalf("build all: %d %q", code, out)
	}
	if out, _, code = run(t, "--root", dir, "scripts", "catalog"); code != 0 || out != "scripts/CATALOG.md\n" {
		t.Fatalf("catalog: %d %q", code, out)
	}
	if out, _, code = run(t, "--root", dir, "new", "script", "data/tidy-rows"); code != 0 || !strings.Contains(out, "scripts/data/tidy-rows/script.yaml") {
		t.Fatalf("new script: %d %q", code, out)
	}
	if _, _, code = run(t, "--root", dir, "lint", "scripts/data/tidy-rows"); code != fail.Blocking {
		t.Fatalf("scaffold should fail lint: %d", code)
	}
}

func TestScriptsUsageErrors(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	for _, args := range [][]string{
		{"run", "no-such-script"},
		{"scripts", "build"},
		{"scripts", "build", "--all", "normalize-csv"},
		{"scripts", "build", "nope"},
		{"scripts", "build", "find-leaked-fixtures"},
		{"new", "script", "no-slash"},
		{"new", "script", "a/b/c"},
		{"--format", "md", "scripts", "list"},
	} {
		if _, errOut, code := run(t, append([]string{"--root", dir}, args...)...); code != fail.Usage {
			t.Errorf("%v: code=%d %s", args, code, errOut)
		}
	}
	empty := t.TempDir()
	for _, args := range [][]string{{"run", "x"}, {"scripts", "list"}, {"scripts", "build", "--all"}, {"scripts", "catalog"}, {"new", "script", "a/bcd"}} {
		if _, _, code := run(t, append([]string{"--root", empty}, args...)...); code != fail.NotInstance {
			t.Errorf("%v outside instance: %d", args, code)
		}
	}
}
