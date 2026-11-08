---
id: hook-telemetry-field-hook
title: "Hook telemetry names the hook in field `hook`"
summary: "The hook telemetry event stores the hook name in `hook` because `event` is the envelope key."
type: decision
tags: [engine, telemetry]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/hooks/hooks.go", at: 2026-11-08 }
---
Using `event` silently overwrote the hook name with "hook". Spec 01 §8 amended.
