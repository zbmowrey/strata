package garden

import (
	"math"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/index"
	"github.com/zbmowrey/strata/engine/internal/textsim"
)

// Duplicate thresholds (docs/spec/07-gardener.md §2).
const (
	TitleDice   = 0.8
	BodyJaccard = 0.6
	lshBands    = 16 // 16 bands × 4 rows: candidate threshold ≈ 0.5
	// stopTrigram skips trigrams shared by more titles than this: they add
	// candidate pairs without discriminating.
	stopTrigram = 400
)

type pair struct{ a, b int }

// duplicates finds shard pairs with near-identical names (trigram Dice) or
// bodies (MinHash Jaccard, LSH-bucketed).
func (r *run) duplicates() error {
	shards := r.data.shards
	scores := map[pair]map[string]any{}
	for p, d := range titlePairs(shards) {
		scores[p] = map[string]any{"title_dice": round2(d)}
	}
	for p, j := range bodyPairs(shards) {
		if scores[p] == nil {
			scores[p] = map[string]any{}
		}
		scores[p]["body_jaccard"] = round2(j)
	}
	for p, ev := range scores {
		a, b := shards[p.a], shards[p.b]
		if negated(a) != negated(b) {
			continue // a negated twin is a contradiction (resolve-conflict), not a duplicate
		}
		risk := Low
		if a.Trust == trustUserStated || b.Trust == trustUserStated {
			risk = Medium
		}
		ev["paths"] = []string{a.Src, b.Src}
		r.add(newTask(MergeDuplicates, risk, "merge into the better-sourced shard; keep the other id as an alias", []string{a.ID, b.ID}, ev))
	}
	return nil
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

// titlePairs returns pairs whose lowercase titles (or an alias equal to the
// other's id) match with Dice ≥ TitleDice, via a trigram inverted index.
func titlePairs(shards []*index.Record) map[pair]float64 {
	sets := make([]map[string]bool, len(shards))
	post := map[string][]int{}
	for i, s := range shards {
		sets[i] = textsim.Trigrams(strings.ToLower(s.Title))
		for t := range sets[i] {
			post[t] = append(post[t], i)
		}
	}
	out := map[pair]float64{}
	for i := range shards {
		for j, n := range sharedTrigrams(i, sets[i], post) {
			if d := 2 * float64(n) / float64(len(sets[i])+len(sets[j])); d >= TitleDice {
				out[pair{i, j}] = d
			}
		}
	}
	aliasPairs(shards, out)
	return out
}

// sharedTrigrams counts, for each later title, the trigrams it shares with
// title i (skipping trigrams too common to discriminate).
func sharedTrigrams(i int, set map[string]bool, post map[string][]int) map[int]int {
	shared := map[int]int{}
	for t := range set {
		if len(post[t]) > stopTrigram {
			continue
		}
		for _, j := range post[t] {
			if j > i {
				shared[j]++
			}
		}
	}
	return shared
}

// aliasPairs adds pairs where one shard lists the other's id as an alias.
func aliasPairs(shards []*index.Record, out map[pair]float64) {
	pos := make(map[string]int, len(shards))
	for i, s := range shards {
		pos[s.ID] = i
	}
	for i, s := range shards {
		for _, a := range s.Aliases {
			if j, ok := pos[strings.ToLower(a)]; ok && j != i {
				out[pair{min(i, j), max(i, j)}] = 1
			}
		}
	}
}

// bodyPairs returns pairs whose bodies have MinHash Jaccard ≥ BodyJaccard.
func bodyPairs(shards []*index.Record) map[pair]float64 {
	sigs := make([]textsim.Signature, len(shards))
	ok := make([]bool, len(shards))
	buckets := map[[2]uint64][]int{}
	for i, s := range shards {
		if len(textsim.Words(s.Body)) < 8 {
			continue // too short to compare meaningfully
		}
		sigs[i], ok[i] = textsim.MinHash(s.Body)
		for b, key := range textsim.Bands(&sigs[i], lshBands) {
			k := [2]uint64{uint64(b), key}
			buckets[k] = append(buckets[k], i)
		}
	}
	out := map[pair]float64{}
	for _, ids := range buckets {
		for x, i := range ids {
			for _, j := range ids[x+1:] {
				p := pair{min(i, j), max(i, j)}
				if _, done := out[p]; done || !ok[i] || !ok[j] {
					continue
				}
				if jac := textsim.Jaccard(&sigs[i], &sigs[j]); jac >= BodyJaccard {
					out[p] = jac
				}
			}
		}
	}
	return out
}
