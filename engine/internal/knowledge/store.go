package knowledge

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/corpus"
	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/schema"
	"github.com/zbmowrey/strata/engine/internal/telemetry"
)

// LockFile serializes knowledge write paths that touch many files.
const LockFile = ".strata/knowledge.lock"

// Store performs knowledge writes for one instance.
type Store struct {
	inst   *config.Instance
	now    func() time.Time
	tel    *telemetry.Writer
	layout corpus.Layout
}

// New returns a Store; now and tel may be nil.
func New(inst *config.Instance, now func() time.Time, tel *telemetry.Writer) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{inst: inst, now: now, tel: tel, layout: corpus.LayoutOf(inst.Config.Paths)}
}

func (s *Store) today() string { return s.now().Format(time.DateOnly) }

func (s *Store) abs(rel string) string { return filepath.Join(s.inst.Root, filepath.FromSlash(rel)) }

func (s *Store) lock() (*fsutil.Lock, error) {
	l, err := fsutil.LockWait(s.inst.Path(LockFile), 10*time.Second)
	if err != nil {
		return nil, fail.Wrap(fail.Locked, "another strata write is in progress; retry", err)
	}
	return l, nil
}

// files walks the corpus.
func (s *Store) files() ([]corpus.File, error) {
	fs, err := corpus.Walk(s.inst.Root, s.layout)
	if err != nil {
		return nil, fmt.Errorf("walk corpus: %w", err)
	}
	return fs, nil
}

// ErrNotFound is returned when an id does not name a shard.
var ErrNotFound = errors.New("not found")

// findShard locates a shard file (active, archived or private) by id.
func (s *Store) findShard(id string) (corpus.File, error) {
	fs, err := s.files()
	if err != nil {
		return corpus.File{}, err
	}
	for _, f := range fs {
		if f.Kind == corpus.Shard && strings.TrimSuffix(path.Base(f.Rel), ".md") == id {
			return f, nil
		}
	}
	return corpus.File{}, fail.Wrap(fail.Partial, "check the id with `strata search`", fmt.Errorf("shard %q: %w", id, ErrNotFound))
}

// idTaken reports whether id is used by any shard, hub or question.
func (s *Store) idTaken(id string) (string, bool, error) {
	fs, err := s.files()
	if err != nil {
		return "", false, err
	}
	for _, f := range fs {
		if (f.Kind == corpus.Shard || f.Kind == corpus.Hub || f.Kind == corpus.Question) && strings.TrimSuffix(path.Base(f.Rel), ".md") == id {
			return f.Rel, true, nil
		}
	}
	return "", false, nil
}

func validID(id string) error {
	if !schema.IDPattern.MatchString(id) || len(id) < 3 || len(id) > 64 {
		return fail.New(fail.Usage, "ids are lowercase kebab-case, 3–64 chars, e.g. pytest-fixture-teardown", "invalid id %q", id)
	}
	return nil
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify lowercases text into a hyphenated slug of about maxLen bytes. A long slug is
// cut at the word break nearest maxLen, before or after it, so no word is split.
func Slugify(text string, maxLen int) string {
	s := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(text), "-"), "-")
	if len(s) > maxLen {
		s = s[:wordBreak(s, maxLen)]
	}
	if s == "" {
		s = "note"
	}
	return s
}

// readFile reads an instance-relative file.
func (s *Store) readFile(rel string) ([]byte, error) {
	b, err := os.ReadFile(s.abs(rel)) //nolint:gosec // G304: paths come from the corpus walk inside the root
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	return b, nil
}

// writeFile writes an instance-relative file atomically.
func (s *Store) writeFile(rel string, b []byte) error {
	return fsutil.WriteAtomic(s.abs(rel), b, 0o644)
}

// edit applies fn to a file's bytes and writes the result if it changed.
func (s *Store) edit(rel string, fn func([]byte) ([]byte, error)) (bool, error) {
	raw, err := s.readFile(rel)
	if err != nil {
		return false, err
	}
	out, err := fn(raw)
	if err != nil {
		return false, fmt.Errorf("%s: %w", rel, err)
	}
	if bytes.Equal(out, raw) {
		return false, nil
	}
	return true, s.writeFile(rel, out)
}

// setKeys sets several frontmatter keys in order.
func setKeys(raw []byte, kv ...[2]string) ([]byte, error) {
	var err error
	for _, p := range kv {
		if raw, err = SetKey(raw, p[0], p[1]); err != nil {
			return nil, err
		}
	}
	return raw, nil
}

// metaOf parses frontmatter.
func metaOf(raw []byte) (*doc.Meta, error) {
	d, err := doc.Parse("", raw)
	if err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	return d.Meta, nil
}

// appendUnique appends v to list unless present.
func appendUnique(list []string, v string) []string {
	if slices.Contains(list, v) {
		return list
	}
	return append(list, v)
}

// wordBreak returns the index of the hyphen in s nearest to maxLen, preferring the
// earlier one on a tie. With no hyphen it falls back to a hard cut at maxLen.
func wordBreak(s string, maxLen int) int {
	back := strings.LastIndexByte(s[:maxLen+1], '-')
	fwd := -1
	if i := strings.IndexByte(s[maxLen:], '-'); i >= 0 {
		fwd = maxLen + i
	}
	switch {
	case back > 0 && (fwd < 0 || maxLen-back <= fwd-maxLen):
		return back
	case fwd > 0:
		return fwd
	default:
		return maxLen
	}
}
