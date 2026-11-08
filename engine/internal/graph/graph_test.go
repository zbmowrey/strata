package graph

import (
	"context"
	"slices"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/index"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func open(t *testing.T) *Graph {
	t.Helper()
	dir := testutil.CopyFixture(t, "instance-basic")
	testutil.WriteFile(t, dir, "knowledge/x/y/via-alias.md", "---\nid: via-alias\ntitle: T\nsummary: s\ntype: fact\ntags: [unique-tag]\nstatus: active\ntrust: inferred\ncreated: 2026-01-01\nupdated: 2026-01-01\n---\nSee [[pg-pool]] and [[via-alias]].\n")
	inst, err := config.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	ix, _, err := index.Open(context.Background(), inst, index.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ix.Close() })
	return New(ix.DB())
}

func TestEdges(t *testing.T) {
	t.Parallel()
	g := open(t)
	ctx := context.Background()
	out, err := g.Out(ctx, "pytest-fixture-teardown")
	if err != nil {
		t.Fatal(err)
	}
	var to []string
	for _, e := range out {
		to = append(to, e.To)
	}
	if !slices.Equal(to, []string{"postgres-connection-pool", "debug-hanging-tests", "find-leaked-fixtures"}) {
		t.Fatalf("out = %v", to)
	}
	in, err := g.In(ctx, "postgres-connection-pool")
	if err != nil {
		t.Fatal(err)
	}
	var from []string
	for _, e := range in {
		from = append(from, e.From)
	}
	if !slices.Contains(from, "via-alias") || !slices.Contains(from, "pytest-fixture-teardown") {
		t.Fatalf("in = %v (alias links must count)", from)
	}
	o, i, err := g.Counts(ctx, "via-alias")
	if err != nil || o != 1 || i != 0 {
		t.Fatalf("self-links excluded: %d %d %v", o, i, err)
	}
}

func TestNeighborhoodAndRelated(t *testing.T) {
	t.Parallel()
	g := open(t)
	ctx := context.Background()
	n1, err := g.Neighborhood(ctx, []string{"deploy-flow"}, 1)
	if err != nil || len(n1) == 0 || slices.Contains(n1, "deploy-flow") {
		t.Fatalf("n1 = %v %v", n1, err)
	}
	n2, err := g.Neighborhood(ctx, []string{"deploy-flow"}, 2)
	if err != nil || len(n2) <= len(n1) || !slices.Equal(n2[:len(n1)], n1) {
		t.Fatalf("n2 = %v (n1 %v) %v", n2, n1, err)
	}
	rel, err := g.Related(ctx, "deploy-flow", 2, 3)
	if err != nil || len(rel) != 3 || rel[0].Score < rel[2].Score {
		t.Fatalf("related %+v %v", rel, err)
	}
	all, err := g.Related(ctx, "via-alias", 1, 0)
	if err != nil || len(all) != 1 || all[0].Relation != "→" {
		t.Fatalf("related unique %+v %v", all, err)
	}
}
