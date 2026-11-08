package projects

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/doc"
)

// Resume limits (docs/spec/05-projects.md §4).
const (
	ResumeMaxProjects = 3
	ResumeMaxLines    = 15
)

// Summary is a project with its activity state and handoff Next section.
type Summary struct {
	Project
	LastActivity string `json:"last_activity"` // YYYY-MM-DD of the last log entry (else updated)
	Next         string `json:"next,omitempty"`
	AgeDays      int    `json:"age_days"`
	Entries      int    `json:"entries"`
	Stale        bool   `json:"stale"`
	last         time.Time
}

// List summarizes projects: all of them with all=true, else non-archived
// ones that are not done. Sorted by most recent activity, then slug.
func (s *Store) List(all bool) ([]Summary, error) {
	ps, err := s.All()
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(ps))
	for _, p := range ps {
		if !all && (p.Archived || p.Status == StatusDone || p.Status == StatusArchived) {
			continue
		}
		sum, err := s.summarize(p)
		if err != nil {
			return nil, err
		}
		out = append(out, sum)
	}
	slices.SortStableFunc(out, func(a, b Summary) int {
		return cmp.Or(b.last.Compare(a.last), cmp.Compare(a.Slug, b.Slug))
	})
	return out, nil
}

// Summary summarizes one project.
func (s *Store) Summary(slug string) (Summary, error) {
	p, err := s.Load(slug)
	if err != nil {
		return Summary{}, err
	}
	return s.summarize(p)
}

func (s *Store) summarize(p *Project) (Summary, error) {
	sum := Summary{Project: *p, LastActivity: p.Updated}
	entries, err := s.Entries(p.Slug)
	if err != nil {
		return sum, err
	}
	sum.Entries = len(entries)
	if t, err := time.ParseInLocation(time.DateOnly, firstN(p.Updated, 10), s.now().Location()); err == nil {
		sum.last = t
	}
	if n := len(entries); n > 0 && !entries[n-1].Time.IsZero() {
		sum.last = entries[n-1].Time
		sum.LastActivity = sum.last.Format(time.DateOnly)
	}
	if !sum.last.IsZero() {
		sum.AgeDays = int(s.now().Sub(sum.last).Hours() / 24)
	}
	staleAfter := s.inst.Config.Projects.StaleAfter.DaysCount()
	sum.Stale = p.Status == StatusActive && staleAfter > 0 && sum.AgeDays > staleAfter
	raw, err := os.ReadFile(filepath.Join(s.inst.Root, filepath.FromSlash(p.Dir), HandoffFile)) //nolint:gosec // G304: path from a loaded project
	if err == nil {
		d, _ := doc.Parse(HandoffFile, raw)
		sum.Next = sectionBody(d, "Next")
	}
	return sum, nil
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Active returns summaries of active named projects (excluding _adhoc),
// most recently active first — the SessionStart and resume view.
func Active(inst *config.Instance, now func() time.Time) ([]Summary, error) {
	sums, err := New(inst, now).List(false)
	if err != nil {
		return nil, err
	}
	out := sums[:0]
	for _, s := range sums {
		if s.Status == StatusActive && s.Slug != Adhoc {
			out = append(out, s)
		}
	}
	return out, nil
}

// RenderResume renders the compact resume view: per project a title line
// and its handoff `## Next` (≤ ResumeMaxLines lines each, at most
// ResumeMaxProjects projects; the rest are counted).
func RenderResume(sums []Summary) string {
	if len(sums) == 0 {
		return "No active projects. Start one: strata project new <slug> \"<title>\" --goal \"…\"\n"
	}
	var b strings.Builder
	for i, s := range sums {
		if i == ResumeMaxProjects {
			fmt.Fprintf(&b, "+%d more active projects (strata project list)\n", len(sums)-i)
			break
		}
		age := fmt.Sprintf("%dd ago", s.AgeDays)
		if s.AgeDays == 0 {
			age = "today"
		}
		stale := ""
		if s.Stale {
			stale = " ⚠ stale"
		}
		fmt.Fprintf(&b, "%s — %s (%s, last activity %s, %s)%s\n", s.Slug, s.Title, s.Status, s.LastActivity, age, stale)
		next := strings.Split(orDefault(s.Next, "(no Next in handoff.md — run strata project handoff "+s.Slug+")"), "\n")
		if len(next) > ResumeMaxLines-2 {
			next = append(next[:ResumeMaxLines-3], "… (see "+s.Dir+"/"+HandoffFile+")")
		}
		b.WriteString("  Next:\n")
		for _, l := range next {
			b.WriteString("    " + strings.TrimRight(l, " ") + "\n")
		}
	}
	return b.String()
}
