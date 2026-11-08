package gates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"

	"github.com/zbmowrey/strata/engine/internal/finding"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
)

// CacheDir holds cached gate results (docs/spec/06-gates.md §8).
const CacheDir = ".strata/cache/gates"

// cacheKey hashes (gate config, argv, tool identity, config files, target
// contents). The tool's identity is its resolved path, size and mtime —
// a cheap stand-in for its version that changes whenever it is upgraded.
func (r *Runner) cacheKey(ctx context.Context, g *Gate, argv, files []string) string {
	h := sha256.New()
	gj, _ := json.Marshal(g)
	writeField(h, "gate", string(gj))
	writeField(h, "argv", fmt.Sprint(argv))
	if p, err := exec.LookPath(argv[0]); err == nil {
		if fi, err := os.Stat(p); err == nil {
			writeField(h, "tool", fmt.Sprintf("%s %d %d", p, fi.Size(), fi.ModTime().UnixNano()))
		}
	}
	for _, c := range g.ConfigFiles {
		hashFile(h, r.Inst.Root, c)
	}
	for _, f := range r.contentSet(ctx, g, files) {
		hashFile(h, r.Inst.Root, f)
	}
	return g.ID + "-" + hex.EncodeToString(h.Sum(nil))[:32]
}

// contentSet lists every file whose content the gate may read: the files
// themselves (file scope), every matched file in their directories (package
// scope), or every matched file in the repository (repo scope).
func (r *Runner) contentSet(ctx context.Context, g *Gate, files []string) []string {
	switch g.Scope {
	case ScopeRepo:
		all, err := Files(ctx, r.Inst.Root, SelectAll, nil)
		if err != nil {
			return files
		}
		return g.Select(all)
	case ScopePackage:
		dirs := map[string]bool{}
		for _, f := range files {
			dirs[path.Dir(f)] = true
		}
		var out []string
		for _, d := range sortedKeys(dirs) {
			entries, err := os.ReadDir(filepath.Join(r.Inst.Root, filepath.FromSlash(d)))
			if err != nil {
				continue
			}
			for _, e := range entries {
				if p := path.Join(d, e.Name()); e.Type().IsRegular() && g.Matches(p) {
					out = append(out, p)
				}
			}
		}
		slices.Sort(out)
		return out
	default:
		return files
	}
}

func writeField(h hash.Hash, k, v string) {
	_, _ = fmt.Fprintf(h, "%s=%d:%s\n", k, len(v), v)
}

func hashFile(h hash.Hash, root, rel string) {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) //nolint:gosec // G304: repo files selected by the gate
	if err != nil {
		writeField(h, "missing", rel)
		return
	}
	sum := sha256.Sum256(raw)
	writeField(h, "file", rel+" "+hex.EncodeToString(sum[:]))
}

func (r *Runner) cachePath(key string) string {
	return r.Inst.Path(filepath.FromSlash(CacheDir), key+".json")
}

func (r *Runner) cacheGet(key string) ([]finding.Finding, bool) {
	raw, err := os.ReadFile(r.cachePath(key))
	if err != nil {
		return nil, false
	}
	var fs []finding.Finding
	if json.Unmarshal(raw, &fs) != nil {
		return nil, false
	}
	return fs, true
}

func (r *Runner) cachePut(key string, fs []finding.Finding) {
	if fs == nil {
		fs = []finding.Finding{}
	}
	raw, err := json.Marshal(fs)
	if err == nil {
		_ = fsutil.WriteAtomic(r.cachePath(key), raw, 0o600)
	}
}
