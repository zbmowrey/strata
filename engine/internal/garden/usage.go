package garden

import (
	"os"
	"slices"
	"strings"
	"time"
)

// Telemetry-driven thresholds (docs/spec/07-gardener.md §2, §4).
const (
	GapRepeats        = 2  // zero-result searches of one query within GapWindow
	GapWindow         = 30 // days
	ScriptifyRepeats  = 3  // same normalized bash command …
	ScriptifySessions = 2  // … across this many sessions
	CoAccessSessions  = 3  // sessions showing the same ≥ 3 shards together
	DeprecatedIdle    = 90 // days without runs before a deprecated script is removed
)

func normQuery(q string) string { return strings.Join(strings.Fields(strings.ToLower(q)), " ") }

// gapStats groups recent search events by normalized query.
type gapStat struct {
	zero, hits int
	lastZero   time.Time
	lastHit    time.Time
}

func (r *run) searchStats() map[string]*gapStat {
	out := map[string]*gapStat{}
	for _, e := range r.tel {
		q := normQuery(e.Str("query"))
		if e.Name != "search" || q == "" {
			continue
		}
		s := out[q]
		if s == nil {
			s = &gapStat{}
			out[q] = s
		}
		if zero, _ := e.Fields["zero"].(bool); zero {
			s.zero++
			s.lastZero = e.Time
		} else {
			s.hits++
			s.lastHit = e.Time
		}
	}
	return out
}

// gaps: queries that keep returning nothing.
func (r *run) gaps() error {
	cutoff := r.today.AddDate(0, 0, -GapWindow)
	recent := map[string]int{}
	for _, e := range r.tel {
		if zero, _ := e.Fields["zero"].(bool); e.Name == "search" && zero && e.Time.After(cutoff) {
			if q := normQuery(e.Str("query")); q != "" {
				recent[q]++
			}
		}
	}
	for q, n := range recent {
		if n >= GapRepeats && r.searchStats()[q].lastHit.Before(cutoff) {
			r.add(newTask(FillGap, Low, "find or write the knowledge this query was looking for", []string{"query:" + q},
				map[string]any{"query": q, "zero_results": n, "window_days": GapWindow}))
		}
	}
	return nil
}

// benchGrow: gap queries that now find something and are not yet golden
// bench queries.
func (r *run) benchGrow() error {
	golden, _ := os.ReadFile(r.inst.Path(".strata", "bench", "retrieval.yaml")) //nolint:gosec // G304: instance bench file
	for q, s := range r.searchStats() {
		if s.zero < GapRepeats || s.hits == 0 || s.lastHit.Before(s.lastZero) || strings.Contains(strings.ToLower(string(golden)), q) {
			continue
		}
		r.add(newTask(BenchGrow, Low, "add the query to .strata/bench/retrieval.yaml with its relevant ids", []string{"query:" + q},
			map[string]any{"query": q, "zero_results_before": s.zero}))
	}
	return nil
}

// scriptify: the same normalized command repeated across sessions.
func (r *run) scriptify() error {
	type stat struct {
		sessions map[string]bool
		token    string
		n        int
	}
	by := map[string]*stat{}
	for _, e := range r.tel {
		h := e.Str("normalized_hash")
		if e.Name != "bash" || h == "" || e.Str("first_token") == "strata" {
			continue
		}
		s := by[h]
		if s == nil {
			s = &stat{sessions: map[string]bool{}, token: e.Str("first_token")}
			by[h] = s
		}
		s.n++
		s.sessions[e.Session] = true
	}
	for h, s := range by {
		if s.n >= ScriptifyRepeats && len(s.sessions) >= ScriptifySessions {
			r.add(newTask(Scriptify, Low, "write a script for this repeated command (strata new script …)", []string{"bash:" + h},
				map[string]any{"first_token": s.token, "count": s.n, "sessions": len(s.sessions)}))
		}
	}
	return nil
}

