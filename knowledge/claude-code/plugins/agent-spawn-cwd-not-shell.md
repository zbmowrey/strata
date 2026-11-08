---
id: agent-spawn-cwd-not-shell
title: "$.agent.spawn cwd does not set the subagent shell"
summary: "The cwd passed to $.agent.spawn does not become the subagent's Bash working directory; it starts in the session's tree."
type: gotcha
tags: [claude-code, plugins, garden]
status: active
trust: observed
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "plugin/hooks/garden.ts", at: 2026-11-08 }
---
## Symptom
In the live /garden run the gardener's `pwd` printed the main tree and `strata new shard` wrote there.
## Cause
`$.agent.spawn({cwd})` is not applied to the Bash tool.
## Fix
The gardener prompt and agent file require `strata --root <worktree>`, `git -C <worktree>` and absolute paths.
