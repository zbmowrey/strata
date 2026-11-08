// Package gates runs deterministic quality gates — formatters, linters,
// scanners, tests and built-in checks — at the edit, stop, commit and ci
// stages, normalizes their output into findings, and applies ratcheting
// baselines (docs/spec/06-gates.md).
package gates

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/pelletier/go-toml/v2"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
)

// File is the gate configuration file relative to the instance root.
const File = ".strata/gates.toml"

// Stages, in escalation order.
const (
	StageEdit   = "edit"
	StageStop   = "stop"
	StageCommit = "commit"
	StageCI     = "ci"
)

// Stages lists every stage.
var Stages = []string{StageEdit, StageStop, StageCommit, StageCI}

// Modes and scopes.
const (
	ModeCheck    = "check"
	ModeFix      = "fix"
	ScopeFile    = "file"
	ScopePackage = "package"
	ScopeRepo    = "repo"
)

// Built-in gate implementations (no external tool).
const (
	BuiltinSuppressions = "suppressions"
	BuiltinLint         = "strata-lint"
)

var idRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Gate is one tool invocation with a contract (docs/spec/06-gates.md §1).
//
// Beyond the spec's keys: check_cmd (the non-modifying variant of a fix
// gate, used when not fixing), workdir (run directory; targets are made
// relative to it), repo_target ({targets} for scope=repo, default "."),
// config_files (their contents key the results cache), builtin
// (suppressions | strata-lint), lines_regex (for parser=lines) and
// severity (default severity for parsers that report none).
type Gate struct {
	Blocking    *bool    `toml:"blocking" json:"blocking,omitempty"`
	ID          string   `toml:"id" json:"id"`
	Desc        string   `toml:"desc" json:"desc,omitempty"`
	Mode        string   `toml:"mode" json:"mode,omitempty"`
	Scope       string   `toml:"scope" json:"scope,omitempty"`
	Timeout     string   `toml:"timeout" json:"timeout,omitempty"`
	Parser      string   `toml:"parser" json:"parser,omitempty"`
	LinesRegex  string   `toml:"lines_regex" json:"lines_regex,omitempty"`
	Baseline    string   `toml:"baseline" json:"baseline,omitempty"`
	Docs        string   `toml:"docs" json:"docs,omitempty"`
	Workdir     string   `toml:"workdir" json:"workdir,omitempty"`
	Module      string   `toml:"module" json:"module,omitempty"` // e.g. go.mod: run once per nearest module root
	RepoTarget  string   `toml:"repo_target" json:"repo_target,omitempty"`
	Builtin     string   `toml:"builtin" json:"builtin,omitempty"`
	Reason      string   `toml:"reason" json:"reason,omitempty"`
	Severity    string   `toml:"severity" json:"severity,omitempty"`
	AppliesTo   []string `toml:"applies_to" json:"applies_to,omitempty"`
	Exclude     []string `toml:"exclude" json:"exclude,omitempty"`
	Stages      []string `toml:"stages" json:"stages,omitempty"`
	Cmd         []string `toml:"cmd" json:"cmd,omitempty"`
	EditCmd     []string `toml:"edit_cmd" json:"edit_cmd,omitempty"`
	CheckCmd    []string `toml:"check_cmd" json:"check_cmd,omitempty"`
	ConfigFiles []string `toml:"config_files" json:"config_files,omitempty"`
	Disabled    bool     `toml:"disabled" json:"disabled,omitempty"`
}

// IsBlocking reports whether findings of this gate block (default true).
func (g *Gate) IsBlocking() bool { return g.Blocking == nil || *g.Blocking }

// InStage reports whether the gate runs at stage.
func (g *Gate) InStage(stage string) bool { return slices.Contains(g.Stages, stage) }

// Matches reports whether a repo-relative slash path is in the gate's scope.
func (g *Gate) Matches(path string) bool {
	if g.Workdir != "" && !strings.HasPrefix(path, g.Workdir+"/") {
		return false
	}
	in := len(g.AppliesTo) == 0
	for _, p := range g.AppliesTo {
		if ok, _ := doublestar.Match(p, path); ok {
			in = true
			break
		}
	}
	if !in {
		return false
	}
	for _, p := range g.Exclude {
		if ok, _ := doublestar.Match(p, path); ok {
			return false
		}
	}
	return true
}

// Config is the parsed gates.toml.
type Config struct {
	Gates []Gate `toml:"gate"`
}

// Get returns the gate with id.
func (c *Config) Get(id string) (*Gate, bool) {
	for i := range c.Gates {
		if c.Gates[i].ID == id {
			return &c.Gates[i], true
		}
	}
	return nil, false
}

