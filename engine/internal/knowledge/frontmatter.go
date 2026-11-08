// Package knowledge implements the knowledge write paths: shard scaffolding,
// capture, rename (mv), supersede, archive, hubs and questions
// (docs/spec/02-knowledge.md). Every write is atomic and content-preserving.
package knowledge

import (
	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/schema"
)

// Value renders a YAML scalar or flow list for frontmatter.
func Value(v any) string { return doc.Value(v) }

// SetKey sets a frontmatter key in canonical shard-key order.
func SetKey(raw []byte, key, rendered string) ([]byte, error) {
	return doc.SetKey(raw, key, rendered, schema.ShardKeys)
}

// RemoveKey deletes a top-level frontmatter key.
func RemoveKey(raw []byte, key string) ([]byte, error) { return doc.RemoveKey(raw, key) }

func blockOf(lines []string, key string) (from, to int) { return doc.BlockOf(lines, key) }
