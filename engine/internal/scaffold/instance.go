// Package scaffold holds the go:embed templates used by init, upgrade and
// the scaffolding commands (docs/spec/01-architecture.md §2).
package scaffold

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
)

//go:embed all:templates/instance
var instanceTemplates embed.FS

// ClaudeBlockVersion is bumped when the protocol block changes; `strata
// upgrade` and doctor compare it.
const ClaudeBlockVersion = "v1"

// ClaudeMarkers delimit the managed block in CLAUDE.md (docs/spec/08 §2).
var ClaudeMarkers = fsutil.Markers{
	Begin:       "<!-- strata:begin " + ClaudeBlockVersion + " — managed by `strata upgrade`; edit outside these markers -->",
	End:         "<!-- strata:end -->",
	BeginPrefix: "<!-- strata:begin",
}

// LocalMarkers delimit the .gitignore block for Strata's machine-local state.
var LocalMarkers = fsutil.Markers{
	Begin:       "# >>> strata:local (GENERATED — machine-local state; see docs/spec/01-architecture.md §3)",
	End:         "# <<< strata:local",
	BeginPrefix: "# >>> strata:local",
}

// LocalIgnores are the gitignored machine-local paths of an instance.
func LocalIgnores(p config.Paths) string {
	return "/.strata/index.db*\n/.strata/index.lock\n/.strata/knowledge.lock\n/.strata/garden.lock\n/.strata/garden.lock.mu\n/.strata/telemetry/\n/.strata/session/\n" +
		"/.strata/cache/\n/.strata/bin/\n/.strata/garden/\n/.strata/worktrees/\n/" + p.Knowledge + "/_private/\n" +
		"/" + p.Scripts + "/.venv/\n"
}

// Vars are the values substituted into instance templates.
type Vars struct {
	Knowledge  string
	Scripts    string
	Projects   string
	MinVersion string
	Version    string
}

// VarsFor derives template values from a configuration.
func VarsFor(cfg config.Config, engineVersion string) Vars {
	return Vars{Knowledge: cfg.Paths.Knowledge, Scripts: cfg.Paths.Scripts, Projects: cfg.Paths.Projects, MinVersion: cfg.Engine.MinVersion, Version: engineVersion}
}

// Render executes the named instance template.
func Render(name string, v Vars) (string, error) {
	raw, err := instanceTemplates.ReadFile("templates/instance/" + name + ".tmpl")
	if err != nil {
		return "", fmt.Errorf("template %s: %w", name, err)
	}
	t, err := template.New(name).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("template %s: %w", name, err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, v); err != nil {
		return "", fmt.Errorf("template %s: %w", name, err)
	}
	return b.String(), nil
}

// ClaudeBlock renders the CLAUDE.md protocol block body.
func ClaudeBlock(v Vars) (string, error) { return Render("claude-block.md", v) }
