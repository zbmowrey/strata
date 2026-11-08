package schema

import (
	"path"
	"strconv"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/finding"
)

// Shard vocabularies (docs/spec/02-knowledge.md §2–§3).
var (
	Types      = []string{"fact", "gotcha", "howto", "decision", "standard", "reference", "glossary", "correction"}
	Statuses   = []string{"draft", "active", "superseded", "deprecated", "archived"}
	Trusts     = []string{"user-stated", "verified", "observed", "inferred"}
	SourceKind = []string{"user", "url", "file", "commit", "session", "script", "doc"}
	Normative  = map[string]bool{"decision": true, "standard": true}
)

// ShardKeys is the canonical key order of shard frontmatter.
var ShardKeys = []string{
	"id", "title", "summary", "type", KeyTags, KeyAliases, "status", "trust", "created", "updated",
	"sources", "verified_at", "review_after", "verify", "applies_to", "scripts", "gates",
	"supersedes", "superseded_by", "occurrences", "sensitive",
}

var shardKnown = set(ShardKeys...)

// ShardInput is what shard validation needs about one file.
type ShardInput struct {
	Doc      *doc.Doc
	ParseErr error
	Rel      string // path relative to the knowledge root, e.g. testing/pytest/x.md
	File     string // path relative to the instance root (for findings)
	Private  bool
	Archived bool
}

// Shard validates a shard against the schema and size limits.
func Shard(in ShardInput, k config.Knowledge) []finding.Finding {
	c := newChecker(in.File, in.Doc, in.Doc.Meta)
	shardLayout(c, in)
	if !frontmatter(c, in.ParseErr, true) {
		return c.out
	}
	c.required(append([]string{"id", "title", "summary", "type", KeyTags, "status", "trust", "created", "updated"}, k.ExtraRequired...)...)
	id := c.str("id")
	c.idFormat(id, "id")
	if stem := strings.TrimSuffix(path.Base(in.Rel), ".md"); id != "" && id != stem {
		c.at(finding.Error, RuleIDMismatch, "id", "rename the file to "+id+".md or fix the id (use `strata mv` to rename ids)", "id %q does not match filename %q", id, stem)
	}
	c.enum("type", Types...)
	status := c.enum("status", Statuses...)
	trust := c.enum("trust", Trusts...)
	for _, key := range []string{"created", "updated", "verified_at"} {
		c.date(key)
	}
	c.duration("review_after")
	for _, key := range []string{KeyTags, KeyAliases, "scripts", "gates", "supersedes"} {
		c.list(key)
	}
	if v, ok := c.meta.Get(KeyTags); ok && isList(v) && len(c.meta.Strings(KeyTags)) == 0 {
		c.at(finding.Error, RuleRequired, KeyTags, "add at least one tag", "tags must have at least one entry")
	}
	c.globs("applies_to")
	checkSources(c, trust)
	checkLifecycle(c, status, in)
	shardSizes(c, in.Doc, k)
	c.unknown(shardKnown)
	return c.out
}

// shardLayout enforces knowledge/<domain>/<topic>/<id>.md.
func shardLayout(c *checker, in ShardInput) {
	parts := strings.Split(in.Rel, "/")
	if in.Archived || in.Private {
		parts = parts[1:]
	}
	if len(parts) < 3 {
		c.add(finding.Error, RuleLayout, 1, 1, "move it to knowledge/<domain>/<topic>/<id>.md", "shards must live at <domain>/<topic>/<id>.md")
	}
}

func checkLifecycle(c *checker, status string, in ShardInput) {
	if status == "superseded" && c.str("superseded_by") == "" {
		c.at(finding.Error, RuleSuperseded, "status", "set superseded_by: <id> (or use `strata supersede`)", "status superseded requires superseded_by")
	}
	if c.str("superseded_by") != "" && status != "superseded" {
		c.at(finding.Warning, RuleSuperseded, "superseded_by", "set status: superseded", "superseded_by is set but status is %q", status)
	}
	if c.str("sensitive") == "true" && !in.Private {
		c.at(finding.Error, RuleSensitive, "sensitive", "move the shard under knowledge/_private/", "sensitive: true is only valid under knowledge/_private/")
	}
	if v, ok := c.meta.Get("occurrences"); ok && v != nil {
		if _, isInt := v.(uint64); !isInt {
			c.at(finding.Error, RuleType, "occurrences", "", "occurrences must be a non-negative integer")
		}
	}
	if v, ok := c.meta.Get("verify"); ok && v != nil {
		m, isMap := v.(map[string]any)
		if !isMap || doc.Scalar(m["script"]) == "" {
			c.at(finding.Error, RuleType, "verify", "verify: { script: <id>, args: [...] }", "verify must name a script")
		}
	}
}

