// Command corpusgen writes the deterministic synthetic benchmark corpus
// (docs/spec/10-evals.md §3): corpusgen --n 10000 --seed 1 --out DIR.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/zbmowrey/strata/engine/internal/bench"
)

func main() {
	spec := bench.DefaultCorpus
	out := flag.String("out", "", "output directory (required)")
	flag.IntVar(&spec.Shards, "n", spec.Shards, "number of shards")
	flag.IntVar(&spec.Scripts, "scripts", spec.Scripts, "number of scripts")
	flag.IntVar(&spec.Playbooks, "playbooks", spec.Playbooks, "number of playbooks")
	flag.IntVar(&spec.Links, "links", spec.Links, "number of wikilinks")
	flag.Uint64Var(&spec.Seed, "seed", spec.Seed, "random seed")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "usage: corpusgen --out DIR [--n N] [--seed S]")
		os.Exit(2)
	}
	if err := bench.GenerateCorpus(*out, spec); err != nil {
		fmt.Fprintln(os.Stderr, "corpusgen:", err)
		os.Exit(1)
	}
}
