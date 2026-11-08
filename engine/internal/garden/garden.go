package garden

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/index"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
)

// Env is set to "1" in sessions the gardener starts; hooks then refuse
// writes to the private tier.
const Env = "STRATA_GARDEN"

// LockFile serializes gardener runs (one garden per repository).
const LockFile = ".strata/garden.lock"

// Options configure a deterministic run.
type Options struct {
	Now func() time.Time
	// Changed limits the mechanical fixes to what these instance-relative
	// paths can affect (the incremental SessionStart path); nil = everything.
	Changed []string
	Apply   bool // apply mechanical fixes (false: report them as pending)
	Plan    bool // run the analyses and write the plan
}

// run carries the state of one gardener invocation.
type run struct {
	ctx   context.Context
	inst  *config.Instance
	opt   Options
	plan  *Plan
	today time.Time
	data  *corpusData
	tel   []telemetry.Record
}

// Run executes the deterministic phase. With Apply it holds garden.lock
// (exit 6 when another run holds it) for the mechanical fixes.
func Run(ctx context.Context, inst *config.Instance, opt Options) (*Plan, error) {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	r := &run{ctx: ctx, inst: inst, opt: opt, today: opt.Now(), plan: &Plan{V: 1, Applied: opt.Apply}}
	r.plan.Generated = r.today.Format(time.DateOnly)
	if opt.Apply {
		lock, err := fsutil.TryLock(inst.Path(LockFile))
		if err != nil {
			return nil, fail.Wrap(fail.Locked, "another gardener run is active; `strata doctor --fix` clears a stale lock", fmt.Errorf("garden lock: %w", err))
		}
		defer func() { _ = lock.Unlock() }()
	}
	if err := r.fixes(); err != nil {
		return nil, err
	}
	if opt.Plan {
		if err := r.analyze(); err != nil {
			return nil, err
		}
	}
	r.plan.finish(inst.Config.Garden.Escalate)
	if opt.Plan {
		if err := r.plan.write(inst.Path(PlanFile)); err != nil {
			return nil, err
		}
	}
	return r.plan, nil
}

// analyze loads the corpus and telemetry once and runs every analysis.
func (r *run) analyze() error {
	var err error
	if r.data, err = loadCorpus(r.inst); err != nil {
		return err
	}
	if r.tel, err = telemetry.Read(r.inst, r.today.AddDate(-1, 0, 0)); err != nil {
		return err
	}
	for _, a := range []func() error{
		r.inbox, r.duplicates, r.oversize, r.orphans, r.stale, r.cold, r.gaps, r.projects,
		r.promotions, r.scriptify, r.brokenLinks, r.conflicts, r.answers, r.deprecatedScripts, r.benchGrow,
	} {
		if err := a(); err != nil {
			return err
		}
	}
	return nil
}

func (r *run) add(t Task) { r.plan.Tasks = append(r.plan.Tasks, t) }

func (r *run) fix(kind, note string, paths []string) {
	if len(paths) == 0 && note == "" {
		return
	}
	slices.Sort(paths)
	r.plan.Fixes = append(r.plan.Fixes, Fix{Kind: kind, Note: note, Paths: paths})
}

// refreshIndex brings the index up to date (a mechanical fix).
func (r *run) refreshIndex() error {
	ix, st, err := index.Open(r.ctx, r.inst, index.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = ix.Close() }()
	if n := st.Added + st.Changed + st.Removed; n > 0 {
		r.fix("index", fmt.Sprintf("refreshed +%d ~%d -%d", st.Added, st.Changed, st.Removed), nil)
	}
	return nil
}
