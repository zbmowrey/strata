// Package projects implements Strata's local working memory: project
// lifecycle, the append-only chronicle, handoff notes, resume summaries and
// the generated .gitignore block (docs/spec/05-projects.md).
package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/schema"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
)

// Adhoc is the standing project for work not tied to a named project.
const Adhoc = "_adhoc"

// Project file names.
const (
	ProjectFile = "project.md"
	PlanFile    = "plan.md"
	LogFile     = "log.md"
	HandoffFile = "handoff.md"
)

// Statuses.
const (
	StatusActive   = "active"
	StatusPaused   = "paused"
	StatusDone     = "done"
	StatusArchived = "archived"
)

// Project is a project's identity and state (project.md frontmatter).
type Project struct {
	Slug     string   `json:"slug"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Goal     string   `json:"goal,omitempty"`
	Created  string   `json:"created,omitempty"`
	Updated  string   `json:"updated,omitempty"`
	Dir      string   `json:"dir"` // instance-relative, slash-separated
	Tags     []string `json:"tags,omitempty"`
	Track    []string `json:"track,omitempty"`
	Archived bool     `json:"archived,omitempty"`
}

// Store operates on an instance's projects.
type Store struct {
	inst    *config.Instance
	now     func() time.Time
	tel     *telemetry.Writer
	session string
}

// New returns a Store; now may be nil (time.Now). The session id comes from
// the environment (telemetry.SessionEnv); hooks override it with WithSession.
func New(inst *config.Instance, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	s := &Store{inst: inst, now: now, tel: telemetry.New(inst, now)}
	for _, k := range telemetry.SessionEnv {
		if v := os.Getenv(k); v != "" {
			s.session = v
			break
		}
	}
	return s
}

// WithSession returns a copy of s attributing writes to session.
func (s *Store) WithSession(session string) *Store {
	c := *s
	c.session = session
	c.tel = s.tel.WithSession(session)
	return &c
}

// Session4 is the short session tag written into log headings (`s:3f9a`).
func Session4(session string) string {
	id := strings.ToLower(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, session))
	if id == "" {
		return "cli"
	}
	return id[:min(4, len(id))]
}

func (s *Store) root() string {
	return s.inst.Path(filepath.FromSlash(s.inst.Config.Paths.Projects))
}

// dir returns the absolute directory of an active (non-archived) project.
func (s *Store) dir(slug string) string { return filepath.Join(s.root(), slug) }

// ValidSlug reports whether slug is a valid project slug.
func ValidSlug(slug string) bool {
	return slug == Adhoc || (schema.IDPattern.MatchString(slug) && len(slug) >= 3 && len(slug) <= 64)
}

func checkSlug(slug string) error {
	if !ValidSlug(slug) {
		return fail.New(fail.Usage, "use lowercase kebab-case, 3–64 chars", "invalid project slug %q", slug)
	}
	return nil
}

// ErrNotFound reports a missing project.
var ErrNotFound = errors.New("project not found")

func notFound(slug string) error {
	return fail.Wrap(fail.Usage, "see `strata project list --all`", fmt.Errorf("%w: %s", ErrNotFound, slug))
}

// Create scaffolds a new project and returns its directory.
func (s *Store) Create(slug, title, goal string, tags []string) (string, error) {
	if err := checkSlug(slug); err != nil {
		return "", err
	}
	if slug == Adhoc {
		return "", fail.New(fail.Usage, "the _adhoc project is created automatically", "cannot create %s", Adhoc)
	}
	dir := s.dir(slug)
	if _, err := os.Stat(dir); err == nil {
		return "", fail.New(fail.Usage, "pick another slug, or `strata project show "+slug+"`", "project %s already exists", slug)
	}
	if strings.TrimSpace(title) == "" {
		return "", fail.New(fail.Usage, `strata project new <slug> "<title>" --goal "…"`, "a project needs a title")
	}
	if err := s.scaffold(slug, title, goal, tags); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *Store) scaffold(slug, title, goal string, tags []string) error {
	today := s.now().Format(time.DateOnly)
	if strings.TrimSpace(goal) == "" {
		goal = "TBD"
	}
	project := fmt.Sprintf("---\nid: %s\ntitle: %s\nstatus: active\ngoal: %s\ncreated: %s\nupdated: %s\ntags: [%s]\nshards: []\ntrack: []\n---\n%s\n",
		slug, yamlScalar(title), yamlScalar(goal), today, today, strings.Join(tags, ", "),
		"Why this project exists, constraints, links.")
	files := map[string]string{
		ProjectFile: project,
		PlanFile:    "# Plan — " + title + "\n\n1. \n",
		LogFile:     "# Log — " + slug + "\n",
		HandoffFile: s.renderHandoff(handoffParts{State: "Just started.", Next: "1. Write the plan (plan.md).", Open: "- none yet"}),
	}
	for _, name := range []string{ProjectFile, PlanFile, LogFile, HandoffFile} {
		if err := fsutil.WriteAtomic(filepath.Join(s.dir(slug), name), []byte(files[name]), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// yamlScalar quotes a value when YAML would misread it.
func yamlScalar(v string) string {
	v = strings.ReplaceAll(strings.TrimSpace(v), "\n", " ")
	if v == "" || strings.ContainsAny(v, ":#[]{},&*!|>'\"%@`") || strings.HasPrefix(v, "-") || strings.HasPrefix(v, "?") {
		return `"` + strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`) + `"`
	}
	return v
}

