package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

// fixtureRoot is a per-package copy of instance-basic shared by read-only tests.
var fixtureRoot string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "strata-cli-*")
	if err != nil {
		panic(err)
	}
	if err := testutil.CopyDir(testutil.Fixture("instance-basic"), dir); err != nil {
		panic(err)
	}
	fixtureRoot = dir
	_ = os.Setenv(NowEnv, "2026-11-07")
	_ = os.Setenv("COLUMNS", "120")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestRetrievalGolden(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []string
		code int
	}{
		{"search-hang", []string{"search", "tests hang at exit"}, 0},
		{"search-teardown", []string{"search", "fixture teardown"}, 0},
		{"search-teardown-explain", []string{"search", "--explain", "fixture teardown", "--limit", "3"}, 0},
		{"search-teardown-table", []string{"--format", "table", "search", "fixture teardown", "--limit", "3"}, 0},
		{"search-teardown-json", []string{"--format", "json", "search", "fixture teardown", "--limit", "3"}, 0},
		{"search-teardown-jsonl", []string{"--format", "jsonl", "search", "fixture teardown", "--limit", "3"}, 0},
		{"search-teardown-md", []string{"--format", "md", "search", "fixture teardown", "--limit", "3"}, 0},
		{"search-filters", []string{"search", "deploy kind:script side:<=writes-local"}, 0},
		{"search-synonym", []string{"search", "kubernetes limits"}, 0},
		{"search-typo", []string{"search", "fixtrue"}, 0},
		{"search-exact-alias", []string{"search", "pg-pool"}, 0},
		{"search-filter-only", []string{"search", "type:decision", "trust:>=verified", "--limit", "4"}, 0},
		{"search-stale", []string{"search", "stale:true"}, 0},
		{"search-negation", []string{"search", "rollback -helm"}, 0},
		{"search-or-phrase", []string{"search", `"cursor pagination" OR versioning kind:shard`}, 0},
		{"search-zero", []string{"search", "fixtur zzzz tag:nosuchtag"}, 0},
		{"search-superseded", []string{"search", "sunset header"}, 0},
		{"search-project", []string{"search", "teardown project:ci-flake"}, 0},
		{"search-dates", []string{"search", "updated:2026-10-20..2026-10-25"}, 0},
		{"search-meta", []string{"search", "meta.owner:gardener"}, 0},
		{"show-multi", []string{"show", "pytest-fixture-teardown", "find-leaked-fixtures", "ci-flake:log#2026-10-21-10-00", "pytest-fixture-teardown"}, 0},
		{"show-section-links", []string{"show", "pytest-fixture-teardown#fix", "--links", "1"}, 0},
		{"show-budget", []string{"show", "pytest-fixture-teardown", "--links", "2", "--budget", "100"}, 0},
		{"show-missing", []string{"show", "deploy-flow", "nope", "deploy-flow#nowhere"}, fail.Partial},
		{"show-json", []string{"--format", "json", "show", "coverage-floor", "--meta"}, 0},
		{"show-brief", []string{"show", "xdist", "--brief"}, 0},
		{"show-body", []string{"show", "alert-routing", "--body"}, 0},
		{"show-proposed", []string{"show", "soft-delete-pattern"}, 0},
		{"related", []string{"related", "pytest-fixture-teardown", "--limit", "5"}, 0},
		{"related-depth2-json", []string{"--format", "json", "related", "deploy-flow", "--depth", "2", "--limit", "4"}, 0},
		{"context", []string{"context", "tests/conftest.py", "app/db.py"}, 0},
		{"context-json", []string{"--format", "json", "context", "deploy/helm/templates/x.yaml"}, 0},
		{"map", []string{"map"}, 0},
		{"map-depth3", []string{"map", "infra", "--depth", "3"}, 0},
		{"map-json", []string{"--format", "json", "map", "--depth", "1"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, errOut, code := run(t, append([]string{"--root", fixtureRoot}, tc.args...)...)
			if code != tc.code {
				t.Fatalf("code=%d want %d stderr=%s", code, tc.code, errOut)
			}
			out = strings.ReplaceAll(out, fixtureRoot, "<root>")
			out = msRE(out)
			testutil.Golden(t, filepath.Join("testdata", "golden", tc.name+".golden"), []byte(out))
		})
	}
}

// msRE blanks the timing field of JSON search output.
func msRE(s string) string {
	var b bytes.Buffer
	for _, line := range strings.SplitAfter(s, "\n") {
		if i := strings.Index(line, `"ms":`); i >= 0 {
			j := i + 5
			for j < len(line) && line[j] >= '0' && line[j] <= '9' {
				j++
			}
			line = line[:i+5] + "0" + line[j:]
		}
		b.WriteString(line)
	}
	return b.String()
}

func TestRetrievalErrors(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"search"},
		{"search", `"open`},
		{"search", "x", "--limit", "500"},
		{"search", "kind:nope"},
		{"search", "trust:>=godlike"},
		{"search", "updated:yesterday"},
		{"search", "stale:maybe"},
		{"search", "-kind:shard x"},
		{"search", "path:[", "x"},
		{"search", "x", "--scope", "galaxy"},
		{"search", "x", "--meta", "novalue"},
		{"--format", "csv", "search", "x"},
		{"show"},
		{"show", "x", "--links", "3"},
		{"show", "x", "--meta", "--brief"},
		{"related", "x", "--depth", "3"},
		{"map", "--depth", "4"},
	} {
		_, errOut, code := run(t, append([]string{"--root", fixtureRoot}, args...)...)
		if code != fail.Usage {
			t.Errorf("%v: code=%d stderr=%s", args, code, errOut)
		}
	}
	if _, _, code := run(t, "--root", fixtureRoot, "related", "nope"); code != fail.Partial {
		t.Errorf("related missing: %d", code)
	}
	if _, _, code := run(t, "--root", fixtureRoot, "map", "nope"); code != fail.Partial {
		t.Errorf("map missing domain: %d", code)
	}
}

func TestSearchFallbackWhenIndexBroken(t *testing.T) {
	t.Parallel()
	dir := testutil.CopyFixture(t, "instance-basic")
	// A directory where the database file should be makes SQLite unusable
	// and prevents the rebuild from deleting it.
	testutil.WriteFile(t, dir, ".strata/index.db/blocker", "x")
	out, errOut, code := run(t, "--root", dir, "search", "fixture teardown", "--limit", "3")
	if code != 0 || !strings.Contains(errOut, "falling back to a full scan") || !strings.Contains(out, "pytest-fixture-teardown") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}
