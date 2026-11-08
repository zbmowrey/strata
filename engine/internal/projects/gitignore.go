package projects

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/fail"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
)

// GitignoreMarkers delimit the generated projects block in .gitignore.
var GitignoreMarkers = fsutil.Markers{
	Begin:       "# >>> strata:projects (GENERATED — edit `track:` in projects/<slug>/project.md)",
	BeginPrefix: "# >>> strata:projects",
	End:         "# <<< strata:projects",
}

// ValidateTrack checks one track: glob (relative to the project folder).
// The first path segment must be literal so the glob can never reach
// scratch/ (throwaway files are never versioned).
func ValidateTrack(glob string) error {
	bad := func(why string) error {
		return fail.New(fail.Usage, "track globs are relative to the project folder, e.g. out/report.md or plan.md", "invalid track glob %q: %s", glob, why)
	}
	switch {
	case strings.TrimSpace(glob) == "":
		return bad("empty")
	case strings.HasPrefix(glob, "/") || strings.HasPrefix(glob, "!") || strings.Contains(glob, "\\"):
		return bad("must be a relative glob")
	case !doublestar.ValidatePattern(glob):
		return bad("malformed glob")
	}
	first, _, _ := strings.Cut(glob, "/")
	switch {
	case first == ".." || first == "." || strings.Contains(glob, "/../"):
		return bad("must stay inside the project folder")
	case strings.ContainsAny(first, "*?[{"):
		return bad("the first path segment must be literal (so scratch/ can never match)")
	case first == "scratch":
		return bad("scratch/ is never tracked")
	}
	return nil
}

// GitignoreBlock renders the projects block body for the given projects.
// Invalid track globs are skipped and reported as warnings.
func GitignoreBlock(projectsDir string, ps []*Project) (body string, warnings []string) {
	base := "/" + strings.Trim(projectsDir, "/")
	lines := []string{base + "/**", "!" + base + "/**/"}
	for _, p := range ps {
		dir := p.Slug
		if p.Archived {
			dir = "_archive/" + p.Slug
		}
		for _, g := range p.Track {
			if err := ValidateTrack(g); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", p.Slug, err))
				continue
			}
			lines = append(lines, "!"+path.Join(base, dir, g))
		}
	}
	return strings.Join(lines, "\n") + "\n", warnings
}

// Sync regenerates the projects block in the instance's .gitignore. It
// reports whether the file changed, plus warnings for skipped globs.
func Sync(inst *config.Instance) (changed bool, warnings []string, err error) {
	ps, err := New(inst, nil).All()
	if err != nil {
		return false, nil, err
	}
	body, warnings := GitignoreBlock(inst.Config.Paths.Projects, ps)
	p := inst.Path(".gitignore")
	raw, err := os.ReadFile(p) //nolint:gosec // G304: the instance's own .gitignore
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, warnings, fmt.Errorf("read .gitignore: %w", err)
	}
	out, changed := fsutil.ReplaceBlock(raw, GitignoreMarkers, body)
	if !changed {
		return false, warnings, nil
	}
	if err := fsutil.WriteAtomic(p, out, 0o644); err != nil {
		return false, warnings, err
	}
	return true, warnings, nil
}
