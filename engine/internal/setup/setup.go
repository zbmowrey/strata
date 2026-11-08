// Package setup implements `strata init` and `strata upgrade`: it creates
// the instance layout and keeps Strata's managed pieces (CLAUDE.md block,
// .gitignore blocks, hook wiring, allowlist, hubs) current without touching
// user-owned content (docs/spec/09-cli.md §3, 01-architecture.md §3).
package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
)

// Change actions.
const (
	Create = "create"
	Update = "update"
	Skip   = "skip"
)

// Change is one planned or applied change.
type Change struct {
	Path   string `json:"path"`
	Action string `json:"action"`
	Note   string `json:"note,omitempty"`
}

// Result lists the changes of an init/upgrade run.
type Result struct {
	Changes  []Change `json:"changes"`
	Warnings []string `json:"warnings,omitempty"`
}

// Changed returns the paths created or updated.
func (r *Result) Changed() []string {
	var out []string
	for _, c := range r.Changes {
		if c.Action != Skip {
			out = append(out, c.Path)
		}
	}
	return out
}

// Options configure init and upgrade.
type Options struct {
	Now      func() time.Time
	Version  string   // engine version (CI template pin)
	Profiles []string // languages; empty = detected
	CI       bool
	Scripts  bool
	NoHooks  bool
	DryRun   bool
}

// run accumulates changes for one invocation.
type run struct {
	ctx  context.Context
	inst *config.Instance
	opt  Options
	res  *Result
}

func (r *run) note(path, action, note string) {
	r.res.Changes = append(r.res.Changes, Change{Path: path, Action: action, Note: note})
}

// createFile writes content to rel unless it exists (user-owned files are
// never overwritten).
func (r *run) createFile(rel string, content []byte) error {
	p := r.inst.Path(filepath.FromSlash(rel))
	if _, err := os.Stat(p); err == nil {
		r.note(rel, Skip, "exists")
		return nil
	}
	r.note(rel, Create, "")
	if r.opt.DryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", rel, err)
	}
	return fsutil.WriteAtomic(p, content, 0o644)
}

// ensureBlock creates or refreshes a managed block inside rel.
func (r *run) ensureBlock(rel, what string, m fsutil.Markers, body string) error {
	p := r.inst.Path(filepath.FromSlash(rel))
	raw, err := os.ReadFile(p) //nolint:gosec // G304: instance-owned file
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", rel, err)
	}
	out, changed := fsutil.ReplaceBlock(raw, m, body)
	if !changed {
		r.note(rel, Skip, what+" current")
		return nil
	}
	action := Update
	if raw == nil {
		action = Create
	}
	r.note(rel, action, what)
	if r.opt.DryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", rel, err)
	}
	return fsutil.WriteAtomic(p, out, 0o644)
}

// hasPrefixLine reports whether any line of raw starts with prefix.
func hasPrefixLine(raw []byte, prefix string) bool {
	for l := range bytes.SplitSeq(raw, []byte("\n")) {
		if strings.HasPrefix(string(l), prefix) {
			return true
		}
	}
	return false
}
