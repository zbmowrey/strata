//go:build !darwin

package corpus

import (
	"os"
	"path/filepath"
)

func readDirFile(f *os.File) ([]entry, error) { return readDirPortable(f) }

func openChild(parent *os.File, name string) (*os.File, error) {
	return os.Open(filepath.Join(parent.Name(), name)) //nolint:gosec,wrapcheck // G304: walking the instance; wrapped by the walker
}
