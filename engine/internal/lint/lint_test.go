package lint

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/corpus"
	"github.com/zbmowrey/strata/engine/internal/finding"
	"github.com/zbmowrey/strata/engine/internal/render"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func open(t *testing.T, dir string) *config.Instance {
	t.Helper()
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

func TestFixtureIsClean(t *testing.T) {
	t.Parallel()
	rep, err := Run(open(t, "../../testdata/instance-basic"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Findings) != 0 || rep.Checked != 54 {
		var b bytes.Buffer
		_ = render.Findings(&b, rep.Findings)
		t.Fatalf("checked %d, findings:\n%s", rep.Checked, b.String())
	}
}

func TestInvalidGolden(t *testing.T) {
	t.Parallel()
	rep, err := Run(open(t, "../../testdata/lint-invalid"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := render.Findings(&b, rep.Findings); err != nil {
		t.Fatal(err)
	}
	testutil.Golden(t, "testdata/invalid.golden", b.Bytes())
	var j bytes.Buffer
	if err := render.JSONL(&j, rep.Findings); err != nil {
		t.Fatal(err)
	}
	testutil.Golden(t, "testdata/invalid.jsonl.golden", j.Bytes())
}

func TestFilters(t *testing.T) {
	t.Parallel()
	inst := open(t, "../../testdata/lint-invalid")
	rep, err := Run(inst, Options{Paths: []string{"knowledge/a/b/links.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Checked != 1 {
		t.Fatalf("checked %d", rep.Checked)
	}
	for _, f := range rep.Findings {
		if f.File != "knowledge/a/b/links.md" {
			t.Fatalf("unexpected finding %+v", f)
		}
	}
	rep, err = Run(inst, Options{Kinds: []corpus.Kind{corpus.Script}, Paths: []string{"."}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Checked != 3 {
		t.Fatalf("script-only checked %d", rep.Checked)
	}
}

func TestFix(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(rel, s string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".strata/config.toml", "")
	write("knowledge/a/b/target.md", "---\nid: target\ntitle: T\nsummary: S\ntype: fact\ntags: [x]\naliases: [old-target]\nstatus: active\ntrust: inferred\ncreated: 2026-01-01\nupdated: 2026-01-01\n---\nBody.\n")
	messy := "---\n# leading comment about id\nupdated: 2026-01-01   \ntags: [Go, go, Testing]\nid: messy\ntitle: Messy\nsummary: S\nstatus: active  # inline comment\nmystery: keep-me\ntype: fact\ntrust: inferred\ncreated: 2026-01-01\napplies_to:\n  - \"x/**\"\n---\nSee [[old-target#Sec|label]] and [[target]].   \n\n\n"
	write("knowledge/a/b/messy.md", messy)
	inst := open(t, dir)
	rep, err := Run(inst, Options{Fix: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Fixed) != 1 || rep.Fixed[0] != "knowledge/a/b/messy.md" {
		t.Fatalf("fixed = %v", rep.Fixed)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "knowledge", "a", "b", "messy.md"))
	testutil.Golden(t, "testdata/fixed.golden", got)
	for _, f := range rep.Findings {
		if f.Severity == finding.Error || f.Rule == RuleAlias {
			t.Errorf("finding after fix: %+v", f)
		}
	}
	// Idempotent.
	rep, err = Run(inst, Options{Fix: true})
	if err != nil || len(rep.Fixed) != 0 {
		t.Fatalf("second fix changed %v (%v)", rep.Fixed, err)
	}
}

func TestNormalizeUnsplittable(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"no frontmatter\n", "---\n---\n", "---\n  indented: 1\n---\n", "---\nid: [\n---\n"} {
		if got := NormalizeFrontmatter([]byte(raw), []string{"id"}); string(got) != raw {
			t.Errorf("changed %q to %q", raw, got)
		}
	}
	if got := normalizeTags("tags: [\"Quoted\"]\n"); got != "tags: [\"Quoted\"]\n" {
		t.Errorf("quoted tags changed: %q", got)
	}
	if got := normalizeTags("tags:\n  - A\n"); got != "tags:\n  - A\n" {
		t.Errorf("block tags changed: %q", got)
	}
}

func TestUnreadableFile(t *testing.T) {
	t.Parallel()
	it := parseOne(corpus.File{Rel: "knowledge/a/b/x.md", Abs: "/nonexistent/x.md", Kind: corpus.Shard})
	fs := validate(it, config.Default(), corpus.LayoutOf(config.Default().Paths))
	if len(fs) != 1 || fs[0].Rule != RuleRead {
		t.Fatalf("%+v", fs)
	}
}
