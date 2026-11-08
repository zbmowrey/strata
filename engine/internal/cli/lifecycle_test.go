package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func TestInitUpgradeDoctorPerms(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "hooks"), 0o750); err != nil {
		t.Fatal(err)
	}
	out, _, code := run(t, "--root", dir, "init", "--dry-run", "--profile", "docs")
	if code != 0 || !strings.Contains(out, "create  .strata/config.toml") {
		t.Fatalf("dry run: %d %q", code, out)
	}
	out, _, code = run(t, "--root", dir, "init", "--profile", "docs", "--format", "json")
	var res struct {
		Changes []struct{ Path, Action string } `json:"changes"`
		V       int                             `json:"v"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res.V != 1 || len(res.Changes) == 0 {
		t.Fatalf("init json: %d %q", code, out)
	}
	_, errOut, code := run(t, "--root", dir, "init", "--profile", "docs")
	if code != 0 || !strings.Contains(errOut, "kept 8 existing files you own") {
		t.Fatalf("re-init: %d %q", code, errOut)
	}
	if _, _, code := run(t, "--root", dir, "init", "--profile", "cobol"); code != fail.Usage {
		t.Fatalf("bad profile code %d", code)
	}
	testutil.WriteFile(t, dir, "CLAUDE.md", "mine\n")
	out, _, code = run(t, "--root", dir, "upgrade", "--dry-run")
	if code != 0 || !strings.Contains(out, "update  CLAUDE.md (protocol block)") {
		t.Fatalf("upgrade dry: %d %q", code, out)
	}
	if out, _, code = run(t, "--root", dir, "upgrade"); code != 0 || out != "CLAUDE.md\n" {
		t.Fatalf("upgrade: %d %q", code, out)
	}
	if out, _, code = run(t, "--root", dir, "perms", "sync"); code != 0 || out != "" {
		t.Fatalf("perms sync: %d %q", code, out)
	}
	out, _, code = run(t, "--root", dir, "doctor", "--format", "json")
	if code != 0 || !strings.Contains(out, `"name":"managed"`) {
		t.Fatalf("doctor: %d %q", code, out)
	}
	testutil.WriteFile(t, dir, "knowledge/a/b/bad.md", "---\nid: bad\n---\n")
	if out, _, code = run(t, "--root", dir, "doctor"); code != fail.Blocking || !strings.Contains(out, "✗ lint") {
		t.Fatalf("doctor fail: %d %q", code, out)
	}
}

func TestHookCommand(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	var out, errb bytes.Buffer
	stdin := `{"session_id":"cli","cwd":"` + dir + `","tool_input":{"file_path":"` + filepath.Join(dir, "knowledge", "x.md") + `"}}`
	code := Main([]string{"hook", "post-edit"}, Streams{In: strings.NewReader(stdin), Out: &out, Err: &errb})
	if code != 0 {
		t.Fatalf("post-edit on missing file: %d %q", code, errb.String())
	}
	errb.Reset()
	code = Main([]string{"hook", "stop", "--subagent"}, Streams{In: strings.NewReader(stdin), Out: &out, Err: &errb})
	if code != 0 || strings.Contains(errb.String(), "strata:") {
		t.Fatalf("stop: %d %q", code, errb.String())
	}
	if _, errOut, code := run(t, "hook", "bogus"); code != fail.Usage || !strings.Contains(errOut, "unknown hook event") {
		t.Fatalf("bogus: %d %q", code, errOut)
	}
}
