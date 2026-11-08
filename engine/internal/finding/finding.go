// Package finding defines the normalized finding shared by lint and gates
// (docs/spec/06-gates.md §3).
package finding

import (
	"cmp"
	"slices"
)

// Severity levels, most severe first.
const (
	Error   = "error"
	Warning = "warning"
	Info    = "info"
)

// Finding is one problem reported by lint or a gate.
type Finding struct {
	Gate     string `json:"gate,omitempty"`
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	File     string `json:"file,omitempty"`
	Message  string `json:"message"`
	FixHint  string `json:"fix_hint,omitempty"`
	Docs     string `json:"docs,omitempty"`
	Line     int    `json:"line,omitempty"`
	Col      int    `json:"col,omitempty"`
}

// Rank orders severities: lower is more severe.
func Rank(sev string) int {
	switch sev {
	case Error:
		return 0
	case Warning:
		return 1
	default:
		return 2
	}
}

// Sort orders findings deterministically: file, line, col, severity, rule, message.
func Sort(fs []Finding) {
	slices.SortStableFunc(fs, func(a, b Finding) int {
		return cmp.Or(
			cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Col, b.Col),
			cmp.Compare(Rank(a.Severity), Rank(b.Severity)), cmp.Compare(a.Gate, b.Gate),
			cmp.Compare(a.Rule, b.Rule), cmp.Compare(a.Message, b.Message),
		)
	})
}

// Count returns the number of findings with each severity.
func Count(fs []Finding) (errs, warns int) {
	for _, f := range fs {
		switch f.Severity {
		case Error:
			errs++
		case Warning:
			warns++
		}
	}
	return errs, warns
}
