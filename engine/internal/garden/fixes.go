package garden

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/gates"
	"github.com/zbmowrey/strata/engine/internal/knowledge"
	"github.com/zbmowrey/strata/engine/internal/lint"
	"github.com/zbmowrey/strata/engine/internal/perms"
	"github.com/zbmowrey/strata/engine/internal/projects"
	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/scripts"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
)

// EmptyBaselineAge is how long an empty gate baseline is kept.
const EmptyBaselineAge = 14 * 24 * time.Hour

// fixes applies the mechanical fixes (docs/spec/07-gardener.md §2). The
// index is always refreshed; the rest only with Apply.
func (r *run) fixes() error {
	if err := r.refreshIndex(); err != nil {
		return err
	}
	if !r.opt.Apply {
		return nil
	}
	if r.opt.Changed != nil {
		return r.incremental()
	}
	for _, f := range []func() error{r.pruneTelemetry, r.regenerate, r.normalize, r.baselines} {
		if err := f(); err != nil {
			return err
		}
	}
	return nil
}

// incremental applies only the fixes the changed paths can affect: per-file
// normalization of changed shards, hubs when shards changed, catalogs and
// the allowlist when scripts changed, the projects block when a project
// manifest changed. The full pass (`garden`, `--plan`) covers the rest.
func (r *run) incremental() error {
	p := r.inst.Config.Paths
	var shards []string
	var hubs, scriptsChanged, projectsChanged bool
	for _, c := range r.opt.Changed {
		switch {
		case strings.HasPrefix(c, p.Knowledge+"/_private/") || strings.HasPrefix(c, p.Knowledge+"/_hubs/"):
		case strings.HasPrefix(c, p.Knowledge+"/") && strings.HasSuffix(c, ".md"):
			hubs = true
			if !strings.HasPrefix(c, p.Knowledge+"/_") {
				shards = append(shards, c)
			}
		case strings.HasPrefix(c, p.Scripts+"/"):
			scriptsChanged = true
		case strings.HasPrefix(c, p.Projects+"/") && path.Base(c) == "project.md":
			projectsChanged = true
		}
	}
	if err := r.normalizeFiles(shards); err != nil {
		return err
	}
	return r.regenerateSome(hubs, scriptsChanged, projectsChanged)
}

// normalizeFiles applies the per-file fixes to the given shard files.
func (r *run) normalizeFiles(rels []string) error {
	var fixed []string
	for _, rel := range rels {
		p := r.inst.Path(filepath.FromSlash(rel))
		raw, err := os.ReadFile(p) //nolint:gosec // G304: a changed public shard
		if err != nil {
			continue // removed since the refresh
		}
		out := lint.NormalizeFile(raw, true)
		if bytes.Equal(out, raw) {
			continue
		}
		if err := fsutil.WriteAtomic(p, out, 0o644); err != nil {
			return err
		}
		fixed = append(fixed, rel)
	}
	r.fix("normalize", "", fixed)
	return nil
}

func (r *run) pruneTelemetry() error {
	n, err := telemetry.Prune(r.inst, r.today)
	if err != nil {
		return err
	}
	if n > 0 {
		r.fix("telemetry", "pruned "+render.Plural(n, "event")+" past retention", nil)
	}
	return nil
}

// regenerate rewrites hubs, catalogs, the projects .gitignore block and the
// permission allowlist.
func (r *run) regenerate() error { return r.regenerateSome(true, true, true) }

func (r *run) regenerateSome(hubs, scriptsChanged, projectsChanged bool) error {
	if hubs {
		written, err := knowledge.New(r.inst, r.opt.Now, telemetry.New(r.inst, r.opt.Now)).Hubs(r.ctx)
		if err != nil {
			return err
		}
		r.fix("hubs", "", written)
	}
	if scriptsChanged {
		cats, err := scripts.WriteCatalog(r.inst)
		if err != nil {
			return err
		}
		r.fix("catalogs", "", cats)
		changed, err := perms.Sync(r.inst, false)
		if err != nil {
			return err
		}
		if changed {
			r.fix("allowlist", "", []string{perms.SettingsFile})
		}
	}
	if projectsChanged {
		changed, warnings, err := projects.Sync(r.inst)
		if err != nil {
			return err
		}
		r.plan.Warnings = append(r.plan.Warnings, warnings...)
		if changed {
			r.fix("gitignore", "projects block", []string{".gitignore"})
		}
	}
	return nil
}

// normalize runs lint's content-preserving fixes (alias links → ids,
// frontmatter order and formats, tags, whitespace) over the public tiers.
func (r *run) normalize() error {
	paths, err := publicKnowledgeRoots(r.inst.Root, r.inst.Config.Paths.Knowledge)
	if err != nil {
		return err
	}
	paths = append(paths, r.inst.Config.Paths.Projects, r.inst.Config.Paths.Scripts)
	rep, err := lint.Run(r.inst, lint.Options{Paths: paths, Fix: true})
	if err != nil {
		return err
	}
	r.fix("normalize", "", rep.Fixed)
	return nil
}

// publicKnowledgeRoots lists knowledge/ subtrees other than _private/.
func publicKnowledgeRoots(root, kdir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(kdir)))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err //nolint:wrapcheck // os errors carry the path
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "_private" {
			out = append(out, path.Join(kdir, e.Name()))
		}
	}
	return out, nil
}

// baselines deletes gate baselines that have been empty for 14 days.
func (r *run) baselines() error {
	cfg, err := gates.Load(r.inst)
	if err != nil {
		return err
	}
	var removed []string
	for _, g := range cfg.Gates {
		if g.Baseline == "" {
			continue
		}
		p := r.inst.Path(filepath.FromSlash(g.Baseline))
		fi, err := os.Stat(p)
		if err != nil || r.today.Sub(fi.ModTime()) < EmptyBaselineAge {
			continue
		}
		b, err := gates.LoadBaseline(p, g.ID)
		if err != nil || len(b.Entries) > 0 {
			continue
		}
		if os.Remove(p) == nil {
			removed = append(removed, g.Baseline)
		}
	}
	r.fix("baselines", "", removed)
	return nil
}
