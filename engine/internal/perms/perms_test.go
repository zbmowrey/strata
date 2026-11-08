package perms

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

const userSettings = `{
  "model": "opus",
  "hooks": {
    "PostToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "my-audit.sh"}, {"type": "command", "command": "strata hook post-bash", "timeout": 9}]}
    ],
    "Notification": [{"hooks": [{"type": "command", "command": "notify-send hi"}]}]
  },
  "permissions": {"allow": ["Bash(make test:*)"], "deny": ["Bash(rm:*)"]}
}
`

func TestSyncHooksPreservesUserEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.WriteFile(t, dir, SettingsFile, userSettings)
	changed, err := SyncHooks(dir, false, false)
	if err != nil || !changed {
		t.Fatalf("first sync %v %v", changed, err)
	}
	if changed, _ := SyncHooks(dir, false, false); changed {
		t.Fatal("second sync not idempotent")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, SettingsFile))
	s := string(raw)
	for _, want := range []string{`"model": "opus"`, "my-audit.sh", "notify-send hi", `"Bash(rm:*)"`, "strata hook session-start", "strata hook stop --subagent"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	if strings.Count(s, "strata hook post-bash") != 1 || strings.Index(s, `"model"`) > strings.Index(s, `"hooks"`) {
		t.Fatalf("duplicate or reordered:\n%s", s)
	}
	if ok, err := HasHooks(dir); err != nil || !ok {
		t.Fatal("HasHooks after sync")
	}
	if dry, _ := SyncHooks(dir, true, true); !dry {
		t.Fatal("dry-run removal should report a change")
	}
	if _, err := SyncHooks(dir, true, false); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, SettingsFile))
	if strings.Contains(string(raw), "strata hook") || !strings.Contains(string(raw), "my-audit.sh") {
		t.Fatalf("removal:\n%s", raw)
	}
	if ok, _ := HasHooks(dir); ok {
		t.Fatal("HasHooks after removal")
	}
}

func TestSyncHooksFreshAndErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := SyncHooks(dir, false, false); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, SettingsFile))
	var parsed map[string]map[string][]matcherGroup
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed["hooks"]["PostToolUse"]) != 2 {
		t.Fatalf("fresh settings: %v\n%s", err, raw)
	}
	bad := t.TempDir()
	testutil.WriteFile(t, bad, SettingsFile, "[1,2]")
	if _, err := SyncHooks(bad, false, false); err == nil {
		t.Fatal("non-object settings")
	}
	testutil.WriteFile(t, bad, SettingsFile, `{"hooks": {"Stop": 5}}`)
	if _, err := SyncHooks(bad, false, false); err == nil {
		t.Fatal("malformed event")
	}
	testutil.WriteFile(t, bad, SettingsFile, `{"hooks": {"Stop": [7, {"hooks": "x"}]}}`)
	if _, err := SyncHooks(bad, false, false); err != nil {
		t.Fatalf("odd groups must be kept verbatim: %v", err)
	}
	if _, err := HasHooks(filepath.Join(bad, "nope")); err != nil {
		t.Fatal("missing settings is not an error")
	}
}

func TestAllowlistSync(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	testutil.WriteFile(t, dir, SettingsFile, userSettings)
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(inst, false); err != nil {
		t.Fatal(err)
	}
	allow := readAllow(t, dir)
	for _, want := range []string{"Bash(make test:*)", "Bash(strata search:*)", "Bash(strata run find-leaked-fixtures:*)", "Bash(strata run render-release-notes:*)"} {
		if !slices.Contains(allow, want) {
			t.Errorf("missing %s in %v", want, allow)
		}
	}
	// A script that disappears loses its entry; user entries stay.
	if err := os.RemoveAll(filepath.Join(dir, "scripts", "tooling", "normalize-csv")); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(inst, false); err != nil {
		t.Fatal(err)
	}
	allow = readAllow(t, dir)
	if slices.Contains(allow, "Bash(strata run normalize-csv:*)") || !slices.Contains(allow, "Bash(make test:*)") {
		t.Fatalf("after removal: %v", allow)
	}
	if changed, _ := Sync(inst, false); changed {
		t.Fatal("not idempotent")
	}
	testutil.WriteFile(t, dir, LockFile, "{bad")
	if _, err := Sync(inst, false); err == nil {
		t.Fatal("corrupt lock")
	}
}

func readAllow(t *testing.T, dir string) []string {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(dir, SettingsFile))
	var s struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s.Permissions.Allow
}
