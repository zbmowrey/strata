// Package config loads, defaults and validates `.strata/config.toml`
// (docs/spec/09-cli.md §6). Every key is optional; defaults are compiled in.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/zbmowrey/strata/engine/internal/fail"
)

// Dir is the instance metadata directory; File is the config file inside it.
const (
	Dir  = ".strata"
	File = "config.toml"
)

// Config is the instance configuration.
type Config struct {
	Federation Federation `toml:"federation"`
	Engine     Engine     `toml:"engine"`
	Hooks      Hooks      `toml:"hooks"`
	Garden     Garden     `toml:"garden"`
	Telemetry  Telemetry  `toml:"telemetry"`
	Search     Search     `toml:"search"`
	Paths      Paths      `toml:"paths"`
	Projects   Projects   `toml:"projects"`
	Gates      Gates      `toml:"gates"`
	Scripts    Scripts    `toml:"scripts"`
	Knowledge  Knowledge  `toml:"knowledge"`
}

// Engine pins the minimum engine version.
type Engine struct {
	MinVersion string `toml:"min_version"`
}

// Paths locates the instance's content directories (relative to the root).
type Paths struct {
	Knowledge string   `toml:"knowledge"`
	Scripts   string   `toml:"scripts"`
	Projects  string   `toml:"projects"`
	Skills    []string `toml:"skills"`
}

// Knowledge holds shard limits and lifecycle defaults.
type Knowledge struct {
	ReviewAfter   map[string]Duration `toml:"review_after"`
	ColdAfter     Duration            `toml:"cold_after"`
	ExtraRequired []string            `toml:"extra_required"`
	MaxBodyLines  int                 `toml:"max_body_lines"`
	MaxBodyBytes  int                 `toml:"max_body_bytes"`
	MaxSummary    int                 `toml:"max_summary"`
	MaxTitle      int                 `toml:"max_title"`
	MaxSections   int                 `toml:"max_sections"`
	MaxLinks      int                 `toml:"max_links"`
}

// Search holds ranking configuration.
type Search struct {
	Weights      map[string]float64 `toml:"weights"`
	KindBoost    map[string]float64 `toml:"kind_boost"`
	Synonyms     string             `toml:"synonyms"`
	DefaultLimit int                `toml:"default_limit"`
}

// Scripts holds scriptorium settings.
type Scripts struct {
	DefaultRuntime string `toml:"default_runtime"`
	CatalogSplitAt int    `toml:"catalog_split_at"`
}

// Projects holds project lifecycle settings.
type Projects struct {
	StaleAfter     Duration `toml:"stale_after"`
	AdhocRetention Duration `toml:"adhoc_retention"`
}

// Gates holds gate-runner settings.
type Gates struct {
	EditBudget    Duration `toml:"edit_budget"`
	StopMaxBlocks int      `toml:"stop_max_blocks"`
}

// Hooks holds hook toggles.
type Hooks struct {
	StopNudge              string `toml:"stop_nudge"`
	PreEditContext         bool   `toml:"pre_edit_context"`
	SessionStartGardenAuto bool   `toml:"session_start_garden_auto"`
}

// Garden holds gardener settings.
type Garden struct {
	Forge          string   `toml:"forge"`
	BranchPrefix   string   `toml:"branch_prefix"`
	Escalate       []string `toml:"escalate"`
	BenchTolerance float64  `toml:"bench_tolerance"`
	// OrphanMinAgeDays keeps young shards out of fix-orphan (D-033).
	OrphanMinAgeDays int `toml:"orphan_min_age_days"`
	// LeaseTTL is the heartbeat age after which a run is lost (07 §9.2).
	LeaseTTL Duration `toml:"lease_ttl"`
	// Garden debt thresholds (docs/spec/07 §9.1).
	DebtInboxDays int `toml:"debt_inbox_days"`
	DebtTasks     int `toml:"debt_tasks"`
	DebtDays      int `toml:"debt_days"`
}

// Telemetry holds local telemetry settings.
type Telemetry struct {
	Retention    Duration `toml:"retention"`
	Enabled      bool     `toml:"enabled"`
	StoreQueries bool     `toml:"store_queries"`
}

// Federation lists member instances of a hub repository.
type Federation struct {
	Members []Member `toml:"members"`
}

// Member is one federated instance.
type Member struct {
	Name string `toml:"name"`
	Path string `toml:"path"`
}

// Default returns the compiled-in defaults.
func Default() Config {
	return Config{
		Engine: Engine{MinVersion: "0.1.0"},
		Paths:  Paths{Knowledge: "knowledge", Scripts: "scripts", Projects: "projects", Skills: []string{".claude/skills"}},
		Knowledge: Knowledge{
			MaxBodyLines: 120, MaxBodyBytes: 6144, MaxSummary: 160, MaxTitle: 80, MaxSections: 6, MaxLinks: 30,
			ReviewAfter: map[string]Duration{
				"fact": Days(180), "gotcha": Days(365), "howto": Days(180), "reference": Days(90),
				"standard": Days(365), "glossary": Days(365), "correction": Days(365),
			},
			ColdAfter: Days(180),
		},
		Search: Search{
			DefaultLimit: 10,
			Weights:      map[string]float64{"title": 10, "aliases": 8, "summary": 5, "tags": 4, "headings": 3, "body": 1},
			KindBoost:    map[string]float64{"shard": 1.0, "playbook": 1.0, "script": 1.0, "hub": 0.6, "project": 0.9, "inbox": 0.8, "question": 0.7},
			Synonyms:     ".strata/synonyms.txt",
		},
		Scripts:   Scripts{DefaultRuntime: "go", CatalogSplitAt: 50},
		Projects:  Projects{StaleAfter: Days(30), AdhocRetention: Days(30)},
		Gates:     Gates{StopMaxBlocks: 3, EditBudget: Duration{Seconds: 2}},
		Hooks:     Hooks{PreEditContext: true, StopNudge: "once", SessionStartGardenAuto: true},
		Garden:    Garden{Forge: "auto", BranchPrefix: "garden/", BenchTolerance: 0.02, OrphanMinAgeDays: 30, LeaseTTL: Duration{Seconds: 1800}, DebtInboxDays: 3, DebtTasks: 10, DebtDays: 7},
		Telemetry: Telemetry{Enabled: true, Retention: Days(180), StoreQueries: true},
	}
}

