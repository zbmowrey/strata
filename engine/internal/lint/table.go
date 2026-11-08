package lint

import (
	"fmt"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/corpus"
	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/finding"
)

// table maps identifiers to the items that declare them.
type table struct {
	knowledge map[string][]*item // shard, hub and question ids
	scripts   map[string][]*item
	playbooks map[string]*item
	aliases   map[string]string // alias → shard id
}

func buildTable(items []*item) *table {
	t := &table{knowledge: map[string][]*item{}, scripts: map[string][]*item{}, playbooks: map[string]*item{}, aliases: map[string]string{}}
	for _, it := range items {
		if it.id == "" {
			continue
		}
		switch it.file.Kind {
		case corpus.Shard, corpus.Hub, corpus.Question:
			t.knowledge[it.id] = append(t.knowledge[it.id], it)
			if it.file.Kind == corpus.Shard {
				for _, a := range it.meta.Strings("aliases") {
					t.aliases[a] = it.id
				}
			}
		case corpus.Script:
			t.scripts[it.id] = append(t.scripts[it.id], it)
		case corpus.Playbook:
			t.playbooks[it.id] = it
		default:
		}
	}
	return t
}

// target resolves a link target to an item that holds markdown.
func (t *table) target(id string) (*item, bool) {
	if its := t.knowledge[id]; len(its) > 0 {
		return its[0], true
	}
	if it, ok := t.playbooks[id]; ok {
		return it, true
	}
	if its := t.scripts[id]; len(its) > 0 {
		return its[0], true
	}
	return nil, false
}

func (t *table) duplicates(it *item) []finding.Finding {
	var others []*item
	switch it.file.Kind {
	case corpus.Shard, corpus.Hub, corpus.Question:
		others = t.knowledge[it.id]
	case corpus.Script:
		others = t.scripts[it.id]
	default:
		return nil
	}
	if len(others) < 2 {
		return nil
	}
	var paths []string
	for _, o := range others {
		if o != it {
			paths = append(paths, o.file.Rel)
		}
	}
	return []finding.Finding{{
		Rule: RuleDuplicate, Severity: finding.Error, File: it.file.Rel, Line: 1, Col: 1,
		Message: fmt.Sprintf("id %q is also used by %s", it.id, strings.Join(paths, ", ")), FixHint: "ids are globally unique; rename one with `strata mv`",
	}}
}

// links checks every wikilink in a markdown item.
func (t *table) links(it *item) []finding.Finding {
	if it.file.Kind == corpus.Script || it.doc == nil {
		return nil
	}
	sev := finding.Warning
	if it.file.Kind == corpus.Shard || it.file.Kind == corpus.Hub {
		sev = finding.Error
	}
	var out []finding.Finding
	for _, l := range it.doc.Links {
		if f, ok := t.checkLink(it, l, sev); ok {
			out = append(out, f)
		}
	}
	return out
}

func (t *table) checkLink(it *item, l doc.Link, sev string) (finding.Finding, bool) {
	mk := func(sev, rule, hint, format string, args ...any) (finding.Finding, bool) {
		return finding.Finding{Rule: rule, Severity: sev, File: it.file.Rel, Line: l.Line, Col: l.Col, Message: fmt.Sprintf(format, args...), FixHint: hint}, true
	}
	if l.Target == "" {
		if _, ok := it.doc.Section(l.Anchor); !ok && l.Anchor != "" {
			return mk(finding.Warning, RuleAnchor, "", "no heading %q in this document", l.Anchor)
		}
		return finding.Finding{}, false
	}
	if strings.Contains(l.Target, ".") {
		return finding.Finding{}, false // attachment embed (image, pdf); not a document id
	}
	dst, ok := t.target(l.Target)
	if !ok {
		if canon, isAlias := t.aliases[l.Target]; isAlias {
			return mk(finding.Warning, RuleAlias, "run `strata lint --fix` to rewrite it", "%s links to alias %q; use the id %q", l.Raw, l.Target, canon)
		}
		return mk(sev, RuleBroken, "fix the id, or create the shard (`strata new shard`)", "%s: no document with id %q", l.Raw, l.Target)
	}
	if l.Anchor != "" && dst.file.Kind != corpus.Script {
		if _, has := dst.doc.Section(l.Anchor); !has {
			return mk(finding.Warning, RuleAnchor, "", "%s: %q has no heading %q", l.Raw, l.Target, l.Anchor)
		}
	}
	return finding.Finding{}, false
}

// refs checks frontmatter/manifest references to other ids.
func (t *table) refs(it *item) []finding.Finding {
	type ref struct {
		key   string
		ids   []string
		space map[string][]*item
	}
	var refs []ref
	switch it.file.Kind {
	case corpus.Shard:
		verify := ""
		if v, ok := it.meta.Get("verify"); ok {
			if m, isMap := v.(map[string]any); isMap {
				verify = doc.Scalar(m["script"])
			}
		}
		refs = []ref{
			{"scripts", it.meta.Strings("scripts"), t.scripts},
			{"supersedes", it.meta.Strings("supersedes"), t.knowledge},
			{"superseded_by", nonEmpty(it.meta.String("superseded_by")), t.knowledge},
			{"verify", nonEmpty(verify), t.scripts},
		}
	case corpus.Script:
		refs = []ref{{"shards", it.meta.Strings("shards"), t.knowledge}, {"replaced_by", nonEmpty(it.meta.String("replaced_by")), t.scripts}}
	case corpus.Playbook:
		refs = []ref{{"shards", it.meta.Strings("shards"), t.knowledge}, {"scripts", it.meta.Strings("scripts"), t.scripts}}
	default:
		return nil
	}
	var out []finding.Finding
	for _, r := range refs {
		for _, id := range r.ids {
			if id == "" || id == "null" || len(r.space[id]) > 0 {
				continue
			}
			line, col := it.meta.Pos(r.key)
			out = append(out, finding.Finding{
				Rule: RuleRef, Severity: finding.Warning, File: it.file.Rel, Line: max(line, 1), Col: max(col, 1),
				Message: fmt.Sprintf("%s: %q does not resolve", r.key, id),
			})
		}
	}
	return out
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}
