package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "a", "b", "f.txt")
	if err := WriteAtomic(p, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(p, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	fi, _ := os.Stat(p)
	if string(b) != "two" || fi.Mode().Perm() != 0o640 {
		t.Fatalf("content %q perm %v", b, fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
	// Writing into a path whose parent is a file fails cleanly.
	if err := WriteAtomic(filepath.Join(p, "child"), nil, 0o600); err == nil {
		t.Fatal("want error")
	}
	if err := os.Mkdir(filepath.Join(dir, "target-dir"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(filepath.Join(dir, "target-dir"), []byte("x"), 0o600); err == nil {
		t.Fatal("renaming over a directory should fail")
	}
}

func TestWriteAtomicReadOnlyDir(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := WriteAtomic(filepath.Join(dir, "f"), []byte("x"), 0o600); err == nil {
		t.Fatal("want error in read-only dir")
	}
}
