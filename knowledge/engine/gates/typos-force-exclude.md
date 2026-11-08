---
id: typos-force-exclude
title: "typos needs --force-exclude for explicit paths"
summary: "typos ignores typos.toml excludes for files passed on the command line unless --force-exclude is set."
type: gotcha
tags: [tooling, gates, typos]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/scaffold/templates/gates/docs.toml", at: 2026-11-08 }
---
## Symptom
The typos gate flagged intentional misspellings in excluded testdata.
## Cause
Explicit targets bypass `extend-exclude`.
## Fix
Gate command `typos --force-exclude --format brief {targets}`.
