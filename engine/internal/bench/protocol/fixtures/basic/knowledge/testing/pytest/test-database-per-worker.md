---
id: test-database-per-worker
title: Each xdist worker uses its own database
summary: The test harness creates app_test_gw0..gwN databases so parallel workers never share state.
type: fact
tags: [testing, database]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
applies_to: ["tests/conftest.py"]
verified_at: 2026-10-01
---
`conftest.py` names the database `app_test_<worker_id>` and creates it from the template database at session start.

## Why
Shared databases made tests order-dependent; see [[flaky-test-quarantine]].
