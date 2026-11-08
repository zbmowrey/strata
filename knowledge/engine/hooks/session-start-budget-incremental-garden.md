---
id: session-start-budget-incremental-garden
title: "Session start runs garden --auto incrementally"
summary: "SessionStart runs the gardener's safe fixes only when the index refresh found changes, and only for those paths (300 ms budget)."
type: decision
tags: [engine, hooks, garden, performance]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/garden/fixes.go", at: 2026-11-08 }
  - { kind: file, ref: "engine/internal/hooks/session.go", at: 2026-11-08 }
applies_to: ["engine/internal/garden/**", "engine/internal/hooks/**"]
---
A full `garden --auto` cost ~1 s at 10k shards. Now: `index.Stats.Paths` → per-file normalization of changed shards, hubs (read from the index, not files) when shards changed, catalogs/allowlist when scripts changed. 10 changed shards at 10k: 80–110 ms.
