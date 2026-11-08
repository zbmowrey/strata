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
	rootReal := realPath(root)
	base := realPath(cwd)
	if r, err := filepath.Rel(rootReal, base); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		base = rootReal // cwd outside the instance (--root): paths are root-relative
	}
	for _, p := range args {
		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(base, p)
		}
		rel, err := filepath.Rel(rootReal, realPath(abs))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fail.New(fail.Usage, "pass paths inside the instance", "path %q is outside the instance root", p)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out, nil
}

// realPath resolves symlinks in the longest existing prefix of p, so paths
// that do not exist yet still compare correctly against a resolved root.
func realPath(p string) string {
	p = filepath.Clean(p)
	var rest []string
	for cur := p; ; {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(append([]string{r}, rest...)...)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
}
