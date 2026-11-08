package garden

import (
	"github.com/zbmowrey/strata/engine/internal/projects"
)

// projects: closed projects to distill, old _adhoc entries, and active
// projects without activity (a question: closing is a human decision).
func (r *run) projects() error {
	st := projects.New(r.inst, r.opt.Now)
	all, err := st.List(true)
	if err != nil {
		return err
	}
	for _, p := range all {
		switch {
		case p.Slug == projects.Adhoc:
			r.adhoc(st)
		case p.Status == projects.StatusDone && !p.Archived:
			r.add(newTask(DistillProject, Low, "distill durable learnings into shards, then archive the project",
				[]string{"project:" + p.Slug}, map[string]any{"entries": p.Entries, "last_activity": p.LastActivity}))
		case p.Status == projects.StatusActive && p.Stale:
			r.add(newTask(StaleProject, Question, "ask whether to pause, close or continue it",
				[]string{"project:" + p.Slug}, map[string]any{"last_activity": p.LastActivity, "age_days": p.AgeDays}))
		}
	}
	return nil
}

// adhoc: _adhoc entries older than adhoc_retention get distilled.
func (r *run) adhoc(st *projects.Store) {
	es, err := st.Entries(projects.Adhoc)
	if err != nil {
		return
	}
	cutoff := r.today.Add(-r.inst.Config.Projects.AdhocRetention.D())
	old := 0
	for _, e := range es {
		if e.Time.Before(cutoff) {
			old++
		}
	}
	if old > 0 {
		r.add(newTask(DistillProject, Low, "distill old _adhoc entries into shards, then drop them",
			[]string{"project:" + projects.Adhoc}, map[string]any{"old_entries": old, "retention_days": r.inst.Config.Projects.AdhocRetention.DaysCount()}))
	}
}
