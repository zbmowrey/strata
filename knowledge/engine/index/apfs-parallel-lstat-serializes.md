---
id: apfs-parallel-lstat-serializes
title: "Parallel lstat walks do not scale on APFS"
summary: "On macOS/APFS, parallel lstat over a 10k-file tree serializes; the darwin walker uses getattrlistbulk with openat instead."
type: gotcha
tags: [engine, macos, performance, index]
aliases: [getattrlistbulk]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/corpus/walk_darwin.go", at: 2026-11-08 }
applies_to: ["engine/internal/corpus/**"]
---
## Symptom
Freshness check ≈ 20 ms over its 15 ms budget despite worker pools.
## Cause
APFS serializes per-directory metadata reads.
## Fix
`getattrlistbulk` via `unix.Syscall6` + `openat` per directory (4 workers): p95 ≈ 11 ms. Do not revert to a parallel lstat walk.
