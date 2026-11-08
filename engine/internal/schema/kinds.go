package schema

import (
	"path"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/finding"
)

// Inbox validates a capture: only the minimal frontmatter is checked;
// captures have no size limits (docs/spec/02-knowledge.md §8.1).
func Inbox(file string, d *doc.Doc, parseErr error) []finding.Finding {
	c := newChecker(file, d, d.Meta)
	if !frontmatter(c, parseErr, true) {
		return c.out
	}
	c.required("type", "captured")
	c.enum("type", Types...)
	c.date("captured")
	c.list("sources")
	c.list(KeyTags)
	return c.out
}

// QuestionStatuses are the allowed question states.
var QuestionStatuses = []string{"open", "answered", "withdrawn"}

// Question validates an escalation file (docs/spec/02-knowledge.md §8.2).
func Question(file string, d *doc.Doc, parseErr error) []finding.Finding {
	c := newChecker(file, d, d.Meta)
	if !frontmatter(c, parseErr, true) {
		return c.out
	}
	c.required("id", "status", "raised_by", "raised")
	id := c.str("id")
	if stem := strings.TrimSuffix(path.Base(file), ".md"); id != "" && id != stem {
		c.at(finding.Error, RuleIDMismatch, "id", "", "id %q does not match filename %q", id, stem)
	}
	c.enum("status", QuestionStatuses...)
	c.enum("raised_by", "gardener", "claude", "user")
	c.date("raised")
	c.list("blocking")
	if _, ok := d.Section("Question"); !ok {
		c.add(finding.Error, RuleRequired, d.BodyLine, 1, "add a `## Question` section", "question has no ## Question section")
	}
	return c.out
}

// Hub validates a generated hub's frontmatter.
func Hub(file string, d *doc.Doc, parseErr error) []finding.Finding {
	c := newChecker(file, d, d.Meta)
	if !frontmatter(c, parseErr, true) {
		return c.out
	}
	c.required("id", "title", "summary", "type")
	c.enum("type", "index")
	return c.out
}

// Playbook validates a Claude Code skill leniently: only name and
// description are required, so it stays a valid skill (§9).
func Playbook(file string, d *doc.Doc, parseErr error) []finding.Finding {
	c := newChecker(file, d, d.Meta)
	if !frontmatter(c, parseErr, true) {
		return c.out
	}
	c.required("name", "description")
	for _, k := range []string{KeyTags, KeyAliases, "scripts", "shards"} {
		c.list(k)
	}
	return c.out
}

// ProjectStatuses are the allowed project states.
var ProjectStatuses = []string{"active", "paused", "done", "archived"}

// LogPrefixes is the chronicle vocabulary (docs/spec/05-projects.md §3.2).
var LogPrefixes = []string{"tried", "failed", "worked", "learned", "decided", "blocked", "next", "note"}

// Rules for project files.
const (
	RuleLogPrefix = "project/log-prefix"
	RuleHandoff   = "project/handoff"
)

// Project validates one project file by role.
func Project(file, role string, d *doc.Doc, parseErr error) []finding.Finding {
	c := newChecker(file, d, d.Meta)
	switch role {
	case "project":
		if !frontmatter(c, parseErr, true) {
			return c.out
		}
		c.required("id", "title", "status", "created", "updated")
		slug := path.Base(path.Dir(file))
		if id := c.str("id"); id != "" && id != slug {
			c.at(finding.Error, RuleIDMismatch, "id", "", "project id %q does not match folder %q", id, slug)
		}
		c.enum("status", ProjectStatuses...)
		c.date("created")
		c.date("updated")
		c.list(KeyTags)
		c.list("shards")
		c.globs("track")
	case "handoff":
		if !frontmatter(c, parseErr, false) {
			return c.out
		}
		handoff(c, d)
	case "log":
		logPrefixes(c, d)
	}
	return c.out
}

func handoff(c *checker, d *doc.Doc) {
	for _, s := range []string{"State", "Next"} {
		if _, ok := d.Section(s); !ok {
			c.add(finding.Warning, RuleHandoff, d.BodyLine, 1, "handoff shape: ## State, ## Next, ## Open threads, ## Don't redo", "handoff has no ## %s section", s)
		}
	}
	if n := strings.Count(string(d.Raw), "\n"); n > 40 {
		c.add(finding.Warning, RuleHandoff, 1, 1, "keep the handoff to 40 lines", "handoff has %d lines > 40", n)
	}
}

// logPrefixes warns on chronicle bullets outside the fixed vocabulary.
func logPrefixes(c *checker, d *doc.Doc) {
	line := d.BodyLine
	for _, l := range strings.Split(string(d.Body()), "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(l, "- ") {
			word, _, ok := strings.Cut(strings.TrimPrefix(t, "- "), ":")
			if !ok || !contains(LogPrefixes, word) {
				c.add(finding.Warning, RuleLogPrefix, line, 1, "start entries with one of: "+strings.Join(LogPrefixes, ", "), "log entry does not start with a known prefix")
			}
		}
		line++
	}
}
