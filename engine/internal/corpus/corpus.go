// Package corpus discovers and classifies an instance's source files
// (docs/spec/03-search.md §2) with a bounded parallel directory walk.
package corpus

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/zbmowrey/strata/engine/internal/config"
)

// Kind is the corpus type of a file.
type Kind string

// Corpus kinds.
const (
	Shard    Kind = "shard"
	Hub      Kind = "hub"
	Inbox    Kind = "inbox"
	Question Kind = "question"
	Script   Kind = "script"
	Playbook Kind = "playbook"
	Project  Kind = "project"
)

// Kinds lists every kind in display order.
var Kinds = []Kind{Shard, Hub, Inbox, Question, Script, Playbook, Project}

// File is one discovered source file.
type File struct {
	Rel      string // slash-separated path relative to the instance root
	Abs      string
	Kind     Kind
	Role     string // project files: project|plan|handoff|log|out
	Size     int64
	ModNS    int64
	Private  bool // under knowledge/_private/
	Archived bool // under knowledge/_archive/ or projects/_archive/
}

// Layout holds the instance-relative content roots.
type Layout struct {
	Knowledge string
	Scripts   string
	Projects  string
	Skills    []string
}

// LayoutOf returns the layout configured for an instance.
func LayoutOf(p config.Paths) Layout {
	return Layout{Knowledge: filepath.ToSlash(p.Knowledge), Scripts: filepath.ToSlash(p.Scripts), Projects: filepath.ToSlash(p.Projects), Skills: slashAll(p.Skills)}
}

func slashAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = filepath.ToSlash(s)
	}
	return out
}

// Classify maps an instance-relative slash path to a file record (without
// stat fields). ok is false for files that are not part of the corpus.
func (l Layout) Classify(rel string) (File, bool) {
	f := File{Rel: rel}
	switch {
	case under(rel, l.Knowledge):
		return l.classifyKnowledge(f, strings.TrimPrefix(rel, l.Knowledge+"/"))
	case under(rel, l.Scripts):
		ok := path(rel).base() == "script.yaml" && !skippedScriptPath(strings.TrimPrefix(rel, l.Scripts+"/"))
		f.Kind = Script
		return f, ok
	case under(rel, l.Projects):
		return classifyProject(f, strings.TrimPrefix(rel, l.Projects+"/"))
	}
	for _, s := range l.Skills {
		if under(rel, s) {
			parts := strings.Split(strings.TrimPrefix(rel, s+"/"), "/")
			f.Kind = Playbook
			return f, len(parts) == 2 && parts[1] == "SKILL.md"
		}
	}
	return f, false
}

func (l Layout) classifyKnowledge(f File, sub string) (File, bool) {
	if !strings.HasSuffix(sub, ".md") || hasDotSegment(sub) {
		return f, false
	}
	first, _, nested := strings.Cut(sub, "/")
	f.Kind = Shard
	switch first {
	case "_hubs":
		f.Kind = Hub
	case "_inbox":
		f.Kind = Inbox
	case "_questions":
		f.Kind = Question
	case "_archive":
		f.Archived = true
	case "_private":
		f.Private = true
	default:
		if strings.HasPrefix(first, "_") || !nested {
			return f, !nested && !strings.HasPrefix(first, "_") // root-level .md: a misplaced shard, reported by lint
		}
	}
	return f, true
}

func classifyProject(f File, sub string) (File, bool) {
	parts := strings.Split(sub, "/")
	f.Kind = Project
	if len(parts) >= 1 && parts[0] == "_archive" {
		f.Archived = true
		parts = parts[1:]
	}
	if len(parts) == 2 {
		switch parts[1] {
		case "project.md", "plan.md", "handoff.md", "log.md":
			f.Role = strings.TrimSuffix(parts[1], ".md")
			return f, true
		}
	}
	if len(parts) >= 3 && parts[1] == "out" && !hasDotSegment(sub) {
		f.Role = "out"
		return f, true
	}
	return f, false
}

