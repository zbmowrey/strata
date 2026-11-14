package garden

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func clean(context.Context, *config.Instance, []string) (bool, error) { return true, nil }

func dirty(context.Context, *config.Instance, []string) (bool, error) { return false, nil }

// startRun starts a run and opens its worktree as an instance.
func startRun(t *testing.T) (*config.Instance, *StartResult, *config.Instance) {
	t.Helper()
	inst, _ := gardenRepo(t)
	res, err := Start(context.Background(), inst, now, Owner{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Tidy || res.Tasks == 0 {
		t.Fatalf("start: %+v", res)
	}
	wt, err := config.Open(res.Worktree, "")
	if err != nil {
		t.Fatal(err)
	}
	return inst, res, wt
}

func firstTask(t *testing.T, inst *config.Instance, run string) string {
	t.Helper()
	p, err := LoadProgress(inst.Root, run)
	if err != nil || len(p.Order) == 0 {
		t.Fatalf("progress: %v", err)
	}
	return p.Order[0]
}

func at(d time.Duration) func() time.Time { return func() time.Time { return today.Add(d) } }

func TestUpdateTask(t *testing.T) { //nolint:paralleltest // sets PATH and git identity
	inst, res, wt := startRun(t)
	id := firstTask(t, inst, res.Run)
	if res.PlanFile != filepath.Join(RunDir(inst.Root, res.Run), runPlanFile) {
		t.Fatalf("plan file: %s", res.PlanFile)
	}
	p, err := UpdateTask(wt, id, "done", "filed it", today.Add(5*time.Minute))
	if err != nil || p.Tasks[id].Status != TaskDone || p.Tasks[id].Note != "filed it" {
		t.Fatalf("done: %+v %v", p, err)
	}
	if s, _ := LoadSession(inst); s.HeartbeatAt != today.Add(5*time.Minute).UTC().Format(time.RFC3339) {
		t.Fatalf("task did not heartbeat: %+v", s)
	}
	for _, tc := range []struct{ action, id string }{{"finish", id}, {"done", "no-such-task"}} {
		if _, err := UpdateTask(wt, tc.id, tc.action, "", today); fail.CodeOf(err) != fail.Usage {
			t.Errorf("%s %s: %v", tc.action, tc.id, err)
		}
	}
	if _, err := UpdateTask(inst, id, "done", "", today); fail.CodeOf(err) != fail.Usage {
		t.Errorf("outside a worktree: %v", err)
	}
	if err := Abort(context.Background(), inst); err != nil {
		t.Fatal(err)
	}
	h, _ := ReadHistory(inst)
	if len(h) != 2 || h[0].Outcome != OutcomeStarted || h[1].Outcome != OutcomeAborted || h[1].TasksDone != 1 {
		t.Fatalf("history: %+v", h)
	}
}

func TestResumeSalvagesAndResets(t *testing.T) { //nolint:paralleltest // sets PATH and git identity
	inst, res, wt := startRun(t)
	ctx := context.Background()
	id := firstTask(t, inst, res.Run)
	if _, err := UpdateTask(wt, id, "start", "", today); err != nil {
		t.Fatal(err)
	}
	// Uncommitted work in the lost worktree, including a new file.
	testutil.WriteFile(t, res.Worktree, "knowledge/_inbox/20261107-130000-salvaged.md", "---\ntype: fact\ncaptured: 2026-11-07T13:00:00Z\n---\nSalvaged.\n")
	// Still live: refused unless forced.
	if _, err := Resume(ctx, inst, ResumeOptions{Now: at(10 * time.Minute), Verify: clean}); fail.CodeOf(err) != fail.Locked {
		t.Fatalf("live resume: %v", err)
	}
	r, err := Resume(ctx, inst, ResumeOptions{Now: at(45 * time.Minute), Verify: clean})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Resumed || r.Fence != 2 || r.Attempt != 2 || r.Salvage != "" || !strings.HasSuffix(r.Worktree, "-a2") {
		t.Fatalf("resume: %+v", r)
	}
	if _, err := os.Stat(res.Worktree); !os.IsNotExist(err) {
		t.Fatal("old worktree kept")
	}
	if log := sh(t, r.Worktree, "git", "log", "-1", "--format=%s"); !strings.Contains(log, "salvage 1 files") {
		t.Fatalf("salvage not committed: %q", log)
	}
	p, _ := LoadProgress(inst.Root, res.Run)
	if p.Tasks[id].Status != TaskPending || p.Tasks[id].Tries != 1 {
		t.Fatalf("interrupted task: %+v", p.Tasks[id])
	}
	// Interrupted again → poison: escalated with a committed question.
	wt2, err := config.Open(r.Worktree, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateTask(wt2, id, "start", "", today.Add(46*time.Minute)); err != nil {
		t.Fatal(err)
	}
	r3, err := Resume(ctx, inst, ResumeOptions{Now: at(2 * time.Hour), Verify: clean})
	if err != nil {
		t.Fatal(err)
	}
	p, _ = LoadProgress(inst.Root, res.Run)
	if p.Tasks[id].Status != TaskEscalated || !strings.Contains(p.Tasks[id].Note, "poison") {
		t.Fatalf("poison task: %+v", p.Tasks[id])
	}
	if log := sh(t, r3.Worktree, "git", "log", "-1", "--format=%s"); !strings.Contains(log, "escalate 1 interrupted") {
		t.Fatalf("question not committed: %q", log)
	}
	// Attempt 3 of 3: the next resume marks the run stuck.
	if _, err := Resume(ctx, inst, ResumeOptions{Now: at(3 * time.Hour), Verify: clean}); fail.CodeOf(err) != fail.Locked || !strings.Contains(err.Error(), "stuck") {
		t.Fatalf("stuck: %v", err)
	}
	if b, err := os.ReadFile(inst.Path(filepath.FromSlash(lastFailure))); err != nil || !strings.Contains(string(b), "poison tasks: "+id) {
		t.Fatalf("last-failure: %s %v", b, err)
	}
	// A human can still force it, then finish with a rendered digest.
	r4, err := Resume(ctx, inst, ResumeOptions{Now: at(4 * time.Hour), Verify: clean, Force: true})
	if err != nil || r4.Attempt != 4 {
		t.Fatalf("forced resume: %+v %v", r4, err)
	}
	if digest := RenderDigest(inst.Root, mustSession(t, inst)); !strings.Contains(digest, "### Questions raised") || !strings.Contains(digest, id) {
		t.Fatalf("digest:\n%s", digest)
	}
	if _, err := Finish(ctx, inst, FinishOptions{Fence: r4.Fence}); err != nil {
		t.Fatal(err)
	}
	h, _ := ReadHistory(inst)
	if last := h[len(h)-1]; last.Outcome != OutcomePR || last.PR != 42 || last.Attempt != 4 {
		t.Fatalf("history: %+v", h)
	}
}

func TestResumeKeepsDirtySalvage(t *testing.T) { //nolint:paralleltest // sets PATH and git identity
	inst, res, _ := startRun(t)
	testutil.WriteFile(t, res.Worktree, "knowledge/_inbox/20261107-130000-bad.md", "not a valid capture")
	r, err := Resume(context.Background(), inst, ResumeOptions{Now: at(time.Hour), Verify: dirty})
	if err != nil {
		t.Fatal(err)
	}
	if r.Salvage == "" {
		t.Fatal("dirty salvage not kept")
	}
	if b, err := os.ReadFile(r.Salvage); err != nil || !strings.Contains(string(b), "not a valid capture") {
		t.Fatalf("salvage patch: %v", err)
	}
	if out := sh(t, r.Worktree, "git", "status", "--porcelain"); out != "" {
		t.Fatalf("new worktree not clean: %q", out)
	}
	if err := Abort(context.Background(), inst); err != nil {
		t.Fatal(err)
	}
}

func TestResumeNeedsARun(t *testing.T) { //nolint:paralleltest // sets PATH and git identity
	inst, _ := gardenRepo(t)
	if _, err := Resume(context.Background(), inst, ResumeOptions{}); fail.CodeOf(err) != fail.Usage {
		t.Fatalf("no run: %v", err)
	}
}

func mustSession(t *testing.T, inst *config.Instance) *Session {
	t.Helper()
	s, err := LoadSession(inst)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
