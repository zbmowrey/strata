package schema

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/finding"
)

const good = `---
id: good-shard
title: Good
summary: A good shard.
type: fact
tags: [a]
status: active
trust: verified
created: 2026-01-01
updated: 2026-01-01T10:00:00Z
sources:
  - { kind: file, ref: x.go, at: 2026-01-01 }
verified_at: 2026-01-02
verify: { script: check-it, args: [] }
occurrences: 2
---
Body [[a]] [[b]].

## One
## Two
`

func rules(fs []finding.Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	sort.Strings(out)
	return slices.Compact(out)
}

func shard(t *testing.T, src, rel string, k config.Knowledge, private bool) []string {
	t.Helper()
	d, err := doc.Parse(rel, []byte(src))
	return rules(Shard(ShardInput{Doc: d, ParseErr: err, Rel: rel, File: "knowledge/" + rel, Private: private, Archived: strings.HasPrefix(rel, "_archive/")}, k))
}

func TestShardRules(t *testing.T) {
	t.Parallel()
	k := config.Default().Knowledge
	if got := shard(t, good, "d/t/good-shard.md", k, false); len(got) != 0 {
		t.Fatalf("good shard: %v", got)
	}
	if got := shard(t, good, "_archive/d/t/good-shard.md", k, false); len(got) != 0 {
		t.Fatalf("archived good shard: %v", got)
	}
	tight := k
	tight.MaxSections, tight.MaxLinks, tight.MaxBodyBytes, tight.ExtraRequired = 1, 1, 10, []string{"owner"}
	got := shard(t, good, "d/t/good-shard.md", tight, false)
	for _, want := range []string{RuleSections, RuleLinks, RuleBodyBytes, RuleRequired} {
		if !slices.Contains(got, want) {
			t.Errorf("tight limits: missing %s in %v", want, got)
		}
	}
	cases := map[string]struct{ from, to, rule string }{
		"tags not list":        {"tags: [a]", "tags: a", RuleType},
		"tags item map":        {"tags: [a]", "tags: [{x: 1}]", RuleType},
		"title list":           {"title: Good", "title: [a]", RuleType},
		"sources not list":     {"sources:\n  - { kind: file, ref: x.go, at: 2026-01-01 }", "sources: nope", RuleType},
		"source missing ref":   {"ref: x.go, ", "", RuleType},
		"source bad kind":      {"kind: file", "kind: rumor", RuleEnum},
		"source not map":       {"  - { kind: file, ref: x.go, at: 2026-01-01 }", "  - just-text", RuleTrustSources},
		"multiline summary":    {"summary: A good shard.", "summary: |\n  line one\n  line two", RuleSummary},
		"superseded_by orphan": {"occurrences: 2", "superseded_by: x", RuleSuperseded},
		"sensitive public":     {"occurrences: 2", "sensitive: true", RuleSensitive},
		"bad verify":           {"verify: { script: check-it, args: [] }", "verify: check-it", RuleType},
		"bad occurrences":      {"occurrences: 2", "occurrences: -1", RuleType},
		"short id":             {"id: good-shard", "id: ab", RuleIDFormat},
	}
	for name, tc := range cases {
		src := strings.Replace(good, tc.from, tc.to, 1)
		if got := shard(t, src, "d/t/good-shard.md", k, false); !slices.Contains(got, tc.rule) {
			t.Errorf("%s: want %s, got %v", name, tc.rule, got)
		}
	}
	if got := shard(t, strings.Replace(good, "occurrences: 2", "sensitive: true", 1), "_private/d/t/good-shard.md", k, true); len(got) != 0 {
		t.Errorf("private sensitive: %v", got)
	}
	if got := shard(t, "# none\n", "d/t/x.md", k, false); !slices.Equal(got, []string{RuleNoFM}) {
		t.Errorf("no frontmatter: %v", got)
	}
	if got := shard(t, "---\nid: [\n---\n", "d/t/x.md", k, false); !slices.Equal(got, []string{RuleSyntax}) {
		t.Errorf("syntax: %v", got)
	}
}

