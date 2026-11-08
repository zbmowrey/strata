package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/fail"
)

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// relPaths converts user-supplied paths (relative to cwd or absolute) to
// slash paths relative to root, rejecting paths outside it.
func relPaths(root string, args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("working directory: %w", err)
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootReal = root
	}
	for _, p := range args {
		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(cwd, p)
		}
		if r, err := filepath.EvalSymlinks(abs); err == nil {
			abs = r
		}
		rel, err := filepath.Rel(rootReal, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fail.New(fail.Usage, "pass paths inside the instance", "path %q is outside the instance root", p)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out, nil
}
