package projects

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

// clock is a settable test clock.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newInst(t *testing.T) *config.Instance {
	t.Helper()
	dir := t.TempDir()
	testutil.WriteFile(t, dir, ".strata/config.toml", "")
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

func newStore(t *testing.T) (*Store, *clock) {
	t.Helper()
	c := &clock{t: time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)}
	return New(newInst(t), c.now).WithSession("abcd1234-xyz"), c
}

func read(t *testing.T, s *Store, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.inst.Root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCreateLoadAll(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	if _, err := s.Create("ci-flake", "Eliminate: CI flakiness", "50 green runs", []string{"ci"}); err != nil {
		t.Fatal(err)
	}
	p, err := s.Load("ci-flake")
	if err != nil || p.Title != "Eliminate: CI flakiness" || p.Goal != "50 green runs" || p.Status != StatusActive || p.Dir != "projects/ci-flake" {
		t.Fatalf("%+v %v", p, err)
	}
	for _, tc := range []struct{ slug, title string }{{"Bad Slug", "x"}, {"ci-flake", "dup"}, {Adhoc, "x"}, {"new-one", "  "}} {
		if _, err := s.Create(tc.slug, tc.title, "", nil); fail.CodeOf(err) != fail.Usage {
			t.Errorf("Create(%q): %v", tc.slug, err)
		}
	}
	if _, err := s.Load("nope-nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	// Archived projects are found too.
	testutil.WriteFile(t, s.inst.Root, "projects/_archive/old-one/project.md", "---\nid: old-one\ntitle: Old\nstatus: archived\n---\n")
	testutil.WriteFile(t, s.inst.Root, "projects/not_a_slug/project.md", "x")
	all, err := s.All()
	if err != nil || len(all) != 2 || all[1].Slug != "old-one" || !all[1].Archived {
		t.Fatalf("all %+v %v", all, err)
	}
	if _, err := s.Load(Adhoc); err != nil {
		t.Fatal(err)
	}
	if !ValidSlug(Adhoc) || ValidSlug("ab") || Session4("") != "cli" || Session4("AB-12xyz") != "ab12" {
		t.Fatal("helpers")
	}
	if yamlScalar("plain") != "plain" || yamlScalar(`a: "b"`) != `"a: \"b\""` {
		t.Fatal("yamlScalar")
	}
}

func TestLogGrouping(t *testing.T) {
	t.Parallel()
	s, c := newStore(t)
	if _, err := s.Create("ci-flake", "CI", "", nil); err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		advance time.Duration
		session string
		prefix  string
		text    string
	}{
		{0, "abcd1234", "tried", "pytest -x\n --timeout 30"},
		{10 * time.Minute, "abcd1234", "failed", "timeout never fires"},
		{0, "ffff0000", "note", "other session"},
		{45 * time.Minute, "ffff0000", "learned", "too late to group"},
	}
	for _, st := range steps {
		c.t = c.t.Add(st.advance)
		if _, err := s.WithSession(st.session).Log("ci-flake", st.prefix, st.text); err != nil {
			t.Fatal(err)
		}
	}
	got := read(t, s, "projects/ci-flake/log.md")
	want := "# Log — ci-flake\n\n## 2026-11-07 10:00 · s:abcd\n- tried: pytest -x --timeout 30\n- failed: timeout never fires\n\n## 2026-11-07 10:10 · s:ffff\n- note: other session\n\n## 2026-11-07 10:55 · s:ffff\n- learned: too late to group\n"
	if got != want {
		t.Fatalf("log:\n%s\nwant:\n%s", got, want)
	}
	es, err := s.Entries("ci-flake")
	if err != nil || len(es) != 3 || es[0].ID != "ci-flake:log#2026-11-07-10-00" || len(es[0].Lines) != 2 {
		t.Fatalf("entries %+v %v", es, err)
	}
	if _, err := s.Log("ci-flake", "guessed", "x"); fail.CodeOf(err) != fail.Usage {
		t.Fatal("bad prefix accepted")
	}
	if _, err := s.Log("ci-flake", "note", "  "); fail.CodeOf(err) != fail.Usage {
		t.Fatal("empty text accepted")
	}
	if p, err := s.Log("", "note", "adhoc"); err != nil || p != "projects/_adhoc/log.md" {
		t.Fatalf("adhoc %q %v", p, err)
	}
	h, err := s.Headlines(2)
	if err != nil || len(h["ci-flake"]) != 2 || !strings.HasPrefix(h["ci-flake"][0], "2026-11-07 10:55 — learned") {
		t.Fatalf("headlines %v %v", h, err)
	}
	if (Entry{Stamp: "x"}).Headline() != "x" {
		t.Fatal("empty headline")
	}
}

func TestHandoffGolden(t *testing.T) {
	t.Parallel()
	s, c := newStore(t)
	if _, err := s.Create("ci-flake", "CI", "", nil); err != nil {
		t.Fatal(err)
	}
	// Hand-edited handoff with an existing Don't-redo item.
	testutil.WriteFile(t, s.inst.Root, "projects/ci-flake/handoff.md", "---\nupdated: 2026-11-07T09:00:00Z\nsession: zzzz\n---\n## State\nTeardown fix landed.\n## Next\n1. Write find-leaked-fixtures.\n2. Re-run CI 50×.\n## Open threads\n- Is redis leaking?\n## Don't redo\n- Raising --timeout (log 2026-11-06 10:00)\n")
	c.t = time.Date(2026, 11, 7, 8, 0, 0, 0, time.UTC)
	mustLog(t, s, "failed", "old failure before the last handoff")
	c.t = time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	mustLog(t, s, "failed", "raising --timeout")
	mustLog(t, s, "failed", "pytest-rerunfailures hides races")
	mustLog(t, s, "worked", "explicit teardown")
	c.t = c.t.Add(2 * time.Hour)
	p, err := s.Handoff("ci-flake")
	if err != nil || p != "projects/ci-flake/handoff.md" {
		t.Fatal(p, err)
	}
	out := read(t, s, p)
	testutil.Golden(t, "testdata/handoff.golden", []byte(out))
	if strings.Count(out, "\n") > MaxHandoffLines {
		t.Fatal("handoff too long")
	}
	// Idempotent: a second handoff adds nothing new.
	c.t = c.t.Add(time.Minute)
	if _, err := s.Handoff("ci-flake"); err != nil {
		t.Fatal(err)
	}
	if again := read(t, s, p); strings.Count(again, "\n- ") != strings.Count(out, "\n- ") {
		t.Fatalf("second handoff changed items:\n%s", again)
	}
}

func mustLog(t *testing.T, s *Store, prefix, text string) {
	t.Helper()
	if _, err := s.Log("ci-flake", prefix, text); err != nil {
		t.Fatal(err)
	}
}

func TestHandoffTrimsToLimit(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	var many []string
	for i := range 60 {
		many = append(many, "- item "+string(rune('a'+i%26))+strings.Repeat("x", i))
	}
	out := s.renderHandoff(handoffParts{DontRedo: many})
	if n := strings.Count(out, "\n"); n != MaxHandoffLines || !strings.Contains(out, many[59]) || strings.Contains(out, many[0]+"\n") {
		t.Fatalf("lines %d:\n%s", n, out)
	}
	if _, err := s.Handoff("nope-nope"); err == nil {
		t.Fatal("missing project")
	}
}

func TestStatusTrackAndSetKey(t *testing.T) {
	t.Parallel()
	s, c := newStore(t)
	if _, err := s.Create("ci-flake", "CI", "", nil); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.AddDate(0, 0, 3)
	if err := s.SetStatus("ci-flake", StatusPaused); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus("ci-flake", "bogus"); fail.CodeOf(err) != fail.Usage {
		t.Fatal("bogus status")
	}
	if err := s.Track("ci-flake", []string{"out/report.md", "plan.md", "out/report.md"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Track("ci-flake", []string{"scratch/x"}); fail.CodeOf(err) != fail.Usage {
		t.Fatal("scratch accepted")
	}
	p, _ := s.Load("ci-flake")
	if p.Status != StatusPaused || p.Updated != "2026-11-10" || strings.Join(p.Track, ",") != "out/report.md,plan.md" {
		t.Fatalf("%+v", p)
	}
	for in, want := range map[string]string{
		"---\na: 1\nk:\n  - x\n  - y\nb: 2\n---\nbody\n": "---\na: 1\nk: v\nb: 2\n---\nbody\n",
		"---\nk: [\n---\n":    "---\nk: [\n---\n", // invalid YAML is left alone
		"---\na: 1\n---\n":    "---\na: 1\nk: v\n---\n",
		"no frontmatter\n":    "no frontmatter\n",
		"---\nunterminated\n": "---\nunterminated\n",
	} {
		if got := string(SetKey([]byte(in), "k", "v")); got != want {
			t.Errorf("SetKey(%q) = %q", in, got)
		}
	}
}

func TestListActiveResume(t *testing.T) {
	t.Parallel()
	s, c := newStore(t)
	for _, slug := range []string{"alpha-one", "beta-two", "gamma-three", "delta-four", "old-stale"} {
		if _, err := s.Create(slug, strings.ToUpper(slug[:1])+slug[1:], "", nil); err != nil {
			t.Fatal(err)
		}
	}
	mustLogTo(t, s, "old-stale", "note", "ancient")
	c.t = c.t.AddDate(0, 0, 40)
	for _, slug := range []string{"alpha-one", "beta-two", "gamma-three"} {
		mustLogTo(t, s, slug, "note", "fresh")
		c.t = c.t.Add(time.Hour)
	}
	testutil.WriteFile(t, s.inst.Root, "projects/beta-two/handoff.md", "---\nupdated: x\n---\n## State\ns\n## Next\n"+strings.Repeat("1. step\n", 20))
	if err := s.SetStatus("delta-four", StatusDone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Log("", "note", "adhoc"); err != nil {
		t.Fatal(err)
	}
	sums, err := s.List(false)
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	stale := map[string]bool{}
	for _, x := range sums {
		slugs = append(slugs, x.Slug)
		stale[x.Slug] = x.Stale
	}
	if strings.Join(slugs, ",") != "_adhoc,gamma-three,beta-two,alpha-one,old-stale" || !stale["old-stale"] || stale["alpha-one"] {
		t.Fatalf("list %v stale %v", slugs, stale)
	}
	all, _ := s.List(true)
	if len(all) != 6 {
		t.Fatalf("all %d", len(all))
	}
	act, err := Active(s.inst, c.now)
	if err != nil || len(act) != 4 {
		t.Fatalf("active %d %v", len(act), err)
	}
	testutil.Golden(t, "testdata/resume.golden", []byte(RenderResume(act)))
	if !strings.HasPrefix(RenderResume(nil), "No active projects") {
		t.Fatal("empty resume")
	}
	one, err := s.Summary("old-stale")
	if err != nil || one.AgeDays != 40 {
		t.Fatalf("summary %+v %v", one, err)
	}
}

func mustLogTo(t *testing.T, s *Store, slug, prefix, text string) {
	t.Helper()
	if _, err := s.Log(slug, prefix, text); err != nil {
		t.Fatal(err)
	}
}

func TestValidateTrack(t *testing.T) {
	t.Parallel()
	for _, g := range []string{"out/report.md", "plan.md", "out/**/*.md", "notes/{a,b}.md"} {
		if err := ValidateTrack(g); err != nil {
			t.Errorf("%q: %v", g, err)
		}
	}
	for _, g := range []string{"", "/abs", "!neg", `a\b`, "[", "../x", "a/../../x", "*/x", "**", "scratch/x", "scratch"} {
		if err := ValidateTrack(g); err == nil {
			t.Errorf("%q accepted", g)
		}
	}
}

// TestGitignoreCheckIgnore asserts git's own view of the generated block.
func TestGitignoreCheckIgnore(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	s, _ := newStore(t)
	root := s.inst.Root
	testutil.WriteFile(t, root, ".gitignore", "# user content\n*.log\n")
	if _, err := s.Create("ci-flake", "CI", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Track("ci-flake", []string{"out/report.md", "plan.md", "out/deep/**/*.csv"}); err != nil {
		t.Fatal(err)
	}
	// A hand-edited invalid glob is skipped with a warning, never emitted.
	testutil.WriteFile(t, root, "projects/legacy-one/project.md", "---\nid: legacy-one\ntitle: L\nstatus: active\ntrack: [\"scratch/notes.md\", \"out/ok.md\"]\n---\n")
	changed, warnings, err := Sync(s.inst)
	if err != nil || !changed || len(warnings) != 1 {
		t.Fatalf("sync %v %v %v", changed, warnings, err)
	}
	if changed, _, _ := Sync(s.inst); changed {
		t.Fatal("sync not idempotent")
	}
	gi := read(t, s, ".gitignore")
	testutil.Golden(t, "testdata/gitignore.golden", []byte(gi))
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil { //nolint:gosec,noctx // G204: test runs git in its temp dir
		t.Fatalf("git init: %v %s", err, out)
	}
	for rel, ignored := range map[string]bool{
		"projects/ci-flake/out/report.md":         false,
		"projects/ci-flake/plan.md":               false,
		"projects/ci-flake/out/deep/a/b/data.csv": false,
		"projects/ci-flake/out/other.md":          true,
		"projects/ci-flake/log.md":                true,
		"projects/ci-flake/handoff.md":            true,
		"projects/ci-flake/scratch/report.md":     true,
		"projects/legacy-one/scratch/notes.md":    true,
		"projects/legacy-one/out/ok.md":           false,
		"projects/_adhoc/log.md":                  true,
		"knowledge/a/b/x.md":                      false,
	} {
		testutil.WriteFile(t, root, rel, "x")
		err := exec.Command("git", "-C", root, "check-ignore", "-q", rel).Run() //nolint:gosec,noctx // G204: test runs git in its temp dir
		if got := err == nil; got != ignored {
			t.Errorf("%s: ignored=%v want %v", rel, got, ignored)
		}
	}
}

func TestSyncErrors(t *testing.T) {
	t.Parallel()
	inst := newInst(t)
	if err := os.Mkdir(filepath.Join(inst.Root, ".gitignore"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Sync(inst); err == nil {
		t.Fatal("want read error")
	}
}
