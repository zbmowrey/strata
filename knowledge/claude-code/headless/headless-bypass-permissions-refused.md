---
id: headless-bypass-permissions-refused
title: "claude -p with bypassPermissions is refused"
summary: "Headless runs with --permission-mode bypassPermissions are denied; use acceptEdits plus a scoped --allowedTools list."
type: gotcha
tags: [claude-code, headless, evals]
status: active
trust: observed
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/bench/protocol.go", at: 2026-11-08 }
---
## Symptom
`claude -p … --permission-mode bypassPermissions` is denied by the safety classifier.
## Cause
Unattended full bypass is not allowed.
## Fix
Use `--permission-mode acceptEdits --allowedTools "Read,Write,Edit,Bash(strata:*),…"`.
