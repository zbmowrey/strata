---
id: coverage-floor
title: Coverage must stay at or above 85 percent
summary: Line coverage for the app package must not drop below 85 percent; CI enforces the floor.
type: standard
tags: [ci, testing, coverage]
status: active
trust: user-stated
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: user, ref: "agreed in planning", at: 2026-06-01 }
---
Coverage of `app/` is at least 85%.

## Rule
Pull requests may not lower total coverage below the floor.

## Rationale
The floor caught two untested migrations last year; see [[migration-rollback]].

## Enforcement
`pytest --cov-fail-under=85` in the unit stage.
