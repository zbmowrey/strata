---
id: stop-hook-decision-block
title: "Stop hooks block with a decision JSON on stdout"
summary: "A Stop hook prints {\"decision\":\"block\",\"reason\":…} to keep Claude working; the reason becomes its next instruction."
type: fact
tags: [claude-code, hooks]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/hooks/stop.go", at: 2026-11-08 }
---
Verified live: `strata hook stop` printing `{"decision":"block","reason":"…"}` made Claude continue with the reason. Strata bounds this with a loop guard (`[gates] stop_max_blocks`, same finding hash) and a once-per-session knowledge nudge.
