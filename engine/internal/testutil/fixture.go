// Package testutil holds shared test helpers: golden files (run tests with
// -update to rewrite them) and fixture instances copied to temp dirs.
package testutil

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// EngineDir returns the absolute path of the engine module root.
func EngineDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// Fixture returns the absolute path of engine/testdata/<name>.
func Fixture(name string) string { return filepath.Join(EngineDir(), "testdata", name) }

// CopyFixture copies engine/testdata/<name> into a fresh temp dir and
// returns its path, so tests may write (index, captures) freely.
func CopyFixture(tb testing.TB, name string) string {
	tb.Helper()
	dst := tb.TempDir()
	if err := CopyDir(Fixture(name), dst); err != nil {
		tb.Fatal(err)
	}
	return dst
}

// CopyDir copies a directory tree (regular files and directories only).
func CopyDir(src, dst string) error {
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err //nolint:wrapcheck // wrapped once after WalkDir
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p) //nolint:gosec // G304: copying test fixtures
		if err != nil {
			return err //nolint:wrapcheck // wrapped once after WalkDir
		}
		return os.WriteFile(target, b, 0o600) //nolint:gosec // G703: test fixture copy into a temp dir
	})
	if err != nil {
		return fmt.Errorf("copy %s: %w", src, err)
	}
	return nil
}

// WriteFile writes content to dir/rel, creating parents.
func WriteFile(tb testing.TB, dir, rel, content string) string {
	tb.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		tb.Fatal(err)
	}
	return p
}
