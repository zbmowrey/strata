// Package garden is the deterministic phase of the gardener
// (docs/spec/07-gardener.md §2): mechanical fixes applied directly, and
// analyses that produce a plan of judgment tasks (.strata/garden/plan.json).
// It never calls a model and never reads knowledge/_private/.
package garden

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/fsutil"
)

// PlanFile is where the plan is written.
const PlanFile = ".strata/garden/plan.json"

// Risk levels; Question means the task becomes a question for a human.
const (
	Low      = "low"
	Medium   = "medium"
	High     = "high"
	Question = "question"
)

// Task types in plan order (docs/spec/07-gardener.md §2).
const (
	FileInbox        = "file-inbox"
	MergeDuplicates  = "merge-duplicates"
	SplitOversize    = "split-oversize"
	FixOrphan        = "fix-orphan"
	ReverifyStale    = "reverify-stale"
	ArchiveCold      = "archive-cold"
	FillGap          = "fill-gap"
	DistillProject   = "distill-project"
	Promote          = "promote"
	Scriptify        = "scriptify"
	FixBrokenLink    = "fix-broken-link"
	ResolveConflict  = "resolve-conflict"
	ApplyAnswer      = "apply-answer"
	StaleProject     = "stale-project"
	DeprecatedScript = "deprecated-script-removal"
	BenchGrow        = "bench-grow"
)

// TaskTypes lists every task type in plan order.
var TaskTypes = []string{
	FileInbox, MergeDuplicates, SplitOversize, FixOrphan, ReverifyStale, ArchiveCold, FillGap, DistillProject,
	Promote, Scriptify, FixBrokenLink, ResolveConflict, ApplyAnswer, StaleProject, DeprecatedScript, BenchGrow,
}

// Task is one unit of judgment work.
type Task struct {
	Evidence        map[string]any `json:"evidence"`
	ID              string         `json:"id"`
	Type            string         `json:"type"`
	Risk            string         `json:"risk"`
	SuggestedAction string         `json:"suggested_action"`
	Targets         []string       `json:"targets"`
	Escalate        bool           `json:"escalate,omitempty"` // [garden] escalate lists this type
}

// Fix is one applied (or, with --plan, pending) mechanical fix.
type Fix struct {
	Kind  string   `json:"kind"`
	Note  string   `json:"note,omitempty"`
	Paths []string `json:"paths,omitempty"`
}

// Plan is the deterministic phase's output contract.
type Plan struct {
	Counts    map[string]int `json:"counts"`
	Generated string         `json:"generated"` // YYYY-MM-DD
	Fixes     []Fix          `json:"fixes"`
	Tasks     []Task         `json:"tasks"`
	Warnings  []string       `json:"warnings,omitempty"`
	V         int            `json:"v"`
	Applied   bool           `json:"applied"` // fixes were applied (false: reported only)
}

// newTask builds a task with a stable id: `<type>:<targets joined by +>`.
func newTask(typ, risk, action string, targets []string, evidence map[string]any) Task {
	if evidence == nil {
		evidence = map[string]any{}
	}
	return Task{ID: typ + ":" + strings.Join(targets, "+"), Type: typ, Risk: risk, SuggestedAction: action, Targets: targets, Evidence: evidence}
}

// withID qualifies a task id (several tasks of one type on one target).
func (t Task) withID(qualifier string) Task {
	t.ID += "#" + qualifier
	return t
}

// finish sorts tasks deterministically, de-duplicates ids, applies the
// escalate list and fills counts.
func (p *Plan) finish(escalate []string) {
	order := map[string]int{}
	for i, t := range TaskTypes {
		order[t] = i
	}
	slices.SortStableFunc(p.Tasks, func(a, b Task) int {
		return cmp.Or(cmp.Compare(order[a.Type], order[b.Type]), cmp.Compare(a.ID, b.ID))
	})
	p.Tasks = slices.CompactFunc(p.Tasks, func(a, b Task) bool { return a.ID == b.ID })
	p.Counts = map[string]int{}
	for i := range p.Tasks {
		if slices.Contains(escalate, p.Tasks[i].Type) {
			p.Tasks[i].Escalate = true
			p.Tasks[i].Risk = Question
		}
		p.Counts[p.Tasks[i].Type]++
	}
	if p.Fixes == nil {
		p.Fixes = []Fix{}
	}
	if p.Tasks == nil {
		p.Tasks = []Task{}
	}
}

// write stores the plan as indented JSON.
func (p *Plan) write(path string) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err //nolint:wrapcheck // marshal of a plain struct cannot fail in practice
	}
	return fsutil.WriteAtomic(path, append(b, '\n'), 0o644)
}
