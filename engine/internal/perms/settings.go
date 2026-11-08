// Package perms manages Strata's entries in .claude/settings.json: the hook
// wiring (docs/spec/08-claude-integration.md §3) and the generated
// permission allowlist (§7). Entries Strata does not own are never touched.
package perms

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/fsutil"
)

// SettingsFile is the project settings file relative to the root.
const SettingsFile = ".claude/settings.json"

// HookPrefix identifies hook commands Strata owns.
const HookPrefix = "strata hook"

// HookSpec is one hook Strata installs.
type HookSpec struct {
	Event   string
	Matcher string
	Command string
	Timeout int
}

// Hooks are the hooks `strata init` installs (docs/spec/08 §3).
var Hooks = []HookSpec{
	{Event: "SessionStart", Command: "strata hook session-start", Timeout: 10},
	{Event: "PreToolUse", Matcher: "Edit|Write|MultiEdit|NotebookEdit", Command: "strata hook pre-edit", Timeout: 5},
	{Event: "PostToolUse", Matcher: "Edit|Write|MultiEdit|NotebookEdit", Command: "strata hook post-edit", Timeout: 30},
	{Event: "PostToolUse", Matcher: "Bash", Command: "strata hook post-bash", Timeout: 2},
	{Event: "Stop", Command: "strata hook stop", Timeout: 120},
	{Event: "SubagentStop", Command: "strata hook stop --subagent", Timeout: 120},
	{Event: "PreCompact", Command: "strata hook pre-compact", Timeout: 5},
}

type hookCmd struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

type matcherGroup struct {
	Matcher string    `json:"matcher,omitempty"`
	Hooks   []hookCmd `json:"hooks"`
}

// loadSettings reads settings.json (empty object when absent).
func loadSettings(root string) (*fsutil.Object, []byte, error) {
	raw, err := os.ReadFile(filepath.Join(root, SettingsFile)) //nolint:gosec // G304: the instance's own settings file
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("read %s: %w", SettingsFile, err)
	}
	o, err := fsutil.ParseObject(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", SettingsFile, err)
	}
	return o, raw, nil
}

// save writes settings.json when its rendering changed and reports whether
// it did.
func save(root string, o *fsutil.Object, old []byte, dryRun bool) (bool, error) {
	out, err := o.Pretty()
	if err != nil {
		return false, err
	}
	if bytes.Equal(out, old) {
		return false, nil
	}
	if dryRun {
		return true, nil
	}
	return true, fsutil.WriteAtomic(filepath.Join(root, SettingsFile), out, 0o644)
}

// SyncHooks installs Strata's hooks, replacing previous Strata entries and
// keeping every other hook. remove uninstalls them instead.
func SyncHooks(root string, remove, dryRun bool) (bool, error) {
	o, old, err := loadSettings(root)
	if err != nil {
		return false, err
	}
	hooks, err := o.Object("hooks")
	if err != nil {
		return false, fmt.Errorf("%s hooks: %w", SettingsFile, err)
	}
	events := map[string]bool{}
	for _, h := range Hooks {
		events[h.Event] = true
	}
	for _, ev := range append(hooks.Keys(), eventOrder()...) {
		if !events[ev] {
			continue
		}
		groups, err := stripOurs(hooks, ev)
		if err != nil {
			return false, err
		}
		if !remove {
			groups = append(groups, ours(ev)...)
		}
		if len(groups) == 0 {
			hooks.Delete(ev)
			continue
		}
		if err := hooks.SetValue(ev, groups); err != nil {
			return false, err
		}
	}
	if len(hooks.Keys()) == 0 {
		o.Delete("hooks")
	} else if err := o.SetObject("hooks", hooks); err != nil {
		return false, err
	}
	return save(root, o, old, dryRun)
}

func eventOrder() []string {
	var out []string
	for _, h := range Hooks {
		if len(out) == 0 || out[len(out)-1] != h.Event {
			out = append(out, h.Event)
		}
	}
	return out
}

// stripOurs returns an event's matcher groups minus Strata-owned commands
// (groups left empty are dropped). Unknown group shapes are kept verbatim.
func stripOurs(hooks *fsutil.Object, ev string) ([]json.RawMessage, error) {
	raw, ok := hooks.Get(ev)
	if !ok {
		return nil, nil
	}
	var groups []json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, fmt.Errorf("%s hooks.%s: %w", SettingsFile, ev, err)
	}
	out := make([]json.RawMessage, 0, len(groups))
	for _, g := range groups {
		obj, err := fsutil.ParseObject(g)
		if err != nil {
			out = append(out, g)
			continue
		}
		cmds, ok := obj.Get("hooks")
		var list []json.RawMessage
		if !ok || json.Unmarshal(cmds, &list) != nil {
			out = append(out, g)
			continue
		}
		kept := list[:0]
		for _, c := range list {
			var hc hookCmd
			if json.Unmarshal(c, &hc) == nil && strings.HasPrefix(hc.Command, HookPrefix) {
				continue
			}
			kept = append(kept, c)
		}
		if len(kept) == 0 {
			continue
		}
		if err := obj.SetValue("hooks", kept); err != nil {
			return nil, err
		}
		b, _ := obj.MarshalJSON()
		out = append(out, b)
	}
	return out, nil
}

func ours(ev string) []json.RawMessage {
	var out []json.RawMessage
	for _, h := range Hooks {
		if h.Event != ev {
			continue
		}
		b, _ := json.Marshal(matcherGroup{Matcher: h.Matcher, Hooks: []hookCmd{{Type: "command", Command: h.Command, Timeout: h.Timeout}}})
		out = append(out, b)
	}
	return out
}

// HasHooks reports whether settings.json wires every Strata hook.
func HasHooks(root string) (bool, error) {
	o, _, err := loadSettings(root)
	if err != nil {
		return false, err
	}
	raw, _ := o.Get("hooks")
	for _, h := range Hooks {
		if !bytes.Contains(raw, []byte(`"`+h.Command+`"`)) {
			return false, nil
		}
	}
	return true, nil
}
