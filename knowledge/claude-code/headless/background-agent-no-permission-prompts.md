---
id: background-agent-no-permission-prompts
title: "Background agents cannot ask for permission"
summary: "A spawned background subagent gets compound Bash commands (cd && …) denied; run plain commands and address paths explicitly."
type: gotcha
tags: [claude-code, agents, garden]
status: active
trust: observed
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "plugin/agents/gardener.md", at: 2026-11-08 }
  - { kind: file, ref: "plugin/hooks/garden.ts", at: 2026-11-08 }
---
## Symptom
The gardener's `cd <worktree> && git …` was denied: "Permission prompts are not available in this context".
## Cause
Allowlist rules match single commands; background agents cannot prompt for the rest.
## Fix
One command per call; `strata --root <wt>`, `git -C <wt>`.
