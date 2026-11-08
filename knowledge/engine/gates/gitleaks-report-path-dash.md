---
id: gitleaks-report-path-dash
title: "gitleaks must report to \"-\", not /dev/stdout"
summary: "Under a git hook with piped stdout, gitleaks cannot open /dev/stdout; use --report-path - ."
type: gotcha
tags: [tooling, gates, gitleaks]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/scaffold/templates/gates/security.toml", at: 2026-11-08 }
---
## Symptom
`secrets/tool-failed: open /dev/stdout: permission denied` during `garden(fixes)` commits.
## Cause
/dev/stdout is not openable for the hook's piped stdout.
## Fix
`--report-path -` (gitleaks 8.30).
