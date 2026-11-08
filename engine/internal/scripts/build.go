package scripts

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/zeebo/xxh3"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
)

// BinDir is the compiled-script cache, relative to the instance root.
const BinDir = ".strata/bin"

// BinPath returns the cached binary path of a Go script.
func BinPath(root, id string) string { return filepath.Join(root, filepath.FromSlash(BinDir), id) }

// scriptsDir is the absolute scripts root of the instance.
func scriptsDir(inst *config.Instance) string {
	return filepath.Join(inst.Root, filepath.FromSlash(inst.Config.Paths.Scripts))
}

// SourceHash hashes everything a Go script's binary depends on: the script
// directory, scripts/lib/go, and scripts/go.mod + go.sum (docs/spec/04 §7).
func SourceHash(inst *config.Instance, s *Script) (string, error) {
	sd := scriptsDir(inst)
	h := xxh3.New()
	add := func(p string) error {
		b, err := os.ReadFile(p) //nolint:gosec // G304: files under the instance's scripts tree
		if err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		rel, _ := filepath.Rel(sd, p)
		_, _ = h.WriteString(filepath.ToSlash(rel) + "\x00")
		_, _ = h.Write(b)
		_, _ = h.WriteString("\x00")
		return nil
	}
	for _, root := range []string{s.AbsDir(inst.Root), filepath.Join(sd, "lib", "go")} {
		files, err := treeFiles(root)
		if err != nil {
			return "", fmt.Errorf("hash %s: %w", s.ID, err)
		}
		for _, f := range files {
			if err := add(f); err != nil {
				return "", fmt.Errorf("hash %s: %w", s.ID, err)
			}
		}
	}
	for _, f := range []string{"go.mod", "go.sum"} {
		if err := add(filepath.Join(sd, f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("hash %s: %w", s.ID, err)
		}
	}
	sum := h.Sum128().Bytes()
	return hex.EncodeToString(sum[:]), nil
}

// treeFiles lists regular files under root, sorted (missing root = none).
func treeFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() && d.Name() != "." && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by the caller
	}
	slices.Sort(out)
	return out, nil
}

// BuildResult describes one build.
type BuildResult struct {
	ID     string
	Binary string
	Built  bool // false = cache hit
}

// Build compiles a Go script into the cache unless its recorded source hash
// is current. force rebuilds regardless.
func Build(ctx context.Context, inst *config.Instance, s *Script, force bool) (BuildResult, error) {
	res := BuildResult{ID: s.ID, Binary: BinPath(inst.Root, s.ID)}
	if s.Runtime != RuntimeGo {
		return res, fail.New(fail.Usage, "only Go scripts are compiled; other runtimes run directly", "script %s has runtime %q", s.ID, s.Runtime)
	}
	hash, err := SourceHash(inst, s)
	if err != nil {
		return res, err
	}
	stamp := res.Binary + ".hash"
	if !force && cacheCurrent(res.Binary, stamp, hash) {
		return res, nil
	}
	lock, err := fsutil.LockWait(res.Binary+".lock", 2*time.Minute)
	if err != nil {
		return res, fmt.Errorf("build %s: %w", s.ID, err)
	}
	defer func() { _ = lock.Unlock() }()
	if !force && cacheCurrent(res.Binary, stamp, hash) { // built by another process meanwhile
		return res, nil
	}
	if err := goBuild(ctx, inst, s, res.Binary); err != nil {
		return res, err
	}
	if err := fsutil.WriteAtomic(stamp, []byte(hash+"\n"), 0o600); err != nil {
		return res, err
	}
	res.Built = true
	return res, nil
}

func cacheCurrent(bin, stamp, hash string) bool {
	if _, err := os.Stat(bin); err != nil {
		return false
	}
	b, err := os.ReadFile(stamp) //nolint:gosec // G304: engine-owned cache file
	return err == nil && strings.TrimSpace(string(b)) == hash
}

// goBuild runs `go build` for the script package inside the scripts module.
func goBuild(ctx context.Context, inst *config.Instance, s *Script, out string) error {
	if _, err := exec.LookPath("go"); err != nil {
		return fail.New(fail.Runtime, "install Go (https://go.dev/dl/)", "go toolchain not found on PATH")
	}
	sd := scriptsDir(inst)
	rel, err := filepath.Rel(sd, s.AbsDir(inst.Root))
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("script %s is outside %s", s.ID, sd)
	}
	tmp := out + ".tmp"
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", tmp, "./"+filepath.ToSlash(rel)) //nolint:gosec // G204: fixed argv; rel is the script's own directory
	cmd.Dir = sd
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
		return fmt.Errorf("build %s: %w", s.ID, err)
	}
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		return fail.New(fail.Runtime, "fix the compile errors above, then retry", "build %s failed: %v\n%s", s.ID, err, strings.TrimSpace(stderr.String()))
	}
	if err := os.Rename(tmp, out); err != nil {
		return fmt.Errorf("build %s: %w", s.ID, err)
	}
	return nil
}
