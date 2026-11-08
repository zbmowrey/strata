---
id: hooks-fail-open
title: "Strata hooks fail open"
summary: "Every hook turns internal errors and panics into exit 0 with a one-line warning; only intended blocks exit 2."
type: standard
tags: [engine, hooks]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/hooks/hooks.go", at: 2026-11-08 }
  - { kind: file, ref: "engine/internal/hooks/hooks_test.go", at: 2026-11-08 }
applies_to: ["engine/internal/hooks/**"]
---
## Rule
A hook must never break a session because of Strata itself.
## Rationale
Hooks run on every edit and stop.
## Enforcement
`hooks.Run` recovers panics; `TestFailOpen` covers bad JSON, missing instance, gate errors and panics.
