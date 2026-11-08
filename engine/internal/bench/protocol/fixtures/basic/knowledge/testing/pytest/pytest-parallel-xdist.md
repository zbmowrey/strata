---
id: pytest-parallel-xdist
title: Run the suite in parallel with xdist
summary: Use pytest -n auto with per-worker database names to parallelize the integration suite.
type: howto
tags: [testing, pytest, performance]
aliases: [xdist]
status: active
trust: observed
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: session, ref: "projects/ci-flake/log.md", at: 2026-06-01 }
---
Run `pytest -n auto`; each worker gets its own database via the `worker_id` fixture.

1. Install `pytest-xdist`.
2. Derive the DB name from `worker_id` in [[test-database-per-worker]].
3. Run `pytest -n auto --dist loadfile`.
