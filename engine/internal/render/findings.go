// Package render formats engine results for humans (line, table, md) and
// machines (json, jsonl). Human formats are stable to read; JSON shapes are
// versioned contracts ("v": 1) covered by golden tests.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/finding"
)

// Findings writes findings one per line:
// `file:line:col  severity  rule  message (hint: …)`.
func Findings(w io.Writer, fs []finding.Finding) error {
	for _, f := range fs {
		rule := f.Rule
		if f.Gate != "" {
			rule = f.Gate + "/" + f.Rule
		}
		line := fmt.Sprintf("%s:%d:%d  %-7s  %s  %s", f.File, f.Line, f.Col, f.Severity, rule, f.Message)
		if f.FixHint != "" {
			line += " (hint: " + f.FixHint + ")"
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// JSON writes v as one JSON document with a trailing newline.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// JSONL writes each element of items as one JSON line.
func JSONL[T any](w io.Writer, items []T) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, it := range items {
		if err := enc.Encode(it); err != nil {
			return err
		}
	}
	return nil
}

// Plural formats "n thing(s)".
func Plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	if strings.HasSuffix(word, "y") && !strings.HasSuffix(word, "ey") {
		return fmt.Sprintf("%d %sies", n, strings.TrimSuffix(word, "y"))
	}
	return fmt.Sprintf("%d %ss", n, word)
}
