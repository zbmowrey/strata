// Package bench holds Strata's measurement harnesses: the synthetic corpus
// generator and performance benchmarks (docs/spec/01-architecture.md §7),
// the retrieval bench, and the protocol-eval runner (docs/spec/10-evals.md).
package bench

import (
	"fmt"
	"math/rand/v2" // nosemgrep: go.lang.security.audit.crypto.math_random.math-random-used -- seeded PRNG for a reproducible synthetic corpus, never security
	"os"
	"path/filepath"
	"strings"
)

// CorpusSpec sizes a synthetic corpus.
type CorpusSpec struct {
	Shards    int
	Scripts   int
	Playbooks int
	Links     int
	Seed      uint64
}

// DefaultCorpus is the §7 benchmark corpus: 10,000 shards (~3 KB), 300
// scripts, 50 playbooks, 2,000 links.
var DefaultCorpus = CorpusSpec{Shards: 10000, Scripts: 300, Playbooks: 50, Links: 2000, Seed: 1}

var (
	syllables = []string{"ka", "lo", "ri", "ten", "mo", "sa", "vel", "dra", "pi", "qu", "zor", "fen", "ga", "lim", "tor", "ex", "ul", "bo", "ne", "cy"}
	types     = []string{"fact", "gotcha", "howto", "decision", "standard", "reference", "glossary", "correction"}
	trusts    = []string{"inferred", "observed", "verified", "user-stated"}
)

type gen struct {
	r     *rand.Rand
	words []string
}

func newGen(seed uint64) *gen {
	g := &gen{r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))} //nolint:gosec // G404: deterministic synthetic data, not security
	seen := map[string]bool{}
	for len(g.words) < 3000 {
		var b strings.Builder
		for range 2 + g.r.IntN(3) {
			b.WriteString(syllables[g.r.IntN(len(syllables))])
		}
		if w := b.String(); !seen[w] {
			seen[w] = true
			g.words = append(g.words, w)
		}
	}
	return g
}

// word draws from a Zipf-like distribution so term frequencies are realistic.
func (g *gen) word() string {
	i := int(float64(len(g.words)) * g.r.Float64() * g.r.Float64() * g.r.Float64())
	return g.words[i]
}

func (g *gen) sentence(n int) string {
	ws := make([]string, n)
	for i := range ws {
		ws[i] = g.word()
	}
	return strings.Join(ws, " ")
}

// GenerateCorpus writes a deterministic synthetic instance into dir.
func GenerateCorpus(dir string, spec CorpusSpec) error {
	g := newGen(spec.Seed)
	ids := make([]string, spec.Shards)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s-%s-%05d", g.words[i%len(g.words)], g.words[(i*7+3)%len(g.words)], i)
	}
	linksPer := make([][]string, spec.Shards)
	for range spec.Links {
		from := g.r.IntN(spec.Shards)
		linksPer[from] = append(linksPer[from], ids[g.r.IntN(spec.Shards)])
	}
	files := map[string]string{".strata/config.toml": "[engine]\nmin_version = \"0.1.0\"\n"}
	for i, id := range ids {
		dom := fmt.Sprintf("domain%02d", i%20)
		topic := fmt.Sprintf("topic%02d", (i/20)%10)
		files[filepath.Join("knowledge", dom, topic, id+".md")] = g.shard(id, linksPer[i])
	}
	for i := range spec.Scripts {
		name := fmt.Sprintf("run-%s-%03d", g.words[(i*13)%len(g.words)], i)
		files[filepath.Join("scripts", fmt.Sprintf("dom%02d", i%10), name, "script.yaml")] = g.script(name)
	}
	for i := range spec.Playbooks {
		name := fmt.Sprintf("play-%s-%02d", g.words[(i*17)%len(g.words)], i)
		files[filepath.Join(".claude", "skills", name, "SKILL.md")] = fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n# %s\n\n%s\n", name, g.sentence(14), name, g.sentence(120))
	}
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil { //nolint:gosec // G703: rel is generated here, not user input
			return fmt.Errorf("corpus: %w", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil { //nolint:gosec // G703: rel is generated here, not user input
			return fmt.Errorf("corpus: %w", err)
		}
	}
	return nil
}

func (g *gen) shard(id string, links []string) string {
	var b strings.Builder
	typ := types[g.r.IntN(len(types))]
	trust := trusts[g.r.IntN(len(trusts))]
	fmt.Fprintf(&b, "---\nid: %s\ntitle: %s\nsummary: %s\ntype: %s\ntags: [%s, %s]\naliases: [%s]\nstatus: active\ntrust: %s\ncreated: 2026-01-%02d\nupdated: 2026-%02d-%02d\n",
		id, g.sentence(5), g.sentence(14), typ, g.word(), g.word(), g.word()+"-"+g.word(), trust, 1+g.r.IntN(28), 1+g.r.IntN(10), 1+g.r.IntN(28))
	if trust != "inferred" {
		b.WriteString("sources:\n  - { kind: user, ref: synthetic, at: 2026-01-01 }\n  - { kind: file, ref: README.md, at: 2026-01-01 }\n")
	}
	if g.r.IntN(5) == 0 {
		fmt.Fprintf(&b, "applies_to: [\"src/%s/**\"]\n", g.word())
	}
	b.WriteString("---\n")
	b.WriteString(g.sentence(40) + ".\n")
	target := 2600 + g.r.IntN(800)
	for _, l := range links {
		fmt.Fprintf(&b, "\nSee [[%s]].\n", l)
	}
	for b.Len() < target {
		fmt.Fprintf(&b, "\n## %s\n%s.\n", g.sentence(3), g.sentence(45))
	}
	return b.String()
}

func (g *gen) script(name string) string {
	return fmt.Sprintf(`id: %s
summary: %s
description: |
  %s
status: active
runtime: go
entry: main.go
usage: %s [--format text|json]
side_effect: read-only
writes: []
timeout: 60s
tags: [%s]
owner: gardener
`, name, g.sentence(10), g.sentence(30), name, g.word())
}
