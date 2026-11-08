package telemetry

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
)

// Record is one decoded telemetry line.
type Record struct {
	Time    time.Time
	Fields  map[string]any
	Session string
	Name    string
}

// Str returns a string field ("" when absent or not a string).
func (r Record) Str(key string) string {
	s, _ := r.Fields[key].(string)
	return s
}

// Int returns a numeric field as int (0 when absent).
func (r Record) Int(key string) int {
	f, _ := r.Fields[key].(float64)
	return int(f)
}

// Strs returns a string-list field.
func (r Record) Strs(key string) []string {
	raw, _ := r.Fields[key].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// monthFiles lists the monthly files, oldest first.
func monthFiles(root string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(Dir), "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("telemetry: %w", err)
	}
	slices.Sort(files)
	return files, nil
}

// Read returns the events at or after since (zero = all), oldest first,
// optionally limited to the given event names. Malformed lines are skipped.
func Read(inst *config.Instance, since time.Time, names ...string) ([]Record, error) {
	files, err := monthFiles(inst.Root)
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, f := range files {
		month := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		if t, err := time.Parse("2006-01", month); err == nil && !since.IsZero() && t.AddDate(0, 1, 0).Before(since) {
			continue
		}
		recs, err := readFile(f, since, names)
		if err != nil {
			return nil, err
		}
		out = append(out, recs...)
	}
	return out, nil
}

func readFile(path string, since time.Time, names []string) ([]Record, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: engine-owned telemetry file
	if err != nil {
		return nil, fmt.Errorf("telemetry: %w", err)
	}
	var out []Record
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		r := Record{Fields: m}
		r.Name, _ = m["event"].(string)
		r.Session, _ = m["session"].(string)
		ts, _ := m["ts"].(string)
		r.Time, _ = time.Parse(time.RFC3339Nano, ts)
		if r.Name == "" || r.Time.IsZero() || r.Time.Before(since) || (len(names) > 0 && !slices.Contains(names, r.Name)) {
			continue
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("telemetry %s: %w", path, err)
	}
	return out, nil
}

// Prune removes events older than the retention window: whole monthly files
// entirely before the cutoff are deleted; the file straddling it is
// rewritten. It returns the number of events removed.
func Prune(inst *config.Instance, now time.Time) (int, error) {
	keep := inst.Config.Telemetry.Retention.D()
	if keep <= 0 {
		return 0, nil
	}
	cutoff := now.Add(-keep)
	files, err := monthFiles(inst.Root)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, f := range files {
		n, err := pruneFile(f, cutoff)
		if err != nil {
			return removed, err
		}
		removed += n
	}
	return removed, nil
}

func pruneFile(path string, cutoff time.Time) (int, error) {
	month, err := time.Parse("2006-01", strings.TrimSuffix(filepath.Base(path), ".jsonl"))
	if err != nil || !month.Before(cutoff) {
		return 0, nil //nolint:nilerr // foreign file names are left alone
	}
	raw, err := os.ReadFile(path) //nolint:gosec // G304: engine-owned telemetry file
	if err != nil {
		return 0, fmt.Errorf("telemetry: %w", err)
	}
	var kept bytes.Buffer
	removed := 0
	for line := range bytes.SplitSeq(raw, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m struct {
			TS string `json:"ts"`
		}
		t := time.Time{}
		if json.Unmarshal(line, &m) == nil {
			t, _ = time.Parse(time.RFC3339Nano, m.TS)
		}
		if t.IsZero() || t.Before(cutoff) {
			removed++
			continue
		}
		kept.Write(line)
		kept.WriteByte('\n')
	}
	if kept.Len() == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, fmt.Errorf("telemetry: %w", err)
		}
		return removed, nil
	}
	if removed == 0 {
		return 0, nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, kept.Bytes(), 0o600); err != nil {
		return 0, fmt.Errorf("telemetry: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return 0, fmt.Errorf("telemetry: %w", err)
	}
	return removed, nil
}
