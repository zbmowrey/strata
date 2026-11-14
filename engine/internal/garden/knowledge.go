package garden

import (
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/index"
	"github.com/zbmowrey/strata/engine/internal/textsim"
)

const (
	evPath          = "path" // evidence key: the target's file
	trustUserStated = "user-stated"
	kindHub         = "hub"
	kindShard       = "shard"
)

// inbox: every _inbox/ item is a filing task.
func (r *run) inbox() error {
	for _, rec := range r.data.recs {
		if rec.Kind == "inbox" {
			r.add(newTask(FileInbox, Low, "file into a shard (new or merged), then delete the inbox item", []string{rec.ID},
				map[string]any{evPath: rec.Src, "type": rec.Type, "title": rec.Title}))
		}
	}
	return nil
}

// oversize: bodies over the size limits are split; an oversize howto is a
// playbook candidate instead (docs/spec/07-gardener.md §4).
func (r *run) oversize() error {
	k := r.inst.Config.Knowledge
	for _, rec := range r.data.shards {
		lines, size := strings.Count(rec.Body, "\n"), len(rec.Body)
		if lines <= k.MaxBodyLines && size <= k.MaxBodyBytes {
			continue
		}
		ev := map[string]any{evPath: rec.Src, "body_lines": lines, "body_bytes": size, "max_lines": k.MaxBodyLines, "max_bytes": k.MaxBodyBytes}
		if rec.Type == "howto" {
			ev["to"] = "playbook"
			r.add(newTask(Promote, Low, "turn this howto into a playbook (skill) linking the relevant shards", []string{rec.ID}, ev).withID("playbook"))
			continue
		}
		r.add(newTask(SplitOversize, Low, "split into focused shards that link each other; keep the id on the core claim", []string{rec.ID}, ev))
	}
	return nil
}

// orphans: shards nothing links to (hubs excluded — they list everything),
// once they are older than [garden] orphan_min_age_days (D-033): a young
// shard has had no chance to be linked yet.
func (r *run) orphans() error {
	minAge := r.inst.Config.Garden.OrphanMinAgeDays
	for _, rec := range r.data.shards {
		if r.tooYoung(rec, minAge) {
			continue
		}
		if r.data.inboundFrom(rec.ID, kindHub) == 0 {
			r.add(newTask(FixOrphan, Low, "link it from related shards (See also) or merge it into one", []string{rec.ID},
				map[string]any{evPath: rec.Src, "domain": rec.Domain, "topic": rec.Topic}))
		}
	}
	return nil
}

// tooYoung reports whether rec was created (or, lacking created, updated)
// fewer than minAge days ago. Undated shards count as old.
func (r *run) tooYoung(rec *index.Record, minAge int) bool {
	if minAge <= 0 {
		return false
	}
	base := rec.Created
	if base == "" {
		base = rec.Updated
	}
	t, err := time.Parse(time.DateOnly, dateOnly(base))
	if err != nil {
		return false
	}
	return r.today.Before(t.AddDate(0, 0, minAge))
}

// stale: review_after elapsed since verified_at (or updated).
func (r *run) stale() error {
	for _, rec := range r.data.shards {
		if rec.ReviewAfterDays <= 0 || rec.Status != "active" {
			continue
		}
		base := rec.VerifiedAt
		if base == "" {
			base = rec.Updated
		}
		t, err := time.Parse(time.DateOnly, dateOnly(base))
		if err != nil || !r.today.After(t.AddDate(0, 0, rec.ReviewAfterDays)) {
			continue
		}
		ev := map[string]any{evPath: rec.Src, "since": dateOnly(base), "review_after_days": rec.ReviewAfterDays, "trust": rec.Trust}
		action := "re-check against sources and code; bump verified_at or correct it"
		if v, ok := rec.Meta["verify"].(map[string]any); ok && v["script"] != nil {
			ev["verify_script"] = v["script"]
			action = "run the verify script; bump verified_at on success, open a correction on failure"
		}
		r.add(newTask(ReverifyStale, Low, action, []string{rec.ID}, ev))
	}
	return nil
}

