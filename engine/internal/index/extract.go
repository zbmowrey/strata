package index

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/corpus"
	"github.com/zbmowrey/strata/engine/internal/doc"
)

// Record is one indexed document. A file yields one record, except project
// logs, which yield one record per dated entry.
type Record struct {
	Meta            map[string]any
	ID              string
	Kind            string
	Path            string
	Src             string // the source file (instance-relative)
	Anchor          string
	Domain          string
	Topic           string
	Type            string
	Status          string
	Trust           string
	Title           string
	Summary         string
	Created         string
	Updated         string
	VerifiedAt      string
	SideEffect      string
	Runtime         string
	Body            string
	Aliases         []string
	Tags            []string
	Headings        []doc.Heading
	Links           []LinkRec
	AppliesTo       []string
	ReviewAfterDays int
	Bytes           int
	Lines           int
	Private         bool
}

// LinkRec is an outbound edge.
type LinkRec struct {
	Dst, Anchor, Kind string
}

// extractor turns corpus files into records.
type extractor struct {
	layout corpus.Layout
	cfg    config.Config
}

// extract parses one file's bytes into records.
func (x *extractor) extract(f corpus.File, raw []byte) []Record {
	switch f.Kind {
	case corpus.Script:
		return []Record{x.script(f, raw)}
	case corpus.Project:
		return x.project(f, raw)
	default:
		d, _ := doc.Parse(f.Rel, raw)
		return []Record{x.markdown(f, d)}
	}
}

func (x *extractor) base(f corpus.File, d *doc.Doc) Record {
	m := d.Meta
	r := Record{
		Kind: string(f.Kind), Path: f.Rel, Status: m.String("status"), Trust: m.String("trust"),
		Title: m.String("title"), Summary: m.String("summary"), Type: m.String("type"),
		Created: m.String("created"), Updated: m.String("updated"), VerifiedAt: m.String("verified_at"),
		Aliases: m.Strings("aliases"), Tags: lower(m.Strings("tags")), AppliesTo: m.Strings("applies_to"),
		Headings: d.Headings, Body: string(d.Body()), Bytes: len(d.Raw), Lines: strings.Count(string(d.Raw), "\n"),
		Private: f.Private, Meta: m.Map(),
	}
	for _, l := range d.Links {
		if l.Target != "" && !strings.Contains(l.Target, ".") {
			r.Links = append(r.Links, LinkRec{Dst: l.Target, Anchor: l.Anchor, Kind: "wikilink"})
		}
	}
	return r
}

func (x *extractor) markdown(f corpus.File, d *doc.Doc) Record {
	r := x.base(f, d)
	stem := strings.TrimSuffix(path.Base(f.Rel), ".md")
	switch f.Kind {
	case corpus.Shard:
		x.shard(&r, f, d, stem)
	case corpus.Hub:
		r.ID, r.Domain = firstNonEmpty(d.Meta.String("id"), "hub-"+stem), stem
		r.Type, r.Status = "index", firstNonEmpty(r.Status, "active")
	case corpus.Inbox:
		r.ID = stem
		r.Status = "active"
		r.Title = firstNonEmpty(r.Title, truncate(d.FirstParagraph(), 80))
		r.Summary = firstNonEmpty(r.Summary, truncate(d.FirstParagraph(), 160))
		r.Updated = firstNonEmpty(r.Updated, dateOf(d.Meta.String("captured")))
	case corpus.Question:
		r.ID = firstNonEmpty(d.Meta.String("id"), stem)
		r.Title = firstNonEmpty(r.Title, sectionFirstLine(d, "Question"), r.ID)
		r.Updated = firstNonEmpty(r.Updated, d.Meta.String("raised"))
	case corpus.Playbook:
		r.ID = firstNonEmpty(d.Meta.String("name"), path.Base(path.Dir(f.Rel)))
		r.Title = firstNonEmpty(r.Title, r.ID)
		r.Summary = d.Meta.String("description")
		r.Status = firstNonEmpty(r.Status, "active")
		r.Links = append(r.Links, refLinks(d.Meta, "shards", "related")...)
		r.Links = append(r.Links, refLinks(d.Meta, "scripts", "scripts")...)
	default:
	}
	if r.Title == "" {
		r.Title = r.ID
	}
	return r
}

