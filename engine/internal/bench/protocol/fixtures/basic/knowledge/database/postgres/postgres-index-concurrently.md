---
id: postgres-index-concurrently
title: Create indexes concurrently in production
summary: Index creation on large tables uses CREATE INDEX CONCURRENTLY outside a transaction.
type: standard
tags: [database, postgres, migrations]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
verified_at: 2026-10-01
---
Large-table indexes are built with `CREATE INDEX CONCURRENTLY`.

## Rule
No plain `CREATE INDEX` on tables over one million rows.

## Rationale
Plain index builds lock writes for minutes.

## Enforcement
none yet; see [[migration-review-checklist]].
