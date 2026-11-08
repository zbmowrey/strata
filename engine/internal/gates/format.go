package gates

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/finding"
)

// MaxReportLines caps the formatted report (docs/spec/06-gates.md §3).
const MaxReportLines = 40

// Format renders blocking findings for Claude: a header, findings grouped by
// file (files ordered by their most severe finding, then path), and the
// explain hint — at most 40 lines. It returns "" when nothing blocks.
func Format(rep *Report) string {
	if !rep.Blocked() {
		return ""
	}
	n := len(rep.Blocking)
	head := fmt.Sprintf("strata gates ✗ %d blocking finding%s (%s stage)", n, plural(n), rep.Stage)
	if rep.Baselined > 0 {
		head += fmt.Sprintf(" + %d baselined", rep.Baselined)
	}
	lines := []string{head}
	var order []string
	byFile := map[string][]finding.Finding{}
	for _, f := range rep.Blocking { // already severity-first
		if _, ok := byFile[f.File]; !ok {
			order = append(order, f.File)
		}
		byFile[f.File] = append(byFile[f.File], f)
	}
	budget := MaxReportLines - 2
	shown := 0
	for _, file := range order {
		if len(lines) >= budget {
			break
		}
		name := file
		if name == "" {
			name = "(no file)"
		}
		lines = append(lines, name)
		for _, f := range byFile[file] {
			if len(lines) >= budget {
				break
			}
			lines = append(lines, findingLine(f))
			shown++
		}
	}
	if rest := n - shown; rest > 0 {
		lines = append(lines, fmt.Sprintf("… %d more (strata check --format json)", rest))
	}
	example := rep.Blocking[0].Gate
	for _, f := range rep.Blocking {
		if f.Rule != "" {
			example = f.Gate + "/" + f.Rule
			break
		}
	}
	lines = append(lines, "Fix these, then continue. Explain a gate: strata gates explain "+example)
	return strings.Join(lines, "\n") + "\n"
}

func findingLine(f finding.Finding) string {
	pos := "-"
	switch {
	case f.Line > 0 && f.Col > 0:
		pos = fmt.Sprintf("%d:%d", f.Line, f.Col)
	case f.Line > 0:
		pos = strconv.Itoa(f.Line)
	}
	rule := f.Gate + "/" + f.Rule
	if f.Rule == "" {
		rule = f.Gate
	}
	msg := strings.ReplaceAll(strings.TrimSpace(f.Message), "\n", " ")
	if f.FixHint != "" {
		msg += " — " + f.FixHint
	}
	line := fmt.Sprintf("  %-6s %-18s %s", pos, rule, msg)
	if f.Docs != "" && !strings.Contains(f.Docs, "://") {
		line += "   [standard: " + f.Docs + "]"
	}
	return line
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// SetHash is a stable hash of a finding set, ignoring positions so that
// unrelated line shifts do not look like progress (Stop-stage loop guard).
func SetHash(fs []finding.Finding) string {
	keys := make([]string, 0, len(fs))
	for _, f := range fs {
		keys = append(keys, f.Gate+"\x00"+f.Rule+"\x00"+f.File+"\x00"+f.Message)
	}
	slices.Sort(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\x01")))
	return hex.EncodeToString(sum[:])
}