func TestOtherKinds(t *testing.T) {
	t.Parallel()
	parse := func(src string) (*doc.Doc, error) { return doc.Parse("x.md", []byte(src)) }
	d, err := parse("---\ntype: fact\ncaptured: 2026-01-01\n---\ntext\n")
	if got := rules(Inbox("i.md", d, err)); len(got) != 0 {
		t.Errorf("inbox: %v", got)
	}
	d, err = parse("---\nid: q-1\nstatus: open\nraised_by: claude\nraised: 2026-01-01\nblocking: [x]\n---\n## Question\nWhy?\n")
	if got := rules(Question("knowledge/_questions/q-1.md", d, err)); len(got) != 0 {
		t.Errorf("question: %v", got)
	}
	d, err = parse("---\nid: hub-a\ntitle: A\nsummary: S\ntype: index\n---\n")
	if got := rules(Hub("h.md", d, err)); len(got) != 0 {
		t.Errorf("hub: %v", got)
	}
	d, err = parse("---\nname: x\ndescription: y\ntags: [a]\n---\n")
	if got := rules(Playbook("p.md", d, err)); len(got) != 0 {
		t.Errorf("playbook: %v", got)
	}
	d, err = parse("---\nid: p\ntitle: P\nstatus: active\ncreated: 2026-01-01\nupdated: 2026-01-01\ntrack: [out/*.md]\n---\n")
	if got := rules(Project("projects/p/project.md", "project", d, err)); len(got) != 0 {
		t.Errorf("project: %v", got)
	}
	d, err = parse("---\nupdated: x\n---\n## State\ns\n## Next\nn\n" + strings.Repeat("x\n", 40))
	if got := rules(Project("projects/p/handoff.md", "handoff", d, err)); !slices.Equal(got, []string{RuleHandoff}) {
		t.Errorf("long handoff: %v", got)
	}
	d, err = parse("# Log\n- note: fine\n-not a bullet\n- tried: x\n")
	if got := rules(Project("projects/p/log.md", "log", d, err)); len(got) != 0 {
		t.Errorf("log: %v", got)
	}
	d, err = parse("# Plan\n")
	if got := Project("projects/p/plan.md", "plan", d, err); len(got) != 0 {
		t.Errorf("plan: %v", got)
	}
	for _, f := range []func(string, *doc.Doc, error) []finding.Finding{Inbox, Question, Hub, Playbook} {
		d, err := parse("---\nx: [\n---\n")
		if got := rules(f("x.md", d, err)); !slices.Equal(got, []string{RuleSyntax}) {
			t.Errorf("syntax: %v", got)
		}
	}
	d, err = parse("---\nx: [\n---\n")
	if got := rules(Project("projects/p/project.md", "project", d, err)); !slices.Equal(got, []string{RuleSyntax}) {
		t.Errorf("project syntax: %v", got)
	}
	d, err = parse("---\nx: [\n---\n")
	if got := rules(Project("projects/p/handoff.md", "handoff", d, err)); !slices.Equal(got, []string{RuleSyntax}) {
		t.Errorf("handoff syntax: %v", got)
	}
}

const manifest = `id: tool-x
summary: Does x.
status: active
runtime: go
entry: main.go
usage: tool-x
side_effect: read-only
timeout: 60s
owner: gardener
examples: [{ cmd: "tool-x", explain: "run" }]
`

func TestManifestRules(t *testing.T) {
	t.Parallel()
	check := func(src string) []string {
		m, err := doc.ParseYAML([]byte(src))
		return rules(Manifest("scripts/d/tool-x/script.yaml", m, err, 160))
	}
	if got := check(manifest); len(got) != 0 {
		t.Fatalf("good manifest: %v", got)
	}
	cases := map[string]struct{ from, to, rule string }{
		"long summary":    {"summary: Does x.", "summary: " + strings.Repeat("s", 161), RuleSummary},
		"placeholder ex":  {`explain: "run"`, `explain: "TODO explain"`, RulePlaceholder},
		"placeholder map": {"examples: [{ cmd: \"tool-x\", explain: \"run\" }]", "inputs: [{name: TODO}]", RulePlaceholder},
		"bash ok writes":  {"side_effect: read-only", "side_effect: destructive", RuleWrites},
		"unknown":         {"owner: gardener", "owner: gardener\nflavor: mint", RuleUnknown},
		"bad glob":        {"owner: gardener", "owner: gardener\nwrites: [\"[\"]", RuleGlob},
	}
	for name, tc := range cases {
		if got := check(strings.Replace(manifest, tc.from, tc.to, 1)); !slices.Contains(got, tc.rule) {
			t.Errorf("%s: want %s, got %v", name, tc.rule, got)
		}
	}
	if !containsPlaceholder(map[string]any{"a": []any{"TODO x"}}) || containsPlaceholder(3) {
		t.Error("containsPlaceholder")
	}
}

func TestSources(t *testing.T) {
	t.Parallel()
	m, _ := doc.ParseYAML([]byte("sources:\n  - { kind: url, ref: u, at: 2026-01-01 }\n  - text\n"))
	if s := Sources(m); len(s) != 1 || s[0].Kind != "url" || s[0].At != "2026-01-01" {
		t.Fatalf("%+v", s)
	}
}
