package garden

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/testutil"
)

func TestComputeDebt(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	inbox := func(stamp string) string { return "knowledge/_inbox/" + stamp + "-note.md" }
	plan := func(generated string, actionable, escalated int) string {
		var tasks []string
		for i := range actionable {
			tasks = append(tasks, `{"id":"file-inbox:a`+string(rune('a'+i))+`","type":"file-inbox","risk":"low"}`)
		}
		for i := range escalated {
			tasks = append(tasks, `{"id":"stale-project:p`+string(rune('a'+i))+`","type":"stale-project","risk":"question","escalate":true}`)
		}
		return `{"v":1,"generated":"` + generated + `","tasks":[` + strings.Join(tasks, ",") + `]}`
	}
	hist := func(outcome, ended string) string {
		return `{"run":"r1","attempt":1,"started":"` + ended + `","ended":"` + ended + `","outcome":"` + outcome + `"}` + "\n"
	}
	for _, tc := range []struct {
		files   map[string]string
		name    string
		reasons []string
		lastRun int
		due     bool
	}{
		{name: "empty instance", lastRun: -1},
		{
			name:    "fresh capture on a never-gardened instance",
			files:   map[string]string{inbox("20261114-090000"): "x"},
			lastRun: -1,
		},
		{
			name:    "old inbox item",
			files:   map[string]string{inbox("20261108-090000"): "x", inbox("20261113-090000"): "y"},
			lastRun: -1, due: true, reasons: []string{"inbox oldest 6d"},
		},
		{
			name:    "many actionable tasks; escalated ones do not count",
			files:   map[string]string{PlanFile: plan("2026-11-14", 10, 3)},
			lastRun: -1, due: true, reasons: []string{"10 tasks"},
		},
		{
			name:    "below the task threshold",
			files:   map[string]string{PlanFile: plan("2026-11-14", 9, 5)},
			lastRun: -1,
		},
		{
			name: "plan older than the last finished run is spent",
			files: map[string]string{
				PlanFile:    plan("2026-11-01", 20, 0),
				HistoryFile: hist(OutcomePR, "2026-11-10T08:00:00Z"),
			},
			lastRun: 4,
		},
		{
			name: "last garden long ago with work waiting",
			files: map[string]string{
				inbox("20261113-090000"): "x",
				HistoryFile:              hist(OutcomeTidy, "2026-11-01T08:00:00Z") + "not json\n" + hist("failed", "2026-11-12T08:00:00Z"),
			},
			lastRun: 13, due: true, reasons: []string{"last garden 13d ago"},
		},
		{
			name:    "last garden long ago, nothing waiting",
			files:   map[string]string{HistoryFile: hist(OutcomePR, "2026-11-01T08:00:00Z")},
			lastRun: 13,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			testutil.WriteFile(t, root, ".strata/config.toml", "")
			inst, err := config.Open(root, "")
			if err != nil {
				t.Fatal(err)
			}
			for p, body := range tc.files {
				testutil.WriteFile(t, inst.Root, p, body)
			}
			d, err := ComputeDebt(inst, at)
			if err != nil {
				t.Fatal(err)
			}
			if d.Due != tc.due || d.LastRunDays != tc.lastRun || !slices.Equal(d.Reasons, append([]string{}, tc.reasons...)) {
				t.Fatalf("got %+v; want due=%v last=%d reasons=%v", d, tc.due, tc.lastRun, tc.reasons)
			}
		})
	}
}

func TestComputeDebtCorruptPlan(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	testutil.WriteFile(t, root, ".strata/config.toml", "")
	testutil.WriteFile(t, root, PlanFile, "{not json")
	inst, err := config.Open(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ComputeDebt(inst, time.Now()); err == nil {
		t.Fatal("want an error for a corrupt plan")
	}
}
