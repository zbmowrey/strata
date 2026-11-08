package search

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/index"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

var today = time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)

func openFixture(t *testing.T) (string, *index.Index) {
	t.Helper()
	dir := testutil.CopyFixture(t, "instance-basic")
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	ix, _, err := index.Open(context.Background(), inst, index.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ix.Close() })
	return dir, ix
}

func ids(hits []Hit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.ID)
	}
	return out
}

func query(t *testing.T, s *Searcher, q string, limit int) *Result {
	t.Helper()
	parsed, err := Parse(q)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Search(context.Background(), Request{Query: parsed, Raw: q, Limit: limit, Scope: Scope{Today: today}, Synonyms: ParseSynonyms([]byte("k8s, kubernetes\npg, postgres\n"))})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestRankingGolden(t *testing.T) {
	t.Parallel()
	_, ix := openFixture(t)
	s := New(ix)
	var b strings.Builder
	for _, q := range []string{
		"tests hang at exit", "fixture teardown", "deploy", "how do we deploy", "rollback", "migration downgrade",
		"pg pool", "k8s limits", "xdist", "jwt", "timeouts", "pagination", "flaky retries", "coverage", "fixtrue",
		"kind:playbook", "kind:script side:read-only", "tag:pytest -tag:database", "status:any sunset",
	} {
		res := query(t, s, q, 5)
		b.WriteString("# " + q + "\n")
		for _, h := range res.Hits {
			fmt.Fprintf(&b, "%s %.2f\n", h.ID, h.Score)
		}
	}
	testutil.Golden(t, "testdata/ranking.golden", []byte(b.String()))
}

func TestDeterministicAndFresh(t *testing.T) {
	t.Parallel()
	dir, ix := openFixture(t)
	s := New(ix)
	a := ids(query(t, s, "deploy OR database", 20).Hits)
	for range 5 {
		if b := ids(query(t, s, "deploy OR database", 20).Hits); !slices.Equal(a, b) {
			t.Fatalf("nondeterministic: %v vs %v", a, b)
		}
	}
	if _, err := ix.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b := ids(query(t, s, "deploy OR database", 20).Hits); !slices.Equal(a, b) {
		t.Fatalf("rebuild changed ranking: %v vs %v", a, b)
	}
	// A deleted file never appears once refreshed.
	if err := os.Remove(filepath.Join(dir, "knowledge", "infra", "deploy", "deploy-flow.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(ids(query(t, s, "deploy", 50).Hits), "deploy-flow") {
		t.Fatal("deleted document returned")
	}
}

func TestShowMatchesFile(t *testing.T) {
	t.Parallel()
	dir, ix := openFixture(t)
	s := New(ix)
	res, err := s.Show(context.Background(), dir, ShowRequest{IDs: []string{"deploy-flow", "pg-pool", "deploy-flow", "hub-nope", "deploy-flow#nope"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Docs) != 2 || res.Docs[1].ID != "postgres-connection-pool" || len(res.Missing) != 2 {
		t.Fatalf("docs %v missing %v", res.Docs, res.Missing)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "knowledge", "infra", "deploy", "deploy-flow.md"))
	body := string(raw[bytes.Index(raw, []byte("\n---\n"))+5:])
	if res.Docs[0].Content != body {
		t.Fatalf("show content differs from file body:\n%q\n%q", res.Docs[0].Content, body)
	}
	for _, mode := range []ShowMode{ShowMeta, ShowBody, ShowBrief} {
		r, err := s.Show(context.Background(), dir, ShowRequest{IDs: []string{"find-leaked-fixtures", "rollback-procedure"}, Mode: mode})
		if err != nil || len(r.Docs) != 2 {
			t.Fatalf("mode %d: %v %v", mode, r, err)
		}
	}
	r, err := s.Show(context.Background(), dir, ShowRequest{IDs: []string{"pytest-fixture-teardown"}, Links: 2, Budget: 60})
	if err != nil || r.Docs[0].Truncated == "" {
		t.Fatalf("budget not applied: %+v %v", r, err)
	}
}

func TestFilterErrors(t *testing.T) {
	t.Parallel()
	_, ix := openFixture(t)
	s := New(ix)
	for _, q := range []string{"kind:x", "trust:x", "side:<=x", "updated:x", "updated:1..x", "stale:x", "path:[", "-type:fact a", "created:>-3d"} {
		parsed, err := Parse(q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		_, err = s.Search(context.Background(), Request{Query: parsed, Scope: Scope{Today: today}})
		if fail.CodeOf(err) != fail.Usage {
			t.Errorf("%s: want usage error, got %v", q, err)
		}
	}
	parsed, _ := Parse("x")
	if _, err := s.Search(context.Background(), Request{Query: parsed, Limit: 999}); fail.CodeOf(err) != fail.Usage {
		t.Errorf("limit: %v", err)
	}
	// Filters that should parse and run.
	for _, q := range []string{"updated:>30d", "updated:<=2026-10-01", "verified:>=2026-01-01", "created:2026-01-01..2026-12-31", "path:knowledge/api/**", "stale:false", "trust:observed", "side:network", "runtime:go", "domain:api topic:http", "project:ci-flake", "type:gotcha", "-meta.owner:user kind:script"} {
		res := query(t, s, q, 5)
		if res.Total == 0 && q != "side:network" {
			t.Errorf("%s: no results", q)
		}
	}
	if res := query(t, s, "path:knowledge/api/http/*.md", 50); res.Total == 0 || slices.ContainsFunc(res.Hits, func(h Hit) bool { return !strings.HasPrefix(h.Path, "knowledge/api/http/") }) {
		t.Errorf("path filter: %v", ids(res.Hits))
	}
}

func TestPrivateArchivedScope(t *testing.T) {
	t.Parallel()
	dir, ix := openFixture(t)
	testutil.WriteFile(t, dir, "knowledge/_private/p/q/where-creds.md", "---\nid: where-creds\ntitle: Zanzibar credentials location\nsummary: s\ntype: fact\ntags: [x]\nstatus: active\ntrust: inferred\ncreated: 2020-01-01\nupdated: 2020-01-01\nsensitive: true\n---\nzanzibar\n")
	testutil.WriteFile(t, dir, "knowledge/_archive/p/q/old-zanzibar.md", "---\nid: old-zanzibar\ntitle: Old zanzibar note\nsummary: s\ntype: fact\ntags: [x]\nstatus: archived\ntrust: inferred\ncreated: 2020-01-01\nupdated: 2020-01-01\n---\nzanzibar\n")
	if _, err := ix.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	s := New(ix)
	parsed, _ := Parse("zanzibar")
	for _, tc := range []struct {
		sc   Scope
		want []string
	}{
		{Scope{Today: today}, nil},
		{Scope{Today: today, Private: true}, []string{"where-creds"}},
		{Scope{Today: today, Archived: true}, []string{"old-zanzibar"}},
	} {
		res, err := s.Search(context.Background(), Request{Query: parsed, Scope: tc.sc})
		if err != nil || !slices.Equal(ids(res.Hits), tc.want) {
			t.Errorf("%+v: %v %v", tc.sc, ids(res.Hits), err)
		}
	}
}

func TestFleet(t *testing.T) {
	t.Parallel()
	hub := t.TempDir()
	member := testutil.CopyFixture(t, "instance-basic")
	testutil.WriteFile(t, hub, ".strata/config.toml", "[federation]\nmembers = [{ name = \"api\", path = \""+member+"\" }, { name = \"gone\", path = \"/nonexistent\" }]\n")
	testutil.WriteFile(t, hub, "knowledge/hub/deploy/hub-deploy-notes.md", "---\nid: hub-deploy-notes\ntitle: Fleet deploy notes\nsummary: deploy everywhere\ntype: fact\ntags: [deploy]\nstatus: active\ntrust: inferred\ncreated: 2026-01-01\nupdated: 2026-01-01\n---\ndeploy\n")
	inst, err := config.Open(hub, "")
	if err != nil {
		t.Fatal(err)
	}
	ix, _, err := index.Open(context.Background(), inst, index.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ix.Close() }()
	parsed, _ := Parse("deploy")
	res, err := Fleet(context.Background(), inst, ix, Request{Query: parsed, Limit: 30, Scope: Scope{Today: today}})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(ids(res.Hits), ",")
	if !strings.Contains(got, "api:") || !strings.Contains(got, "hub-deploy-notes") || len(res.Suggestions) != 1 {
		t.Fatalf("fleet: %s %v", got, res.Suggestions)
	}
	if ExpandHome("~/x") == "~/x" || ExpandHome("/abs") != "/abs" {
		t.Fatal("ExpandHome")
	}
}

func TestContextAndMap(t *testing.T) {
	t.Parallel()
	_, ix := openFixture(t)
	s := New(ix)
	hits, err := s.Context(context.Background(), []string{"tests/conftest.py"}, 2)
	if err != nil || len(hits) != 2 || hits[0].ID != "test-database-per-worker" {
		t.Fatalf("context %+v %v", hits, err)
	}
	if Specificity("tests/conftest.py") <= Specificity("tests/**/conftest.py") || Specificity("a?[b]{c}") != 6 {
		t.Fatal("specificity")
	}
	nodes, err := s.Map(context.Background(), "", 1)
	if err != nil || len(nodes) != 4 || nodes[0].Children != nil {
		t.Fatalf("map %+v %v", nodes, err)
	}
}

func TestSynonymsAndEdit(t *testing.T) {
	t.Parallel()
	syn := ParseSynonyms([]byte("# c\nk8s, Kubernetes # trailing\n\n pg , postgres, postgresql\n"))
	if !slices.Equal(syn["kubernetes"], []string{"k8s"}) || len(syn["pg"]) != 2 {
		t.Fatalf("%v", syn)
	}
	if s, err := LoadSynonyms("/nonexistent"); err != nil || len(s) != 0 {
		t.Fatal("missing synonyms file")
	}
	if _, err := LoadSynonyms(t.TempDir()); err == nil {
		t.Fatal("directory as synonyms file")
	}
	if editSimilarity("fixtrue", "fixture") < 0.85 || editSimilarity("", "") != 0 || editSimilarity("a", "abcd") != 0 {
		t.Fatal("editSimilarity")
	}
	if dice(nil, nil) != 0 || similarity(nil, nil) != 0 {
		t.Fatal("degenerate similarity")
	}
	var pe *ParseError
	if _, err := Parse(`"x`); !errors.As(err, &pe) {
		t.Fatal("parse error type")
	}
}

func TestFallbackMatchesIndexedOrdering(t *testing.T) {
	t.Parallel()
	dir, ix := openFixture(t)
	inst := ix.Instance()
	s := New(ix)
	for _, q := range []string{"deploy", "rollback -helm", `"connection pool" OR pgbouncer`, "kind:script side:<=writes-local", "tag:pytest -tag:database", "updated:>2026-09-01 type:gotcha", "trust:>=verified kind:shard", "meta.owner:gardener", "path:knowledge/api/** status:any", "stale:false project:ci-flake", "domain:infra topic:k8s"} {
		parsed, err := Parse(q)
		if err != nil {
			t.Fatal(err)
		}
		req := Request{Query: parsed, Limit: 50, Scope: Scope{Today: today}}
		idx, err := s.Search(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		fb, err := Fallback(context.Background(), inst, req)
		if err != nil {
			t.Fatal(err)
		}
		a, b := ids(idx.Hits), ids(fb.Hits)
		slices.Sort(a)
		slices.Sort(b)
		// Same documents; order may differ (no BM25 statistics). Fuzzy-only
		// indexed hits are allowed to be missing from the scan.
		for _, id := range b {
			if !slices.Contains(a, id) {
				t.Errorf("%q: fallback-only hit %s (indexed %v)", q, id, a)
			}
		}
		if len(b) == 0 && len(a) > 0 {
			t.Errorf("%q: fallback found nothing, indexed %v", q, a)
		}
	}
	_ = dir
}
