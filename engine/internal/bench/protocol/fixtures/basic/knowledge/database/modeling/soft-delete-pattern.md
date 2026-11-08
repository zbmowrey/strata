---
id: soft-delete-pattern
title: Use deleted_at for soft deletes
summary: User-facing records are soft-deleted with a nullable deleted_at timestamp column.
type: standard
tags: [database, modeling]
status: active
trust: inferred
created: 2026-06-01
updated: 2026-10-01
---
Soft-deletable tables carry `deleted_at timestamptz NULL`, and queries filter on it.

## Rule
Never hard-delete user-facing rows.

## Rationale
Support restores mistaken deletions.

## Enforcement
none yet