type path string

func (p path) base() string {
	if i := strings.LastIndexByte(string(p), '/'); i >= 0 {
		return string(p[i+1:])
	}
	return string(p)
}

func under(rel, dir string) bool { return strings.HasPrefix(rel, dir+"/") }

func hasDotSegment(p string) bool {
	return strings.HasPrefix(p, ".") || strings.Contains(p, "/.")
}

var skippedScriptDirs = map[string]bool{"lib": true, "node_modules": true, "testdata": true, "vendor": true, "target": true}

func skippedScriptPath(sub string) bool {
	for _, seg := range strings.Split(sub, "/") {
		if skippedScriptDirs[seg] || strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

// skipDir reports whether a directory is never descended into.
func (l Layout) skipDir(rel string) bool {
	name := path(rel).base()
	if strings.HasPrefix(name, ".") && !l.isSkillAncestor(rel) {
		return true
	}
	switch name {
	case "node_modules", ".git", "scratch", "vendor", "target", "__pycache__", ".venv":
		return true
	}
	return false
}

func (l Layout) isSkillAncestor(rel string) bool {
	for _, s := range l.Skills {
		if s == rel || strings.HasPrefix(s, rel+"/") || strings.HasPrefix(rel, s+"/") {
			return true
		}
	}
	return false
}

// roots returns the top-level directories to walk.
func (l Layout) roots() []string {
	r := append([]string{l.Knowledge, l.Scripts, l.Projects}, l.Skills...)
	slices.Sort(r)
	return slices.Compact(r)
}

// Walk discovers every corpus file under root, sorted by Rel.
func Walk(root string, l Layout) ([]File, error) {
	w := &walker{root: root, layout: l, sem: make(chan struct{}, max(4, runtime.GOMAXPROCS(0)*2))}
	for _, r := range l.roots() {
		fi, err := os.Lstat(filepath.Join(root, filepath.FromSlash(r)))
		if err != nil || !fi.IsDir() {
			continue
		}
		w.wg.Add(1)
		go w.dir(r)
	}
	w.wg.Wait()
	slices.SortFunc(w.files, func(a, b File) int { return strings.Compare(a.Rel, b.Rel) })
	return w.files, w.err
}

type walker struct {
	err    error
	sem    chan struct{}
	root   string
	files  []File
	layout Layout
	wg     sync.WaitGroup
	mu     sync.Mutex
}

// dir reads one directory; subdirectories are walked concurrently, bounded
// by the semaphore (falling back to inline recursion when it is full).
func (w *walker) dir(rel string) {
	defer w.wg.Done()
	w.readDir(rel)
}

func (w *walker) readDir(rel string) {
	entries, err := os.ReadDir(filepath.Join(w.root, filepath.FromSlash(rel)))
	if err != nil {
		w.fail(err)
		return
	}
	var local []File
	for _, e := range entries {
		child := rel + "/" + e.Name()
		switch {
		case e.Type()&fs.ModeSymlink != 0:
			continue // never follow symlinks (docs/spec/11-security.md §5)
		case e.IsDir():
			if !w.layout.skipDir(child) {
				w.spawn(child)
			}
		case e.Type().IsRegular():
			if f, ok := w.layout.Classify(child); ok {
				if info, err := e.Info(); err == nil {
					f.Abs = filepath.Join(w.root, filepath.FromSlash(child))
					f.Size, f.ModNS = info.Size(), info.ModTime().UnixNano()
					local = append(local, f)
				}
			}
		}
	}
	if len(local) > 0 {
		w.mu.Lock()
		w.files = append(w.files, local...)
		w.mu.Unlock()
	}
}

func (w *walker) spawn(child string) {
	select {
	case w.sem <- struct{}{}:
		w.wg.Add(1)
		go func() {
			defer func() { <-w.sem }()
			w.dir(child)
		}()
	default:
		w.readDir(child)
	}
}

func (w *walker) fail(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err == nil {
		w.err = err
	}
}
