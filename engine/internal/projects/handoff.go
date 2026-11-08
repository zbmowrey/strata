package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
)

// MaxHandoffLines is the handoff size limit (docs/spec/05-projects.md §3.3).
const MaxHandoffLines = 40

const noDontRedo = "- nothing ruled out yet"

// handoffParts are the sections of a handoff.
type handoffParts struct {
	State, Next, Open string
	DontRedo          []string
}

// renderHandoff renders the fixed handoff shape, trimming the oldest
// Don't-redo items so the file stays within MaxHandoffLines.
func (s *Store) renderHandoff(p handoffParts) string {
	head := fmt.Sprintf("---\nupdated: %s\nsession: %s\n---\n## State\n%s\n## Next\n%s\n## Open threads\n%s\n## Don't redo\n",
		s.now().Format(time.RFC3339), Session4(s.session), orDefault(p.State, "(where things stand)"),
		orDefault(p.Next, "1. (next concrete action)"), orDefault(p.Open, "- none"))
	dont := p.DontRedo
	room := MaxHandoffLines - strings.Count(head, "\n")
	if len(dont) > room {
		dont = dont[len(dont)-max(room, 0):]
	}
	if len(dont) == 0 {
		dont = []string{noDontRedo}
	}
	return head + strings.Join(dont, "\n") + "\n"
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}

// sectionBody returns the text under a `## heading` (without the heading).
func sectionBody(d *doc.Doc, heading string) string {
	h, ok := d.Section(heading)
	if !ok {
		return ""
	}
	_, body, _ := strings.Cut(string(d.Raw[h.Start:h.End]), "\n")
	return strings.TrimSpace(body)
}

// Handoff rewrites handoff.md: State, Next and Open threads are kept;
// `## Don't redo` is pre-filled from `failed:` log lines written since the
// previous handoff's `updated`, so it is never written from memory alone.
// It returns the instance-relative path.
func (s *Store) Handoff(slug string) (string, error) {
	p, err := s.Load(slug)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(s.inst.Root, filepath.FromSlash(p.Dir))
	entries, err := s.Entries(slug)
	if err != nil {
		return "", err
	}
	lock, err := s.lockProject(dir)
	if err != nil {
		return "", err
	}
	defer func() { _ = lock.Unlock() }()
	path := filepath.Join(dir, HandoffFile)
	raw, err := os.ReadFile(path) //nolint:gosec // G304: path from a loaded project
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read handoff %s: %w", slug, err)
	}
	d, _ := doc.Parse(HandoffFile, raw)
	parts := handoffParts{State: sectionBody(d, "State"), Next: sectionBody(d, "Next"), Open: sectionBody(d, "Open threads")}
	parts.DontRedo = mergeDontRedo(sectionBody(d, "Don't redo"), failedSince(entries, d.Meta.String("updated")))
	if err := fsutil.WriteAtomic(path, []byte(s.renderHandoff(parts)), 0o644); err != nil {
		return "", err
	}
	rel, _ := filepath.Rel(s.inst.Root, path)
	return filepath.ToSlash(rel), nil
}

// failedSince lists `failed:` lines of entries newer than the RFC 3339
// timestamp since (all entries when since is empty or unparsable).
func failedSince(entries []Entry, since string) []string {
	cut, err := time.Parse(time.RFC3339, since)
	var out []string
	for _, e := range entries {
		// Headings have minute precision: an entry in the handoff's own
		// minute counts as new.
		if err == nil && !e.Time.IsZero() && e.Time.Before(cut.Truncate(time.Minute)) {
			continue
		}
		for _, l := range e.Lines {
			if l.Prefix == "failed" {
				out = append(out, "- "+l.Text+" (log "+e.Stamp+")")
			}
		}
	}
	return out
}

// mergeDontRedo keeps existing bullets and appends new ones not yet listed.
func mergeDontRedo(existing string, fresh []string) []string {
	var out []string
	seen := map[string]bool{}
	key := func(b string) string {
		b = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(b), "- "))
		if i := strings.LastIndex(b, " (log "); i >= 0 {
			b = b[:i]
		}
		return strings.ToLower(b)
	}
	for _, l := range strings.Split(existing, "\n") {
		if t := strings.TrimSpace(l); t != "" && t != noDontRedo && !seen[key(t)] {
			seen[key(t)] = true
			out = append(out, t)
		}
	}
	for _, f := range fresh {
		if !seen[key(f)] {
			seen[key(f)] = true
			out = append(out, f)
		}
	}
	return slices.Clip(out)
}
