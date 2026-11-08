package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/version"
)

const (
	checkPlugin   = "plugin"
	pluginInstall = "in Claude Code: /plugin marketplace add zbmowrey/strata, then /plugin install strata@strata"
)

// DefaultPluginsFile is Claude Code's record of installed plugins
// ($CLAUDE_CONFIG_DIR, else ~/.claude). Empty when the home is unknown.
func DefaultPluginsFile() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, "plugins", "installed_plugins.json")
}

// plugin reports whether the strata plugin is installed and matches the
// engine's release (docs/spec/09-cli.md §4). The plugin is optional — the
// hooks enforce without it — so problems are warnings. Skipped when
// Options.PluginsFile is empty.
func (d *doctor) plugin() []Check {
	if d.opt.PluginsFile == "" {
		return nil
	}
	raw, err := os.ReadFile(d.opt.PluginsFile)
	if err != nil {
		return []Check{{Name: checkPlugin, Status: Warn, Message: "strata plugin not installed (slash commands, status line, pane)", Fix: pluginInstall}}
	}
	var rec struct {
		Plugins map[string][]struct {
			Version string `json:"version"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return []Check{{Name: checkPlugin, Status: Warn, Message: fmt.Sprintf("cannot read %s: %v", d.opt.PluginsFile, err)}}
	}
	var have []string
	for key, installs := range rec.Plugins {
		if !strings.HasPrefix(key, "strata@") {
			continue
		}
		for _, in := range installs {
			have = append(have, in.Version)
		}
	}
	if len(have) == 0 {
		return []Check{{Name: checkPlugin, Status: Warn, Message: "strata plugin not installed (slash commands, status line, pane)", Fix: pluginInstall}}
	}
	engine := strings.TrimSuffix(version.Current(), "-dev")
	if !slices.Contains(have, engine) {
		slices.Sort(have)
		return []Check{{
			Name: checkPlugin, Status: Warn,
			Message: fmt.Sprintf("strata plugin %s ≠ engine %s", strings.Join(have, ", "), engine),
			Fix:     "claude plugin update strata@strata (or install the matching engine)",
		}}
	}
	return []Check{{Name: checkPlugin, Status: OK, Message: "strata plugin " + engine}}
}
