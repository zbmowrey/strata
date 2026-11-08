package telemetry

import (
	"cmp"
	"math"
	"slices"
)

// Count is a name with its number of occurrences.
type Count struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

// HookStat summarizes hook latency for one hook event.
type HookStat struct {
	Hook string  `json:"hook"`
	N    int     `json:"n"`
	P50  float64 `json:"p50_ms"`
	P95  float64 `json:"p95_ms"`
}

// GateStat counts gate results for one gate.
type GateStat struct {
	Gate  string `json:"gate"`
	Pass  int    `json:"pass"`
	Fail  int    `json:"fail"`
	Other int    `json:"other"` // fixed, cached, skipped, warnings, abandoned…
}

// Stats is the `strata telemetry stats` summary.
type Stats struct {
	Events      []Count    `json:"events"`
	BashTop     []Count    `json:"bash_top"`
	Hooks       []HookStat `json:"hooks"`
	Gates       []GateStat `json:"gates"`
	Total       int        `json:"total"`
	Sessions    int        `json:"sessions"`
	Searches    int        `json:"searches"`
	ZeroResults int        `json:"zero_results"`
	ZeroRate    float64    `json:"zero_rate"`
}

// BashTopN is how many bash programs Stats lists.
const BashTopN = 10

// Summarize aggregates records into Stats (deterministic ordering).
func Summarize(recs []Record) Stats {
	events, bash, sessions := map[string]int{}, map[string]int{}, map[string]bool{}
	hooks := map[string][]float64{}
	gates := map[string]*GateStat{}
	s := Stats{Total: len(recs)}
	for _, r := range recs {
		events[r.Name]++
		if r.Session != "" {
			sessions[r.Session] = true
		}
		switch r.Name {
		case "bash":
			bash[r.Str("first_token")]++
		case "search":
			s.Searches++
			if zero, _ := r.Fields["zero"].(bool); zero {
				s.ZeroResults++
			}
		case "hook":
			hooks[r.Str("hook")] = append(hooks[r.Str("hook")], float64(r.Int("ms")))
		case "gate":
			countGate(gates, r)
		}
	}
	s.Sessions = len(sessions)
	if s.Searches > 0 {
		s.ZeroRate = math.Round(float64(s.ZeroResults)/float64(s.Searches)*1000) / 1000
	}
	s.Events = counts(events, 0)
	s.BashTop = counts(bash, BashTopN)
	s.Hooks = hookStats(hooks)
	s.Gates = make([]GateStat, 0, len(gates))
	for _, g := range gates {
		s.Gates = append(s.Gates, *g)
	}
	slices.SortFunc(s.Gates, func(a, b GateStat) int { return cmp.Compare(a.Gate, b.Gate) })
	return s
}

func countGate(gates map[string]*GateStat, r Record) {
	name := r.Str("gate")
	if name == "" {
		name = r.Str("stage") + "-stage"
	}
	g, ok := gates[name]
	if !ok {
		g = &GateStat{Gate: name}
		gates[name] = g
	}
	switch r.Str("result") {
	case "pass":
		g.Pass++
	case "fail":
		g.Fail++
	default:
		g.Other++
	}
}

// counts sorts a tally by count desc then name; limit 0 keeps all.
func counts(m map[string]int, limit int) []Count {
	out := make([]Count, 0, len(m))
	for k, v := range m {
		out = append(out, Count{Name: k, N: v})
	}
	slices.SortFunc(out, func(a, b Count) int { return cmp.Or(cmp.Compare(b.N, a.N), cmp.Compare(a.Name, b.Name)) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func hookStats(m map[string][]float64) []HookStat {
	out := make([]HookStat, 0, len(m))
	for k, ms := range m {
		slices.Sort(ms)
		out = append(out, HookStat{Hook: k, N: len(ms), P50: percentile(ms, 50), P95: percentile(ms, 95)})
	}
	slices.SortFunc(out, func(a, b HookStat) int { return cmp.Compare(a.Hook, b.Hook) })
	return out
}

// percentile is the nearest-rank percentile of sorted values.
func percentile(sorted []float64, p int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := (len(sorted)*p + 99) / 100
	return sorted[max(0, min(len(sorted), rank)-1)]
}
