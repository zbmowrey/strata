package garden

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
)

// Run state lives in the main tree, outside any worktree, so losing a
// worktree loses only uncommitted edits (docs/spec/07 §9.3).
const (
	runsDir      = ".strata/garden/runs"
	progressFile = "progress.json"
	runPlanFile  = "plan.json"
	lastFailure  = ".strata/garden/last-failure"
	summaryFile  = ".strata/garden-summary.md" // optional, written by the gardener in its worktree
)

// Task statuses in progress.json.
const (
	TaskPending    = "pending"
	TaskInProgress = "in_progress"
	TaskDone       = "done"
	TaskSkipped    = "skipped"
	TaskEscalated  = "escalated"
)

// Outcomes beyond OutcomePR and OutcomeTidy.
const (
	OutcomeStarted      = "started"
	OutcomeLost         = "lost"
	OutcomeAborted      = "aborted"
	OutcomeStuck        = "stuck"
	OutcomeFailed       = "failed"
	OutcomeFailedVerify = "failed-verify"
	OutcomeTimeout      = "timeout"
	OutcomeDeferred     = "deferred"
)

// TaskState is one task's progress.
type TaskState struct {
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
	Tries  int    `json:"tries,omitempty"`
}

// Progress is a run's task ledger, in plan order.
type Progress struct {
	Tasks map[string]*TaskState `json:"tasks"`
	Order []string              `json:"order"`
	Run   string                `json:"run"`
}

// Counts tallies tasks by status.
func (p *Progress) Counts() map[string]int {
	c := map[string]int{}
	for _, id := range p.Order {
		c[p.Tasks[id].Status]++
	}
	return c
}

// Remaining lists pending and in-progress task ids in plan order.
func (p *Progress) Remaining() []string {
	var out []string
	for _, id := range p.Order {
		if s := p.Tasks[id].Status; s == TaskPending || s == TaskInProgress {
			out = append(out, id)
		}
	}
	return out
}

// RunDir is the run's state directory in the main tree.
func RunDir(mainRoot, run string) string {
	return filepath.Join(mainRoot, filepath.FromSlash(runsDir), run)
}

// initRun stores the plan and a fresh ledger for a new run.
func initRun(mainRoot, run string, p *Plan) error {
	dir := RunDir(mainRoot, run)
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("garden run: %w", err)
	}
	if err := fsutil.WriteAtomic(filepath.Join(dir, runPlanFile), append(b, '\n'), 0o600); err != nil {
		return err //nolint:wrapcheck // fsutil errors carry the path
	}
	prog := &Progress{Run: run, Tasks: map[string]*TaskState{}}
	for _, t := range p.Tasks {
		prog.Order = append(prog.Order, t.ID)
		prog.Tasks[t.ID] = &TaskState{Status: TaskPending}
	}
	return writeProgress(mainRoot, prog)
}

// LoadProgress reads a run's ledger.
func LoadProgress(mainRoot, run string) (*Progress, error) {
	raw, err := os.ReadFile(filepath.Join(RunDir(mainRoot, run), progressFile)) //nolint:gosec // G304: engine-owned run state
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fail.New(fail.Usage, "the run predates progress tracking; finish or abort it", "no progress recorded for run %s", run)
	}
	if err != nil {
		return nil, fmt.Errorf("garden progress: %w", err)
	}
	var p Progress
	if err := json.Unmarshal(raw, &p); err != nil || p.Tasks == nil {
		return nil, fmt.Errorf("garden progress for %s is unreadable", run)
	}
	return &p, nil
}

func writeProgress(mainRoot string, p *Progress) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("garden progress: %w", err)
	}
	return fsutil.WriteAtomic(filepath.Join(RunDir(mainRoot, p.Run), progressFile), append(b, '\n'), 0o600) //nolint:wrapcheck // fsutil errors carry the path
}

// appendHistory adds one line to the run history.
func appendHistory(mainRoot string, e HistoryEntry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("garden history: %w", err)
	}
	p := filepath.Join(mainRoot, filepath.FromSlash(HistoryFile))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return fmt.Errorf("garden history: %w", err)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // G304: engine-owned history
	if err != nil {
		return fmt.Errorf("garden history: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("garden history: %w", err)
	}
	return f.Close() //nolint:wrapcheck // close of an append-only file
}

// record appends a history line for the lease's current attempt.
func record(mainRoot string, s *Session, outcome string, now time.Time, extra func(*HistoryEntry)) {
	e := HistoryEntry{
		Run: s.Run, Attempt: s.Attempt, Launcher: s.Owner.Launcher, Model: s.Owner.Model,
		Started: s.Started, Outcome: outcome,
	}
	if outcome != OutcomeStarted {
		e.Ended = now.UTC().Format(time.RFC3339)
	}
	if p, err := LoadProgress(mainRoot, s.Run); err == nil {
		c := p.Counts()
		e.TasksDone = c[TaskDone] + c[TaskSkipped] + c[TaskEscalated]
		e.TasksLeft = c[TaskPending] + c[TaskInProgress]
	}
	if extra != nil {
		extra(&e)
	}
	_ = appendHistory(mainRoot, e) // the history is advisory; never fail a run over it
}

// TaskStatuses accepted by `strata garden task`.
var TaskStatuses = []string{"start", "done", "skip", "escalate"}

