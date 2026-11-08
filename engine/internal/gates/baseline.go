package gates

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/finding"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
)

// BaselineEntry is one accepted pre-existing finding.
type BaselineEntry struct {
	Rule        string `json:"rule"`
	File        string `json:"file"`
	Fingerprint string `json:"fingerprint"`
}

// Baseline is a ratchet file (docs/spec/06-gates.md §5).
type Baseline struct {
	Gate    string          `json:"gate"`
	Entries []BaselineEntry `json:"entries"`
}

// LoadBaseline reads a baseline (an empty one when the file is absent).
func LoadBaseline(path, gate string) (*Baseline, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: baseline path from the instance's gate config
	if errors.Is(err, os.ErrNotExist) {
		return &Baseline{Gate: gate}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read baseline: %w", err)
	}
	var b Baseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("baseline %s: %w", path, err)
	}
	return &b, nil
}

// Save writes the baseline deterministically (sorted entries).
func (b *Baseline) Save(path string) error {
	slices.SortFunc(b.Entries, func(x, y BaselineEntry) int {
		return cmp.Or(cmp.Compare(x.File, y.File), cmp.Compare(x.Rule, y.Rule), cmp.Compare(x.Fingerprint, y.Fingerprint))
	})
	if b.Entries == nil {
		b.Entries = []BaselineEntry{}
	}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("baseline: %w", err)
	}
	return fsutil.WriteAtomic(path, append(raw, '\n'), 0o644)
}

var symbolRE = regexp.MustCompile(`^\s*(?:func|def|class|fn|function|async function|pub fn|pub(?:\(crate\))? fn|impl|interface|type|export (?:async )?function|export class)\b[^\n]*`)

// sourceContext returns the trimmed source line and the nearest enclosing
// symbol line above it (best effort) for a 1-based line.
func sourceContext(lines []string, line int) (src, symbol string) {
	if line < 1 || line > len(lines) {
		return "", ""
	}
	src = strings.Join(strings.Fields(lines[line-1]), " ")
	for i := line - 1; i >= 0; i-- {
		if symbolRE.MatchString(lines[i]) {
			symbol = strings.Join(strings.Fields(lines[i]), " ")
			break
		}
	}
	return src, symbol
}

// Fingerprinter computes stable finding fingerprints, caching file lines.
type Fingerprinter struct {
	files map[string][]string
	root  string
}

// NewFingerprinter reads sources relative to root.
func NewFingerprinter(root string) *Fingerprinter {
	return &Fingerprinter{root: root, files: map[string][]string{}}
}

// Of returns hash(rule + normalized source line + enclosing symbol): stable
// under unrelated line shifts. Without a readable line the message stands in.
func (fp *Fingerprinter) Of(f *finding.Finding) string {
	lines, ok := fp.files[f.File]
	if !ok {
		raw, err := os.ReadFile(filepath.Join(fp.root, filepath.FromSlash(f.File))) //nolint:gosec // G304: finding paths are repo-relative
		if err == nil {
			lines = strings.Split(string(raw), "\n")
		}
		fp.files[f.File] = lines
	}
	src, sym := sourceContext(lines, f.Line)
	if src == "" {
		src = f.Message
	}
	h := sha256.Sum256([]byte(f.Rule + "\x00" + src + "\x00" + sym))
	return hex.EncodeToString(h[:8])
}

// apply splits findings into new and baselined (matching as a multiset, so
// N baselined copies absorb at most N findings) and returns the entries that
// no longer match anything among the covered files (fixed findings).
func (b *Baseline) apply(fs []finding.Finding, fp *Fingerprinter, covered func(file string) bool) (fresh []finding.Finding, baselined int, stale []BaselineEntry) {
	pool := map[BaselineEntry]int{}
	for _, e := range b.Entries {
		pool[e]++
	}
	for i := range fs {
		e := BaselineEntry{Rule: fs[i].Rule, File: fs[i].File, Fingerprint: fp.Of(&fs[i])}
		if pool[e] > 0 {
			pool[e]--
			baselined++
			continue
		}
		fresh = append(fresh, fs[i])
	}
	for _, e := range b.Entries {
		if pool[e] > 0 && covered(e.File) {
			pool[e]--
			stale = append(stale, e)
		}
	}
	return fresh, baselined, stale
}

// shrink removes stale entries; it reports whether anything changed.
func (b *Baseline) shrink(stale []BaselineEntry) bool {
	if len(stale) == 0 {
		return false
	}
	drop := map[BaselineEntry]int{}
	for _, e := range stale {
		drop[e]++
	}
	kept := b.Entries[:0]
	for _, e := range b.Entries {
		if drop[e] > 0 {
			drop[e]--
			continue
		}
		kept = append(kept, e)
	}
	b.Entries = kept
	return true
}
