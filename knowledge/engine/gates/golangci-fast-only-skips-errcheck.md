---
id: golangci-fast-only-skips-errcheck
title: "Edit-stage golangci (--fast-only) skips errcheck"
summary: "The edit-stage lint uses --fast-only, which excludes type-aware linters such as errcheck; they fire at stop."
type: gotcha
tags: [engine, gates, go]
status: active
trust: observed
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/testdata/engine-gates.toml", at: 2026-11-08 }
---
## Symptom
An unchecked error passes post-edit but blocks at Stop.
## Cause
`--fast-only` keeps edit latency under 2 s by skipping type-aware linters.
## Fix
By design; the Stop gate runs the full linter.