// promotions: corrections → gotcha, recurring gotchas/standards → gate,
// all-command howtos → script, co-accessed shards → playbook.
func (r *run) promotions() error {
	for _, rec := range r.data.shards {
		occ := metaInt(rec, "occurrences")
		switch {
		case rec.Type == "correction" && occ >= 2:
			r.add(newTask(Promote, Low, "convert into a gotcha with Symptom/Cause/Fix", []string{rec.ID}, map[string]any{"to": "gotcha", "occurrences": occ}).withID("gotcha"))
		case (rec.Type == "gotcha" || rec.Type == "standard") && occ >= 3:
			r.add(newTask(Promote, Medium, "enforce it as a gate (advisory unless the codebase is clean)", []string{rec.ID}, map[string]any{"to": "gate", "occurrences": occ}).withID("gate"))
		case rec.Type == "standard" && enforcementNone(rec.Body):
			r.add(newTask(Promote, Medium, "enforce it as a gate (advisory unless the codebase is clean)", []string{rec.ID}, map[string]any{"to": "gate", "enforcement": "none yet"}).withID("gate"))
		case rec.Type == "howto" && allCommands(rec.Body):
			r.add(newTask(Promote, Low, "turn the steps into a script", []string{rec.ID}, map[string]any{"to": "script"}).withID("script"))
		}
	}
	r.coAccess()
	return nil
}

func enforcementNone(body string) bool {
	_, after, ok := strings.Cut(body, "## Enforcement")
	if !ok {
		return false
	}
	section, _, _ := strings.Cut(after, "\n## ")
	return strings.Contains(strings.ToLower(section), "none yet")
}

// allCommands reports a howto whose list steps are each a single code span.
func allCommands(body string) bool {
	steps := 0
	for _, l := range strings.Split(body, "\n") {
		t := strings.TrimSpace(l)
		item := strings.TrimLeft(t, "0123456789.-*) ")
		if item == t || item == "" {
			continue
		}
		steps++
		if !strings.HasPrefix(item, "`") || !strings.HasSuffix(item, "`") || strings.Count(item, "`") != 2 {
			return false
		}
	}
	return steps >= 2
}

// coAccess: the same ≥ 3 shards shown together in ≥ 3 sessions.
func (r *run) coAccess() {
	sessions := map[string]map[string]bool{}
	for _, e := range r.tel {
		ids := e.Strs("ids")
		if e.Name != "show" || len(ids) < 3 {
			continue
		}
		var shards []string
		for _, id := range ids {
			if rec := r.data.byID[id]; rec != nil && rec.Kind == kindShard {
				shards = append(shards, id)
			}
		}
		if len(shards) < 3 {
			continue
		}
		slices.Sort(shards)
		key := strings.Join(slices.Compact(shards), "+")
		if sessions[key] == nil {
			sessions[key] = map[string]bool{}
		}
		sessions[key][e.Session] = true
	}
	for key, s := range sessions {
		if len(s) >= CoAccessSessions {
			r.add(newTask(Promote, Low, "draft a playbook (skill) linking these shards", strings.Split(key, "+"),
				map[string]any{"to": "playbook", "sessions": len(s)}).withID("playbook"))
		}
	}
}

// deprecatedScripts: deprecated scripts nobody ran for DeprecatedIdle days.
func (r *run) deprecatedScripts() error {
	cutoff := r.today.AddDate(0, 0, -DeprecatedIdle)
	ran := map[string]bool{}
	for _, e := range r.tel {
		if e.Name == "script_run" && e.Time.After(cutoff) {
			ran[e.Str("id")] = true
		}
	}
	for _, rec := range r.data.recs {
		if rec.Kind == "script" && rec.Status == "deprecated" && !ran[rec.ID] && r.telemetrySince(cutoff) {
			r.add(newTask(DeprecatedScript, Low, "remove the script (and its catalog entry); point references at its replacement",
				[]string{rec.ID}, map[string]any{evPath: rec.Src, "replaced_by": rec.Meta["replaced_by"], "idle_days": DeprecatedIdle}))
		}
	}
	return nil
}

// telemetrySince reports whether telemetry reaches back to t.
func (r *run) telemetrySince(t time.Time) bool {
	return len(r.tel) > 0 && !r.tel[0].Time.After(t)
}

func nonNil[T any](xs []T) []T {
	if xs == nil {
		return []T{}
	}
	return xs
}
