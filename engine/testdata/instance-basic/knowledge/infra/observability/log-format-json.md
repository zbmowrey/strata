---
id: log-format-json
title: Services log JSON lines
summary: All services emit one JSON object per line with ts, level, msg, and request_id fields.
type: standard
tags: [logging, observability]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
verified_at: 2026-10-01
---
Logs are JSON lines with `ts`, `level`, `msg`, and `request_id`.

## Rule
No plain-text logging in services.

## Rationale
The log pipeline parses JSON only.

## Enforcement
none yet
