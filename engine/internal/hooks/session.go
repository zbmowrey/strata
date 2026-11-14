package hooks

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/index"
	"github.com/zbmowrey/strata/engine/internal/projects"
	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/search"
)

// Counts summarizes an instance for the session-start banner and status line.
type Counts struct {
	Shards    int `json:"shards"`
	Scripts   int `json:"scripts"`
	Playbooks int `json:"playbooks"`
	Inbox     int `json:"inbox"`
	Questions int `json:"questions"` // open
	Stale     int `json:"stale"`
}

// CountIndex reads banner counts from an open index.
func CountIndex(ctx context.Context, ix *index.Index, today time.Time) (Counts, error) {
	var c Counts
	rows, err := ix.DB().QueryContext(ctx, `SELECT kind, count(*) FROM docs WHERE private = 0 AND status != 'archived'
		AND (kind != 'question' OR status = 'open') GROUP BY kind`)
	if err != nil {
		return c, fmt.Errorf("counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			return c, fmt.Errorf("counts: %w", err)
		}
		switch kind {
		case "shard":
			c.Shards = n
		case "script":
			c.Scripts = n
		case "playbook":
			c.Playbooks = n
		case "inbox":
			c.Inbox = n
		case "question":
			c.Questions = n
		}
	}
	if err := rows.Err(); err != nil {
		return c, fmt.Errorf("counts: %w", err)
	}
	err = ix.DB().QueryRowContext(ctx, "SELECT count(*) FROM docs d WHERE d.kind = 'shard' AND d.private = 0 AND d.status = 'active' AND "+search.StaleSQL,
		today.Format(time.DateOnly)).Scan(&c.Stale)
	if err != nil {
		return c, fmt.Errorf("counts: %w", err)
	}
	return c, nil
}

// sessionStart refreshes the index, runs the safe gardener subset, and
// prints the ≤ 25-line orientation banner (stdout becomes session context).
func (h *handler) sessionStart() (outcome, error) {
	start := time.Now()
	ix, st, err := index.Open(h.ctx, h.inst, index.Options{})
	if err != nil {
		return outcome{}, err
	}
	defer func() { _ = ix.Close() }()
	var lines []string
	// The safe gardener subset runs only when the refresh found changes.
	if h.env.GardenAuto != nil && h.inst.Config.Hooks.SessionStartGardenAuto && len(st.Paths) > 0 {
		if note, err := h.env.GardenAuto(h.ctx, h.inst, st.Paths); err != nil {
			lines = append(lines, "⚠ garden --auto: "+err.Error())
		} else if note != "" {
			lines = append(lines, note)
		}
	}
	c, err := CountIndex(h.ctx, ix, h.env.Now())
	if err != nil {
		return outcome{}, err
	}
	fresh := "index fresh"
	if st.Added+st.Changed+st.Removed > 0 {
		fresh = fmt.Sprintf("index refreshed +%d ~%d -%d", st.Added, st.Changed, st.Removed)
	}
	head := fmt.Sprintf("strata ▸ %s · %s · %s · %s (%d ms)",
		count(c.Shards, "shard"), count(c.Scripts, "script"), count(c.Playbooks, "playbook"), fresh, time.Since(start).Milliseconds())
	lines = append([]string{head}, lines...)
	lines = append(lines, h.activeProjects()...)
	tail := []string{fmt.Sprintf("Inbox %d", c.Inbox)}
	if c.Questions > 0 {
		tail = append(tail, fmt.Sprintf("Open questions %d (strata questions list)", c.Questions))
	}
	if c.Stale > 0 {
		tail = append(tail, fmt.Sprintf("Stale %d", c.Stale))
	}
	lines = append(lines, strings.Join(tail, " · "))
	if h.env.GardenLine != nil {
		if line, err := h.env.GardenLine(h.ctx, h.inst, h.env.Now()); err != nil {
			lines = append(lines, "⚠ garden: "+err.Error())
		} else if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > 25 {
		lines = lines[:25]
	}
	return outcome{stdout: strings.Join(lines, "\n") + "\n", label: "ok"}, nil
}

// activeProjects renders up to three active projects with their next step.
func (h *handler) activeProjects() []string {
	sums, err := projects.Active(h.inst, h.env.Now)
	if err != nil || len(sums) == 0 {
		return nil
	}
	out := []string{"Active projects (strata resume):"}
	for i, s := range sums {
		if i == 3 {
			out = append(out, fmt.Sprintf("  +%d more", len(sums)-3))
			break
		}
		next := firstLine(s.Next)
		if next == "" {
			next = "(no handoff yet — update projects/" + s.Slug + "/handoff.md)"
		}
		out = append(out, render.Truncate("  "+s.Slug+" — Next: "+next, 140))
	}
	return out
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

// count renders "1 shard" / "1,204 shards".
func count(n int, word string) string {
	if n != 1 {
		word += "s"
	}
	return thousands(n) + " " + word
}

// Thousands renders n with thousands separators (1,204).
func Thousands(n int) string { return thousands(n) }

func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// preCompact reminds Claude to update handoffs and restates recent threads.
func (h *handler) preCompact() (outcome, error) {
	ps := projects.New(h.inst, h.env.Now)
	heads, err := ps.Headlines(5)
	if err != nil {
		return outcome{}, err
	}
	sums, _ := projects.Active(h.inst, h.env.Now)
	if len(sums) == 0 {
		return outcome{label: "none"}, nil
	}
	var b strings.Builder
	b.WriteString("strata ▸ context is about to be compacted. Before it is, update handoff.md for active projects (strata project handoff <slug>).\n")
	for _, s := range sums {
		b.WriteString(s.Slug + " — recent log:\n")
		for _, l := range heads[s.Slug] {
			b.WriteString("  " + render.Truncate(l, 140) + "\n")
		}
	}
	return outcome{stdout: b.String(), label: "reminded"}, nil
}
