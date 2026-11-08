// Package lint checks every corpus file against its schema, size limits and
// the cross-file rules (unique ids, resolvable links and references), and
// applies safe content-preserving fixes.
package lint

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/corpus"
	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/finding"
	"github.com/zbmowrey/strata/engine/internal/schema"
)

// Cross-file rules.
const (
	RuleDuplicate = "id/duplicate"
	RuleBroken    = "link/broken"
	RuleAnchor    = "link/anchor"
	RuleAlias     = "link/alias"
	RuleRef       = "ref/unresolved"
	RuleRead      = "file/read"
)

// Options select what to lint.
type Options struct {
	Paths []string      // instance-relative path prefixes; empty = everything
	Kinds []corpus.Kind // empty = all kinds
	Fix   bool
}

// Report is the outcome of a lint run.
type Report struct {
	Findings []finding.Finding
	Fixed    []string
	Checked  int
}

// item is one parsed corpus file.
type item struct {
	doc  *doc.Doc
	meta *doc.Meta
	err  error
	file corpus.File
	id   string
}

// Run lints the instance.
func Run(inst *config.Instance, opt Options) (*Report, error) {
	layout := corpus.LayoutOf(inst.Config.Paths)
	files, err := corpus.Walk(inst.Root, layout)
	if err != nil {
		return nil, err
	}
	items := parseAll(files)
	rep := check(items, inst.Config, layout, opt)
	if !opt.Fix {
		return rep, nil
	}
	fixed, err := applyFixes(items, rep.Findings, inst.Config, opt)
	if err != nil {
		return nil, err
	}
	if len(fixed) == 0 {
		return rep, nil
	}
	files, err = corpus.Walk(inst.Root, layout)
	if err != nil {
		return nil, err
	}
	rep = check(parseAll(files), inst.Config, layout, opt)
	rep.Fixed = fixed
	return rep, nil
}

// parseAll reads and parses files concurrently, preserving order.
func parseAll(files []corpus.File) []*item {
	items := make([]*item, len(files))
	var wg sync.WaitGroup
	next := make(chan int)
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for i := range next {
				items[i] = parseOne(files[i])
			}
		})
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()
	return items
}

func parseOne(f corpus.File) *item {
	it := &item{file: f}
	raw, err := os.ReadFile(f.Abs)
	if err != nil {
		it.err = err
		it.doc = &doc.Doc{Path: f.Rel, Meta: &doc.Meta{}}
		it.meta = it.doc.Meta
		return it
	}
	if f.Kind == corpus.Script {
		it.meta, it.err = doc.ParseYAML(raw)
		if it.meta == nil {
			it.meta = &doc.Meta{}
		}
		it.doc = &doc.Doc{Path: f.Rel, Raw: raw, Meta: it.meta}
	} else {
		it.doc, it.err = doc.Parse(f.Rel, raw)
		it.meta = it.doc.Meta
	}
	it.id = idOf(it)
	return it
}

// idOf returns the identifier a file is addressed by.
func idOf(it *item) string {
	switch it.file.Kind {
	case corpus.Shard, corpus.Hub:
		return strings.TrimSuffix(path.Base(it.file.Rel), ".md")
	case corpus.Question, corpus.Script:
		if id := it.meta.String("id"); id != "" {
			return id
		}
		if it.file.Kind == corpus.Script {
			return path.Base(path.Dir(it.file.Rel))
		}
		return strings.TrimSuffix(path.Base(it.file.Rel), ".md")
	case corpus.Playbook:
		if n := it.meta.String("name"); n != "" {
			return n
		}
		return path.Base(path.Dir(it.file.Rel))
	default:
		return ""
	}
}

func selected(f corpus.File, opt Options) bool {
	if len(opt.Kinds) > 0 {
		ok := false
		for _, k := range opt.Kinds {
			ok = ok || f.Kind == k
		}
		if !ok {
			return false
		}
	}
	if len(opt.Paths) == 0 {
		return true
	}
	for _, p := range opt.Paths {
		p = strings.TrimSuffix(filepath.ToSlash(p), "/")
		if p == "." || f.Rel == p || strings.HasPrefix(f.Rel, p+"/") {
			return true
		}
	}
	return false
}

// check validates every selected item; the id table always spans the whole corpus.
func check(items []*item, cfg config.Config, layout corpus.Layout, opt Options) *Report {
	tbl := buildTable(items)
	rep := &Report{}
	for _, it := range items {
		if !selected(it.file, opt) {
			continue
		}
		rep.Checked++
		rep.Findings = append(rep.Findings, validate(it, cfg, layout)...)
		rep.Findings = append(rep.Findings, tbl.links(it)...)
		rep.Findings = append(rep.Findings, tbl.refs(it)...)
		rep.Findings = append(rep.Findings, tbl.duplicates(it)...)
	}
	finding.Sort(rep.Findings)
	return rep
}

// validate dispatches schema validation by kind.
func validate(it *item, cfg config.Config, layout corpus.Layout) []finding.Finding {
	f := it.file
	if it.doc.Raw == nil && it.err != nil {
		return []finding.Finding{{Rule: RuleRead, Severity: finding.Error, File: f.Rel, Line: 1, Col: 1, Message: it.err.Error()}}
	}
	switch f.Kind {
	case corpus.Shard:
		rel := strings.TrimPrefix(f.Rel, layout.Knowledge+"/")
		return schema.Shard(schema.ShardInput{Doc: it.doc, ParseErr: it.err, Rel: rel, File: f.Rel, Private: f.Private, Archived: f.Archived}, cfg.Knowledge)
	case corpus.Hub:
		return schema.Hub(f.Rel, it.doc, it.err)
	case corpus.Inbox:
		return schema.Inbox(f.Rel, it.doc, it.err)
	case corpus.Question:
		return schema.Question(f.Rel, it.doc, it.err)
	case corpus.Playbook:
		return schema.Playbook(f.Rel, it.doc, it.err)
	case corpus.Project:
		return schema.Project(f.Rel, f.Role, it.doc, it.err)
	case corpus.Script:
		return schema.Manifest(f.Rel, it.meta, it.err, cfg.Knowledge.MaxSummary)
	}
	return nil
}
