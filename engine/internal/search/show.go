package search

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/graph"
	"github.com/zbmowrey/strata/engine/internal/index"
)

// ShowMode selects which part of each document to return.
type ShowMode int

// Show modes.
const (
	ShowFull ShowMode = iota
	ShowMeta
	ShowBody
	ShowBrief
)

// ShowRequest asks for documents by id.
type ShowRequest struct {
	IDs    []string
	Links  int // BFS depth 0-2
	Budget int // tokens (bytes/4); 0 = unlimited
	Mode   ShowMode
}

// ShownDoc is one document in a show response.
type ShownDoc struct {
	Meta       map[string]any `json:"meta,omitempty"`
	Requested  string         `json:"requested,omitempty"`
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Type       string         `json:"type,omitempty"`
	Path       string         `json:"path"`
	Title      string         `json:"title"`
	Summary    string         `json:"summary,omitempty"`
	Status     string         `json:"status,omitempty"`
	Trust      string         `json:"trust,omitempty"`
	Updated    string         `json:"updated,omitempty"`
	VerifiedAt string         `json:"verified_at,omitempty"`
	SideEffect string         `json:"side_effect,omitempty"`
	Section    string         `json:"section,omitempty"`
	Content    string         `json:"content"`
	Truncated  string         `json:"truncated,omitempty"`
	LinksOut   int            `json:"links_out"`
	LinksIn    int            `json:"links_in"`
	Linked     bool           `json:"linked,omitempty"`
	Private    bool           `json:"private,omitempty"`
	sections   []doc.Heading
	fm         string
}

// Missing is a requested id that does not resolve.
type Missing struct {
	ID          string   `json:"id"`
	Suggestions []string `json:"suggestions,omitempty"`
}

// ShowResult is the response to a show request.
type ShowResult struct {
	Docs    []ShownDoc `json:"docs"`
	Missing []Missing  `json:"missing,omitempty"`
}

// ErrNoSection reports an unknown #section on an existing document.
var ErrNoSection = errors.New("no such section")

// Show resolves and loads documents in the order requested (deduplicated,
// first occurrence wins), then the link neighborhood, within the budget.
func (s *Searcher) Show(ctx context.Context, root string, req ShowRequest) (*ShowResult, error) {
	res := &ShowResult{}
	seen := map[string]bool{}
	var printed []string
	for _, raw := range req.IDs {
		raw = strings.TrimSpace(raw)
		if raw == "" || seen[raw] {
			continue
		}
		seen[raw] = true
		d, err := s.loadDoc(ctx, root, raw)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrNoSection) {
			res.Missing = append(res.Missing, Missing{ID: raw, Suggestions: s.didYouMean(ctx, raw)})
			continue
		}
		if err != nil {
			return nil, err
		}
		if slices.Contains(printed, d.ID) && d.Section == "" {
			continue
		}
		d.Requested = raw
		res.Docs = append(res.Docs, *d)
		printed = append(printed, d.ID)
	}
	if req.Links > 0 && len(printed) > 0 {
		if err := s.appendLinked(ctx, root, req.Links, printed, res); err != nil {
			return nil, err
		}
	}
	for i := range res.Docs {
		applyMode(&res.Docs[i], req.Mode)
	}
	applyBudget(res, req.Budget)
	return res, nil
}

