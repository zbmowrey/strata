package bench

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
)

// Protocol eval files (docs/spec/10-evals.md §2).
const (
	ProtocolDir    = ".strata/evals/protocol"
	ProtocolResult = ".strata/bench/protocol.json"
	SuiteStandard  = "standard"
	fixtureSuffix  = ".fixture" // embedded files that Go would otherwise treat specially (go.mod)
)

//go:embed all:protocol
var protocolFS embed.FS

// Scenario is one protocol eval.
type Scenario struct {
	Files        map[string]string `yaml:"files"` // written into the fixture copy before init
	ID           string            `yaml:"id"`
	Description  string            `yaml:"description"`
	Fixture      string            `yaml:"fixture"`
	Prompt       string            `yaml:"prompt"`
	Setup        []string          `yaml:"setup"`         // shell steps run in the fixture after init
	AllowedTools []string          `yaml:"allowed_tools"` // replaces DefaultAllowedTools when set
	Assert       []Assertion       `yaml:"assert"`
	Budget       Budget            `yaml:"budget"`
	Runs         int               `yaml:"runs"`
	dir          string            // where an instance scenario was loaded from
}

// Budget bounds one run.
type Budget struct {
	Timeout    string  `yaml:"timeout"` // Go duration; default 10m
	MaxTurns   int     `yaml:"max_turns"`
	MaxCostUSD float64 `yaml:"max_cost_usd"`
}

// Assertion is one check over a run; exactly one kind is set.
type Assertion struct {
	CalledWith     *CalledWith `yaml:"called_with"`
	FileCreated    *FileMatch  `yaml:"file_created"`
	MaxCalls       *MaxCalls   `yaml:"max_calls"`
	Called         string      `yaml:"called"`
	NotCalled      string      `yaml:"not_called"`
	FileChanged    string      `yaml:"file_changed"`
	FileUnchanged  string      `yaml:"file_unchanged"`
	FirstOf        []string    `yaml:"first_of"`
	Before         []string    `yaml:"before"`
	CalledAny      []string    `yaml:"called_any"`
	AnswerMentions []string    `yaml:"answer_mentions"`
	GateClean      bool        `yaml:"gate_clean"`
}

// CalledWith requires a call of Tool whose primary argument matches Match
// (a regular expression).
type CalledWith struct {
	Tool  string `yaml:"tool"`
	Match string `yaml:"match"`
}

// FileMatch requires a new file matching Glob, optionally with frontmatter
// values.
type FileMatch struct {
	Frontmatter map[string]string `yaml:"frontmatter"`
	Glob        string            `yaml:"glob"`
}

// MaxCalls bounds how often the given tool patterns are called (together).
type MaxCalls struct {
	Tools []string `yaml:"tools"`
	Max   int      `yaml:"max"`
}

