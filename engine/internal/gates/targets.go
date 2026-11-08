package gates

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/fail"
)

// Selection names which files a check covers.
type Selection int

// Selections (docs/spec/06-gates.md §8).
const (
	SelectChanged Selection = iota // working tree vs HEAD + untracked, not ignored
	SelectStaged                   // the index
	SelectAll                      // every tracked or untracked-not-ignored file
	SelectPaths                    // explicit paths
)

// DefaultStage is the stage implied by a selection when --stage is absent.
func DefaultStage(sel Selection) string {
	switch sel {
	case SelectStaged:
		return StageCommit
	case SelectAll:
		return StageCI
	default:
		return StageStop
	}
}

// git runs git in root and returns NUL-separated paths.
func git(ctx context.Context, root string, args ...string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...) //nolint:gosec // G204: fixed git subcommands
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	var paths []string
	for _, p := range bytes.Split(out, []byte{0}) {
		if len(p) > 0 {
			paths = append(paths, string(p))
		}
	}
	return paths, nil
}

// Files resolves a selection to existing, repo-relative slash paths, sorted.
func Files(ctx context.Context, root string, sel Selection, explicit []string) ([]string, error) {
	var (
		paths []string
		err   error
	)
	switch sel {
	case SelectPaths:
		paths, err = expandPaths(root, explicit)
	case SelectStaged:
		paths, err = git(ctx, root, "diff", "--cached", "--name-only", "-z", "--diff-filter=ACMR")
	case SelectAll:
		paths, err = git(ctx, root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	default:
		paths, err = changed(ctx, root)
	}
	if err != nil {
		return nil, fail.Wrap(fail.Runtime, "run inside a git repository, or pass explicit paths", err)
	}
	return existing(root, paths), nil
}

func changed(ctx context.Context, root string) ([]string, error) {
	untracked, err := git(ctx, root, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	diff, err := git(ctx, root, "diff", "HEAD", "--name-only", "-z", "--diff-filter=ACMR")
	if err != nil {
		// No commits yet: everything in the index is new.
		if diff, err = git(ctx, root, "ls-files", "-z", "--cached"); err != nil {
			return nil, err
		}
	}
	return append(diff, untracked...), nil
}

// expandPaths turns explicit files/directories into files (directories are
// walked, skipping .git and dot-directories).
func expandPaths(root string, args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		abs := filepath.Join(root, filepath.FromSlash(a))
		fi, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("check %s: %w", a, err)
		}
		if !fi.IsDir() {
			out = append(out, filepath.ToSlash(a))
			continue
		}
		files, err := walkFiles(root, abs)
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", a, err)
		}
		out = append(out, files...)
	}
	return out, nil
}

// walkFiles lists regular files under dir (root-relative), skipping
// dot-directories.
func walkFiles(root, dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && p != dir && strings.HasPrefix(d.Name(), "."):
			return filepath.SkipDir
		case d.Type().IsRegular():
			if rel, rerr := filepath.Rel(root, p); rerr == nil {
				out = append(out, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}
	return out, nil
}

func existing(root string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = path.Clean(filepath.ToSlash(p))
		if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil && fi.Mode().IsRegular() {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Targets expands a gate's {targets} for the matched files according to its
// scope: file → the files; package → their directories ("./dir"); repo →
// repo_target. Paths are relative to the gate's workdir.
func (g *Gate) Targets(files []string) []string {
	switch g.Scope {
	case ScopeRepo:
		return []string{g.RepoTarget}
	case ScopePackage:
		var dirs []string
		for _, f := range files {
			d := path.Dir(g.rel(f))
			if d == "." {
				dirs = append(dirs, ".")
			} else {
				dirs = append(dirs, "./"+d)
			}
		}
		slices.Sort(dirs)
		return slices.Compact(dirs)
	default:
		out := make([]string, len(files))
		for i, f := range files {
			out[i] = g.rel(f)
		}
		return out
	}
}

// rel makes a repo-relative path relative to the gate's workdir.
func (g *Gate) rel(p string) string {
	if g.Workdir == "" {
		return p
	}
	return strings.TrimPrefix(p, g.Workdir+"/")
}

// Argv expands {targets} in cmd (as separate arguments).
func Argv(cmd, targets []string) []string {
	out := make([]string, 0, len(cmd)+len(targets))
	for _, a := range cmd {
		if a == "{targets}" {
			out = append(out, targets...)
			continue
		}
		out = append(out, a)
	}
	return out
}

// Select returns the gate's matched files from files.
func (g *Gate) Select(files []string) []string {
	var out []string
	for _, f := range files {
		if g.Matches(f) {
			out = append(out, f)
		}
	}
	return out
}
