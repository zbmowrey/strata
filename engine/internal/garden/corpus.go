package garden

import (
	"slices"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/index"
)

// corpusData is the public corpus as seen by the analyses.
type corpusData struct {
	byID    map[string]*index.Record
	alias   map[string]string // lowercase alias → id
	inbound map[string][]string
	recs    []*index.Record // every public record, sorted by id
	shards  []*index.Record // active/draft public shards, sorted by id
}

// loadCorpus extracts every public record (never the private tier) and
// derives ids, aliases and inbound links.
func loadCorpus(inst *config.Instance) (*corpusData, error) {
	all, err := index.Scan(inst)
	if err != nil {
		return nil, err
	}
	d := &corpusData{byID: map[string]*index.Record{}, alias: map[string]string{}, inbound: map[string][]string{}}
	for i := range all {
		rec := &all[i]
		if rec.Private {
			continue
		}
		d.recs = append(d.recs, rec)
	}
	slices.SortFunc(d.recs, func(a, b *index.Record) int { return strings.Compare(a.ID, b.ID) })
	for _, rec := range d.recs {
		if _, dup := d.byID[rec.ID]; !dup {
			d.byID[rec.ID] = rec
		}
		for _, a := range rec.Aliases {
			d.alias[strings.ToLower(a)] = rec.ID
		}
		if rec.Kind == "shard" && (rec.Status == "active" || rec.Status == "draft" || rec.Status == "") {
			d.shards = append(d.shards, rec)
		}
	}
	for _, rec := range d.recs {
		for _, l := range rec.Links {
			if dst, ok := d.resolve(l.Dst); ok && dst != rec.ID {
				d.inbound[dst] = append(d.inbound[dst], rec.ID)
			}
		}
	}
	return d, nil
}

// resolve maps a link target to a document id.
func (d *corpusData) resolve(target string) (string, bool) {
	if _, ok := d.byID[target]; ok {
		return target, true
	}
	id, ok := d.alias[strings.ToLower(target)]
	return id, ok
}

// inboundFrom reports inbound links from documents other than hubs.
func (d *corpusData) inboundFrom(id string, skipKinds ...string) int {
	n := 0
	for _, src := range d.inbound[id] {
		if r := d.byID[src]; r != nil && !slices.Contains(skipKinds, r.Kind) {
			n++
		}
	}
	return n
}

// metaInt reads an integer frontmatter value.
func metaInt(r *index.Record, key string) int {
	switch v := r.Meta[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint64:
		return int(v) //nolint:gosec // G115: frontmatter counters are small
	case float64:
		return int(v)
	}
	return 0
}
