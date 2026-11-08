package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

type recorder struct {
	testing.TB
	failed bool
}

func (r *recorder) Helper()               {}
func (r *recorder) Errorf(string, ...any) { r.failed = true }
func (r *recorder) Fatalf(string, ...any) { r.failed = true }
func (r *recorder) Fatal(...any)          { r.failed = true }

func TestGolden(t *testing.T) { //nolint:paralleltest // toggles the package-level update flag
	p := filepath.Join(t.TempDir(), "sub", "x.golden")
	*update = true
	Golden(t, p, []byte("hello"))
	*update = false
	Golden(t, p, []byte("hello"))
	r := &recorder{TB: t}
	Golden(r, p, []byte("other"))
	if !r.failed {
		t.Fatal("mismatch not reported")
	}
	r = &recorder{TB: t}
	Golden(r, filepath.Join(t.TempDir(), "missing"), nil)
	if !r.failed {
		t.Fatal("missing golden not reported")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}