func (s *Searcher) appendLinked(ctx context.Context, root string, depth int, printed []string, res *ShowResult) error {
	ids, err := graph.New(s.db()).Neighborhood(ctx, printed, min(depth, 2))
	if err != nil {
		return err
	}
	for _, id := range ids {
		d, err := s.loadDoc(ctx, root, id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if d.Private || d.Status == "archived" {
			continue
		}
		d.Linked = true
		res.Docs = append(res.Docs, *d)
	}
	return nil
}

// resolveID finds the docs row for an id, alias, or id#section.
func (s *Searcher) resolveID(ctx context.Context, raw string) (rowid int64, section string, err error) {
	q := "SELECT rowid FROM docs WHERE id = ? UNION ALL SELECT doc FROM aliases WHERE alias = lower(?) LIMIT 1"
	if err = s.db().QueryRowContext(ctx, q, raw, raw).Scan(&rowid); err == nil {
		return rowid, "", nil
	}
	i := strings.LastIndexByte(raw, '#')
	if i <= 0 {
		return 0, "", sql.ErrNoRows
	}
	id, section := raw[:i], raw[i+1:]
	if err = s.db().QueryRowContext(ctx, q, id, id).Scan(&rowid); err != nil {
		return 0, "", fmt.Errorf("resolve %s: %w", raw, err)
	}
	return rowid, section, nil
}

func (s *Searcher) loadDoc(ctx context.Context, root, raw string) (*ShownDoc, error) {
	rowid, section, err := s.resolveID(ctx, raw)
	if err != nil {
		return nil, err
	}
	d := &ShownDoc{Section: section}
	var anchor, metaJSON sql.NullString
	var typ, status, trust, summary, updated, verified, side sql.NullString
	err = s.db().QueryRowContext(ctx, `SELECT id, kind, type, path, title, summary, status, trust, updated, verified_at, side_effect, private, anchor, meta_json
		FROM docs WHERE rowid = ?`, rowid).Scan(&d.ID, &d.Kind, &typ, &d.Path, &d.Title, &summary, &status, &trust, &updated, &verified, &side, &d.Private, &anchor, &metaJSON)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", raw, err)
	}
	d.Type, d.Summary, d.Status, d.Trust, d.Updated, d.VerifiedAt, d.SideEffect = typ.String, summary.String, status.String, trust.String, updated.String, verified.String, side.String
	if err := d.read(root, anchor.String); err != nil {
		return nil, err
	}
	if section != "" {
		if err := d.slice(section); err != nil {
			return nil, err
		}
	}
	if d.LinksOut, d.LinksIn, err = graph.New(s.db()).Counts(ctx, d.ID); err != nil {
		return nil, err
	}
	return d, nil
}

// read loads the document's current content from disk.
func (d *ShownDoc) read(root, anchor string) error {
	if d.Kind == "script" {
		raw, err := os.ReadFile(path.Join(root, d.Path, "script.yaml")) //nolint:gosec // G304: path from the index, inside the root
		if err != nil {
			return fmt.Errorf("read %s: %w", d.Path, err)
		}
		m, _ := doc.ParseYAML(raw)
		if m != nil {
			d.Meta = m.Map()
		}
		d.Content = renderManifest(m)
		d.fm = string(raw)
		return nil
	}
	raw, err := os.ReadFile(path.Join(root, d.Path)) //nolint:gosec // G304: path from the index, inside the root
	if err != nil {
		return fmt.Errorf("read %s: %w", d.Path, err)
	}
	parsed, _ := doc.Parse(d.Path, raw)
	d.Meta = parsed.Meta.Map()
	d.fm = string(parsed.Frontmatter)
	body := parsed.Body()
	d.sections = parsed.Headings
	if anchor != "" {
		for _, e := range index.LogEntries(parsed) {
			if e.Anchor == anchor {
				body = raw[e.Heading.Start:e.Heading.End]
				d.sections = nil
				break
			}
		}
	}
	d.Content = string(body)
	d.sections = rebase(d.sections, parsed.BodyStart)
	return nil
}

func rebase(hs []doc.Heading, off int) []doc.Heading {
	out := make([]doc.Heading, 0, len(hs))
	for _, h := range hs {
		h.Start -= off
		h.End -= off
		if h.Start >= 0 {
			out = append(out, h)
		}
	}
	return out
}

// slice narrows Content to one section.
func (d *ShownDoc) slice(section string) error {
	want := doc.Slug(section)
	for _, h := range d.sections {
		if h.Slug == want && h.End <= len(d.Content) {
			d.Content = d.Content[h.Start:h.End]
			d.Section = h.Slug
			return nil
		}
	}
	return ErrNoSection
}

