package cli

import (
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func TestCaptureThenSearchImmediately(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	// Build the index first so the capture must be picked up incrementally.
	if _, _, code := run(t, "--root", dir, "search", "anything"); code != 0 {
		t.Fatal("initial search")
	}
	out, _, code := run(t, "--root", dir, "capture", "Zeppelin cache warmup takes 90 seconds on cold nodes", "--type", "gotcha", "--tags", "k8s,cache")
	if code != 0 || !strings.HasPrefix(out, "knowledge/_inbox/") {
		t.Fatalf("capture: %d %q", code, out)
	}
	out, _, code = run(t, "--root", dir, "search", "zeppelin warmup", "kind:inbox")
	if code != 0 || !strings.Contains(out, "inbox/gotcha") || !strings.Contains(out, "Zeppelin cache warmup") {
		t.Fatalf("capture not searchable: %q", out)
	}
}

func TestKnowledgeCommands(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	steps := []struct {
		args []string
		want string
		code int
	}{
		{[]string{"new", "shard", "testing/pytest/new-fixture-rule", "--type", "standard", "--title", "New rule", "--summary", "A rule."}, "knowledge/testing/pytest/new-fixture-rule.md", 0},
		{[]string{"new", "shard", "testing/pytest/x"}, "", fail.Usage},
		{[]string{"new", "playbook", "do-the-thing"}, ".claude/skills/do-the-thing/SKILL.md", 0},
		{[]string{"mv", "jwt-clock-skew", "jwt-leeway"}, "knowledge/api/auth/jwt-leeway.md", 0},
		{[]string{"supersede", "deprecation-header", "api-versioning"}, "knowledge/api/versioning/api-versioning.md", 0},
		{[]string{"archive", "alert-routing"}, "knowledge/_archive/infra/observability/alert-routing.md", 0},
		{[]string{"archive", "nope-nope"}, "", fail.Partial},
		{[]string{"hubs"}, "knowledge/_hubs/hub-api.md", 0},
		{[]string{"questions", "ask", "Which", "coverage", "target?", "--blocking", "coverage-floor"}, "knowledge/_questions/q-2026", 0},
		{[]string{"questions", "list"}, "q-20261103-coverage-floor", 0},
		{[]string{"--format", "json", "questions", "list", "--all"}, `"questions"`, 0},
		{[]string{"questions", "show", "q-20261103-coverage-floor"}, "## Question", 0},
		{[]string{"questions", "answer", "q-20261103-coverage-floor"}, "", fail.Usage},
		{[]string{"capture", "x", "--source", "bogus"}, "", fail.Usage},
		{[]string{"lint"}, "", 0},
	}
	for _, s := range steps {
		out, errOut, code := run(t, append([]string{"--root", dir}, s.args...)...)
		if code != s.code || !strings.Contains(out, s.want) {
			t.Fatalf("%v: code=%d out=%q err=%q", s.args, code, out, errOut)
		}
	}
}
