// Package telemetry appends local, never-transmitted JSONL events under
// .strata/telemetry/YYYY-MM.jsonl (docs/spec/01-architecture.md §8).
package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
)

// Dir is the telemetry directory relative to the instance root.
const Dir = ".strata/telemetry"

// SessionEnv names the environment variables consulted for the session id
// of CLI-originated events (hooks pass it explicitly).
var SessionEnv = []string{"STRATA_SESSION", "CLAUDE_SESSION_ID", "CLAUDE_CODE_SESSION_ID"}

// Event is one telemetry record. Fields are flattened into the JSON object.
type Event struct {
	Time    time.Time
	Fields  map[string]any
	Session string
	Name    string
}

// Writer appends events for one instance.
type Writer struct {
	now     func() time.Time
	root    string
	session string
	enabled bool
}

// New returns a writer for inst; now may be nil (time.Now).
func New(inst *config.Instance, now func() time.Time) *Writer {
	if now == nil {
		now = time.Now
	}
	session := ""
	for _, k := range SessionEnv {
		if v := os.Getenv(k); v != "" {
			session = v
			break
		}
	}
	return &Writer{root: inst.Root, enabled: inst.Config.Telemetry.Enabled, now: now, session: session}
}

// WithSession returns a copy of w that stamps events with session.
func (w *Writer) WithSession(session string) *Writer {
	c := *w
	c.session = session
	return &c
}

// Emit appends one event. Errors are returned but callers usually ignore
// them: telemetry must never break a command.
func (w *Writer) Emit(name string, fields map[string]any) error {
	if w == nil || !w.enabled {
		return nil
	}
	t := w.now().UTC()
	rec := make(map[string]any, len(fields)+4)
	for k, v := range fields {
		rec[k] = v
	}
	rec["ts"] = t.Format(time.RFC3339Nano)
	rec["v"] = 1
	rec["session"] = w.session
	rec["event"] = name
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("telemetry: %w", err)
	}
	dir := filepath.Join(w.root, filepath.FromSlash(Dir))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("telemetry: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, t.Format("2006-01")+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // G304: engine-chosen path under .strata/
	if err != nil {
		return fmt.Errorf("telemetry: %w", err)
	}
	// One write call per line: O_APPEND makes concurrent appends atomic for
	// lines under PIPE_BUF on the local filesystems Strata targets.
	_, werr := f.Write(append(line, '\n'))
	cerr := f.Close()
	if werr != nil {
		return fmt.Errorf("telemetry: %w", werr)
	}
	if cerr != nil {
		return fmt.Errorf("telemetry: %w", cerr)
	}
	return nil
}
