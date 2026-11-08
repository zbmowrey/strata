package knowledge

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/schema"
)

// CaptureInput is a low-friction capture into the inbox.
type CaptureInput struct {
	Text    string
	Type    string
	Source  string // kind:ref
	Project string
	Shard   string // for --type correction: the shard being corrected
	Session string
	Tags    []string
}

// Capture writes _inbox/<YYYYMMDD-HHMMSS>-<slug>.md and returns its path.
// A correction naming a shard increments that shard's occurrences.
func (s *Store) Capture(in CaptureInput) (string, error) {
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return "", fail.New(fail.Usage, `strata capture "<what, why, evidence>"`, "nothing to capture")
	}
	if in.Type == "" {
		in.Type = "fact"
	}
	if !contains(schema.Types, in.Type) {
		return "", fail.New(fail.Usage, "types: "+strings.Join(schema.Types, ", "), "unknown type %q", in.Type)
	}
	sources, err := captureSources(in.Source)
	if err != nil {
		return "", err
	}
	if in.Shard != "" && in.Type != "correction" {
		return "", fail.New(fail.Usage, "use --shard with --type correction", "--shard only applies to corrections")
	}
	project := in.Project
	if project == "" {
		project = "_adhoc"
	}
	now := s.now()
	var b strings.Builder
	b.WriteString("---\ntype: " + in.Type + "\ncaptured: " + now.Format("2006-01-02T15:04:05Z07:00") + "\n")
	b.WriteString("session: " + Value(in.Session) + "\nproject: " + Value(project) + "\n")
	if len(in.Tags) > 0 {
		b.WriteString("tags: " + Value(in.Tags) + "\n")
	}
	if in.Shard != "" {
		b.WriteString("shard: " + in.Shard + "\n")
	}
	b.WriteString("sources: " + sources + "\n---\n" + text + "\n")
	base := now.Format("20060102-150405") + "-" + Slugify(text, 40)
	rel, err := s.uniqueInbox(base)
	if err != nil {
		return "", err
	}
	if err := s.writeFile(rel, []byte(b.String())); err != nil {
		return "", fmt.Errorf("capture: %w", err)
	}
	if in.Type == "correction" && in.Shard != "" {
		if err := s.bumpOccurrences(in.Shard); err != nil {
			return rel, err
		}
		_ = s.tel.Emit("correction", map[string]any{"shard_id": in.Shard})
	}
	_ = s.tel.Emit("capture", map[string]any{"kind": "inbox", "type": in.Type, "project": project})
	return rel, nil
}

func (s *Store) uniqueInbox(base string) (string, error) {
	dir := path.Join(s.layout.Knowledge, "_inbox")
	for i := 1; i < 1000; i++ {
		name := base
		if i > 1 {
			name += "-" + strconv.Itoa(i)
		}
		rel := path.Join(dir, name+".md")
		if _, err := os.Stat(s.abs(rel)); os.IsNotExist(err) {
			return rel, nil
		}
	}
	return "", fmt.Errorf("capture: too many captures named %s", base)
}

// captureSources renders --source kind:ref as a sources list.
func captureSources(src string) (string, error) {
	if src == "" {
		return "[]", nil
	}
	kind, ref, ok := strings.Cut(src, ":")
	if !ok || ref == "" || !contains(schema.SourceKind, kind) {
		return "", fail.New(fail.Usage, "use --source kind:ref with kind in "+strings.Join(schema.SourceKind, ", "), "bad --source %q", src)
	}
	return "[{ kind: " + kind + ", ref: " + Value(ref) + " }]", nil
}

// bumpOccurrences increments a shard's occurrences counter.
func (s *Store) bumpOccurrences(id string) error {
	f, err := s.findShard(id)
	if err != nil {
		return err
	}
	_, err = s.edit(f.Rel, func(raw []byte) ([]byte, error) {
		m, err := metaOf(raw)
		if err != nil {
			return nil, err
		}
		n, _ := strconv.Atoi(m.String("occurrences"))
		return SetKey(raw, "occurrences", strconv.Itoa(n+1))
	})
	return err
}
