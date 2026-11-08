package render

import (
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// DefaultWidth is used when neither the terminal nor $COLUMNS says otherwise.
const DefaultWidth = 120

// Width returns the output width: $COLUMNS, else the terminal width when
// stdout is a TTY (tty != 0 is the terminal fd), else DefaultWidth.
func Width(ttyFd uintptr, isTTY bool) int {
	if c, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && c >= 40 {
		return c
	}
	if isTTY {
		if w := termWidth(ttyFd); w >= 40 {
			return w
		}
	}
	return DefaultWidth
}

// Truncate shortens s to at most n runes, ending with "…" when cut.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// Pad right-pads s with spaces to n runes (truncating if longer).
func Pad(s string, n int) string {
	s = Truncate(s, n)
	if k := utf8.RuneCountInString(s); k < n {
		return s + strings.Repeat(" ", n-k)
	}
	return s
}

// OneLine collapses whitespace runs (including newlines) into single spaces.
func OneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// Runes returns the rune length of s.
func Runes(s string) int { return utf8.RuneCountInString(s) }
