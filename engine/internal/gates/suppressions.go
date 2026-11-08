package gates

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/zbmowrey/strata/engine/internal/finding"
)

// MinReason is the minimum length of a suppression's reason (§7).
const MinReason = 15

// Suppression rules.
const (
	RuleSuppBare    = "bare"
	RuleSuppReason  = "reason"
	RuleSuppBlanket = "blanket"
)

type suppMatcher struct {
	re    *regexp.Regexp
	check func(m []string, line string) (rule, msg string)
	name  string
	// quotes counted before the match to detect markers inside strings.
	quotes string
}

// reasonOf strips comment leaders/separators from the text after a
// suppression and returns the remaining reason.
func reasonOf(rest string) string {
	r := strings.TrimSpace(rest)
	r = strings.TrimSuffix(r, "*/")
	for {
		t := strings.TrimSpace(r)
		next := strings.TrimLeft(t, "/#-:;,")
		if next == t {
			return strings.TrimSpace(t)
		}
		r = next
	}
}

func needReason(rest, what string) (string, string) {
	if utf8.RuneCountInString(reasonOf(rest)) < MinReason {
		return RuleSuppReason, fmt.Sprintf("%s needs a reason of at least %d characters", what, MinReason)
	}
	return "", ""
}

var suppMatchers = []suppMatcher{
	{name: "nolint", quotes: "\"`", re: regexp.MustCompile(`//\s*nolint\b(?::([\w,./-]*))?(.*)$`), check: func(m []string, line string) (string, string) {
		if strings.HasPrefix(strings.TrimSpace(line), "package ") || m[1] == "all" {
			return RuleSuppBlanket, "file-wide or blanket //nolint is not allowed; suppress one rule on one line"
		}
		if m[1] == "" {
			return RuleSuppBare, "//nolint needs a linter id: //nolint:<linter> // <reason>"
		}
		return needReason(m[2], "//nolint:"+m[1])
	}},
	{name: "noqa", quotes: "\"'", re: regexp.MustCompile(`#\s*(ruff:\s*|flake8:\s*)?noqa\b(?::\s*([A-Z]+[0-9]+(?:\s*,\s*[A-Z]+[0-9]+)*))?(.*)$`), check: func(m []string, _ string) (string, string) {
		if m[1] != "" {
			return RuleSuppBlanket, "file-wide noqa is not allowed"
		}
		if m[2] == "" {
			return RuleSuppBare, "# noqa needs a rule code: # noqa: <CODE> -- <reason>"
		}
		return needReason(m[3], "# noqa: "+m[2])
	}},
	{name: "type-ignore", quotes: "\"'", re: regexp.MustCompile(`#\s*(?:type|pyright):\s*ignore\b(\[[^\]]*\])?(.*)$`), check: func(m []string, _ string) (string, string) {
		if m[1] == "" || m[1] == "[]" {
			return RuleSuppBare, "type: ignore needs an error code: # type: ignore[<code>] -- <reason>"
		}
		return needReason(m[2], "type: ignore"+m[1])
	}},
	{name: "eslint", quotes: "\"'`", re: regexp.MustCompile(`(?://|/\*)\s*eslint-disable(-next-line|-line)?\b([^\n]*)$`), check: func(m []string, _ string) (string, string) {
		if m[1] == "" {
			return RuleSuppBlanket, "block/file eslint-disable is not allowed; use eslint-disable-next-line <rule> -- <reason>"
		}
		rules, reason, _ := strings.Cut(m[2], "--")
		if strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rules), "*/")) == "" {
			return RuleSuppBare, "eslint-disable" + m[1] + " needs a rule id"
		}
		return needReason(reason, "eslint-disable"+m[1])
	}},
	{name: "ts-directive", quotes: "\"'`", re: regexp.MustCompile(`//\s*@ts-(ignore|nocheck|expect-error)\b(.*)$`), check: func(m []string, _ string) (string, string) {
		switch m[1] {
		case "nocheck":
			return RuleSuppBlanket, "@ts-nocheck is not allowed"
		case "ignore":
			return RuleSuppBare, "@ts-ignore is not allowed; use @ts-expect-error -- <reason>"
		}
		return needReason(m[2], "@ts-expect-error")
	}},
	{name: "nosemgrep", quotes: "\"'`", re: regexp.MustCompile(`(?://|#|/\*)\s*nosemgrep\b(?::\s*([\w.,-]+))?(.*)$`), check: func(m []string, _ string) (string, string) {
		if m[1] == "" {
			return RuleSuppBare, "nosemgrep needs a rule id: nosemgrep: <rule-id> -- <reason>"
		}
		return needReason(m[2], "nosemgrep: "+m[1])
	}},
	{name: "rust-allow", quotes: "", re: regexp.MustCompile(`#(!?)\[allow\(([^)]*)\)\]`), check: func(m []string, _ string) (string, string) {
		if m[1] == "!" {
			return RuleSuppBlanket, "crate-wide #![allow] is not allowed"
		}
		_, reason, ok := strings.Cut(m[2], "reason")
		if !ok {
			return RuleSuppReason, "#[allow] needs reason = \"…\""
		}
		return needReason(strings.Trim(strings.TrimSpace(strings.TrimLeft(reason, " =")), `"`), "#[allow]")
	}},
	{name: "shellcheck", quotes: "\"'", re: regexp.MustCompile(`#\s*shellcheck\s+disable=([\w,]*)(.*)$`), check: func(m []string, _ string) (string, string) {
		if m[1] == "" || m[1] == "all" {
			return RuleSuppBlanket, "shellcheck disable needs specific codes"
		}
		return needReason(m[2], "shellcheck disable="+m[1])
	}},
}

// inString reports whether position i of line lies inside a string literal
// delimited by any of quotes (heuristic: odd count of unescaped quotes).
func inString(line string, i int, quotes string) bool {
	for _, q := range quotes {
		n := 0
		for j := 0; j < i; j++ {
			if rune(line[j]) == q && (j == 0 || line[j-1] != '\\') {
				n++
			}
		}
		if n%2 == 1 {
			return true
		}
	}
	return false
}

// CheckSuppressions scans files (repo-relative) for suppressions without a
// rule id and a reason ≥ 15 chars, and for file-wide/blanket ones.
func CheckSuppressions(root string, files []string) []finding.Finding {
	var out []finding.Finding
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f))) //nolint:gosec // G304: repo files selected by the gate
		if err != nil || bytes.IndexByte(raw[:min(len(raw), 8000)], 0) >= 0 {
			continue
		}
		out = append(out, scanSuppressions(f, string(raw))...)
	}
	return out
}

func scanSuppressions(file, text string) []finding.Finding {
	var out []finding.Finding
	for n, line := range strings.Split(text, "\n") {
		for _, sm := range suppMatchers {
			loc := sm.re.FindStringSubmatchIndex(line)
			if len(loc) < 2 || inString(line, loc[0], sm.quotes) {
				continue
			}
			m := make([]string, len(loc)/2)
			for i := range m {
				if loc[2*i] >= 0 {
					m[i] = line[loc[2*i]:loc[2*i+1]]
				}
			}
			if rule, msg := sm.check(m, line); rule != "" {
				out = append(out, finding.Finding{
					Rule: rule, Severity: finding.Error, File: file, Line: n + 1, Col: loc[0] + 1, Message: msg,
					FixHint: "suppress one rule with a reason, or fix the finding",
				})
			}
		}
	}
	return out
}