func applyMode(d *ShownDoc, mode ShowMode) {
	switch mode {
	case ShowMeta:
		d.Content = d.fm
		d.Meta = nil
	case ShowBrief:
		fp, _ := doc.Parse("", []byte(d.Content))
		d.Content = strings.TrimSpace(d.Summary + "\n\n" + fp.FirstParagraph())
		d.Meta = nil
	case ShowBody:
		d.Meta = nil
	case ShowFull:
		d.Meta = nil
	}
}

// applyBudget enforces a token budget (≈ bytes/4): linked documents are
// dropped from the tail first; requested documents are truncated at a
// section boundary with a marker.
func applyBudget(res *ShowResult, budget int) {
	if budget <= 0 {
		return
	}
	limit := budget * 4
	used := 0
	for _, d := range res.Docs {
		used += len(d.Content) + 120
	}
	for used > limit && len(res.Docs) > 0 && res.Docs[len(res.Docs)-1].Linked {
		used -= len(res.Docs[len(res.Docs)-1].Content) + 120
		res.Docs = res.Docs[:len(res.Docs)-1]
	}
	if used <= limit {
		return
	}
	per := max(limit/max(len(res.Docs), 1)-120, 200)
	for i := range res.Docs {
		truncateDoc(&res.Docs[i], per)
	}
}

// truncateDoc cuts Content to at most n bytes at the last section boundary
// (or line boundary) that fits.
func truncateDoc(d *ShownDoc, n int) {
	if len(d.Content) <= n {
		return
	}
	cut := 0
	next := ""
	for _, h := range d.sections {
		if h.Start <= n && h.Start > cut {
			cut = h.Start
		} else if h.Start > n && next == "" {
			next = h.Slug
		}
	}
	if cut == 0 {
		cut = strings.LastIndexByte(d.Content[:n], '\n') + 1
	}
	if cut <= 0 {
		cut = n
	}
	rest := (len(d.Content) - cut + 3) / 4
	target := d.ID
	if sec := sectionAt(d.sections, cut); sec != "" {
		target += "#" + sec
	} else if next != "" {
		target += "#" + next
	}
	d.Content = d.Content[:cut]
	d.Truncated = fmt.Sprintf("… [truncated: %d more tokens — show %s]", rest, target)
}

func sectionAt(hs []doc.Heading, off int) string {
	for _, h := range hs {
		if h.Start == off {
			return h.Slug
		}
	}
	return ""
}

// didYouMean suggests ids similar to a missing one.
func (s *Searcher) didYouMean(ctx context.Context, raw string) []string {
	id, _, _ := strings.Cut(raw, "#")
	all, err := buildWhere(nil, Scope{})
	if err != nil {
		return nil
	}
	sims, ids, err := s.fuzzyCandidates(ctx, []string{strings.ToLower(id)}, all, 0.67)
	if err != nil {
		return nil
	}
	out := rankBySim(sims, ids)
	return out[:min(3, len(out))]
}

// renderManifest renders a script manifest for show.
func renderManifest(m *doc.Meta) string {
	if m == nil {
		return ""
	}
	var b strings.Builder
	line := func(label, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, v)
		}
	}
	b.WriteString(strings.TrimSpace(m.String("summary")) + "\n\n")
	if desc := strings.TrimSpace(m.String("description")); desc != "" && desc != strings.TrimSpace(m.String("summary")) {
		b.WriteString(desc + "\n\n")
	}
	line("usage", m.String("usage"))
	line("runtime", m.String("runtime"))
	line("side effect", m.String("side_effect"))
	line("writes", strings.Join(m.Strings("writes"), ", "))
	line("network", strings.Join(m.Strings("network"), ", "))
	line("requires", strings.Join(m.Strings("requires"), ", "))
	line("status", m.String("status"))
	if v, ok := m.Get("examples"); ok {
		if items, isList := v.([]any); isList && len(items) > 0 {
			b.WriteString("examples:\n")
			for _, it := range items {
				if e, isMap := it.(map[string]any); isMap {
					fmt.Fprintf(&b, "  %s\n      %s\n", doc.Scalar(e["cmd"]), doc.Scalar(e["explain"]))
				}
			}
		}
	}
	return b.String()
}
