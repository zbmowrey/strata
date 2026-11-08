package index

import (
	"path"
	"strconv"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/corpus"
	"github.com/zbmowrey/strata/engine/internal/doc"
)

// ProjectSlug returns the project slug of a project file path.
func ProjectSlug(rel, projectsDir string) string {
	sub := strings.TrimPrefix(rel, projectsDir+"/")
	sub = strings.TrimPrefix(sub, "_archive/")
	slug, _, _ := strings.Cut(sub, "/")
	return slug
}

// project yields records for one project file. project.md is addressed by
// its slug; other files by composite ids `<slug>:plan`, `<slug>:handoff`,
// `<slug>:out/<file>` and `<slug>:log#<entry>` (one record per log entry).
func (x *extractor) project(f corpus.File, raw []byte) []Record {
	slug := ProjectSlug(f.Rel, x.layout.Projects)
	if f.Role == "out" {
		return []Record{{ID: slug + ":out/" + path.Base(f.Rel), Kind: string(corpus.Project), Path: f.Rel, Title: path.Base(f.Rel), Domain: slug, Status: "active", Bytes: len(raw), Meta: map[string]any{}}}
	}
	d, _ := doc.Parse(f.Rel, raw)
	r := x.base(f, d)
	r.Domain = slug
	r.Status = firstNonEmpty(r.Status, "active")
	if f.Archived {
		r.Status = "archived"
	}
	switch f.Role {
	case "project":
		r.ID = slug
		r.Title = firstNonEmpty(r.Title, slug)
		r.Summary = firstNonEmpty(d.Meta.String("goal"), truncate(d.FirstParagraph(), 160))
		r.Type = "project"
		r.Links = append(r.Links, refLinks(d.Meta, "shards", "related")...)
	case "log":
		return x.logEntries(f, d, slug, r.Status)
	default:
		r.ID = slug + ":" + f.Role
		r.Title = slug + " " + f.Role
		r.Summary = truncate(firstNonEmpty(sectionFirstLine(d, "Next"), sectionFirstLine(d, "State"), d.FirstParagraph()), 160)
		r.Updated = dateOf(firstNonEmpty(d.Meta.String("updated"), r.Updated))
		r.Type = f.Role
	}
	return []Record{r}
}

// LogEntry is one dated chronicle entry.
type LogEntry struct {
	Anchor  string
	Stamp   string
	Heading doc.Heading
}

// LogEntries returns a chronicle's `## ` entries with stable anchors: the
// slug of the heading's timestamp (text before "·"), suffixed -2, -3… when
// repeated.
func LogEntries(d *doc.Doc) []LogEntry {
	out := make([]LogEntry, 0, len(d.Headings))
	seen := map[string]int{}
	for _, h := range d.Headings {
		if h.Level != 2 {
			continue
		}
		stamp, _, _ := strings.Cut(h.Text, "·")
		anchor := doc.Slug(stamp)
		if anchor == "" {
			anchor = h.Slug
		}
		seen[anchor]++
		if n := seen[anchor]; n > 1 {
			anchor += "-" + strconv.Itoa(n)
		}
		out = append(out, LogEntry{Anchor: anchor, Stamp: strings.TrimSpace(stamp), Heading: h})
	}
	return out
}

// logEntries splits a chronicle into one record per `## ` entry.
func (x *extractor) logEntries(f corpus.File, d *doc.Doc, slug, status string) []Record {
	entries := LogEntries(d)
	out := make([]Record, 0, len(entries))
	for _, e := range entries {
		h, anchor, stamp := e.Heading, e.Anchor, e.Stamp
		body := string(d.Raw[h.Start:h.End])
		lines := strings.Split(body, "\n")[1:]
		var bullets []string
		for _, l := range lines {
			if t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "- ")); t != "" {
				bullets = append(bullets, t)
			}
		}
		date := stamp
		out = append(out, Record{
			ID: slug + ":log#" + anchor, Kind: string(corpus.Project), Path: f.Rel, Anchor: anchor,
			Domain: slug, Type: "log", Status: status, Title: slug + " log " + date,
			Summary: truncate(strings.Join(bullets, " · "), 160), Updated: dateOf(date), Created: dateOf(date),
			Body: body, Bytes: len(body), Lines: len(lines), Meta: map[string]any{},
			Links: x.entryLinks(d, h),
		})
	}
	return out
}

func (x *extractor) entryLinks(d *doc.Doc, h doc.Heading) []LinkRec {
	var out []LinkRec
	for _, l := range d.Links {
		if l.Offset >= h.Start && l.Offset < h.End && l.Target != "" {
			out = append(out, LinkRec{Dst: l.Target, Anchor: l.Anchor, Kind: "wikilink"})
		}
	}
	return out
}
