package scaffold_test

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/zbmowrey/strata/engine/internal/gates"
	"github.com/zbmowrey/strata/engine/internal/scaffold"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func TestDetectProfiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.WriteFile(t, dir, "engine/go.mod", "module x\n")
	testutil.WriteFile(t, dir, "web/app/tsconfig.json", "{}")
	testutil.WriteFile(t, dir, "scripts/run.sh", "echo\n")
	testutil.WriteFile(t, dir, "a/b/c/Cargo.toml", "") // too deep
	testutil.WriteFile(t, dir, "node_modules/x/pyproject.toml", "")
	testutil.WriteFile(t, dir, ".hidden/requirements.txt", "")
	got := scaffold.DetectProfiles(dir)
	if want := []string{"go", "ts", "shell", "docs", "security"}; !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if got := scaffold.DetectProfiles(t.TempDir()); !slices.Equal(got, []string{"docs", "security"}) {
		t.Fatalf("empty: %v", got)
	}
}

// Every profile template must parse as a valid gate set, and all of them
// together must not collide on gate ids.
func TestRenderGatesParses(t *testing.T) {
	t.Parallel()
	for _, p := range scaffold.Profiles {
		raw, err := scaffold.RenderGates([]string{p})
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := gates.Parse(raw)
		if err != nil {
			t.Fatalf("%s: %v\n%s", p, err, raw)
		}
		if len(cfg.Gates) == 0 {
			t.Errorf("%s: no gates", p)
		}
	}
	raw, err := scaffold.RenderGates(scaffold.Profiles)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := gates.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"go-lint", "suppressions", "strata-lint", "secrets"} {
		if _, ok := cfg.Get(id); !ok {
			t.Errorf("missing gate %s", id)
		}
	}
	if !strings.Contains(string(raw), "profiles: go, python") {
		t.Error("header lists profiles")
	}
	if _, err := scaffold.RenderGates([]string{"cobol"}); !errors.Is(err, scaffold.ErrUnknownProfile) {
		t.Fatalf("unknown profile: %v", err)
	}
}

func TestToolPins(t *testing.T) {
	t.Parallel()
	pins, err := scaffold.ToolPins([]string{"go"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pins["golangci-lint"]; !ok || len(pins) == 0 {
		t.Fatalf("go pins %v", pins)
	}
	for name, p := range pins {
		if p.Profile != "go" || p.Version == "" || p.Install == "" || len(p.Check) == 0 {
			t.Errorf("%s: incomplete pin %+v", name, p)
		}
	}
	raw, err := scaffold.RenderTools(scaffold.Profiles)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Tools map[string]scaffold.ToolPin `toml:"tools"`
	}
	if err := toml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("tools.toml does not parse: %v\n%s", err, raw)
	}
	all, _ := scaffold.ToolPins(scaffold.Profiles)
	if len(doc.Tools) != len(all) {
		t.Fatalf("rendered %d tools, want %d", len(doc.Tools), len(all))
	}
	again, _ := scaffold.RenderTools(scaffold.Profiles)
	if !bytes.Equal(again, raw) {
		t.Fatal("RenderTools not deterministic")
	}
}
