---
id: pretooluse-additional-context
title: "PreToolUse additionalContext reaches the model"
summary: "hookSpecificOutput.additionalContext from a PreToolUse hook is shown to Claude before the tool runs."
type: fact
tags: [claude-code, hooks]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/hooks/edit.go", at: 2026-11-08 }
  - { kind: file, ref: "engine/internal/hooks/hooks.go", at: 2026-11-08 }
---
Verified live: the pre-edit advisory (`ℹ strata: … applies to this file`) emitted as `{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":…}}` was cited by Claude in its answer. Strata emits it once per path per session.