func (x *extractor) shard(r *Record, f corpus.File, d *doc.Doc, stem string) {
	r.ID = stem
	rel := strings.TrimPrefix(f.Rel, x.layout.Knowledge+"/")
	parts := strings.Split(rel, "/")
	if f.Archived || f.Private {
		parts = parts[1:]
	}
	if len(parts) >= 3 {
		r.Domain, r.Topic = parts[0], parts[1]
	} else if len(parts) == 2 {
		r.Domain = parts[0]
	}
	if f.Archived {
		r.Status = "archived"
	}
	r.ReviewAfterDays = x.reviewDays(d.Meta)
	for _, ref := range []struct{ key, kind string }{{"scripts", "scripts"}, {"gates", "gates"}, {"supersedes", "supersedes"}} {
		r.Links = append(r.Links, refLinks(d.Meta, ref.key, ref.kind)...)
	}
	if v, ok := d.Meta.Get("verify"); ok {
		if m, isMap := v.(map[string]any); isMap && doc.Scalar(m["script"]) != "" {
			r.Links = append(r.Links, LinkRec{Dst: doc.Scalar(m["script"]), Kind: "verify"})
		}
	}
}

// reviewDays resolves review_after (explicit, else the type default; 0 = never).
func (x *extractor) reviewDays(m *doc.Meta) int {
	if v := m.String("review_after"); v != "" {
		if d, err := config.ParseDuration(v); err == nil {
			return d.DaysCount()
		}
	}
	if d, ok := x.cfg.Knowledge.ReviewAfter[m.String("type")]; ok {
		return d.DaysCount()
	}
	return 0
}

func (x *extractor) script(f corpus.File, raw []byte) Record {
	m, err := doc.ParseYAML(raw)
	if err != nil || m == nil {
		m = &doc.Meta{}
	}
	dir := path.Dir(f.Rel)
	r := Record{
		ID: firstNonEmpty(m.String("id"), path.Base(dir)), Kind: string(corpus.Script), Path: dir + "/",
		Title: firstNonEmpty(m.String("id"), path.Base(dir)), Summary: m.String("summary"),
		Status: m.String("status"), SideEffect: m.String("side_effect"), Runtime: m.String("runtime"),
		Aliases: m.Strings("aliases"), Tags: lower(m.Strings("tags")), AppliesTo: m.Strings("applies_to"),
		Bytes: len(raw), Lines: strings.Count(string(raw), "\n"), Meta: m.Map(),
	}
	if parts := strings.Split(strings.TrimPrefix(dir, x.layout.Scripts+"/"), "/"); len(parts) >= 2 {
		r.Domain = parts[0]
	}
	r.Body = strings.Join([]string{m.String("description"), m.String("usage"), examplesText(m)}, "\n")
	r.Links = refLinks(m, "shards", "related")
	return r
}

func examplesText(m *doc.Meta) string {
	v, _ := m.Get("examples")
	items, _ := v.([]any)
	var b strings.Builder
	for _, it := range items {
		if e, ok := it.(map[string]any); ok {
			b.WriteString(doc.Scalar(e["cmd"]) + " " + doc.Scalar(e["explain"]) + "\n")
		}
	}
	return b.String()
}

func refLinks(m *doc.Meta, key, kind string) []LinkRec {
	ids := m.Strings(key)
	out := make([]LinkRec, 0, len(ids))
	for _, id := range ids {
		if id != "" && id != "null" {
			out = append(out, LinkRec{Dst: id, Kind: kind})
		}
	}
	return out
}

// MetaJSON marshals a record's frontmatter deterministically (map keys are
// sorted by encoding/json).
func (r *Record) MetaJSON() string {
	if len(r.Meta) == 0 {
		return "{}"
	}
	b, err := json.Marshal(r.Meta)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// TokensEst estimates tokens as bytes/4.
func (r *Record) TokensEst() int { return (r.Bytes + 3) / 4 }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func lower(in []string) []string {
	for i, s := range in {
		in[i] = strings.ToLower(s)
	}
	return in
}

// truncate shortens s to at most n runes, adding an ellipsis.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func dateOf(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

func sectionFirstLine(d *doc.Doc, heading string) string {
	h, ok := d.Section(heading)
	if !ok {
		return ""
	}
	for _, l := range strings.Split(string(d.Raw[h.Start:h.End]), "\n")[1:] {
		if t := strings.TrimSpace(l); t != "" {
			return truncate(t, 120)
		}
	}
	return ""
}

// Scan extracts records for every corpus file without touching SQLite; it
// backs the degraded search path when the index cannot be opened.
func Scan(inst *config.Instance) ([]Record, error) {
	x := &extractor{layout: corpus.LayoutOf(inst.Config.Paths), cfg: inst.Config}
	files, err := corpus.Walk(inst.Root, x.layout)
	if err != nil {
		return nil, fmt.Errorf("scan corpus: %w", err)
	}
	ix := &Index{x: x}
	var out []Record
	for _, c := range ix.readChanges(files) {
		if c.err == nil {
			out = append(out, c.recs...)
		}
	}
	return out, nil
}
