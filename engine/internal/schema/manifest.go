package schema

import (
	"path"

	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/finding"
)

// Script vocabularies (docs/spec/04-scriptorium.md §3–§5).
var (
	Runtimes      = []string{"go", "python", "rust", "typescript", "bash"}
	SideEffects   = []string{"read-only", "writes-local", SideNetwork, "destructive"}
	ScriptStatus  = []string{"draft", "active", "deprecated"}
	ScriptOwners  = []string{"gardener", "user"}
	ScriptKeys    = []string{"id", "summary", "description", "status", "replaced_by", "runtime", "runtime_justification", "entry", "usage", "examples", "inputs", "outputs", "exit_codes", "side_effect", "writes", SideNetwork, "idempotent", "deterministic", "requires", "env", "timeout", KeyTags, KeyAliases, "applies_to", "shards", "tests", "owner"}
	scriptKnown   = set(ScriptKeys...)
	minJustifyLen = 40
)

// Script manifest rules.
const (
	RulePlaceholder = "script/placeholder"
	RuleJustify     = "script/runtime-justification"
	RuleWrites      = "script/writes"
	RuleNetwork     = "script/network"
	RuleDeprecated  = "script/replaced-by"
)

// Placeholder marks scaffolded manifest values that must be filled in.
const Placeholder = "TODO"

// Manifest validates a script.yaml. file is the instance-relative path.
func Manifest(file string, meta *doc.Meta, parseErr error, maxSummary int) []finding.Finding {
	c := newChecker(file, nil, meta)
	if parseErr != nil {
		frontmatter(c, parseErr, true)
		return c.out
	}
	c.required("id", "summary", "runtime", "entry", "usage", "side_effect", "status", "timeout", "owner")
	id := c.str("id")
	c.idFormat(id, "id")
	if dir := path.Base(path.Dir(file)); id != "" && id != dir {
		c.at(finding.Error, RuleIDMismatch, "id", "", "script id %q does not match its directory %q", id, dir)
	}
	if n := len([]rune(c.str("summary"))); n > maxSummary {
		c.at(finding.Error, RuleSummary, "summary", "", "summary has %d chars > %d", n, maxSummary)
	}
	rt := c.enum("runtime", Runtimes...)
	side := c.enum("side_effect", SideEffects...)
	status := c.enum("status", ScriptStatus...)
	c.enum("owner", ScriptOwners...)
	c.duration("timeout")
	for _, k := range []string{"writes", SideNetwork, "requires", "env", KeyTags, KeyAliases, "shards", "tests"} {
		c.list(k)
	}
	c.globs("applies_to")
	c.globs("writes")
	manifestPolicy(c, rt, side, status)
	placeholders(c)
	c.unknown(scriptKnown)
	return c.out
}

// manifestPolicy checks runtime justification and side-effect declarations.
func manifestPolicy(c *checker, rt, side, status string) {
	if rt != "" && rt != "go" && len([]rune(c.str("runtime_justification"))) < minJustifyLen {
		c.at(finding.Error, RuleJustify, "runtime", "explain why Go is not suitable (≥ 40 chars)", "runtime %q requires runtime_justification of at least %d chars", rt, minJustifyLen)
	}
	if side != "" && side != "read-only" && len(c.meta.Strings("writes")) == 0 && side != SideNetwork {
		c.at(finding.Error, RuleWrites, "side_effect", "list the globs it may write in writes:", "side_effect %q requires writes globs", side)
	}
	if side == SideNetwork && len(c.meta.Strings(SideNetwork)) == 0 {
		c.at(finding.Error, RuleNetwork, "side_effect", "list hosts in network:", "side_effect network requires network hosts")
	}
	if status == "deprecated" && c.str("replaced_by") == "" {
		c.at(finding.Warning, RuleDeprecated, "status", "set replaced_by: <id>", "deprecated script has no replaced_by")
	}
}

// placeholders flags scaffold TODO values left in the manifest.
func placeholders(c *checker) {
	for _, k := range c.meta.Keys() {
		v, _ := c.meta.Get(k)
		if containsPlaceholder(v) {
			c.at(finding.Error, RulePlaceholder, k, "replace the TODO with a real value", "%s still contains a %s placeholder", k, Placeholder)
		}
	}
}

func containsPlaceholder(v any) bool {
	switch x := v.(type) {
	case string:
		return len(x) >= len(Placeholder) && x[:len(Placeholder)] == Placeholder
	case []any:
		for _, it := range x {
			if containsPlaceholder(it) {
				return true
			}
		}
	case map[string]any:
		for _, it := range x {
			if containsPlaceholder(it) {
				return true
			}
		}
	}
	return false
}