// UpdateTask records a task transition for the run that owns the worktree
// at wt.Root. It heartbeats the lease and fails with fail.Fenced when the
// worktree no longer owns the run.
func UpdateTask(wt *config.Instance, id, action, note string, now time.Time) (*Progress, error) {
	status, ok := map[string]string{"start": TaskInProgress, "done": TaskDone, "skip": TaskSkipped, "escalate": TaskEscalated}[action]
	if !ok {
		return nil, fail.New(fail.Usage, "actions: "+strings.Join(TaskStatuses, ", "), "unknown task action %q", action)
	}
	f, inWorktree := worktreeFence(wt.Root)
	if !inWorktree {
		return nil, fail.New(fail.Usage, "run it with --root <garden worktree>", "not inside a garden worktree")
	}
	var out *Progress
	err := updateLease(f.Main, f.Run, f.Fence, func(s *Session) {
		s.HeartbeatAt = now.UTC().Format(time.RFC3339)
	})
	if err != nil {
		return nil, err
	}
	err = withLeaseMutex(f.Main, func() error {
		p, err := LoadProgress(f.Main, f.Run)
		if err != nil {
			return err
		}
		t, known := p.Tasks[id]
		if !known {
			return fail.New(fail.Usage, "use a task id from the run's plan.json", "unknown task %q in run %s", id, f.Run)
		}
		t.Status = status
		if note != "" {
			t.Note = note
		}
		out = p
		return writeProgress(f.Main, p)
	})
	return out, err
}

// writeLastFailure records why a run was marked stuck.
func writeLastFailure(mainRoot string, s *Session, p *Progress, reason string) {
	var b strings.Builder
	fmt.Fprintf(&b, "run %s stuck after %d attempts: %s\n", s.Run, s.Attempt, reason)
	if p != nil {
		var poison []string
		for _, id := range p.Order {
			if t := p.Tasks[id]; t.Tries >= poisonTries {
				poison = append(poison, id)
			}
		}
		if len(poison) > 0 {
			fmt.Fprintf(&b, "poison tasks: %s\n", strings.Join(poison, ", "))
		}
	}
	matches, _ := filepath.Glob(filepath.Join(RunDir(mainRoot, s.Run), "salvage-*.patch"))
	slices.Sort(matches)
	for _, m := range matches {
		fmt.Fprintf(&b, "salvage: %s\n", m)
	}
	fmt.Fprintf(&b, "resume with `strata garden resume --force`, or discard with `strata garden abort`\n")
	_ = fsutil.WriteAtomic(filepath.Join(mainRoot, filepath.FromSlash(lastFailure)), []byte(b.String()), 0o600)
}

// RenderDigest builds the PR body from the ledger and the gardener's
// optional summary (docs/spec/07 §6).
func RenderDigest(mainRoot string, s *Session) string {
	p, err := LoadProgress(mainRoot, s.Run)
	if err != nil {
		return ""
	}
	var b strings.Builder
	summary := ""
	if raw, err := os.ReadFile(filepath.Join(s.Worktree, filepath.FromSlash(summaryFile))); err == nil { //nolint:gosec // G304: the run's own worktree
		summary = strings.TrimSpace(string(raw))
	}
	// The gardener's summary usually brings its own "## Garden …" heading.
	if !strings.HasPrefix(summary, "## ") {
		fmt.Fprintf(&b, "## Garden %s\n\n", s.Run)
	}
	if summary != "" {
		b.WriteString(summary + "\n\n")
	}
	c := p.Counts()
	fmt.Fprintf(&b, "%d done · %d skipped · %d escalated · attempt %d\n", c[TaskDone], c[TaskSkipped], c[TaskEscalated], s.Attempt)
	for _, sec := range []struct{ status, title string }{
		{TaskEscalated, "Questions raised"}, {TaskDone, "Done"}, {TaskSkipped, "Skipped / deferred"},
	} {
		var lines []string
		for _, id := range p.Order {
			if t := p.Tasks[id]; t.Status == sec.status {
				line := "- `" + id + "`"
				if t.Note != "" {
					line += " — " + t.Note
				}
				lines = append(lines, line)
			}
		}
		if len(lines) > 0 {
			fmt.Fprintf(&b, "\n### %s\n%s\n", sec.title, strings.Join(lines, "\n"))
		}
	}
	return b.String()
}

// RecordOutcome appends an outcome for the current lease holder's attempt
// (the supervisor's view of how its agent ended).
func RecordOutcome(inst *config.Instance, outcome string, now time.Time, pr int) {
	s, err := LoadSession(inst)
	if err != nil {
		return
	}
	record(inst.Root, s, outcome, now, func(e *HistoryEntry) { e.PR = pr })
}

// RecordDeferred notes a launch that did not start because garden PRs
// await review; it counts toward launch_per_day.
func RecordDeferred(inst *config.Instance, now time.Time, why string) {
	stamp := now.UTC().Format(time.RFC3339)
	_ = appendHistory(inst.Root, HistoryEntry{
		Run: "deferred-" + now.Format("20060102-1504"), Launcher: "headless", Started: stamp, Ended: stamp,
		Outcome: OutcomeDeferred + ": " + why,
	})
}