// Load reads gates.toml from the instance (a missing file is an empty config).
func Load(inst *config.Instance) (*Config, error) {
	raw, err := os.ReadFile(inst.Path(filepath.FromSlash(File))) //nolint:gosec // G304: the instance's own gate config
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read gates: %w", err)
	}
	return Parse(raw)
}

const configHint = "fix .strata/gates.toml (see docs/spec/06-gates.md §1)"

// Parse decodes and validates gate configuration, applying defaults.
func Parse(raw []byte) (*Config, error) {
	var c Config
	dec := toml.NewDecoder(bytes.NewReader(raw)).DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		var de *toml.DecodeError
		if errors.As(err, &de) {
			row, col := de.Position()
			return nil, fail.New(fail.NotInstance, configHint, "gates.toml:%d:%d: %s", row, col, de.Error())
		}
		return nil, fail.Wrap(fail.NotInstance, configHint, fmt.Errorf("gates.toml: %w", err))
	}
	for i := range c.Gates {
		c.Gates[i].defaults()
	}
	if err := c.Validate(); err != nil {
		return nil, fail.Wrap(fail.NotInstance, configHint, err)
	}
	return &c, nil
}

func (g *Gate) defaults() {
	if g.Mode == "" {
		g.Mode = ModeCheck
	}
	if g.Scope == "" {
		g.Scope = ScopeFile
	}
	if g.Parser == "" {
		g.Parser = ParserExitcode
	}
	if g.Timeout == "" {
		g.Timeout = "60s"
	}
	if g.RepoTarget == "" {
		g.RepoTarget = "."
	}
	if len(g.Stages) == 0 {
		g.Stages = []string{StageStop, StageCommit, StageCI}
	}
	g.Workdir = strings.Trim(filepath.ToSlash(g.Workdir), "/")
	if g.Workdir == "." {
		g.Workdir = ""
	}
}

// Validate checks every gate.
func (c *Config) Validate() error {
	var errs []error
	seen := map[string]bool{}
	for i := range c.Gates {
		g := &c.Gates[i]
		if err := g.validate(); err != nil {
			errs = append(errs, fmt.Errorf("gate %q: %w", g.ID, err))
		}
		if seen[g.ID] {
			errs = append(errs, fmt.Errorf("gate %q defined twice", g.ID))
		}
		seen[g.ID] = true
	}
	return errors.Join(errs...)
}

func (g *Gate) validate() error {
	var errs []error
	if !idRE.MatchString(g.ID) {
		errs = append(errs, errors.New("id must be lowercase kebab-case"))
	}
	if g.Builtin != "" && g.Builtin != BuiltinSuppressions && g.Builtin != BuiltinLint {
		errs = append(errs, fmt.Errorf("unknown builtin %q", g.Builtin))
	}
	if g.Builtin == "" && len(g.Cmd) == 0 {
		errs = append(errs, errors.New("cmd is required (or builtin)"))
	}
	errs = append(errs, g.validateEnums()...)
	if _, err := config.ParseDuration(g.Timeout); err != nil {
		errs = append(errs, err)
	}
	if g.Disabled && strings.TrimSpace(g.Reason) == "" {
		errs = append(errs, errors.New("disabled gates need a reason"))
	}
	if g.LinesRegex != "" {
		if _, err := compileLines(g.LinesRegex); err != nil {
			errs = append(errs, err)
		}
	}
	for _, p := range append(slices.Clone(g.AppliesTo), g.Exclude...) {
		if !doublestar.ValidatePattern(p) {
			errs = append(errs, fmt.Errorf("invalid glob %q", p))
		}
	}
	return errors.Join(errs...)
}

// validateEnums checks mode, scope, stages and parser.
func (g *Gate) validateEnums() []error {
	var errs []error
	if g.Module != "" && g.Workdir != "" {
		errs = append(errs, errors.New("module and workdir are exclusive"))
	}
	if g.Mode != ModeCheck && g.Mode != ModeFix {
		errs = append(errs, fmt.Errorf("mode must be check|fix, got %q", g.Mode))
	}
	if g.Scope != ScopeFile && g.Scope != ScopePackage && g.Scope != ScopeRepo {
		errs = append(errs, fmt.Errorf("scope must be file|package|repo, got %q", g.Scope))
	}
	for _, s := range g.Stages {
		if !slices.Contains(Stages, s) {
			errs = append(errs, fmt.Errorf("unknown stage %q", s))
		}
	}
	if _, ok := parsers[g.Parser]; !ok {
		errs = append(errs, fmt.Errorf("unknown parser %q", g.Parser))
	}
	return errs
}
