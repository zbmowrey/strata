package garden

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
)

// HistoryFile is the local, append-only run history (docs/spec/07 §9.1).
const HistoryFile = ".strata/garden/history.jsonl"

// Run outcomes recorded in the history.
const (
	OutcomePR   = "pr"
	OutcomeTidy = "tidy"
)

// HistoryEntry is one attempt of one garden run.
type HistoryEntry struct {
	Run       string `json:"run"`
	Launcher  string `json:"launcher,omitempty"`
	Model     string `json:"model,omitempty"`
	Started   string `json:"started"`
	Ended     string `json:"ended,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
	Attempt   int    `json:"attempt"`
	PR        int    `json:"pr,omitempty"`
	TasksDone int    `json:"tasks_done,omitempty"`
	TasksLeft int    `json:"tasks_left,omitempty"`
}

// ReadHistory returns the history in file order; a missing file is empty.
// Malformed lines are skipped: the history is advisory, never fatal.
func ReadHistory(inst *config.Instance) ([]HistoryEntry, error) {
	f, err := os.Open(inst.Path(filepath.FromSlash(HistoryFile)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("garden history: %w", err)
	}
	defer func() { _ = f.Close() }()
	var out []HistoryEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e HistoryEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Run != "" {
			out = append(out, e)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("garden history: %w", err)
	}
	return out, nil
}

// lastFinished is the end time of the latest run that finished (a PR or a
// tidy garden); ok is false when no run ever finished.
func lastFinished(h []HistoryEntry) (time.Time, bool) {
	var last time.Time
	for _, e := range h {
		if e.Outcome != OutcomePR && e.Outcome != OutcomeTidy {
			continue
		}
		if t, err := time.Parse(time.RFC3339, e.Ended); err == nil && t.After(last) {
			last = t
		}
	}
	return last, !last.IsZero()
}

// Debt is the cheap garden-debt snapshot (docs/spec/07 §9.1).
type Debt struct {
	Reasons         []string `json:"reasons"`
	Inbox           int      `json:"inbox"`
	InboxOldestDays int      `json:"inbox_oldest_days"`
	Tasks           int      `json:"tasks"`         // actionable tasks in the last plan
	LastRunDays     int      `json:"last_run_days"` // -1: no run has finished yet
	Due             bool     `json:"due"`
}

// ComputeDebt reads the inbox directory, the last written plan and the run
// history. It runs no analyses and makes no forge calls, so it fits the
// SessionStart budget.
func ComputeDebt(inst *config.Instance, now time.Time) (Debt, error) {
	cfg := inst.Config.Garden
	d := Debt{Reasons: []string{}, LastRunDays: -1}
	hist, err := ReadHistory(inst)
	if err != nil {
		return d, err
	}
	last, ran := lastFinished(hist)
	if ran {
		d.LastRunDays = daysBetween(last, now)
	}
	if d.Inbox, d.InboxOldestDays, err = inboxAge(inst, now); err != nil {
		return d, err
	}
	if d.Tasks, err = actionableTasks(inst, last); err != nil {
		return d, err
	}
	if d.Inbox > 0 && d.InboxOldestDays >= cfg.DebtInboxDays {
		d.Reasons = append(d.Reasons, fmt.Sprintf("inbox oldest %dd", d.InboxOldestDays))
	}
	if d.Tasks >= cfg.DebtTasks {
		d.Reasons = append(d.Reasons, fmt.Sprintf("%d tasks", d.Tasks))
	}
	// A run that never finished is left to the first two signals, so a new
	// instance is not gardened for a single fresh capture.
	if ran && d.LastRunDays >= cfg.DebtDays && d.Inbox+d.Tasks > 0 {
		d.Reasons = append(d.Reasons, fmt.Sprintf("last garden %dd ago", d.LastRunDays))
	}
	d.Due = len(d.Reasons) > 0
	return d, nil
}

// inboxAge counts public inbox items and the age in days of the oldest,
// read from the capture filename stamp (YYYYMMDD-HHMMSS-…), else mtime.
func inboxAge(inst *config.Instance, now time.Time) (n, oldestDays int, err error) {
	dir := inst.Path(filepath.FromSlash(inst.Config.Paths.Knowledge), "_inbox")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("garden debt: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		n++
		t, perr := time.ParseInLocation("20060102-150405", firstField(e.Name()), now.Location())
		if perr != nil {
			info, ierr := e.Info()
			if ierr != nil {
				continue
			}
			t = info.ModTime()
		}
		oldestDays = max(oldestDays, daysBetween(t, now))
	}
	return n, oldestDays, nil
}

// firstField returns the "YYYYMMDD-HHMMSS" prefix of a capture filename.
func firstField(name string) string {
	const stamp = len("20060102-150405")
	if len(name) < stamp {
		return name
	}
	return name[:stamp]
}

// actionableTasks counts non-escalated tasks in the last written plan,
// ignoring a plan generated before the last finished run (it is spent).
func actionableTasks(inst *config.Instance, lastRun time.Time) (int, error) {
	b, err := os.ReadFile(inst.Path(filepath.FromSlash(PlanFile)))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("garden debt: %w", err)
	}
	var p Plan
	if err := json.Unmarshal(b, &p); err != nil {
		return 0, fmt.Errorf("garden debt: read %s: %w", PlanFile, err)
	}
	if g, err := time.Parse(time.DateOnly, p.Generated); err == nil && !lastRun.IsZero() && g.Before(dayOf(lastRun)) {
		return 0, nil
	}
	n := 0
	for _, t := range p.Tasks {
		if !t.Escalate && t.Risk != Question {
			n++
		}
	}
	return n, nil
}

func dayOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func daysBetween(from, to time.Time) int {
	if to.Before(from) {
		return 0
	}
	return int(to.Sub(from).Hours() / 24)
}
