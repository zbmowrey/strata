package garden

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

var today = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func now() time.Time { return today }

func shard(id, title, typ, created, extra, body string) string {
	return fmt.Sprintf("---\nid: %s\ntitle: %s\nsummary: %s.\ntype: %s\ntags: [garden]\nstatus: active\ntrust: observed\ncreated: %s\nupdated: %s\nsources:\n  - { kind: file, ref: \"README.md\", at: %s }\n%s---\n%s\n",
		id, title, title, typ, created, created, created, extra, body)
}

func tel(t *testing.T, root, month string, lines ...string) {
	t.Helper()
	testutil.WriteFile(t, root, ".strata/telemetry/"+month+".jsonl", strings.Join(lines, "\n")+"\n")
}

func ev(ts, session, name, fields string) string {
	return fmt.Sprintf(`{"ts":%q,"v":1,"session":%q,"event":%q,%s}`, ts+"T10:00:00Z", session, name, fields)
}

// seeded copies instance-basic and adds one trigger per task type.
func seeded(t *testing.T) *config.Instance {
	t.Helper()
	root := testutil.CopyFixture(t, "instance-basic")
	k := "knowledge/"
	// merge-duplicates: near-identical title and body to postgres-connection-pool.
	testutil.WriteFile(t, root, k+"database/postgres/pg-connection-pool-size.md", shard("pg-connection-pool-size", "The app uses a pool of 20 connections", "fact", "2026-06-01", "",
		"Every app instance opens a `psycopg_pool.ConnectionPool` with `max_size=20` and a non-daemon maintenance thread.\n\n## See also\n- [[postgres-connection-pool]]"))
	// split-oversize.
	testutil.WriteFile(t, root, k+"testing/pytest/pytest-everything.md", shard("pytest-everything", "Everything about pytest", "reference", "2026-06-01", "",
		strings.Repeat("A long line about pytest configuration and fixtures.\n", 130)+"\n[[pytest-markers-registered]]"))
	// archive-cold: created long ago, never searched, nothing links to it.
	testutil.WriteFile(t, root, k+"infra/k8s/k8s-old-ingress.md", shard("k8s-old-ingress", "Old ingress controller notes", "reference", "2025-01-10", "", "The previous ingress controller used annotations."))
	// fix-broken-link (repairable typo) and ambiguous broken link.
	testutil.WriteFile(t, root, k+"api/http/api-retries.md", shard("api-retries", "API clients retry idempotent requests", "standard", "2026-06-01", "",
		"## Rule\nRetry idempotent requests.\n## Rationale\nSee [[postgres-connection-pol]] and [[no-such-document-anywhere]].\n## Enforcement\nnone yet"))
	// resolve-conflict: same applies_to, one negated.
	testutil.WriteFile(t, root, k+"database/postgres/pg-pool-reuse.md", shard("pg-pool-reuse", "Reuse the database connection pool in workers", "standard", "2026-06-01", "applies_to: [\"app/worker.py\"]\n", "## Rule\nShare it.\n## Rationale\nCost.\n## Enforcement\nreview"))
	testutil.WriteFile(t, root, k+"database/postgres/pg-pool-per-worker.md", shard("pg-pool-per-worker", "Never reuse the database connection pool in workers", "gotcha", "2026-06-01", "applies_to: [\"app/worker.py\"]\n", "## Symptom\nx\n## Cause\ny\n## Fix\nz\n\n[[pg-pool-reuse]]"))
	// apply-answer.
	q := root + "/knowledge/_questions/q-20261103-coverage-floor.md"
	raw, _ := os.ReadFile(q)
	testutil.WriteFile(t, root, k+"_questions/q-20261103-coverage-floor.md", strings.Replace(string(raw), "status: open", "status: answered\nanswered: 2026-11-05", 1))
	// distill-project (done) and stale-project (no activity for months).
	testutil.WriteFile(t, root, "projects/old-migration/project.md", "---\nid: old-migration\ntitle: Old migration\nstatus: done\ncreated: 2026-05-01\nupdated: 2026-06-01\ntags: []\nshards: []\ntrack: []\n---\nDone.\n")
	testutil.WriteFile(t, root, "projects/old-migration/log.md", "## 2026-06-01 10:00 · s:aaaa\n- worked: migrated\n")
	testutil.WriteFile(t, root, "projects/dormant/project.md", "---\nid: dormant\ntitle: Dormant work\nstatus: active\ncreated: 2026-05-01\nupdated: 2026-06-01\ntags: []\nshards: []\ntrack: []\n---\nIdle.\n")
	testutil.WriteFile(t, root, "projects/dormant/log.md", "## 2026-06-01 10:00 · s:bbbb\n- tried: something\n")
	// deprecated-script-removal.
	m := root + "/scripts/database/check-migrations/script.yaml"
	raw, _ = os.ReadFile(m)
	testutil.WriteFile(t, root, "scripts/database/check-migrations/script.yaml", strings.Replace(string(raw), "status: active", "status: deprecated\nreplaced_by: check-deploy-config", 1))
	// Telemetry: coverage back to April (cold/deprecated windows), gaps,
	// a resolved gap (bench-grow), a repeated command (scriptify), and
	// co-accessed shards (playbook promotion).
	tel(t, root, "2026-04", ev("2026-04-01", "s0", "search", `"query":"anything","zero":false,"top_ids":["postgres-connection-pool"]`))
	var nov []string
	for i, s := range []string{"s1", "s2", "s3"} {
		d := fmt.Sprintf("2026-11-0%d", i+1)
		nov = append(nov,
			ev(d, s, "search", `"query":"Kafka consumer lag","zero":true,"top_ids":[]`),
			ev(d, s, "bash", `"first_token":"docker","normalized_hash":"00000000000000aa","length":40`),
			ev(d, s, "show", `"ids":["jwt-clock-skew","auth-token-storage","oauth-scopes-glossary"]`))
	}
	nov = append(nov,
		ev("2026-10-20", "s4", "search", `"query":"helm rollback","zero":true,"top_ids":[]`),
		ev("2026-10-21", "s5", "search", `"query":"helm rollback","zero":true,"top_ids":[]`),
		ev("2026-11-04", "s6", "search", `"query":"helm rollback","zero":false,"top_ids":["helm-values-layering"]`))
	tel(t, root, "2026-11", nov...)
	inst, err := config.Open(root, "")
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

func TestPlanGoldenEveryTaskType(t *testing.T) {
	t.Parallel()
	inst := seeded(t)
	// The first run builds the index (a fix); compare the steady state.
	if _, err := Run(context.Background(), inst, Options{Now: now, Plan: true}); err != nil {
		t.Fatal(err)
	}
	p, err := Run(context.Background(), inst, Options{Now: now, Plan: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range TaskTypes {
		if p.Counts[typ] == 0 {
			t.Errorf("no %s task", typ)
		}
	}
	raw, err := os.ReadFile(inst.Path(PlanFile))
	if err != nil {
		t.Fatal(err)
	}
	testutil.Golden(t, filepath.Join("testdata", "plan.golden.json"), raw)
	// Deterministic: a second run yields the same plan.
	if _, err := Run(context.Background(), inst, Options{Now: now, Plan: true}); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(inst.Path(PlanFile))
	if !bytes.Equal(again, raw) {
		t.Fatal("plan not deterministic")
	}
}

func TestApplyFixesIdempotent(t *testing.T) {
	t.Parallel()
	inst := seeded(t)
	testutil.WriteFile(t, inst.Root, "knowledge/api/http/api-messy.md", "---\ntitle: Messy frontmatter order\nid: api-messy\nsummary: Keys out of order.\ntype: fact\ntags: [API, api]\nstatus: active\ntrust: observed\ncreated: 2026-06-01\nupdated: 2026-06-01\nsources:\n  - { kind: file, ref: \"README.md\", at: 2026-06-01 }\n---\nBody.   \n\nSee [[pg-pool]].\n")
	testutil.WriteFile(t, inst.Root, ".strata/gates.toml", "[[gate]]\nid = \"x\"\ncmd = [\"true\"]\nstages = [\"ci\"]\nbaseline = \".strata/baselines/x.json\"\n")
	bl := testutil.WriteFile(t, inst.Root, ".strata/baselines/x.json", "{\"gate\":\"x\",\"entries\":[]}\n")
	old := today.Add(-20 * 24 * time.Hour)
	if err := os.Chtimes(bl, old, old); err != nil {
		t.Fatal(err)
	}
	p, err := Run(context.Background(), inst, Options{Now: now, Apply: true, Plan: true})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]Fix{}
	for _, f := range p.Fixes {
		kinds[f.Kind] = f
	}
	for _, want := range []string{"hubs", "catalogs", "normalize", "baselines", "links", "telemetry"} {
		if _, ok := kinds[want]; !ok {
			t.Errorf("missing fix %s: %+v", want, p.Fixes)
		}
	}
	body, _ := os.ReadFile(inst.Path("knowledge/api/http/api-retries.md"))
	if !strings.Contains(string(body), "[[postgres-connection-pool]]") || !strings.Contains(string(body), "[[no-such-document-anywhere]]") {
		t.Fatalf("link repair:\n%s", body)
	}
	messy, _ := os.ReadFile(inst.Path("knowledge/api/http/api-messy.md"))
	if !strings.HasPrefix(string(messy), "---\nid: api-messy\n") || !strings.Contains(string(messy), "[[postgres-connection-pool]]") {
		t.Fatalf("normalize:\n%s", messy)
	}
	if _, err := os.Stat(bl); !os.IsNotExist(err) {
		t.Fatal("empty baseline kept")
	}
	again, err := Run(context.Background(), inst, Options{Now: now, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range again.Fixes {
		if f.Kind != "index" {
			t.Errorf("second run changed something: %+v", f)
		}
	}
}

func TestLockedAndEscalate(t *testing.T) {
	t.Parallel()
	inst := seeded(t)
	lock, err := fsutil.TryLock(inst.Path(LockFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), inst, Options{Now: now, Apply: true}); fail.CodeOf(err) != fail.Locked {
		t.Fatalf("want locked, got %v", err)
	}
	_ = lock.Unlock()
	inst.Config.Garden.Escalate = []string{FixOrphan}
	p, err := Run(context.Background(), inst, Options{Now: now, Plan: true})
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(p.Tasks, func(x Task) bool { return x.Type == FixOrphan })
	if i < 0 || !p.Tasks[i].Escalate || p.Tasks[i].Risk != Question {
		t.Fatalf("escalate: %+v", p.Tasks[i])
	}
	var decoded Plan
	b, _ := json.Marshal(p)
	if json.Unmarshal(b, &decoded) != nil || decoded.V != 1 {
		t.Fatal("plan json")
	}
}

func TestPrivateTierNeverRead(t *testing.T) {
	t.Parallel()
	inst := seeded(t)
	testutil.WriteFile(t, inst.Root, "knowledge/_private/secret-orphan.md", shard("secret-orphan", "Private client notes", "fact", "2026-06-01", "", "client stuff"))
	p, err := Run(context.Background(), inst, Options{Now: now, Plan: true})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "secret-orphan") {
		t.Fatal("plan mentions a private shard")
	}
}

func TestIncrementalAuto(t *testing.T) {
	t.Parallel()
	inst := seeded(t)
	rel := "knowledge/api/http/api-trailing.md"
	testutil.WriteFile(t, inst.Root, rel, shard("api-trailing", "Trailing whitespace example", "fact", "2026-06-01", "", "Body with trailing spaces.   "))
	testutil.WriteFile(t, inst.Root, "projects/ci-flake/project.md", strings.Replace(string(must(os.ReadFile(inst.Path("projects/ci-flake/project.md")))), "track: []", "track: [\"notes/**\"]", 1))
	p, err := Run(context.Background(), inst, Options{Now: now, Apply: true, Changed: []string{rel, "projects/ci-flake/project.md", "scripts/infra/check-deploy-config/script.yaml", "knowledge/_private/x.md"}})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, f := range p.Fixes {
		kinds[f.Kind] = true
	}
	for _, want := range []string{"normalize", "hubs", "catalogs", "gitignore"} {
		if !kinds[want] {
			t.Errorf("missing %s in %+v", want, p.Fixes)
		}
	}
	if kinds["baselines"] || kinds["telemetry"] {
		t.Errorf("incremental ran full-pass fixes: %+v", p.Fixes)
	}
	if b := must(os.ReadFile(inst.Path(rel))); strings.Contains(string(b), "spaces.   ") {
		t.Fatal("not normalized")
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
