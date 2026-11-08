package hooks

import (
	"fmt"

	"github.com/zbmowrey/strata/engine/internal/render"
)

// NudgeText is the once-per-session knowledge checkpoint (docs/spec/08 §4.5).
const NudgeText = "Knowledge checkpoint: capture anything non-obvious you learned (strata capture …) and update the project handoff if this is multi-session work — or reply that there is nothing to capture."

// stop runs stop-stage gates with the loop guard, then the knowledge nudge.
func (h *handler) stop() (outcome, error) {
	var gr GateResult
	if h.env.Gates != nil {
		var err error
		if gr, err = h.env.Gates(h.ctx, h.inst, "stop", nil); err != nil {
			return outcome{}, err
		}
	}
	var out outcome
	err := update(h.inst.Root, h.in.SessionID, func(s *State) { out = h.decideStop(s, gr) })
	if err != nil {
		return outcome{}, err
	}
	if out.label == "abandoned" {
		_ = h.tel.Emit("gate", map[string]any{"stage": "stop", "result": "abandoned", "blocking": gr.Blocking, "rules": gr.Rules})
	}
	return out, nil
}

// decideStop applies the loop guard (docs/spec/06 §2.1) and the nudge.
func (h *handler) decideStop(s *State, gr GateResult) outcome {
	if gr.Blocking > 0 {
		s.GateFailures++
		switch {
		case gr.Hash != s.LastFindings:
			// New or changed finding set: progress resets the counter.
			s.LastFindings, s.StopBlocks = gr.Hash, 1
		case s.StopBlocks < h.inst.Config.Gates.StopMaxBlocks:
			s.StopBlocks++
		default:
			msg := fmt.Sprintf("✗ %s unresolved — see strata check", render.Plural(gr.Blocking, "blocking finding"))
			return outcome{stdout: systemMessage(msg), stderr: msg + "\n", label: "abandoned"}
		}
		return outcome{stdout: blockJSON(gr.Report), label: "blocked"}
	}
	s.LastFindings, s.StopBlocks = "", 0
	if h.wantNudge(s) {
		s.Nudged = true
		return outcome{stdout: blockJSON(NudgeText), label: "nudged"}
	}
	return outcome{label: "clean"}
}

func (h *handler) wantNudge(s *State) bool {
	if h.inst.Config.Hooks.StopNudge != "once" || h.env.Subagent || s.Nudged || s.Captured {
		return false
	}
	return s.CodeChanged || s.GateFailures > 0 || s.Corrections
}
