// Package scripts implements the Scriptorium (docs/spec/04-scriptorium.md):
// manifest loading, scaffolding, build cache, catalogs, and the policy-
// enforcing run wrapper.
package scripts

import (
	"cmp"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/corpus"
	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/finding"
	"github.com/zbmowrey/strata/engine/internal/schema"
)

// Side-effect classes, least to most impactful.
const (
	ReadOnly    = "read-only"
	WritesLocal = "writes-local"
	Network     = "network"
	Destructive = "destructive"
)

// Runtimes with dedicated handling.
const (
	RuntimeGo     = "go"
	RuntimePython = "python"
)

// DefaultTimeout applies when a manifest's timeout is missing or invalid.
const DefaultTimeout = 60 * time.Second

// Script is a loaded manifest.
type Script struct {
	Meta          *doc.Meta
	ID            string
	Domain        string
	Dir           string // instance-relative slash path of the script directory
	Entry         string
	Runtime       string
	Status        string
	ReplacedBy    string
	Summary       string
	Usage         string
	SideEffect    string
	Writes        []string
	NetworkHosts  []string
	Requires      []string
	Env           []string
	Tags          []string
	Timeout       time.Duration
	Deterministic bool
}

// fromMeta builds a Script from a parsed manifest at rel (…/script.yaml).
func fromMeta(rel string, m *doc.Meta, scriptsRoot string) Script {
	dir := path.Dir(rel)
	s := Script{
		Meta: m, ID: m.String("id"), Dir: dir, Entry: m.String("entry"), Runtime: m.String("runtime"),
		Status: m.String("status"), ReplacedBy: m.String("replaced_by"), Summary: m.String("summary"),
		Usage: m.String("usage"), SideEffect: m.String("side_effect"), Writes: m.Strings("writes"),
		NetworkHosts: m.Strings("network"), Requires: m.Strings("requires"), Env: m.Strings("env"),
		Tags: m.Strings("tags"), Timeout: DefaultTimeout, Deterministic: m.String("deterministic") == "true",
	}
	if s.ID == "" {
		s.ID = path.Base(dir)
	}
	if parts := strings.Split(strings.TrimPrefix(dir, scriptsRoot+"/"), "/"); len(parts) >= 2 {
		s.Domain = parts[0]
	}
	if d, err := config.ParseDuration(m.String("timeout")); err == nil && d.Seconds > 0 {
		s.Timeout = d.D()
	}
	return s
}

// Load reads every script manifest of the instance, sorted by id. Manifests
// that fail to parse are skipped (lint reports them).
func Load(inst *config.Instance) ([]Script, error) {
	layout := corpus.LayoutOf(inst.Config.Paths)
	files, err := corpus.Walk(inst.Root, layout)
	if err != nil {
		return nil, fmt.Errorf("walk scripts: %w", err)
	}
	var out []Script
	for _, f := range files {
		if f.Kind != corpus.Script {
			continue
		}
		raw, err := os.ReadFile(f.Abs)
		if err != nil {
			continue
		}
		m, err := doc.ParseYAML(raw)
		if err != nil {
			continue
		}
		out = append(out, fromMeta(f.Rel, m, layout.Scripts))
	}
	slices.SortFunc(out, func(a, b Script) int { return cmp.Or(cmp.Compare(a.ID, b.ID), cmp.Compare(a.Dir, b.Dir)) })
	return out, nil
}

// Find returns the script with id.
func Find(inst *config.Instance, id string) (*Script, error) {
	all, err := Load(inst)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, fail.New(fail.Usage, "list scripts with `strata scripts list` or `strata search '<need> kind:script'`", "no script with id %q", id)
}

// Validate lints one script's manifest with the compiled schema.
func Validate(inst *config.Instance, s *Script) []finding.Finding {
	return schema.Manifest(s.Dir+"/script.yaml", s.Meta, nil, inst.Config.Knowledge.MaxSummary)
}

// AllowListable returns the ids of active scripts whose side effect may be
// auto-allowed (read-only, writes-local) — the input of `strata perms sync`
// (docs/spec/08-claude-integration.md §7).
func AllowListable(inst *config.Instance) ([]string, error) {
	all, err := Load(inst)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, s := range all {
		if (s.SideEffect == ReadOnly || s.SideEffect == WritesLocal) && s.Status != "deprecated" && schema.IDPattern.MatchString(s.ID) {
			ids = append(ids, s.ID)
		}
	}
	return slices.Compact(ids), nil
}

// AbsDir is the script directory on disk.
func (s *Script) AbsDir(root string) string {
	return filepath.Join(root, filepath.FromSlash(s.Dir))
}
