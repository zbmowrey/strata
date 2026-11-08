---
id: ci-cache-keys
title: CI cache keys must include the lockfile hash
summary: Caching dependencies without hashing uv.lock restores stale packages after an upgrade.
type: gotcha
tags: [ci, caching]
status: active
trust: observed
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: session, ref: "projects/ci-flake/log.md", at: 2026-06-01 }
---
A cache keyed only on the OS restores old wheels after `uv.lock` changes.

## Symptom
Tests fail in CI with import errors that do not reproduce locally.

## Cause
The cache key omitted `hashFiles('uv.lock')`.

## Fix
Key on the lockfile hash; see [[ci-pipeline-stages]].
