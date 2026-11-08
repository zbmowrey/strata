---
id: migration-review-checklist
title: Review checklist for migrations
summary: Check locks, backfills, defaults, and reversibility before approving a migration.
type: howto
tags: [database, migrations, review]
status: active
trust: observed
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: session, ref: "projects/ci-flake/log.md", at: 2026-06-01 }
---
Before approving a migration:

1. No long locks; see [[postgres-index-concurrently]].
2. Backfills run in batches.
3. New columns are nullable or have a constant default.
4. A downgrade exists.
