---
id: missing-tool-is-warning
title: "A missing gate tool is a warning, not a block"
summary: "Gates report a missing tool or timeout as a non-blocking warning; a failing tool with unparsable output yields one blocking tool-failed finding."
type: decision
tags: [engine, gates]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/gates/runner.go", at: 2026-11-08 }
---
Rationale: a fresh clone without every tool must still be workable; `strata doctor` lists missing tools with install commands. Hooks never rewrite baselines; only `strata check` ratchets.
