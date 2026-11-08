---
id: garden-task-ids
title: "Garden task ids"
summary: "Plan task ids are <type>:<targets joined by +>, qualified with #detail when one target has several tasks of a type."
type: reference
tags: [garden]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/garden/plan.go", at: 2026-11-08 }
---
Telemetry targets: `query:<normalized query>`, `bash:<hash>`; projects: `project:<slug>`. Tasks sort by spec table order then id; `[garden] escalate` sets `escalate: true` and risk `question`.
