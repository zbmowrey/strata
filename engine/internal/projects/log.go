package projects

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/index"
	"github.com/zbmowrey/strata/engine/internal/schema"
)

// StampLayout is the timestamp format of chronicle entry headings.
const StampLayout = "2006-01-02 15:04"

// groupWindow is how long consecutive log lines from one session share an entry.
const groupWindow = 30 * time.Minute

// Line is one chronicle bullet: `- <prefix>: <text>`.
type Line struct {
	Prefix string `json:"prefix"`
	Text   string `json:"text"`
}

// Entry is one dated chronicle entry.
type Entry struct {
	Time    time.Time `json:"-"`
	ID      string    `json:"id"` // index id: <slug>:log#<anchor>
	Stamp   string    `json:"stamp"`
	Session string    `json:"session"`
	Lines   []Line    `json:"lines"`
}

// Headline is the first line of an entry, for summaries.
func (e Entry) Headline() string {
	if len(e.Lines) == 0 {
		return e.Stamp
	}
	return e.Stamp + " — " + e.Lines[0].Prefix + ": " + e.Lines[0].Text
}

// Entries parses a project's chronicle (oldest first).
func (s *Store) Entries(slug string) ([]Entry, error) {
	p, err := s.Load(slug)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(s.inst.Root, filepath.FromSlash(p.Dir), LogFile)) //nolint:gosec // G304: path from a loaded project
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read log %s: %w", slug, err)
	}
	return ParseEntries(slug, raw, s.now().Location()), nil
}

// ParseEntries splits chronicle text into entries; heading timestamps are
// interpreted in loc. Anchors match the index (index.LogEntries).
func ParseEntries(slug string, raw []byte, loc *time.Location) []Entry {
	d, _ := doc.Parse(LogFile, raw)
	les := index.LogEntries(d)
	out := make([]Entry, 0, len(les))
	for _, le := range les {
		e := Entry{ID: slug + ":log#" + le.Anchor, Stamp: le.Stamp}
		if t, err := time.ParseInLocation(StampLayout, le.Stamp, loc); err == nil {
			e.Time = t
		}
		if _, after, ok := strings.Cut(le.Heading.Text, "s:"); ok {
			e.Session = strings.TrimSpace(after)
		}
		body := string(d.Raw[le.Heading.Start:le.Heading.End])
		for _, l := range strings.Split(body, "\n")[1:] {
			t := strings.TrimSpace(l)
			if !strings.HasPrefix(t, "- ") {
				continue
			}
			prefix, text, ok := strings.Cut(strings.TrimPrefix(t, "- "), ":")
			if !ok {
				prefix, text = "", strings.TrimPrefix(t, "- ")
			}
			e.Lines = append(e.Lines, Line{Prefix: strings.TrimSpace(prefix), Text: strings.TrimSpace(text)})
		}
		out = append(out, e)
	}
	return out
}

// Log appends one chronicle line atomically. Lines from the same session
// within 30 minutes of the last entry's heading join that entry; otherwise a
// new `## <stamp> · s:<session>` entry starts. An empty slug logs to _adhoc.
// It returns the instance-relative path of log.md.
func (s *Store) Log(slug, prefix, text string) (string, error) {
	if slug == "" {
		slug = Adhoc
	}
	if !slices.Contains(schema.LogPrefixes, prefix) {
		return "", fail.New(fail.Usage, "one of: "+strings.Join(schema.LogPrefixes, ", "), "unknown log prefix %q", prefix)
	}
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "", fail.New(fail.Usage, `strata project log <slug> --tried "…"`, "empty log text")
	}
	p, err := s.Load(slug)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(s.inst.Root, filepath.FromSlash(p.Dir))
	lock, err := s.lockProject(dir)
	if err != nil {
		return "", err
	}
	defer func() { _ = lock.Unlock() }()
	path := filepath.Join(dir, LogFile)
	raw, err := os.ReadFile(path) //nolint:gosec // G304: path from a loaded project
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read log %s: %w", slug, err)
	}
	if len(raw) == 0 {
		raw = []byte("# Log — " + slug + "\n")
	}
	out := s.appendLine(slug, raw, Line{Prefix: prefix, Text: text})
	if err := fsutil.WriteAtomic(path, out, 0o644); err != nil {
		return "", err
	}
	_ = s.tel.Emit("capture", map[string]any{"kind": "project_log", "type": prefix, "project": slug})
	rel, _ := filepath.Rel(s.inst.Root, path)
	return filepath.ToSlash(rel), nil
}

// appendLine returns raw with the line added (grouping per Log's rules).
func (s *Store) appendLine(slug string, raw []byte, l Line) []byte {
	now := s.now()
	tag := Session4(s.session)
	bullet := "- " + l.Prefix + ": " + l.Text + "\n"
	out := bytes.TrimRight(raw, "\n")
	entries := ParseEntries(slug, raw, now.Location())
	if n := len(entries); n > 0 {
		last := entries[n-1]
		age := now.Sub(last.Time)
		if last.Session == tag && !last.Time.IsZero() && age >= 0 && age < groupWindow {
			return append(append(out, '\n'), bullet...)
		}
	}
	return append(out, []byte("\n\n## "+now.Format(StampLayout)+" · s:"+tag+"\n"+bullet)...)
}

// Headlines returns the last n entry headlines of every active project
// (newest first within a project; projects by slug), for the pre-compact
// reminder.
func (s *Store) Headlines(n int) (map[string][]string, error) {
	all, err := s.All()
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, p := range all {
		if p.Status != StatusActive || p.Archived {
			continue
		}
		es, err := s.Entries(p.Slug)
		if err != nil {
			return nil, err
		}
		for i := len(es) - 1; i >= 0 && len(out[p.Slug]) < n; i-- {
			out[p.Slug] = append(out[p.Slug], es[i].Headline())
		}
	}
	return out, nil
}
