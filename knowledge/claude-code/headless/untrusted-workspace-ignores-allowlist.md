---
id: untrusted-workspace-ignores-allowlist
title: "Untrusted workspaces ignore settings.json permissions"
summary: "In a never-trusted directory, claude -p ignores .claude/settings.json permissions.allow; pass --allowedTools explicitly."
type: gotcha
tags: [claude-code, headless, evals, permissions]
status: active
trust: observed
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/perms/allow.go", at: 2026-11-08 }
---
## Symptom
stderr: `Ignoring N permissions.allow entries … this workspace has not been trusted`.
## Cause
Temp fixture repos were never opened interactively.
## Fix
Eval and smoke runs pass `--allowedTools` explicitly; do not rely on `strata perms sync` output there.
