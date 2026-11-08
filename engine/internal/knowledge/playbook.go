package knowledge

import (
	"fmt"
	"os"
	"path"

	"github.com/zbmowrey/strata/engine/internal/fail"
)

// NewPlaybook scaffolds a Claude Code skill in the first skills directory.
func (s *Store) NewPlaybook(name string) (string, error) {
	if err := validID(name); err != nil {
		return "", err
	}
	if len(s.layout.Skills) == 0 {
		return "", fail.New(fail.NotInstance, "set [paths] skills in .strata/config.toml", "no skills directory configured")
	}
	rel := path.Join(s.layout.Skills[0], name, "SKILL.md")
	if _, err := os.Stat(s.abs(rel)); err == nil {
		return "", fail.New(fail.Usage, "", "%s already exists", rel)
	}
	content := "---\nname: " + name + "\ndescription: TODO when to use this procedure and what it achieves (Claude reads this to decide).\ntags: []\nshards: []\nscripts: []\n---\n# " +
		titleCase(name) + "\n\nTODO: the procedure. Steps that need judgment stay here; deterministic steps belong in scripts\n(`strata search '<step> kind:script'`), and facts belong in shards (link them with [[id]]).\n\n1. TODO\n"
	if err := s.writeFile(rel, []byte(content)); err != nil {
		return "", fmt.Errorf("new playbook: %w", err)
	}
	return rel, nil
}
