package knowledge

import (
	"fmt"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/corpus"
	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/lint"
)

// keyUpdated is bumped on every content change.
const (
	keyUpdated = "updated"
	keyStatus  = "status"
)

// refKeys are frontmatter list keys that hold document ids, by kind.
var refKeys = map[corpus.Kind][]string{
	corpus.Shard:    {"supersedes", "superseded_by"},
	corpus.Script:   {"shards"},
	corpus.Playbook: {"shards"},
	corpus.Project:  {"shards"},
	corpus.Question: {"blocking"},
}

// Move renames shard old to new: the file is renamed in place, old becomes
// an alias, and every inbound wikilink and id reference is rewritten.
// It returns the changed paths (sorted).
func (s *Store) Move(oldID, newID string) ([]string, error) {
	if err := validID(newID); err != nil {
		return nil, err
	}
	l, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer func() { _ = l.Unlock() }()
	if rel, taken, err := s.idTaken(newID); err != nil {
		return nil, err
	} else if taken {
		return nil, fail.New(fail.Usage, "pick an unused id", "id %q already used by %s", newID, rel)
	}
	src, err := s.findShard(oldID)
	if err != nil {
		return nil, err
	}
	raw, err := s.readFile(src.Rel)
	if err != nil {
		return nil, err
	}
	m, err := metaOf(raw)
	if err != nil {
		return nil, err
	}
	out, err := setKeys(raw, [2]string{"id", newID}, [2]string{"aliases", Value(appendUnique(m.Strings("aliases"), oldID))}, [2]string{keyUpdated, s.today()})
	if err != nil {
		return nil, err
	}
	dst := path.Join(path.Dir(src.Rel), newID+".md")
	if err := s.writeFile(dst, out); err != nil {
		return nil, err
	}
	if err := os.Remove(s.abs(src.Rel)); err != nil {
		return nil, fmt.Errorf("mv: remove %s: %w", src.Rel, err)
	}
	changed, err := s.rewriteRefs(oldID, newID)
	if err != nil {
		return nil, err
	}
	changed = append(changed, src.Rel, dst)
	slices.Sort(changed)
	return slices.Compact(changed), nil
}

// rewriteRefs rewrites wikilinks and id-list references from old to new
// across the corpus (except the private tier is included: it is local).
func (s *Store) rewriteRefs(oldID, newID string) ([]string, error) {
	fs, err := s.files()
	if err != nil {
		return nil, err
	}
	var changed []string
	for _, f := range fs {
		ok, err := s.edit(f.Rel, func(raw []byte) ([]byte, error) { return rewriteOne(f, raw, oldID, newID) })
		if err != nil {
			return nil, err
		}
		if ok {
			changed = append(changed, f.Rel)
		}
	}
	return changed, nil
}

func rewriteOne(f corpus.File, raw []byte, oldID, newID string) ([]byte, error) {
	out := raw
	if f.Kind != corpus.Script {
		d, _ := doc.Parse(f.Rel, raw)
		out = lint.RewriteLinks(d, func(t string) (string, bool) { return newID, t == oldID })
	}
	for _, key := range refKeys[f.Kind] {
		var m *doc.Meta
		var err error
		if f.Kind == corpus.Script {
			m, err = doc.ParseYAML(out)
		} else {
			m, err = metaOf(out)
		}
		if err != nil || m == nil || !slices.Contains(m.Strings(key), oldID) {
			continue
		}
		v, _ := m.Get(key)
		rendered := newID
		if _, isList := v.([]any); isList {
			ids := slices.Clone(m.Strings(key))
			ids[slices.Index(ids, oldID)] = newID
			rendered = Value(ids)
		}
		if f.Kind == corpus.Script {
			out = setYAMLKey(out, key, rendered)
		} else if out, err = SetKey(out, key, rendered); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// setYAMLKey replaces a top-level key line in a plain YAML document.
func setYAMLKey(raw []byte, key, rendered string) []byte {
	lines := strings.SplitAfter(string(raw), "\n")
	from, to := blockOf(lines, key)
	if from < 0 {
		return raw
	}
	return []byte(strings.Join(slices.Replace(lines, from, to, key+": "+rendered+"\n"), ""))
}

// Supersede marks old as superseded by new and links both ways.
func (s *Store) Supersede(oldID, newID string) ([]string, error) {
	if oldID == newID {
		return nil, fail.New(fail.Usage, "", "a shard cannot supersede itself")
	}
	l, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer func() { _ = l.Unlock() }()
	oldF, err := s.findShard(oldID)
	if err != nil {
		return nil, err
	}
	newF, err := s.findShard(newID)
	if err != nil {
		return nil, err
	}
	today := s.today()
	if _, err := s.edit(oldF.Rel, func(raw []byte) ([]byte, error) {
		return setKeys(raw, [2]string{keyStatus, "superseded"}, [2]string{"superseded_by", newID}, [2]string{keyUpdated, today})
	}); err != nil {
		return nil, err
	}
	if _, err := s.edit(newF.Rel, func(raw []byte) ([]byte, error) {
		m, err := metaOf(raw)
		if err != nil {
			return nil, err
		}
		return setKeys(raw, [2]string{"supersedes", Value(appendUnique(m.Strings("supersedes"), oldID))}, [2]string{keyUpdated, today})
	}); err != nil {
		return nil, err
	}
	return []string{newF.Rel, oldF.Rel}, nil
}

// Archive moves shards to knowledge/_archive/<domain>/… with status archived.
// Ids are kept, so links still resolve.
func (s *Store) Archive(ids []string) ([]string, error) {
	l, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer func() { _ = l.Unlock() }()
	var out []string
	for _, id := range ids {
		f, err := s.findShard(id)
		if err != nil {
			return out, err
		}
		if f.Archived {
			continue
		}
		if f.Private {
			return out, fail.New(fail.Usage, "private shards stay in knowledge/_private/", "refusing to archive private shard %q", id)
		}
		raw, err := s.readFile(f.Rel)
		if err != nil {
			return out, err
		}
		body, err := setKeys(raw, [2]string{keyStatus, "archived"}, [2]string{keyUpdated, s.today()})
		if err != nil {
			return out, err
		}
		dst := path.Join(s.layout.Knowledge, "_archive", strings.TrimPrefix(f.Rel, s.layout.Knowledge+"/"))
		if err := s.writeFile(dst, body); err != nil {
			return out, err
		}
		if err := os.Remove(s.abs(f.Rel)); err != nil {
			return out, fmt.Errorf("archive: %w", err)
		}
		out = append(out, dst)
	}
	return out, nil
}
