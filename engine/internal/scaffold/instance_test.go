package scaffold

import (
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/config"
)

func TestInstanceTemplates(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Paths.Knowledge = "kb"
	v := VarsFor(cfg, "1.2.3")
	for _, name := range []string{"config.toml", "synonyms.txt", "retrieval.yaml", "ci.yml", "claude-block.md"} {
		out, err := Render(name, v)
		if err != nil || out == "" {
			t.Fatalf("%s: %v", name, err)
		}
	}
	block, err := ClaudeBlock(v)
	if err != nil || !strings.Contains(block, "kb/") || strings.Contains(block, "{{") {
		t.Fatalf("claude block: %v\n%s", err, block)
	}
	if ci, _ := Render("ci.yml", v); !strings.Contains(ci, "@v1.2.3") {
		t.Fatalf("ci pin:\n%s", ci)
	}
	if _, err := Render("missing", v); err == nil {
		t.Fatal("missing template rendered")
	}
	if ig := LocalIgnores(cfg.Paths); !strings.Contains(ig, "/kb/_private/") || strings.Contains(ig, "perms.lock") {
		t.Fatalf("ignores:\n%s", ig)
	}
}
