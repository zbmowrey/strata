package hooks

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zeebo/xxh3"
)

var (
	reQuoted = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'[^']*'`)
	reNumber = regexp.MustCompile(`\b\d+(?:\.\d+)*\b`)
	rePath   = regexp.MustCompile(`(?:~|\.{1,2})?(?:/[^\s/'"|;&<>()]+)+/?|\b[^\s/'"|;&<>()]+(?:/[^\s/'"|;&<>()]+)+/?`)
	reSpace  = regexp.MustCompile(`\s+`)
	reAssign = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	reStrata = regexp.MustCompile(`(?:^|[\s;&|(])(?:\S*/)?strata\s+(capture|search|run|project\s+(?:log|handoff)|correct|supersede|new)\b`)
)

// NormalizeCommand replaces quoted strings, paths and numbers with
// placeholders so repeated commands hash alike (docs/spec/01 §8).
func NormalizeCommand(cmd string) string {
	s := reQuoted.ReplaceAllString(cmd, "S")
	s = rePath.ReplaceAllString(s, "P")
	s = reNumber.ReplaceAllString(s, "N")
	return strings.TrimSpace(reSpace.ReplaceAllString(s, " "))
}

// CommandHash is the 16-hex-digit hash of the normalized command.
func CommandHash(cmd string) string {
	return fmt.Sprintf("%016x", xxh3.HashString(NormalizeCommand(cmd)))
}

// FirstToken returns the program name: the first word after environment
// assignments, without its directory.
func FirstToken(cmd string) string {
	for _, f := range strings.Fields(cmd) {
		if reAssign.MatchString(f) {
			continue
		}
		return filepath.Base(strings.Trim(f, `"'(`))
	}
	return ""
}

// postBash records a `bash` telemetry event and Strata usage counts.
func (h *handler) postBash() (outcome, error) {
	cmd := h.in.BashCommand()
	if cmd == "" {
		return outcome{label: "empty"}, nil
	}
	_ = h.tel.Emit("bash", map[string]any{"first_token": FirstToken(cmd), "normalized_hash": CommandHash(cmd), "length": len(cmd)})
	uses := reStrata.FindAllStringSubmatch(cmd, -1)
	if len(uses) == 0 {
		return outcome{label: "ok"}, nil
	}
	err := update(h.inst.Root, h.in.SessionID, func(s *State) {
		for _, u := range uses {
			switch sub := strings.Fields(u[1])[0]; sub {
			case "search":
				s.StrataSearch++
			case "run":
				s.StrataRun++
			case "correct", "supersede":
				s.Corrections = true
				s.Captured = true
			default: // capture, new, project log|handoff
				s.Captured = true
			}
		}
	})
	return outcome{label: "ok"}, err
}