// ensureAdhoc creates the standing _adhoc project on first use.
func (s *Store) ensureAdhoc() error {
	if _, err := os.Stat(filepath.Join(s.dir(Adhoc), ProjectFile)); err == nil {
		return nil
	}
	return s.scaffold(Adhoc, "Ad-hoc work", "Chronicle for sessions not tied to a named project.", nil)
}

// Load reads a project (active or archived). _adhoc is created on demand.
func (s *Store) Load(slug string) (*Project, error) {
	if err := checkSlug(slug); err != nil {
		return nil, err
	}
	if slug == Adhoc {
		if err := s.ensureAdhoc(); err != nil {
			return nil, err
		}
	}
	for _, dir := range []string{s.dir(slug), filepath.Join(s.root(), "_archive", slug)} {
		raw, err := os.ReadFile(filepath.Join(dir, ProjectFile)) //nolint:gosec // G304: path built from a validated slug
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read project %s: %w", slug, err)
		}
		return s.parse(slug, dir, raw, dir != s.dir(slug)), nil
	}
	return nil, notFound(slug)
}

func (s *Store) parse(slug, dir string, raw []byte, archived bool) *Project {
	d, _ := doc.Parse(ProjectFile, raw)
	m := d.Meta
	rel, _ := filepath.Rel(s.inst.Root, dir)
	p := &Project{
		Slug: slug, Title: m.String("title"), Status: m.String("status"), Goal: m.String("goal"),
		Created: m.String("created"), Updated: m.String("updated"), Tags: m.Strings("tags"), Track: m.Strings("track"),
		Dir: filepath.ToSlash(rel), Archived: archived,
	}
	if p.Status == "" {
		p.Status = StatusActive
	}
	if p.Title == "" {
		p.Title = slug
	}
	return p
}

// All lists projects (active folders, then _archive), sorted by slug.
func (s *Store) All() ([]*Project, error) {
	var out []*Project
	for _, base := range []string{s.root(), filepath.Join(s.root(), "_archive")} {
		entries, err := os.ReadDir(base)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("list projects: %w", err)
		}
		for _, e := range entries {
			if !e.IsDir() || !ValidSlug(e.Name()) {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(base, e.Name(), ProjectFile)) //nolint:gosec // G304: walking the projects dir
			if err != nil {
				continue
			}
			out = append(out, s.parse(e.Name(), filepath.Join(base, e.Name()), raw, base != s.root()))
		}
	}
	slices.SortStableFunc(out, func(a, b *Project) int { return strings.Compare(a.Slug, b.Slug) })
	return out, nil
}

// SetStatus changes a project's status (pause, close, reactivate). Closing
// sets `done`; the gardener's distill-project task picks done projects up.
func (s *Store) SetStatus(slug, status string) error {
	if !slices.Contains(schema.ProjectStatuses, status) {
		return fail.New(fail.Usage, "", "invalid status %q", status)
	}
	p, err := s.Load(slug)
	if err != nil {
		return err
	}
	return s.editFrontmatter(p, map[string]string{"status": status, "updated": s.now().Format(time.DateOnly)})
}

// Track adds globs (relative to the project folder) to its track: list.
func (s *Store) Track(slug string, globs []string) error {
	p, err := s.Load(slug)
	if err != nil {
		return err
	}
	merged := slices.Clone(p.Track)
	for _, g := range globs {
		if err := ValidateTrack(g); err != nil {
			return err
		}
		if !slices.Contains(merged, g) {
			merged = append(merged, g)
		}
	}
	quoted := make([]string, len(merged))
	for i, g := range merged {
		quoted[i] = `"` + g + `"`
	}
	return s.editFrontmatter(p, map[string]string{"track": "[" + strings.Join(quoted, ", ") + "]", "updated": s.now().Format(time.DateOnly)})
}

func (s *Store) lockProject(dir string) (*fsutil.Lock, error) {
	lock, err := fsutil.LockWait(filepath.Join(dir, ".lock"), 10*time.Second)
	if err != nil {
		return nil, fail.Wrap(fail.Locked, "another strata process is writing this project; retry", err)
	}
	return lock, nil
}

func (s *Store) editFrontmatter(p *Project, kv map[string]string) error {
	dir := filepath.Join(s.inst.Root, filepath.FromSlash(p.Dir))
	lock, err := s.lockProject(dir)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Unlock() }()
	path := filepath.Join(dir, ProjectFile)
	raw, err := os.ReadFile(path) //nolint:gosec // G304: path from a loaded project
	if err != nil {
		return fmt.Errorf("read project %s: %w", p.Slug, err)
	}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		raw = SetKey(raw, k, kv[k])
	}
	return fsutil.WriteAtomic(path, raw, 0o644)
}

// SetKey replaces (or adds before the closing ---) a top-level frontmatter
// key with `key: value`, dropping the old value's block continuation lines.
// Everything else is preserved verbatim; input without frontmatter is
// returned unchanged.
func SetKey(raw []byte, key, value string) []byte {
	lines := strings.SplitAfter(string(raw), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r\n") != "---" {
		return raw
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r\n") == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return raw
	}
	newLine := key + ": " + value + "\n"
	for i := 1; i < end; i++ {
		if !strings.HasPrefix(lines[i], key+":") {
			continue
		}
		j := i + 1
		for j < end && (strings.HasPrefix(lines[j], " ") || strings.HasPrefix(lines[j], "\t") || strings.HasPrefix(lines[j], "- ")) {
			j++
		}
		out := append(append(slices.Clone(lines[:i]), newLine), lines[j:]...)
		return []byte(strings.Join(out, ""))
	}
	out := append(append(slices.Clone(lines[:end]), newLine), lines[end:]...)
	return []byte(strings.Join(out, ""))
}
