---
id: postgres-vacuum-settings
title: Autovacuum tuning for hot tables
summary: Hot tables use autovacuum_vacuum_scale_factor 0.02 to keep bloat under control.
type: reference
tags: [database, postgres, performance]
status: active
trust: observed
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: url, ref: "https://wiki.example.com/ops", at: 2026-06-01 }
---
The `events` and `sessions` tables set `autovacuum_vacuum_scale_factor = 0.02`.
