---
id: garden-flow-engine-helpers
title: "How /garden runs"
summary: "strata garden start (worktree + plan + fixes commit) → gardener agent → strata garden finish (push, draft PR, ready) — never merge."
type: howto
tags: [garden, workflow]
aliases: [garden-workflow]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/garden/session.go", at: 2026-11-08 }
  - { kind: file, ref: "docs/spec/07-gardener.md", at: 2026-11-08 }
applies_to: ["engine/internal/garden/**", "plugin/hooks/garden.ts"]
---
1. `strata garden start --format json` — run marker `.strata/garden.lock`, worktree `.strata/worktrees/garden-<stamp>`, branch from `origin/<default>`.
2. The `strata:gardener` agent works the plan with `--root <worktree>`, one commit per category.
3. `strata check --all` + `strata bench retrieval`.
4. `strata garden finish --body-file <digest>` — push, draft PR, mark ready, clean up. `strata garden abort` abandons.
