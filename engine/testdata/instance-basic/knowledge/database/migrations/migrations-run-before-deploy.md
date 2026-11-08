---
id: migrations-run-before-deploy
title: Migrations run before the new image deploys
summary: Schema migrations run in a pre-deploy job, so new code always sees the new schema.
type: decision
tags: [database, migrations, deploy]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
verified_at: 2026-10-01
---
Migrations run in a Helm pre-upgrade hook job.

## Context
Running migrations at app start raced between replicas.

## Decision
A single pre-upgrade job runs `alembic upgrade head`.

## Alternatives
Running at app start with an advisory lock was more fragile.

## Consequences
Migrations must be backward compatible with the old code; see [[deploy-flow]].
