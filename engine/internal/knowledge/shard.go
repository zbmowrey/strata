package knowledge

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/schema"
)

// bodies are the type-specific section skeletons (docs/spec/02-knowledge.md §4).
var bodies = map[string]string{
	"gotcha":   "TODO: state the trap plainly in ≤ 3 sentences.\n\n## Symptom\n\n## Cause\n\n## Fix\n",
	"decision": "TODO: state the decision plainly in ≤ 3 sentences.\n\n## Context\n\n## Decision\n\n## Alternatives\n\n## Consequences\n",
	"standard": "TODO: state the rule plainly in ≤ 3 sentences.\n\n## Rule\n\n## Rationale\n\n## Enforcement\nnone yet\n",
	"howto":    "TODO: what this procedure achieves, in one sentence.\n\n1. TODO\n",
}

// NewShardInput describes a shard to scaffold.
type NewShardInput struct {
	Path    string // <domain>/<topic>/<id>
	Type    string
	Title   string
	Summary string
}

// NewShard scaffolds a shard and returns its instance-relative path.
func (s *Store) NewShard(in NewShardInput) (string, error) {
	parts := strings.Split(strings.Trim(in.Path, "/"), "/")
	if len(parts) < 3 {
		return "", fail.New(fail.Usage, "use <domain>/<topic>/<id>, e.g. testing/pytest/fixture-teardown", "bad shard path %q", in.Path)
	}
	for _, p := range parts[:len(parts)-1] {
		if p == "" || strings.HasPrefix(p, "_") || strings.HasPrefix(p, ".") {
			return "", fail.New(fail.Usage, "domains and topics are plain names (no _ or . prefix)", "bad shard path %q", in.Path)
		}
	}
	id := parts[len(parts)-1]
	if err := validID(id); err != nil {
		return "", err
	}
	if !contains(schema.Types, in.Type) {
		return "", fail.New(fail.Usage, "types: "+strings.Join(schema.Types, ", "), "unknown type %q", in.Type)
	}
	if rel, taken, err := s.idTaken(id); err != nil {
		return "", err
	} else if taken {
		return "", fail.New(fail.Usage, "ids are globally unique; pick another or edit "+rel, "id %q already exists", id)
	}
	title := in.Title
	if title == "" {
		title = "TODO " + strings.ReplaceAll(id, "-", " ")
	}
	summary := in.Summary
	if summary == "" {
		summary = "TODO one-line summary shown in search results"
	}
	body, ok := bodies[in.Type]
	if !ok {
		body = "TODO: state the claim plainly in ≤ 3 sentences.\n"
	}
	today := s.today()
	content := "---\n" +
		"id: " + id + "\n" +
		"title: " + Value(title) + "\n" +
		"summary: " + Value(summary) + "\n" +
		"type: " + in.Type + "\n" +
		"tags: [" + parts[0] + "]\n" +
		"status: draft\n" +
		"trust: inferred\n" +
		"created: " + today + "\n" +
		"updated: " + today + "\n" +
		"---\n" + body
	rel := path.Join(s.layout.Knowledge, strings.Join(parts, "/")) + ".md"
	if _, err := os.Stat(s.abs(rel)); err == nil {
		return "", fail.New(fail.Usage, "", "%s already exists", rel)
	}
	if err := s.writeFile(rel, []byte(content)); err != nil {
		return "", fmt.Errorf("new shard: %w", err)
	}
	return rel, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
