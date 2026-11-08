package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/zbmowrey/strata/engine/internal/fsutil"
)

// SessionDir holds per-session hook state (gitignored).
const SessionDir = ".strata/session"

// State is the per-session hook state.
type State struct {
	Edited       []string `json:"edited,omitempty"` // paths seen by pre-edit (advisory once per path)
	LastFindings string   `json:"last_findings,omitempty"`
	StopBlocks   int      `json:"stop_blocks,omitempty"`
	GateFailures int      `json:"gate_failures,omitempty"`
	CodeChanged  bool     `json:"code_changed,omitempty"`
	Captured     bool     `json:"captured,omitempty"`
	Nudged       bool     `json:"nudged,omitempty"`
	Corrections  bool     `json:"corrections,omitempty"`
	StrataSearch int      `json:"strata_search,omitempty"`
	StrataRun    int      `json:"strata_run,omitempty"`
	LastBlocking int      `json:"last_blocking"` // blocking findings of the last edit/stop gate run
}

var safeID = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// statePath returns the state file for a session id (sanitized).
func statePath(root, session string) string {
	if session == "" {
		session = "unknown"
	}
	return filepath.Join(root, filepath.FromSlash(SessionDir), safeID.ReplaceAllString(session, "_")+".json")
}

// LoadState returns a session's hook state (zero value when absent).
func LoadState(root, session string) *State { return loadState(root, session) }

// loadState reads a session's state (zero value when absent or unreadable).
func loadState(root, session string) *State {
	s := &State{}
	raw, err := os.ReadFile(statePath(root, session)) //nolint:gosec // G304: sanitized engine-owned path
	if err == nil {
		_ = json.Unmarshal(raw, s)
	}
	return s
}

// update loads, mutates and saves a session's state under a lock.
func update(root, session string, fn func(*State)) error {
	p := statePath(root, session)
	lock, err := fsutil.LockWait(p+".lock", 2e9)
	if err != nil {
		return fmt.Errorf("session state lock: %w", err)
	}
	defer func() { _ = lock.Unlock() }()
	s := loadState(root, session)
	fn(s)
	b, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("session state: %w", err)
	}
	return fsutil.WriteAtomic(p, b, 0o600)
}

// markEdited records path and reports whether it was new this session.
func (s *State) markEdited(path string) bool {
	if slices.Contains(s.Edited, path) {
		return false
	}
	s.Edited = append(s.Edited, path)
	return true
}
