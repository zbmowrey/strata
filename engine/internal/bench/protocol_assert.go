package bench

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/zbmowrey/strata/engine/internal/doc"
)

// snapshot maps instance-relative slash paths to content hashes, skipping
// git internals and machine-local state.
type snapshot map[string][32]byte

var snapshotSkip = []string{".git/", ".strata/index", ".strata/session/", ".strata/telemetry/", ".strata/cache/", ".strata/bin/", ".strata/garden", ".strata/worktrees/", "scripts/.venv/"}

func takeSnapshot(dir string) (snapshot, error) {
	s := snapshot{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		for _, skip := range snapshotSkip {
			if strings.HasPrefix(rel+"/", skip) || strings.HasPrefix(rel, skip) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p) //nolint:gosec // G304: walking our own temp fixture
		if err != nil {
			return err //nolint:wrapcheck // wrapped once after WalkDir
		}
		s[rel] = sha256.Sum256(b)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	return s, nil
}

// runState is everything assertions look at.
type runState struct {
	trace  *Trace
	before snapshot
	after  snapshot
	dir    string
	gate   func() (bool, string)
}

// evaluate returns the failed assertions (empty = pass).
func (rs *runState) evaluate(as []Assertion) []string {
	var failures []string
	for _, a := range as {
		if msg := rs.check(a); msg != "" {
			failures = append(failures, msg)
		}
	}
	return failures
}

func (rs *runState) check(a Assertion) string {
	if msg, ok := rs.callCheck(a); ok {
		return msg
	}
	if msg, ok := rs.fileCheck(a); ok {
		return msg
	}
	switch {
	case a.GateClean:
		if ok, out := rs.gate(); !ok {
			return "gates not clean: " + firstLines(out, 3)
		}
		return ""
	case len(a.AnswerMentions) > 0:
		return rs.mentions(a.AnswerMentions)
	}
	return "empty assertion"
}

// callCheck evaluates the tool-call assertions; ok is false when a is not one.
func (rs *runState) callCheck(a Assertion) (msg string, ok bool) {
	t := rs.trace
	switch {
	case a.Called != "":
		if n, _ := t.count([]string{a.Called}); n == 0 {
			return "not called: " + a.Called, true
		}
	case len(a.CalledAny) > 0:
		if n, _ := t.count(a.CalledAny); n == 0 {
			return "none called: " + strings.Join(a.CalledAny, " | "), true
		}
	case a.NotCalled != "":
		if n, _ := t.count([]string{a.NotCalled}); n > 0 {
			return fmt.Sprintf("called %d× but must not be: %s", n, a.NotCalled), true
		}
	case len(a.FirstOf) > 0:
		return rs.firstOf(a.FirstOf, a.Before), true
	case a.CalledWith != nil:
		return rs.calledWith(a.CalledWith), true
	case a.MaxCalls != nil:
		if n, _ := t.count(a.MaxCalls.Tools); n > a.MaxCalls.Max {
			return fmt.Sprintf("%s called %d× > %d", strings.Join(a.MaxCalls.Tools, " | "), n, a.MaxCalls.Max), true
		}
	default:
		return "", false
	}
	return "", true
}

// fileCheck evaluates the file assertions; ok is false when a is not one.
func (rs *runState) fileCheck(a Assertion) (msg string, ok bool) {
	switch {
	case a.FileCreated != nil:
		return rs.fileCreated(a.FileCreated), true
	case a.FileChanged != "":
		return rs.fileChanged(a.FileChanged), true
	case a.FileUnchanged != "":
		return rs.fileUnchanged(a.FileUnchanged), true
	}
	return "", false
}

func (rs *runState) firstOf(first, before []string) string {
	n, i := rs.trace.count(first)
	if n == 0 {
		return "never called: " + strings.Join(first, " | ")
	}
	if len(before) == 0 {
		return ""
	}
	if m, j := rs.trace.count(before); m > 0 && j < i {
		return fmt.Sprintf("%s (call %d) came before %s (call %d)", rs.trace.Calls[j].Name, j+1, strings.Join(first, " | "), i+1)
	}
	return ""
}

func (rs *runState) calledWith(cw *CalledWith) string {
	re, err := regexp.Compile(cw.Match)
	if err != nil {
		return "bad called_with regex: " + err.Error()
	}
	p := parsePattern(cw.Tool)
	for _, c := range rs.trace.Calls {
		if !p.matches(c) {
			continue
		}
		for _, arg := range callArgs(c) {
			if re.MatchString(arg) {
				return ""
			}
		}
	}
	return fmt.Sprintf("no %s call matching /%s/", cw.Tool, cw.Match)
}

func (rs *runState) matching(glob string, s snapshot) []string {
	var out []string
	for p := range s {
		if ok, _ := doublestar.Match(glob, p); ok {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

func (rs *runState) fileCreated(fm *FileMatch) string {
	for _, p := range rs.matching(fm.Glob, rs.after) {
		if _, existed := rs.before[p]; existed {
			continue
		}
		if len(fm.Frontmatter) == 0 || rs.frontmatterMatches(p, fm.Frontmatter) {
			return ""
		}
	}
	if len(fm.Frontmatter) > 0 {
		return fmt.Sprintf("no new file %s with frontmatter %v", fm.Glob, fm.Frontmatter)
	}
	return "no new file " + fm.Glob
}

func (rs *runState) frontmatterMatches(rel string, want map[string]string) bool {
	raw, err := os.ReadFile(filepath.Join(rs.dir, filepath.FromSlash(rel))) //nolint:gosec // G304: our own temp fixture
	if err != nil {
		return false
	}
	d, err := doc.Parse(rel, raw)
	if err != nil {
		return false
	}
	for k, v := range want {
		if d.Meta.String(k) != v {
			return false
		}
	}
	return true
}

func (rs *runState) fileChanged(glob string) string {
	paths := append(rs.matching(glob, rs.before), rs.matching(glob, rs.after)...)
	for _, p := range paths {
		if rs.before[p] != rs.after[p] {
			return ""
		}
	}
	return "unchanged: " + glob
}

func (rs *runState) fileUnchanged(glob string) string {
	before := rs.matching(glob, rs.before)
	if len(before) == 0 {
		return "no file matches " + glob
	}
	for _, p := range before {
		if h, ok := rs.after[p]; !ok || h != rs.before[p] {
			return "changed: " + p
		}
	}
	return ""
}

func (rs *runState) mentions(words []string) string {
	answer := strings.ToLower(rs.trace.Answer)
	var missing []string
	for _, w := range words {
		if !strings.Contains(answer, strings.ToLower(w)) {
			missing = append(missing, w)
		}
	}
	if len(missing) > 0 {
		return "answer does not mention: " + strings.Join(missing, ", ")
	}
	return ""
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.Join(lines[:min(n, len(lines))], " / ")
}
