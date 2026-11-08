package knowledge

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/lint"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

var fixedNow = func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) }

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := testutil.CopyFixture(t, "instance-basic")
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	return New(inst, fixedNow, telemetry.New(inst, fixedNow)), dir
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func lintClean(t *testing.T, dir string) {
	t.Helper()
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := lint.Run(inst, lint.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Findings {
		if f.Severity == "error" {
			t.Errorf("lint: %s:%d %s %s", f.File, f.Line, f.Rule, f.Message)
		}
	}
}

func TestMoveGolden(t *testing.T) {
	t.Parallel()
	s, dir := newStore(t)
	changed, err := s.Move("postgres-connection-pool", "pg-connection-pool")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("changed:\n" + strings.Join(changed, "\n") + "\n\n")
	b.WriteString("=== moved shard\n" + read(t, dir, "knowledge/database/postgres/pg-connection-pool.md"))
	for _, rel := range changed {
		if strings.Contains(rel, "pg-connection-pool") || strings.Contains(rel, "postgres-connection-pool") {
			continue
		}
		for _, l := range strings.Split(read(t, dir, rel), "\n") {
			if strings.Contains(l, "connection-pool") {
				b.WriteString("=== " + rel + ": " + l + "\n")
			}
		}
	}
	testutil.Golden(t, "testdata/mv.golden", []byte(b.String()))
	if _, err := os.Stat(filepath.Join(dir, "knowledge", "database", "postgres", "postgres-connection-pool.md")); !os.IsNotExist(err) {
		t.Fatal("old file still present")
	}
	lintClean(t, dir)
	// Old id now an alias: moving onto a taken id fails; bad ids fail.
	if _, err := s.Move("pg-connection-pool", "deploy-flow"); fail.CodeOf(err) != fail.Usage {
		t.Fatalf("taken id: %v", err)
	}
	if _, err := s.Move("pg-connection-pool", "Bad_ID"); fail.CodeOf(err) != fail.Usage {
		t.Fatalf("bad id: %v", err)
	}
	if _, err := s.Move("nope-nope", "fresh-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestMoveRewritesManifestsAndPlaybooks(t *testing.T) {
	t.Parallel()
	s, dir := newStore(t)
	if _, err := s.Move("pytest-fixture-teardown", "db-fixture-teardown"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "scripts/testing/find-leaked-fixtures/script.yaml"); !strings.Contains(got, "shards: [db-fixture-teardown]") {
		t.Fatalf("manifest not rewritten:\n%s", got)
	}
	if got := read(t, dir, ".claude/skills/debug-hanging-tests/SKILL.md"); !strings.Contains(got, "shards: [db-fixture-teardown, hanging-test-diagnosis]") || !strings.Contains(got, "[[db-fixture-teardown]]") {
		t.Fatalf("playbook not rewritten:\n%s", got)
	}
	lintClean(t, dir)
}

func TestHubsDeterministicAndPreserveIntro(t *testing.T) {
	t.Parallel()
	s, dir := newStore(t)
	first, err := s.Hubs()
	if err != nil || len(first) != 5 {
		t.Fatalf("first run %v %v", first, err)
	}
	again, err := s.Hubs()
	if err != nil || len(again) != 0 {
		t.Fatalf("second run rewrote %v (%v)", again, err)
	}
	hub := "knowledge/_hubs/hub-testing.md"
	raw := read(t, dir, hub)
	withIntro := strings.Replace(raw, "---\n\n<!-- strata:hub:begin", "---\nHand-written intro: testing is sacred.\n\n<!-- strata:hub:begin", 1)
	testutil.WriteFile(t, dir, hub, withIntro)
	if _, err := s.NewShard(NewShardInput{Path: "testing/newtopic/brand-new-thing", Type: "fact", Title: "Brand new", Summary: "s"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Supersede("ci-cache-keys", "ci-pipeline-stages"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Hubs(); err != nil {
		t.Fatal(err)
	}
	got := read(t, dir, hub)
	if !strings.Contains(got, "Hand-written intro: testing is sacred.") {
		t.Fatal("intro lost")
	}
	testutil.Golden(t, "testdata/hub-testing.golden", []byte(got))
	testutil.Golden(t, "testdata/hub-index.golden", []byte(read(t, dir, "knowledge/_hubs/index.md")))
	lintClean(t, dir)
}

func TestCaptureAndCorrection(t *testing.T) {
	t.Parallel()
	s, dir := newStore(t)
	rel, err := s.Capture(CaptureInput{Text: "Redis fixture leaks too!", Type: "gotcha", Tags: []string{"redis"}, Source: "session:projects/ci-flake/log.md", Session: "abcd"})
	if err != nil {
		t.Fatal(err)
	}
	if rel != "knowledge/_inbox/20261107-120000-redis-fixture-leaks-too.md" {
		t.Fatalf("path %s", rel)
	}
	rel2, err := s.Capture(CaptureInput{Text: "Redis fixture leaks too!"})
	if err != nil || !strings.HasSuffix(rel2, "-2.md") {
		t.Fatalf("collision suffix %s %v", rel2, err)
	}
	testutil.Golden(t, "testdata/capture.golden", []byte(read(t, dir, rel)))
	if _, err := s.Capture(CaptureInput{Text: "wrong timeout advice", Type: "correction", Shard: "http-timeouts"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Capture(CaptureInput{Text: "again", Type: "correction", Shard: "http-timeouts"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, dir, "knowledge/api/http/http-timeouts.md"), "\noccurrences: 2\n") {
		t.Fatal("occurrences not bumped twice")
	}
	tel := read(t, dir, ".strata/telemetry/2026-11.jsonl")
	if strings.Count(tel, `"event":"capture"`) != 4 || strings.Count(tel, `"event":"correction"`) != 2 {
		t.Fatalf("telemetry:\n%s", tel)
	}
	for _, bad := range []CaptureInput{{Text: " "}, {Text: "x", Type: "rumor"}, {Text: "x", Source: "nope"}, {Text: "x", Source: "alien:x"}, {Text: "x", Shard: "y"}} {
		if _, err := s.Capture(bad); fail.CodeOf(err) != fail.Usage {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	if _, err := s.Capture(CaptureInput{Text: "x", Type: "correction", Shard: "no-such-shard"}); err == nil {
		t.Fatal("correction of a missing shard")
	}
	lintClean(t, dir)
}

func TestNewShardAndPlaybook(t *testing.T) {
	t.Parallel()
	s, dir := newStore(t)
	for _, typ := range []string{"gotcha", "decision", "standard", "howto", "fact"} {
		rel, err := s.NewShard(NewShardInput{Path: "newdom/topic/new-" + typ, Type: typ})
		if err != nil {
			t.Fatal(err)
		}
		if typ == "gotcha" {
			testutil.Golden(t, "testdata/new-shard-gotcha.golden", []byte(read(t, dir, rel)))
		}
	}
	for _, in := range []NewShardInput{
		{Path: "a/b", Type: "fact"},
		{Path: "_x/b/ok-id", Type: "fact"},
		{Path: "a/b/Bad", Type: "fact"},
		{Path: "a/b/ok-id", Type: "rumor"},
		{Path: "a/b/deploy-flow", Type: "fact"},
	} {
		if _, err := s.NewShard(in); fail.CodeOf(err) != fail.Usage {
			t.Errorf("%+v: %v", in, err)
		}
	}
	rel, err := s.NewPlaybook("rotate-credentials")
	if err != nil || rel != ".claude/skills/rotate-credentials/SKILL.md" {
		t.Fatalf("%s %v", rel, err)
	}
	if _, err := s.NewPlaybook("rotate-credentials"); fail.CodeOf(err) != fail.Usage {
		t.Fatal("duplicate playbook")
	}
	lintClean(t, dir)
}

func TestArchiveAndSupersede(t *testing.T) {
	t.Parallel()
	s, dir := newStore(t)
	moved, err := s.Archive([]string{"snapshot-test-updates", "snapshot-test-updates"})
	if err != nil || !slices.Equal(moved, []string{"knowledge/_archive/testing/debugging/snapshot-test-updates.md"}) {
		t.Fatalf("archive %v %v", moved, err)
	}
	if !strings.Contains(read(t, dir, moved[0]), "status: archived") {
		t.Fatal("status not archived")
	}
	if again, err := s.Archive([]string{"snapshot-test-updates"}); err != nil || len(again) != 0 {
		t.Fatalf("re-archive %v %v", again, err)
	}
	if _, err := s.Supersede("soft-delete-pattern", "soft-delete-pattern"); fail.CodeOf(err) != fail.Usage {
		t.Fatal("self-supersede")
	}
	if _, err := s.Supersede("soft-delete-pattern", "uuid-primary-keys"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, dir, "knowledge/database/modeling/uuid-primary-keys.md"), "supersedes: [soft-delete-pattern]") {
		t.Fatal("supersedes not set")
	}
	if _, err := s.Supersede("nope-x", "uuid-primary-keys"); err == nil {
		t.Fatal("missing old")
	}
	lintClean(t, dir)
}

func TestQuestions(t *testing.T) {
	t.Parallel()
	s, dir := newStore(t)
	rel, err := s.Ask("Is 85% still the coverage target?", []string{"coverage-floor"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if rel2, err := s.Ask("Is 85% still the coverage target?", nil, "gardener"); err != nil || rel2 == rel {
		t.Fatalf("duplicate ask %s %v", rel2, err)
	}
	qs, err := s.Questions(false)
	if err != nil || len(qs) != 3 {
		t.Fatalf("open questions %v %v", qs, err)
	}
	id := strings.TrimSuffix(filepath.Base(rel), ".md")
	if _, err := s.Answer(id); fail.CodeOf(err) != fail.Usage {
		t.Fatalf("unanswered: %v", err)
	}
	raw := read(t, dir, rel)
	testutil.WriteFile(t, dir, rel, strings.Replace(raw, "## Answer\n", "## Answer\nYes — 85% stands.\n", 1))
	if _, err := s.Answer(id); err != nil {
		t.Fatal(err)
	}
	if qs, _ := s.Questions(false); len(qs) != 2 {
		t.Fatalf("answered question still open: %v", qs)
	}
	if all, _ := s.Questions(true); len(all) != 3 {
		t.Fatal("--all")
	}
	if text, err := s.ShowQuestion(id); err != nil || !strings.Contains(text, "status: answered") {
		t.Fatalf("show %v", err)
	}
	if _, err := s.ShowQuestion("q-nope"); err == nil {
		t.Fatal("missing question")
	}
	if _, err := s.Answer("q-nope"); err == nil {
		t.Fatal("answer missing question")
	}
	if _, err := s.Ask("  ", nil, ""); fail.CodeOf(err) != fail.Usage {
		t.Fatal("empty ask")
	}
	lintClean(t, dir)
}

func TestFrontmatterEditing(t *testing.T) {
	t.Parallel()
	raw := []byte("---\nid: x\ntitle: T\ntags:\n  - a\n  - b\n# comment for status\nstatus: active\nmystery: 1\n---\nbody\n")
	out, err := SetKey(raw, "tags", Value([]string{"c"}))
	if err != nil {
		t.Fatal(err)
	}
	out, _ = SetKey(out, "trust", "inferred")
	out, _ = SetKey(out, "sensitive", "false")
	out, _ = SetKey(out, "zzz", "1")
	out, _ = RemoveKey(out, "mystery")
	out, _ = RemoveKey(out, "absent")
	want := "---\nid: x\ntitle: T\ntags: [c]\n# comment for status\nstatus: active\ntrust: inferred\nsensitive: false\nzzz: 1\n---\nbody\n"
	if string(out) != want {
		t.Fatalf("got:\n%s", out)
	}
	if _, err := SetKey([]byte("no frontmatter"), "a", "b"); err == nil {
		t.Fatal("want error")
	}
	if _, err := SetKey([]byte("---\na: [\n---\n"), "a", "b"); err == nil {
		t.Fatal("want parse error")
	}
	if got, _ := RemoveKey([]byte("plain"), "a"); string(got) != "plain" {
		t.Fatal("remove on plain")
	}
	for in, want := range map[any]string{"plain": "plain", "a: b": `"a: b"`, "": `""`, "yes": `"yes"`, 3: "3"} {
		if got := Value(in); got != want {
			t.Errorf("Value(%v) = %s", in, got)
		}
	}
	if Slugify("!!!", 10) != "note" || Slugify("Hello, World of Go", 11) != "hello-world" || titleCase("ci-cd") != "Ci Cd" {
		t.Fatal("slug/title")
	}
}
