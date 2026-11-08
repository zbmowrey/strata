package perms

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/scripts"
)

// LockFile records which allowlist entries Strata owns.
const LockFile = ".strata/perms.lock"

// Always are the allowlisted read and capture commands (docs/spec/08 §7).
// Never allowlisted: network/destructive scripts, `strata garden`,
// `strata gates baseline --accept`.
var Always = []string{
	"Bash(strata search:*)", "Bash(strata show:*)", "Bash(strata context:*)", "Bash(strata related:*)",
	"Bash(strata map:*)", "Bash(strata capture:*)", "Bash(strata check:*)", "Bash(strata lint:*)",
	"Bash(strata resume:*)", "Bash(strata project:*)", "Bash(strata questions list:*)",
}

// Allowlist computes the entries Strata wants allowed.
func Allowlist(inst *config.Instance) ([]string, error) {
	ids, err := scripts.AllowListable(inst)
	if err != nil {
		return nil, err
	}
	out := slices.Clone(Always)
	for _, id := range ids {
		out = append(out, "Bash(strata run "+id+":*)")
	}
	return out, nil
}

// Sync rewrites Strata's allowlist entries in settings.json: entries it
// owned before (per perms.lock) are replaced; user entries are kept.
func Sync(inst *config.Instance, dryRun bool) (bool, error) {
	want, err := Allowlist(inst)
	if err != nil {
		return false, err
	}
	owned, err := readLock(inst.Root)
	if err != nil {
		return false, err
	}
	o, old, err := loadSettings(inst.Root)
	if err != nil {
		return false, err
	}
	permsObj, err := o.Object("permissions")
	if err != nil {
		return false, fmt.Errorf("%s permissions: %w", SettingsFile, err)
	}
	var allow []string
	if raw, ok := permsObj.Get("allow"); ok {
		if err := json.Unmarshal(raw, &allow); err != nil {
			return false, fmt.Errorf("%s permissions.allow: %w", SettingsFile, err)
		}
	}
	kept := slices.DeleteFunc(slices.Clone(allow), func(e string) bool { return slices.Contains(owned, e) || slices.Contains(want, e) })
	merged := slices.Concat(kept, want)
	if err := permsObj.SetValue("allow", merged); err != nil {
		return false, err
	}
	if err := o.SetObject("permissions", permsObj); err != nil {
		return false, err
	}
	changed, err := save(inst.Root, o, old, dryRun)
	if err != nil || dryRun {
		return changed, err
	}
	return changed, writeLock(inst.Root, want)
}

func readLock(root string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(root, LockFile)) //nolint:gosec // G304: engine-owned file
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", LockFile, err)
	}
	var owned []string
	if err := json.Unmarshal(raw, &owned); err != nil {
		return nil, fmt.Errorf("%s: %w", LockFile, err)
	}
	return owned, nil
}

func writeLock(root string, entries []string) error {
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("perms lock: %w", err)
	}
	return fsutil.WriteAtomic(filepath.Join(root, LockFile), append(b, '\n'), 0o644)
}