// dateOnly trims a date or timestamp to its YYYY-MM-DD prefix.
func dateOnly(s string) string {
	if len(s) > len(time.DateOnly) {
		return s[:len(time.DateOnly)]
	}
	return s
}

// cold: no search/show hits within cold_after and no inbound links. Only
// judged when telemetry covers the whole window (else every shard of a new
// instance would look cold).
func (r *run) cold() error {
	window := r.inst.Config.Knowledge.ColdAfter.D()
	if window <= 0 || len(r.tel) == 0 || r.tel[0].Time.After(r.today.Add(-window)) {
		return nil
	}
	cutoff := r.today.Add(-window)
	seen := map[string]bool{}
	for _, e := range r.tel {
		if e.Time.Before(cutoff) {
			continue
		}
		switch e.Name {
		case "search":
			for _, id := range e.Strs("top_ids") {
				seen[id] = true
			}
		case "show":
			for _, id := range e.Strs("ids") {
				seen[id] = true
			}
		}
	}
	for _, rec := range r.data.shards {
		created, err := time.Parse(time.DateOnly, dateOnly(rec.Created))
		if seen[rec.ID] || r.data.inboundFrom(rec.ID, kindHub) > 0 || err != nil || created.After(cutoff) {
			continue
		}
		r.add(newTask(ArchiveCold, Low, "move to _archive/ (never delete)", []string{rec.ID},
			map[string]any{evPath: rec.Src, "window_days": int(window.Hours() / 24)}))
	}
	return nil
}

// answers: answered questions waiting to be applied.
func (r *run) answers() error {
	for _, rec := range r.data.recs {
		if rec.Kind == "question" && rec.Status == "answered" {
			r.add(newTask(ApplyAnswer, Low, "apply the answer to the shards it concerns, then mark the question applied", []string{rec.ID},
				map[string]any{evPath: rec.Src, "title": rec.Title}))
		}
	}
	return nil
}

// conflicts: active claims about the same code (shared applies_to glob or
// same topic with ≥ 2 shared tags) where exactly one side is negated and
// their titles overlap. The agent confirms before acting (risk high).
func (r *run) conflicts() error {
	claims := make([]*index.Record, 0, len(r.data.shards))
	for _, rec := range r.data.shards {
		if rec.Status == "active" && (rec.Type == "fact" || rec.Type == "standard" || rec.Type == "gotcha") {
			claims = append(claims, rec)
		}
	}
	for i, a := range claims {
		for _, b := range claims[i+1:] {
			if !related(a, b) || negated(a) == negated(b) || sharedWords(a.Title, b.Title) < 2 {
				continue
			}
			r.add(newTask(ResolveConflict, High, "confirm the contradiction from code or sources; correct the weaker claim or raise a question",
				[]string{a.ID, b.ID}, map[string]any{"titles": []string{a.Title, b.Title}}))
		}
	}
	return nil
}

func related(a, b *index.Record) bool {
	for _, g := range a.AppliesTo {
		for _, h := range b.AppliesTo {
			if g == h {
				return true
			}
		}
	}
	if a.Domain != b.Domain || a.Topic != b.Topic {
		return false
	}
	n := 0
	for _, t := range a.Tags {
		for _, u := range b.Tags {
			if t == u {
				n++
			}
		}
	}
	return n >= 2
}

var negations = []string{"not", "never", "no", "don't", "dont", "doesn't", "avoid", "without", "cannot", "can't"}

func negated(r *index.Record) bool {
	for _, w := range strings.Fields(strings.ToLower(r.Title + " " + r.Summary)) {
		for _, n := range negations {
			if strings.Trim(w, ".,;:!?`'\"()") == n {
				return true
			}
		}
	}
	return false
}

func sharedWords(a, b string) int {
	stop := map[string]bool{"the": true, "a": true, "an": true, "is": true, "are": true, "to": true, "of": true, "in": true, "on": true, "for": true, "and": true}
	set := map[string]bool{}
	for _, w := range textsim.Words(a) {
		if !stop[w] {
			set[w] = true
		}
	}
	n := 0
	for _, w := range textsim.Words(b) {
		if set[w] {
			n++
			delete(set, w)
		}
	}
	return n
}