// LoadSuite returns the scenarios of a built-in suite, sorted by id.
func LoadSuite(name string) ([]Scenario, error) {
	if name != SuiteStandard {
		return nil, fail.New(fail.Usage, "suites: "+SuiteStandard, "unknown protocol suite %q", name)
	}
	entries, err := fs.ReadDir(protocolFS, "protocol/"+name)
	if err != nil {
		return nil, fmt.Errorf("protocol suite: %w", err)
	}
	var out []Scenario
	for _, e := range entries {
		raw, err := protocolFS.ReadFile(path.Join("protocol", name, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("protocol suite: %w", err)
		}
		sc, err := parseScenario(raw, e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	slices.SortFunc(out, func(a, b Scenario) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// LoadInstanceScenarios reads the instance's own scenarios
// (.strata/evals/protocol/*.yaml); none is not an error.
func LoadInstanceScenarios(inst *config.Instance) ([]Scenario, error) {
	dir := inst.Path(filepath.FromSlash(ProtocolDir))
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("protocol scenarios: %w", err)
	}
	slices.Sort(files)
	var out []Scenario
	for _, f := range files {
		raw, err := os.ReadFile(f) //nolint:gosec // G304: the instance's own eval files
		if err != nil {
			return nil, fmt.Errorf("protocol scenarios: %w", err)
		}
		sc, err := parseScenario(raw, f)
		if err != nil {
			return nil, err
		}
		sc.dir = dir
		out = append(out, sc)
	}
	return out, nil
}

func parseScenario(raw []byte, name string) (Scenario, error) {
	var sc Scenario
	if err := yaml.Unmarshal(raw, &sc); err != nil {
		return sc, fail.Wrap(fail.Usage, "fix the scenario file", fmt.Errorf("scenario %s: %w", name, err))
	}
	switch {
	case sc.ID == "":
		return sc, fail.New(fail.Usage, "add `id:`", "scenario %s has no id", name)
	case sc.Prompt == "":
		return sc, fail.New(fail.Usage, "add `prompt:`", "scenario %s has no prompt", sc.ID)
	case len(sc.Assert) == 0:
		return sc, fail.New(fail.Usage, "add `assert:`", "scenario %s has no assertions", sc.ID)
	}
	if sc.Fixture == "" {
		sc.Fixture = "fixtures/basic"
	}
	return sc, nil
}

// Select filters scenarios by id (all when ids is empty).
func Select(all []Scenario, ids []string) ([]Scenario, error) {
	if len(ids) == 0 {
		return all, nil
	}
	var out []Scenario
	for _, id := range ids {
		i := slices.IndexFunc(all, func(s Scenario) bool { return s.ID == id })
		if i < 0 {
			known := make([]string, 0, len(all))
			for _, s := range all {
				known = append(known, s.ID)
			}
			return nil, fail.New(fail.Usage, "scenarios: "+strings.Join(known, ", "), "unknown scenario %q", id)
		}
		out = append(out, all[i])
	}
	return out, nil
}

// fixtureFS resolves a scenario's fixture: a directory next to an instance
// scenario, else a fixture embedded in the engine.
func (sc *Scenario) fixtureFS() (fs.FS, error) {
	if sc.dir != "" {
		p := filepath.Join(sc.dir, filepath.FromSlash(sc.Fixture))
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return os.DirFS(p), nil
		}
	}
	sub, err := fs.Sub(protocolFS, path.Join("protocol", sc.Fixture))
	if err != nil {
		return nil, fmt.Errorf("fixture %s: %w", sc.Fixture, err)
	}
	if _, err := fs.Stat(sub, "."); err != nil || !hasEntries(sub) {
		return nil, fail.New(fail.Usage, "built-in fixtures: fixtures/basic", "scenario %s: unknown fixture %q", sc.ID, sc.Fixture)
	}
	return sub, nil
}

func hasEntries(f fs.FS) bool {
	entries, err := fs.ReadDir(f, ".")
	return err == nil && len(entries) > 0
}

// copyFixture writes a fixture tree into dir (stripping fixtureSuffix).
func copyFixture(src fs.FS, dir string) error {
	err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, filepath.FromSlash(strings.TrimSuffix(p, fixtureSuffix)))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o750)
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err //nolint:wrapcheck // wrapped once after WalkDir
		}
		return os.WriteFile(dst, b, 0o600) //nolint:gosec // G703: destination is our own temp copy
	})
	if err != nil {
		return fmt.Errorf("copy fixture: %w", err)
	}
	return nil
}

// writeFiles writes a scenario's extra files (paths are slash-relative and
// must stay inside dir).
func writeFiles(dir string, files map[string]string) error {
	for rel, content := range files {
		clean := path.Clean(rel)
		if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			return errors.New("scenario file path escapes the fixture: " + rel)
		}
		p := filepath.Join(dir, filepath.FromSlash(clean))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			return fmt.Errorf("scenario file: %w", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			return fmt.Errorf("scenario file: %w", err)
		}
	}
	return nil
}
