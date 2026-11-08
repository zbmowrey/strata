package index

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func openInst(t *testing.T, dir string) *config.Instance {
	t.Helper()
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

func mustOpen(t *testing.T, inst *config.Instance, opts Options) (*Index, Stats) {
	t.Helper()
	ix, st, err := Open(context.Background(), inst, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ix.Close() })
	return ix, st
}

// dump renders the whole index (minus rowids and mtimes) for comparison.
func dump(t *testing.T, db *sql.DB) string {
	t.Helper()
	var b strings.Builder
	queries := []string{
		"SELECT id, kind, path, src, coalesce(anchor,''), domain, topic, type, status, trust, title, summary, created, updated, verified_at, review_after_days, side_effect, runtime, bytes, lines, tokens_est, private, meta_json FROM docs ORDER BY id",
		"SELECT d.id, t.tag FROM tags t JOIN docs d ON d.rowid = t.doc ORDER BY 1, 2",
		"SELECT a.alias, d.id FROM aliases a JOIN docs d ON d.rowid = a.doc ORDER BY 1",
		"SELECT d.id, l.dst_id, l.anchor, l.kind FROM links l JOIN docs d ON d.rowid = l.src ORDER BY 1, 2, 3, 4",
		"SELECT d.id, s.ord, s.heading, s.slug, s.start_byte, s.end_byte FROM sections s JOIN docs d ON d.rowid = s.doc ORDER BY 1, 2",
		"SELECT d.id, a.glob FROM applies_to a JOIN docs d ON d.rowid = a.doc ORDER BY 1, 2",
		"SELECT path, size, hex(hash), kind FROM files ORDER BY path",
		"SELECT d.id FROM docs_fts f JOIN docs d ON d.rowid = f.rowid WHERE docs_fts MATCH 'teardown OR deploy OR pool' ORDER BY 1",
		"SELECT d.id FROM names_tri n JOIN docs d ON d.rowid = n.rowid WHERE names_tri MATCH 'fixtur' ORDER BY 1",
	}
	for _, q := range queries {
		rows, err := db.QueryContext(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		dumpRows(t, rows, ptrs, vals, &b)
		b.WriteString("--\n")
	}
	return b.String()
}

func dumpRows(t *testing.T, rows *sql.Rows, ptrs, vals []any, b *strings.Builder) {
	t.Helper()
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(b, vals...)
	}
}

func TestIncrementalRefresh(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	inst := openInst(t, dir)
	ix, st := mustOpen(t, inst, Options{})
	if st.Added != 54 || st.Files != 54 {
		t.Fatalf("initial %+v", st)
	}
	var docs int
	if err := ix.DB().QueryRowContext(context.Background(), "SELECT count(*) FROM docs").Scan(&docs); err != nil || docs != 55 {
		t.Fatalf("docs = %d (%v)", docs, err)
	}
	ctx := context.Background()
	shard := filepath.Join(dir, "knowledge", "testing", "pytest", "pytest-fixture-teardown.md")
	raw, _ := os.ReadFile(shard)
	testutil.WriteFile(t, dir, "knowledge/testing/pytest/pytest-fixture-teardown.md", strings.Replace(string(raw), "the last test", "the last quokka", 1))
	testutil.WriteFile(t, dir, "knowledge/testing/pytest/brand-new.md", "---\nid: brand-new\ntitle: New\nsummary: s\ntype: fact\ntags: [x]\nstatus: active\ntrust: inferred\ncreated: 2026-01-01\nupdated: 2026-01-01\n---\nzebra quagga\n")
	if err := os.Remove(filepath.Join(dir, "knowledge", "api", "http", "http-timeouts.md")); err != nil {
		t.Fatal(err)
	}
	// Same content, new mtime: touched, not re-indexed.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "knowledge", "api", "auth", "jwt-clock-skew.md"), future, future); err != nil {
		t.Fatal(err)
	}
	st, err := ix.Refresh(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if st.Added != 1 || st.Changed != 1 || st.Removed != 1 {
		t.Fatalf("refresh %+v", st)
	}
	var touched int64
	_ = ix.DB().QueryRowContext(context.Background(), "SELECT mtime_ns FROM files WHERE path = 'knowledge/api/auth/jwt-clock-skew.md'").Scan(&touched)
	if touched != future.UnixNano() {
		t.Fatal("mtime-only change not recorded")
	}
	var n int
	_ = ix.DB().QueryRowContext(context.Background(), "SELECT count(*) FROM docs_fts WHERE docs_fts MATCH 'quokka'").Scan(&n)
	if n != 1 {
		t.Fatal("changed content not indexed")
	}
	testutil.WriteFile(t, dir, "knowledge/testing/pytest/pytest-fixture-teardown.md", string(raw))
	if _, err := ix.Refresh(ctx, false); err != nil {
		t.Fatal(err)
	}
	_ = ix.DB().QueryRowContext(context.Background(), "SELECT count(*) FROM docs_fts WHERE docs_fts MATCH 'quokka'").Scan(&n)
	if n != 0 {
		t.Fatal("old content still indexed")
	}
	_ = ix.DB().QueryRowContext(context.Background(), "SELECT count(*) FROM docs WHERE id = 'http-timeouts'").Scan(&n)
	if n != 0 {
		t.Fatal("deleted file still indexed")
	}
	st, err = ix.Refresh(ctx, false)
	if err != nil || st.Added+st.Changed+st.Removed != 0 {
		t.Fatalf("second refresh not a no-op: %+v %v", st, err)
	}
	// Property: incremental result == full rebuild.
	before := dump(t, ix.DB())
	if _, err := ix.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(before, dump(t, ix.DB())); diff != "" {
		t.Fatalf("incremental != rebuild (-inc +rebuild):\n%s", diff)
	}
}