// Instance is a discovered instance: its root directory and configuration.
type Instance struct {
	Root     string
	Config   Config
	Warnings []string // unknown keys and other non-fatal problems
}

// Path joins elements onto the instance root.
func (i *Instance) Path(elem ...string) string {
	return filepath.Join(append([]string{i.Root}, elem...)...)
}

// Discover walks up from start to the nearest directory holding
// .strata/config.toml.
func Discover(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", start, err)
	}
	for {
		if fi, err := os.Stat(filepath.Join(dir, Dir, File)); err == nil && !fi.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fail.New(fail.NotInstance, "run `strata init` here, or pass --root", "not a strata instance (no %s/%s found above %s)", Dir, File, start)
		}
		dir = parent
	}
}

// Open discovers (when root is empty, from cwd) and loads an instance.
func Open(root, cwd string) (*Instance, error) {
	var err error
	if root == "" {
		root, err = Discover(cwd)
		if err != nil {
			return nil, err
		}
	} else if root, err = filepath.Abs(root); err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, Dir, File)) //nolint:gosec // G304: path is the instance's own config under the chosen root
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fail.New(fail.NotInstance, "run `strata init`", "not a strata instance: %s/%s missing in %s", Dir, File, root)
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg, warnings, err := Parse(raw)
	if err != nil {
		return nil, fail.Wrap(fail.NotInstance, "fix .strata/config.toml (see docs/spec/09-cli.md §6)", err)
	}
	return &Instance{Root: root, Config: cfg, Warnings: warnings}, nil
}

// Parse decodes config TOML over the defaults and validates it. Unknown keys
// are returned as warnings, not errors.
func Parse(raw []byte) (Config, []string, error) {
	cfg := Default()
	var warnings []string
	dec := toml.NewDecoder(bytes.NewReader(raw)).DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		var strict *toml.StrictMissingError
		if !errors.As(err, &strict) {
			var de *toml.DecodeError
			if errors.As(err, &de) {
				row, col := de.Position()
				return Config{}, nil, fmt.Errorf("config.toml:%d:%d: %s", row, col, de.Error())
			}
			return Config{}, nil, fmt.Errorf("config.toml: %w", err)
		}
		for _, e := range strict.Errors {
			warnings = append(warnings, "unknown config key: "+strings.Join(e.Key(), "."))
		}
		sort.Strings(warnings)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, nil, err
	}
	return cfg, warnings, nil
}

// Validate checks value ranges and enumerations.
func (c *Config) Validate() error {
	var errs []error
	positive := map[string]int{
		"knowledge.max_body_lines": c.Knowledge.MaxBodyLines, "knowledge.max_body_bytes": c.Knowledge.MaxBodyBytes,
		"knowledge.max_summary": c.Knowledge.MaxSummary, "knowledge.max_title": c.Knowledge.MaxTitle,
		"search.default_limit": c.Search.DefaultLimit, "scripts.catalog_split_at": c.Scripts.CatalogSplitAt,
		"gates.stop_max_blocks": c.Gates.StopMaxBlocks,
	}
	for _, k := range sortedKeys(positive) {
		if positive[k] <= 0 {
			errs = append(errs, fmt.Errorf("%s must be > 0", k))
		}
	}
	if !oneOf(c.Hooks.StopNudge, "once", "off") {
		errs = append(errs, fmt.Errorf("hooks.stop_nudge must be once|off, got %q", c.Hooks.StopNudge))
	}
	for _, kv := range []struct {
		k string
		v int
	}{
		{"orphan_min_age_days", c.Garden.OrphanMinAgeDays},
		{"debt_inbox_days", c.Garden.DebtInboxDays},
		{"debt_days", c.Garden.DebtDays},
	} {
		if kv.v < 0 {
			errs = append(errs, fmt.Errorf("garden.%s must be >= 0, got %d", kv.k, kv.v))
		}
	}
	if c.Garden.DebtTasks < 1 {
		errs = append(errs, fmt.Errorf("garden.debt_tasks must be >= 1, got %d", c.Garden.DebtTasks))
	}
	if !oneOf(c.Garden.Forge, "auto", "github", "forgejo") {
		errs = append(errs, fmt.Errorf("garden.forge must be auto|github|forgejo, got %q", c.Garden.Forge))
	}
	if !oneOf(c.Scripts.DefaultRuntime, "go", "python", "rust", "typescript", "bash") {
		errs = append(errs, fmt.Errorf("scripts.default_runtime unsupported: %q", c.Scripts.DefaultRuntime))
	}
	for _, p := range []string{c.Paths.Knowledge, c.Paths.Scripts, c.Paths.Projects} {
		if p == "" || filepath.IsAbs(p) || strings.HasPrefix(filepath.Clean(p), "..") {
			errs = append(errs, fmt.Errorf("paths must be relative and inside the instance, got %q", p))
		}
	}
	return errors.Join(errs...)
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
