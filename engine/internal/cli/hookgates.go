package cli

import (
	"context"
	"time"

	"github.com/zbmowrey/strata/engine/internal/config"
	"github.com/zbmowrey/strata/engine/internal/gates"
	"github.com/zbmowrey/strata/engine/internal/hooks"
)

func init() { hookGates = runHookGates }

// runHookGates adapts gates.RunStage to the hook contract: edit runs on the
// touched files, stop on the changed files; fix-mode gates apply first.
func runHookGates(ctx context.Context, inst *config.Instance, stage string, paths []string) (hooks.GateResult, error) {
	files := paths
	if files == nil {
		var err error
		if files, err = gates.Files(ctx, inst.Root, gates.SelectChanged, nil); err != nil {
			return hooks.GateResult{}, err
		}
	}
	if len(files) == 0 {
		return hooks.GateResult{}, nil
	}
	rep, err := gates.RunStage(ctx, inst, stage, files, gates.StageOptions{Now: time.Now, Fix: true})
	if err != nil {
		return hooks.GateResult{}, err
	}
	if !rep.Blocked() {
		return hooks.GateResult{}, nil
	}
	rules := map[string]int{}
	for _, f := range rep.Blocking {
		rules[f.Gate+"/"+f.Rule]++
	}
	return hooks.GateResult{Report: gates.Format(rep), Hash: gates.SetHash(rep.Blocking), Rules: rules, Blocking: len(rep.Blocking)}, nil
}
