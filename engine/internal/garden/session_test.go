package garden

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func sh(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gardenRepo builds a git repo holding the fixture instance, pushed to a
// local bare origin, with a fake gh on PATH that logs its argv.
func gardenRepo(t *testing.T) (*config.Instance, string) {
	t.Helper()
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com"} {
		t.Setenv(k, v)
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "gh.log")
	fake := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"" + log + "\"\ncase \"$1 $2\" in\n\"pr create\") echo https://github.com/o/r/pull/42 ;;\n\"pr list\") echo '[]' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fake), 0o755); err != nil { //nolint:gosec // G306: test fake must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	bare := t.TempDir()
	sh(t, bare, "git", "init", "--quiet", "--bare", "-b", "main")
	root := testutil.CopyFixture(t, "instance-basic")
	cfg, _ := os.ReadFile(filepath.Join(root, ".strata", "config.toml"))
	testutil.WriteFile(t, root, ".strata/config.toml", string(cfg)+"\n[garden]\nforge = \"github\"\n")
	testutil.WriteFile(t, root, ".gitignore", "/.strata/index.db*\n/.strata/*.lock\n/.strata/garden/\n/.strata/worktrees/\n/.strata/telemetry/\n/.strata/session/\n/.strata/garden.lock\n")
	sh(t, root, "git", "init", "--quiet", "-b", "main")
	sh(t, root, "git", "add", "-A")
	sh(t, root, "git", "commit", "--quiet", "-m", "init")
	sh(t, root, "git", "remote", "add", "origin", bare)
	sh(t, root, "git", "push", "--quiet", "-u", "origin", "main")
	sh(t, root, "git", "remote", "set-head", "origin", "main")
	inst, err := config.Open(root, "")
	if err != nil {
		t.Fatal(err)
	}
	return inst, log
}

func TestGardenStartFinish(t *testing.T) { //nolint:paralleltest // sets PATH and git identity
	inst, log := gardenRepo(t)
	ctx := context.Background()
	res, err := Start(ctx, inst, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tidy || res.Branch != "garden/20261107-1200" || res.Base != "main" || res.Tasks == 0 || res.Fixes == 0 {
		t.Fatalf("start: %+v", res)
	}
	if _, err := os.Stat(res.PlanFile); err != nil {
		t.Fatalf("plan file: %v", err)
	}
	if _, err := Start(ctx, inst, now); fail.CodeOf(err) != fail.Locked {
		t.Fatalf("second start: %v", err)
	}
	// The gardener's work in the worktree.
	testutil.WriteFile(t, res.Worktree, "knowledge/_inbox/20261101-120000-retry-budget.md", "")
	if err := os.Remove(filepath.Join(res.Worktree, "knowledge", "_inbox", "20261101-120000-retry-budget.md")); err != nil {
		t.Fatal(err)
	}
	sh(t, res.Worktree, "git", "commit", "--quiet", "-am", "garden(knowledge): file 1 inbox item")
	fin, err := Finish(ctx, inst, FinishOptions{Body: "## Garden digest"})
	if err != nil {
		t.Fatal(err)
	}
	if fin.PR == nil || fin.PR.Number != 42 || !fin.Ready {
		t.Fatalf("finish: %+v", fin)
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{"pr list --state open", "pr create --draft --head garden/20261107-1200 --base main --title garden: 20261107-1200 --body ## Garden digest", "pr ready 42"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("gh calls missing %q:\n%s", want, calls)
		}
	}
	if _, err := os.Stat(res.Worktree); !os.IsNotExist(err) {
		t.Error("worktree not removed")
	}
	if _, err := LoadSession(inst); fail.CodeOf(err) != fail.Usage {
		t.Errorf("marker not removed: %v", err)
	}
	if out := sh(t, inst.Root, "git", "ls-remote", "origin", "garden/20261107-1200"); out == "" {
		t.Error("branch not pushed")
	}
}

func TestGardenAbortAndNothingToPropose(t *testing.T) { //nolint:paralleltest // sets PATH and git identity
	inst, _ := gardenRepo(t)
	ctx := context.Background()
	res, err := Start(ctx, inst, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := Abort(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(res.Worktree); !os.IsNotExist(err) {
		t.Fatal("worktree kept")
	}
	if out := sh(t, inst.Root, "git", "branch", "--list", res.Branch); out != "" {
		t.Fatalf("branch kept: %q", out)
	}
	// A run whose branch has no commits beyond the base proposes nothing.
	res, err = Start(ctx, inst, now)
	if err != nil {
		t.Fatal(err)
	}
	sh(t, res.Worktree, "git", "reset", "--quiet", "--hard", "origin/main")
	fin, err := Finish(ctx, inst, FinishOptions{})
	if err != nil || fin.Message != "nothing to propose" || fin.PR != nil {
		t.Fatalf("finish empty: %+v %v", fin, err)
	}
	if err := Abort(ctx, inst); fail.CodeOf(err) != fail.Usage {
		t.Fatalf("abort without run: %v", err)
	}
	testutil.WriteFile(t, inst.Root, RunMarker, "")
	if err := Abort(ctx, inst); err != nil {
		t.Fatalf("abort of a broken marker: %v", err)
	}
}

func TestDropTouching(t *testing.T) {
	t.Parallel()
	p := &Plan{Tasks: []Task{
		newTask(FileInbox, Low, "", []string{"a"}, map[string]any{evPath: "knowledge/_inbox/a.md"}),
		newTask(MergeDuplicates, Low, "", []string{"b", "c"}, map[string]any{"paths": []string{"knowledge/x/b.md", "knowledge/x/c.md"}}),
		newTask(FixOrphan, Low, "", []string{"d"}, map[string]any{evPath: "knowledge/x/d.md"}),
	}}
	skipped := p.dropTouching(map[string]bool{"knowledge/_inbox/a.md": true, "knowledge/x/c.md": true})
	if len(skipped) != 2 || len(p.Tasks) != 1 || p.Tasks[0].Type != FixOrphan {
		t.Fatalf("skipped %v left %+v", skipped, p.Tasks)
	}
}