func TestRecovery(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	inst := openInst(t, dir)
	ix, _ := mustOpen(t, inst, Options{})
	_ = ix.Close()
	// Corrupt the database file.
	if err := os.WriteFile(filepath.Join(dir, DBFile), []byte("this is not a database at all, not even close......"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(dir, DBFile+"-wal"))
	ix2, st := mustOpen(t, inst, Options{})
	if st.Added != 54 || len(ix2.Events) == 0 {
		t.Fatalf("recovery %+v events %v", st, ix2.Events)
	}
	// Schema version mismatch triggers a rebuild.
	if _, err := ix2.DB().ExecContext(context.Background(), "PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	_ = ix2.Close()
	ix3, st := mustOpen(t, inst, Options{})
	if st.Added != 54 || !strings.Contains(strings.Join(ix3.Events, ","), "rebuild") {
		t.Fatalf("version mismatch %+v %v", st, ix3.Events)
	}
}

func TestNoRefreshAndLocking(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	inst := openInst(t, dir)
	ix, st := mustOpen(t, inst, Options{NoRefresh: true})
	if !st.Skipped {
		t.Fatalf("%+v", st)
	}
	if _, err := ix.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, dir, "knowledge/a/b/new-one.md", "---\nid: new-one\n---\nx\n")
	lock, err := fsutil.TryLock(inst.Path(LockFile))
	if err != nil {
		t.Fatal(err)
	}
	st, err = ix.Refresh(context.Background(), false)
	if err != nil || !st.Skipped {
		t.Fatalf("locked refresh %+v %v", st, err)
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = lock.Unlock()
	}()
	st, err = ix.Refresh(context.Background(), true)
	if err != nil || st.Added != 1 {
		t.Fatalf("strict refresh %+v %v", st, err)
	}
}

func TestIDCollisionAndKinds(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	testutil.WriteFile(t, dir, ".claude/skills/deploy-flow/SKILL.md", "---\nname: deploy-flow\ndescription: Collides with a shard id.\n---\nbody\n")
	testutil.WriteFile(t, dir, "knowledge/_hubs/hub-testing.md", "---\nid: hub-testing\ntitle: Testing\nsummary: Map of testing.\ntype: index\n---\nintro\n")
	testutil.WriteFile(t, dir, "knowledge/_archive/old/stuff/ancient.md", "---\nid: ancient\ntitle: Ancient\nsummary: s\ntype: fact\ntags: [x]\nstatus: active\ntrust: inferred\ncreated: 2020-01-01\nupdated: 2020-01-01\n---\nold\n")
	testutil.WriteFile(t, dir, "knowledge/_private/p/q/secret-ish.md", "---\nid: secret-ish\ntitle: Where creds live\nsummary: s\ntype: fact\ntags: [x]\nstatus: active\ntrust: inferred\ncreated: 2020-01-01\nupdated: 2020-01-01\nsensitive: true\n---\nin 1password\n")
	testutil.WriteFile(t, dir, "projects/ci-flake/out/report.md", "# Report\n")
	testutil.WriteFile(t, dir, "projects/ci-flake/log.md", "# Log\n\n## 2026-10-21 10:00 · s:a\n- note: one\n\n## 2026-10-21 10:00 · s:b\n- note: two\n")
	ix, _ := mustOpen(t, openInst(t, dir), Options{})
	t.Run("shard wins regardless of order", func(t *testing.T) {
		// Re-add the shard after the playbook holds the id: the shard must win.
		p := filepath.Join(dir, "knowledge", "infra", "deploy", "deploy-flow.md")
		raw, _ := os.ReadFile(p)
		_ = os.Remove(p)
		if _, err := ix.Refresh(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		var kind string
		_ = ix.DB().QueryRowContext(context.Background(), "SELECT kind FROM docs WHERE id = 'deploy-flow'").Scan(&kind)
		if kind != "playbook" {
			t.Fatalf("freed id not given to playbook: %q", kind)
		}
		testutil.WriteFile(t, dir, "knowledge/infra/deploy/deploy-flow.md", string(raw))
		if _, err := ix.Refresh(context.Background(), false); err != nil {
			t.Fatal(err)
		}
	})
	want := map[string]string{
		"playbook:deploy-flow":            "playbook",
		"hub-testing":                     "hub",
		"ancient":                         "shard archived",
		"secret-ish":                      "shard private",
		"ci-flake:out/report.md":          "project",
		"ci-flake:log#2026-10-21-10-00":   "project",
		"ci-flake:log#2026-10-21-10-00-2": "project",
	}
	for id, w := range want {
		var kind, status string
		var private bool
		if err := ix.DB().QueryRowContext(context.Background(), "SELECT kind, status, private FROM docs WHERE id = ?", id).Scan(&kind, &status, &private); err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		got := kind
		if status == "archived" {
			got += " archived"
		}
		if private {
			got += " private"
		}
		if got != w {
			t.Errorf("%s: got %q want %q", id, got, w)
		}
	}
}

func TestExtractDefaults(t *testing.T) {
	t.Parallel()
	r := Record{Bytes: 9}
	if r.TokensEst() != 3 || r.MetaJSON() != "{}" {
		t.Fatal("record helpers")
	}
	if truncate("abcdef", 4) != "abc…" || dateOf("2026") != "2026" || firstNonEmpty() != "" {
		t.Fatal("helpers")
	}
	if ProjectSlug("projects/_archive/old/plan.md", "projects") != "old" {
		t.Fatal("slug")
	}
}
