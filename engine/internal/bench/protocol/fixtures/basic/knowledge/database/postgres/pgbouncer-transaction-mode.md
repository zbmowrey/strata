---
id: pgbouncer-transaction-mode
title: PgBouncer transaction mode breaks prepared statements
summary: In transaction pooling mode, server-side prepared statements fail; disable them in the driver.
type: gotcha
tags: [database, postgres, pgbouncer]
status: active
trust: observed
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: session, ref: "projects/ci-flake/log.md", at: 2026-06-01 }
---
Server-side prepared statements do not survive transaction pooling.

## Symptom
`prepared statement "_pg3_0" does not exist` under load.

## Cause
Consecutive transactions land on different server connections.

## Fix
Set `prepare_threshold=None` in the driver.
