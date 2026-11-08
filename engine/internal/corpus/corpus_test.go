package corpus

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/zbmowrey/strata/engine/internal/config"
)

var layout = LayoutOf(config.Default().Paths)

func TestClassify(t *testing.T) {
	t.Parallel()
	type want struct {
		kind     Kind
		role     string
		ok       bool
		private  bool
		archived bool
	}
	cases := map[string]want{
		"knowledge/testing/pytest/x.md":  {Shard, "", true, false, false},
		"knowledge/testing/x.md":         {Shard, "", true, false, false},
		"knowledge/README.md":            {Shard, "", true, false, false},
		"knowledge/_hubs/testing.md":     {Hub, "", true, false, false},
		"knowledge/_inbox/1-x.md":        {Inbox, "", true, false, false},
		"knowledge/_questions/q.md":      {Question, "", true, false, false},
		"knowledge/_archive/a/b/x.md":    {Shard, "", true, false, true},
		"knowledge/_private/a/b/x.md":    {Shard, "", true, true, false},
		"knowledge/_other/x.md":          {Shard, "", false, false, false},
		"knowledge/.obsidian/x.md":       {Shard, "", false, false, false},
		"knowledge/a/b/x.txt":            {"", "", false, false, false},
		"scripts/a/b/script.yaml":        {Script, "", true, false, false},
		"scripts/lib/go/x/script.yaml":   {Script, "", false, false, false},
		"scripts/a/b/main.go":            {Script, "", false, false, false},
		".claude/skills/x/SKILL.md":      {Playbook, "", true, false, false},
		".claude/skills/x/ref/SKILL.md":  {Playbook, "", false, false, false},
		"projects/p/project.md":          {Project, "project", true, false, false},
		"projects/p/log.md":              {Project, "log", true, false, false},
		"projects/p/notes.md":            {Project, "", false, false, false},
		"projects/p/out/report.md":       {Project, "out", true, false, false},
		"projects/_archive/p/handoff.md": {Project, "handoff", true, false, true},
		"projects/p/scratch/x.md":        {Project, "", false, false, false},
		"README.md":                      {"", "", false, false, false},
	}
	for rel, w := range cases {
		f, ok := layout.Classify(rel)
		if ok != w.ok {
			t.Errorf("%s: ok=%v want %v", rel, ok, w.ok)
			continue
		}
		if ok && (f.Kind != w.kind || f.Role != w.role || f.Private != w.private || f.Archived != w.archived) {
			t.Errorf("%s: got %+v want %+v", rel, f, w)
		}
	}
}

func TestWalk(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, rel := range []string{
		"knowledge/a/b/one.md", "knowledge/a/b/two.md", "knowledge/_hubs/a.md", "knowledge/.obsidian/x.md",
		"scripts/a/tool/script.yaml", "scripts/node_modules/x/script.yaml",
		".claude/skills/s/SKILL.md", ".git/x.md", "projects/p/project.md", "projects/p/scratch/x.md", "other/x.md",
	} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "other"), filepath.Join(root, "knowledge", "a", "link")); err != nil {
		t.Fatal(err)
	}
	files, err := Walk(root, layout)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(files))
	for _, f := range files {
		got = append(got, f.Rel)
		if f.Size != 1 || f.ModNS == 0 || f.Abs == "" {
			t.Errorf("stat fields missing: %+v", f)
		}
	}
	want := []string{".claude/skills/s/SKILL.md", "knowledge/_hubs/a.md", "knowledge/a/b/one.md", "knowledge/a/b/two.md", "projects/p/project.md", "scripts/a/tool/script.yaml"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("walk (-want +got):\n%s", diff)
	}
}

func TestWalkManyDirsAndMissingRoots(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for i := range 200 {
		p := filepath.Join(root, "knowledge", "d", string(rune('a'+i%26))+string(rune('a'+i/26)), "x.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := Walk(root, layout)
	if err != nil || len(files) != 200 {
		t.Fatalf("got %d files, err %v", len(files), err)
	}
	if files, err := Walk(t.TempDir(), layout); err != nil || len(files) != 0 {
		t.Fatalf("empty instance: %v %v", files, err)
	}
}
