// Package textsim holds the text-similarity primitives shared by search
// (fuzzy matching) and the gardener (duplicate detection): rune trigrams with
// the Sørensen–Dice coefficient, and MinHash signatures over word shingles.
package textsim

import (
	"strings"
	"unicode"

	"github.com/zeebo/xxh3"
)

// Trigrams returns the set of rune trigrams of s.
func Trigrams(s string) map[string]bool {
	r := []rune(s)
	out := make(map[string]bool, max(0, len(r)-2))
	for i := 0; i+3 <= len(r); i++ {
		out[string(r[i:i+3])] = true
	}
	return out
}

// Dice is the Sørensen–Dice coefficient of two trigram sets.
func Dice(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := 0
	for t := range a {
		if b[t] {
			n++
		}
	}
	return 2 * float64(n) / float64(len(a)+len(b))
}

// Words splits text into lowercase letter/digit words.
func Words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// SigSize is the number of MinHash permutations.
const SigSize = 64

// Signature is a MinHash signature; Empty reports a text with no shingles.
type Signature [SigSize]uint64

// MinHash computes the signature of text's word k-shingles (k = 3, or the
// whole text when shorter). It returns ok = false for empty text.
func MinHash(text string) (sig Signature, ok bool) {
	words := Words(text)
	if len(words) == 0 {
		return sig, false
	}
	for i := range sig {
		sig[i] = ^uint64(0)
	}
	k := min(3, len(words))
	for i := 0; i+k <= len(words); i++ {
		h := xxh3.HashString(strings.Join(words[i:i+k], " "))
		for j := range sig {
			// Cheap independent permutations: mix the base hash with a per-slot seed.
			v := mix(h ^ seeds[j])
			if v < sig[j] {
				sig[j] = v
			}
		}
	}
	return sig, true
}

// Jaccard estimates the Jaccard similarity of two signatures.
func Jaccard(a, b *Signature) float64 {
	n := 0
	for i := range a {
		if a[i] == b[i] {
			n++
		}
	}
	return float64(n) / SigSize
}

// Bands splits a signature into LSH band keys (bands × rows = SigSize).
func Bands(s *Signature, bands int) []uint64 {
	rows := SigSize / bands
	out := make([]uint64, bands)
	for b := range bands {
		h := uint64(b) * 0x9e3779b97f4a7c15
		for r := range rows {
			h = mix(h ^ s[b*rows+r])
		}
		out[b] = h
	}
	return out
}

// mix is the splitmix64 finalizer.
func mix(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	return x ^ (x >> 31)
}

var seeds = func() (s [SigSize]uint64) {
	x := uint64(0x5eed)
	for i := range s {
		x = mix(x + 0x9e3779b97f4a7c15)
		s[i] = x
	}
	return s
}()
