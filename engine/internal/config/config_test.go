package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zbmowrey/strata/engine/internal/fail"
)

func TestDefaultsValid(t *testing.T) {
	t.Parallel()
	c := Default()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Knowledge.ReviewAfter["reference"].DaysCount() != 90 || c.Gates.EditBudget.D().Seconds() != 2 {
		t.Fatal("defaults")
	}
}

func TestParseOverridesAndWarnings(t *testing.T) {
	t.Parallel()
	cfg, warns, err := Parse([]byte(`
[knowledge]
max_body_lines = 200
review_after = { fact = "30d" }
bogus = 1
[search]
weights = { title = 20 }
[hooks]
stop_nudge = "off"
[federation]
members = [{ name = "api", path = "~/code/api" }]
[mystery]
x = 1
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Knowledge.MaxBodyLines != 200 || cfg.Knowledge.MaxBodyBytes != 6144 {
		t.Errorf("override/default mix wrong: %+v", cfg.Knowledge)
	}
	if cfg.Knowledge.ReviewAfter["fact"].DaysCount() != 30 || cfg.Hooks.StopNudge != "off" || cfg.Federation.Members[0].Name != "api" {
		t.Errorf("parsed %+v", cfg)
	}
	if len(warns) != 2 || !strings.Contains(strings.Join(warns, ","), "knowledge.bogus") {
		t.Errorf("warnings = %v", warns)
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"[knowledge\n",
		"[hooks]\nstop_nudge = \"loud\"\n",
		"[garden]\nforge = \"svn\"\n",
		"[scripts]\ndefault_runtime = \"cobol\"\n",
		"[knowledge]\nmax_body_lines = 0\n",
		"[paths]\nknowledge = \"../x\"\n",
		"[knowledge]\ncold_after = \"soon\"\n",
		"[search]\ndefault_limit = \"ten\"\n",
	} {
		if _, _, err := Parse([]byte(src)); err == nil {
			t.Errorf("Parse(%q) succeeded", src)
		}
	}
}

func TestDuration(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"180d": "180d", "2w": "2w", "14d": "2w", "36h": "36h", "90m": "90m", "5s": "5s", "never": "0", "0": "0", "86400s": "1d"} {
		d, err := ParseDuration(in)
		if err != nil || d.String() != want {
			t.Errorf("ParseDuration(%q) = %v, %v; want %s", in, d, err, want)
		}
	}
	for _, bad := range []string{"", "d", "-1d", "1y", "x1d"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("ParseDuration(%q) succeeded", bad)
		}
	}
	b, _ := Days(7).MarshalText()
	if string(b) != "1w" {
		t.Errorf("marshal %s", b)
	}
}

func TestDiscoverAndOpen(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := Open("", deep); fail.CodeOf(err) != fail.NotInstance {
		t.Fatalf("want NotInstance, got %v", err)
	}
	if _, err := Open(root, ""); fail.CodeOf(err) != fail.NotInstance {
		t.Fatalf("explicit root without config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, Dir, File), []byte("[unknown]\nk=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inst, err := Open("", deep)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(root)
	got, _ := filepath.EvalSymlinks(inst.Root)
	if got != want || len(inst.Warnings) != 1 || inst.Path("x") != filepath.Join(inst.Root, "x") {
		t.Fatalf("inst %+v", inst)
	}
	if err := os.WriteFile(filepath.Join(root, Dir, File), []byte("[hooks]\nstop_nudge = 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, ""); fail.CodeOf(err) != fail.NotInstance {
		t.Fatalf("invalid config: %v", err)
	}
}
