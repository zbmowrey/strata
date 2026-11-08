package telemetry

import (
	"testing"
)

func rec(name, session string, fields map[string]any) Record {
	return Record{Name: name, Session: session, Fields: fields}
}

func TestSummarize(t *testing.T) {
	t.Parallel()
	var recs []Record
	for i := range 12 {
		recs = append(recs, rec("bash", "s1", map[string]any{"first_token": string(rune('a' + i))}))
	}
	recs = append(recs,
		rec("bash", "s2", map[string]any{"first_token": "a"}),
		rec("hook", "", map[string]any{"hook": "stop", "ms": float64(5)}),
		rec("gate", "s2", map[string]any{"gate": "typos", "result": "fixed"}),
		rec("gate", "s2", map[string]any{"stage": "stop", "result": "abandoned"}),
	)
	s := Summarize(recs)
	if s.Total != 16 || s.Sessions != 2 || s.Searches != 0 || s.ZeroRate != 0 {
		t.Fatalf("%+v", s)
	}
	if len(s.BashTop) != BashTopN || s.BashTop[0] != (Count{Name: "a", N: 2}) || s.BashTop[1].Name != "b" {
		t.Fatalf("bash top %+v", s.BashTop)
	}
	if s.Events[0] != (Count{Name: "bash", N: 13}) || len(s.Hooks) != 1 || s.Hooks[0].P95 != 5 {
		t.Fatalf("events %+v hooks %+v", s.Events, s.Hooks)
	}
	if len(s.Gates) != 2 || s.Gates[0].Gate != "stop-stage" || s.Gates[1].Other != 1 {
		t.Fatalf("gates %+v", s.Gates)
	}
	if percentile(nil, 95) != 0 || percentile([]float64{1, 2, 3, 4}, 50) != 2 {
		t.Fatal("percentile")
	}
}
