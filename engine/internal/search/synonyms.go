package search

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
)

// Synonyms maps a lowercase term to its alternatives.
type Synonyms map[string][]string

// LoadSynonyms reads a synonyms file: one comma-separated group per line,
// `#` comments. A missing file yields an empty set.
func LoadSynonyms(path string) (Synonyms, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: path comes from the instance config
	if err != nil {
		if os.IsNotExist(err) {
			return Synonyms{}, nil
		}
		return nil, fmt.Errorf("read synonyms: %w", err)
	}
	return ParseSynonyms(raw), nil
}

// ParseSynonyms parses synonym groups.
func ParseSynonyms(raw []byte) Synonyms {
	s := Synonyms{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		var group []string
		for _, t := range strings.Split(line, ",") {
			if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
				group = append(group, t)
			}
		}
		for _, t := range group {
			for _, o := range group {
				if o != t && !contains(s[t], o) {
					s[t] = append(s[t], o)
				}
			}
		}
	}
	return s
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
