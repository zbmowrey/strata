package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

// gateInstance copies instance-basic into a git repository with a gate
// that flags lines containing BAD.
func gateInstance(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := testutil.CopyDir(testutil.Fixture("instance-basic"), dir); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "find-bad")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ngrep -Hn BAD \"$@\"\nexit 0\n"), 0o700); err != nil { //nolint:gosec // G306: test tool must be executable
		t.Fatal(err)
	}
	testutil.WriteFile(t, dir, ".strata/gates.toml", `
[[gate]]
id = "bad"
desc = "no BAD markers"
applies_to = ["src/**"]
cmd = ["`+bin+`", "{targets}"]
parser = "lines"
baseline = ".strata/baselines/bad.json"
docs = "pytest-fixture-teardown"
stages = ["edit", "stop", "commit", "ci"]
[[gate]]
id = "suppressions"
builtin = "suppressions"
applies_to = ["src/**"]
blocking = false
`)
	testutil.WriteFile(t, dir, "src/a.txt", "fine\n")
	testutil.WriteFile(t, dir, ".gitignore", ".strata/cache/\n.strata/index/\n.strata/telemetry/\n")
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"commit", "-q", "-m", "init"}} {
		cmd := exec.Command("git", args...) //nolint:gosec,noctx // G204: test git setup
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestCheckCommand(t *testing.T) {
	t.Parallel()
	dir := gateInstance(t)
	if out, errOut, code := run(t, "--root", dir, "check"); code != 0 {
		t.Fatalf("clean tree: %d %s %s", code, out, errOut)
	}
	testutil.WriteFile(t, dir, "src/a.txt", "fine\nBAD here\n")
	out, errOut, code := run(t, "--root", dir, "check")
	if code != fail.Blocking || !strings.Contains(out, "strata gates ✗ 1 blocking finding (stop stage)") || !strings.Contains(out, "src/a.txt") {
		t.Fatalf("blocking: %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "stop stage") {
		t.Fatalf("status line: %s", errOut)
	}
	out, _, code = run(t, "--root", dir, "check", "--format", "json", "src/a.txt")
	var rep struct {
		Blocked  bool   `json:"blocked"`
		SetHash  string `json:"set_hash"`
		Files    int    `json:"files"`
		Blocking []struct {
			Gate string `json:"gate"`
		} `json:"blocking"`
	}
	if code != fail.Blocking || json.NewDecoder(strings.NewReader(out)).Decode(&rep) != nil || !rep.Blocked || rep.SetHash == "" || rep.Files != 1 || rep.Blocking[0].Gate != "bad" {
		t.Fatalf("json: %d %s", code, out)
	}
	// Advisory gates never block.
	testutil.WriteFile(t, dir, "src/a.txt", "x //nolint\n")
	out, _, code = run(t, "--root", dir, "check", "--all", "--no-cache")
	if code != 0 || !strings.Contains(out, "advisory (non-blocking)") {
		t.Fatalf("advisory: %d %s", code, out)
	}
	if _, _, code := run(t, "--root", dir, "check", "--staged"); code != 0 {
		t.Fatalf("staged: %d", code)
	}
	for _, args := range [][]string{{"check", "--all", "--staged"}, {"check", "--stage", "deploy"}, {"check", "--format", "jsonl"}} {
		if _, _, code := run(t, append([]string{"--root", dir}, args...)...); code != fail.Usage {
			t.Errorf("%v: code %d", args, code)
		}
	}
}

func TestGatesCommands(t *testing.T) {
	t.Parallel()
	dir := gateInstance(t)
	out, _, code := run(t, "--root", dir, "gates", "list")
	if code != 0 || !strings.Contains(out, "bad") || !strings.Contains(out, "advisory") {
		t.Fatalf("list: %d %s", code, out)
	}
	out, _, code = run(t, "--root", dir, "gates", "list", "--format", "json")
	var list struct {
		Gates []struct {
			ID string `json:"id"`
		} `json:"gates"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || len(list.Gates) != 2 {
		t.Fatalf("list json: %d %s", code, out)
	}
	out, _, code = run(t, "--root", dir, "gates", "explain", "bad/x")
	if code != 0 || !strings.Contains(out, "bad — no BAD markers") || !strings.Contains(out, "rule: x") || !strings.Contains(out, "rationale") {
		t.Fatalf("explain: %d %s", code, out)
	}
	out, _, code = run(t, "--root", dir, "gates", "explain", "suppressions")
	if code != 0 || !strings.Contains(out, "no linked shard") {
		t.Fatalf("explain builtin: %d %s", code, out)
	}
	if _, _, code := run(t, "--root", dir, "gates", "explain", "nope"); code != fail.Usage {
		t.Fatalf("explain unknown: %d", code)
	}

	testutil.WriteFile(t, dir, "src/old.txt", "BAD legacy\n")
	out, _, code = run(t, "--root", dir, "gates", "baseline", "bad")
	if code != 0 || !strings.Contains(out, "1 finding would be added") {
		t.Fatalf("baseline dry-run: %d %s", code, out)
	}
	out, _, code = run(t, "--root", dir, "gates", "baseline", "bad", "--accept")
	if code != 0 || !strings.Contains(out, "added 1 finding") {
		t.Fatalf("baseline accept: %d %s", code, out)
	}
	out, _, code = run(t, "--root", dir, "gates", "baseline", "bad")
	if code != 0 || !strings.Contains(out, "nothing to add") {
		t.Fatalf("baseline current: %d %s", code, out)
	}
	if _, _, code := run(t, "--root", dir, "check", "src/old.txt"); code != 0 {
		t.Fatalf("baselined finding blocks: %d", code)
	}
	if _, _, code := run(t, "--root", dir, "gates", "baseline", "nope"); code != fail.Usage {
		t.Fatalf("baseline unknown: %d", code)
	}

	testutil.WriteFile(t, dir, ".strata/gates.toml", "[[gate]]\nid = \"x\"\n")
	if _, _, code := run(t, "--root", dir, "gates", "list"); code != fail.NotInstance {
		t.Fatalf("invalid gates.toml: %d", code)
	}
}