// Source is one entry of a shard's sources list.
type Source struct {
	Kind, Ref, At string
}

// Sources returns the parsed sources of meta (invalid entries skipped).
func Sources(meta *doc.Meta) []Source {
	v, _ := meta.Get("sources")
	items, _ := v.([]any)
	out := make([]Source, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, Source{Kind: doc.Scalar(m["kind"]), Ref: doc.Scalar(m["ref"]), At: doc.Scalar(m["at"])})
	}
	return out
}

// checkSources enforces source requirements by trust (docs/spec/11-security.md §2).
func checkSources(c *checker, trust string) {
	v, has := c.meta.Get("sources")
	if has && v != nil && !isList(v) {
		c.at(finding.Error, RuleType, "sources", "", "sources must be a list")
		return
	}
	items, _ := v.([]any)
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok || doc.Scalar(m["kind"]) == "" || doc.Scalar(m["ref"]) == "" {
			c.at(finding.Error, RuleType, "sources", "{ kind: <kind>, ref: <ref>, at: <date> }", "each source needs kind and ref")
			continue
		}
		if k := doc.Scalar(m["kind"]); !contains(SourceKind, k) {
			c.at(finding.Error, RuleEnum, "sources", "one of: "+strings.Join(SourceKind, ", "), "source kind %q is not allowed", k)
		}
	}
	if trust == "" || trust == "inferred" {
		return
	}
	srcs := Sources(c.meta)
	if len(srcs) == 0 {
		c.at(finding.Error, RuleTrustSources, "trust", "add sources, or lower trust to inferred", "trust %q requires at least one source", trust)
		return
	}
	need := map[string][]string{"verified": {"script", "file", "commit", "url"}, "user-stated": {"user"}}[trust]
	if len(need) == 0 {
		return
	}
	for _, s := range srcs {
		if contains(need, s.Kind) {
			return
		}
	}
	c.at(finding.Error, RuleTrustEvid, "trust", "add a source of kind "+strings.Join(need, "|"), "trust %q requires a %s source", trust, strings.Join(need, "|"))
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// shardSizes applies the size limits of docs/spec/02-knowledge.md §5.
func shardSizes(c *checker, d *doc.Doc, k config.Knowledge) {
	const split = "split into focused shards; see `strata new shard --help`"
	if d.BodyLines > float64(k.MaxBodyLines) {
		c.add(finding.Error, RuleBodyLines, d.BodyLine, 1, split, "body has %.1f lines > %d", d.BodyLines, k.MaxBodyLines)
	}
	if d.BodyBytes > k.MaxBodyBytes {
		c.add(finding.Error, RuleBodyBytes, d.BodyLine, 1, split, "body has %d bytes > %d", d.BodyBytes, k.MaxBodyBytes)
	}
	if n := len([]rune(c.str("summary"))); n > k.MaxSummary {
		c.at(finding.Error, RuleSummary, "summary", "one line of at most "+strconv.Itoa(k.MaxSummary)+" chars", "summary has %d chars > %d", n, k.MaxSummary)
	}
	if strings.Contains(c.str("summary"), "\n") {
		c.at(finding.Error, RuleSummary, "summary", "", "summary must be one line")
	}
	if n := len([]rune(c.str("title"))); n > k.MaxTitle {
		c.at(finding.Error, RuleTitle, "title", "", "title has %d chars > %d", n, k.MaxTitle)
	}
	h2 := 0
	for _, h := range d.Headings {
		if h.Level == 2 {
			h2++
		}
	}
	if h2 > k.MaxSections {
		c.add(finding.Warning, RuleSections, d.BodyLine, 1, split, "%d H2 sections > %d", h2, k.MaxSections)
	}
	if n := len(d.Links); n > k.MaxLinks {
		c.add(finding.Warning, RuleLinks, d.BodyLine, 1, "", "%d outbound links > %d", n, k.MaxLinks)
	}
}
