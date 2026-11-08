package garden

import (
	"os"
	"slices"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/fsutil"
	"github.com/zbmowrey/strata/engine/internal/lint"
	"github.com/zbmowrey/strata/engine/internal/textsim"
)

// LinkFixDice is the minimum similarity for an automatic link repair.
const LinkFixDice = 0.9

// brokenLinks repairs links with exactly one strong candidate (Apply) and
// turns the rest into fix-broken-link tasks.
func (r *run) brokenLinks() error {
	names := r.linkNames()
	for _, rec := range r.data.recs {
		if rec.Kind == kindHub {
			continue // hubs are regenerated
		}
		repl := map[string]string{}
		for _, l := range rec.Links {
			if _, ok := r.data.resolve(l.Dst); ok || l.Dst == "" {
				continue
			}
			cands := candidates(l.Dst, names)
			if len(cands) == 1 && r.opt.Apply && l.Kind == "wikilink" {
				repl[l.Dst] = cands[0]
				continue
			}
			r.add(newTask(FixBrokenLink, Medium, "point the link at the right document or remove it", []string{rec.ID},
				map[string]any{evPath: rec.Src, "target": l.Dst, "link_kind": l.Kind, "candidates": nonNil(cands)}).withID(l.Dst))
		}
		if len(repl) > 0 {
			if err := r.rewrite(rec.Src, repl); err != nil {
				return err
			}
		}
	}
	return nil
}

type linkName struct {
	tri map[string]bool
	id  string
}

func (r *run) linkNames() []linkName {
	out := make([]linkName, 0, len(r.data.recs))
	for _, rec := range r.data.recs {
		if rec.Kind == kindShard || rec.Kind == "playbook" || rec.Kind == "script" {
			out = append(out, linkName{id: rec.ID, tri: textsim.Trigrams(rec.ID)})
		}
	}
	return out
}

// candidates lists ids whose trigram similarity to target is ≥ LinkFixDice.
func candidates(target string, names []linkName) []string {
	t := textsim.Trigrams(strings.ToLower(target))
	var out []string
	for _, n := range names {
		if textsim.Dice(t, n.tri) >= LinkFixDice {
			out = append(out, n.id)
		}
	}
	slices.Sort(out)
	return out
}

func (r *run) rewrite(rel string, repl map[string]string) error {
	p := r.inst.Path(rel)
	raw, err := os.ReadFile(p) //nolint:gosec // G304: a public corpus file
	if err != nil {
		return err //nolint:wrapcheck // os errors carry the path
	}
	d, err := doc.Parse(rel, raw)
	if err != nil {
		return nil //nolint:nilerr // unparsable files are lint's business
	}
	out := lint.RewriteLinks(d, func(target string) (string, bool) {
		id, ok := repl[target]
		return id, ok
	})
	if err := fsutil.WriteAtomic(p, out, 0o644); err != nil {
		return err
	}
	r.fix("links", "repaired broken links", []string{rel})
	return nil
}
