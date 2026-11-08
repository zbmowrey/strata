package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFixtureHelpers(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat(filepath.Join(EngineDir(), "go.mod")); err != nil {
		t.Fatalf("EngineDir: %v", err)
	}
	dir := CopyFixture(t, "instance-basic")
	if _, err := os.Stat(filepath.Join(dir, ".strata", "config.toml")); err != nil {
		t.Fatal(err)
	}
	p := WriteFile(t, dir, "a/b/c.txt", "hi")
	if b, _ := os.ReadFile(p); string(b) != "hi" {
		t.Fatal("WriteFile")
	}
	if err := CopyDir("/nonexistent-dir", t.TempDir()); err == nil {
		t.Fatal("want error")
	}
}
