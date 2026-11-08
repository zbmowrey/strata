---
id: pytest-fixture-teardown
title: DB fixtures need explicit teardown
summary: Session-scoped DB fixtures leak connections unless torn down; tests then hang at exit.
type: gotcha
tags: [testing, pytest, database]
aliases: [fixture-leak, hanging-tests]
status: active
trust: observed
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: session, ref: "projects/ci-flake/log.md", at: 2026-06-01 }
applies_to: ["tests/**/conftest.py"]
scripts: [find-leaked-fixtures]
occurrences: 2
---
Session-scoped database fixtures that open a connection without a `yield` teardown leak it; pytest then hangs after the last test.

## Symptom
The suite prints its summary and never exits. `--timeout` does not fire because no test is running.

## Cause
The connection pool keeps a non-daemon thread alive. See [[postgres-connection-pool]].

## Fix
Yield the connection and close it after the yield. Scan for offenders with the `find-leaked-fixtures` script; procedure in [[debug-hanging-tests]].
