---
id: alembic-autogenerate-limits
title: Alembic autogenerate misses renames
summary: Autogenerate treats a column rename as drop plus add; hand-edit the migration to use alter_column.
type: gotcha
tags: [database, migrations, alembic]
status: active
trust: observed
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: session, ref: "projects/ci-flake/log.md", at: 2026-06-01 }
---
Autogenerate emits a drop and an add for a rename, which loses data.

## Symptom
Data in the renamed column disappears after migrating.

## Cause
Autogenerate cannot detect renames.

## Fix
Replace the pair with `op.alter_column(..., new_column_name=...)`.
