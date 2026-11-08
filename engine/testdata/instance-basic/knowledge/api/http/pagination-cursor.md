---
id: pagination-cursor
title: List endpoints use cursor pagination
summary: List endpoints page with opaque cursors instead of offsets to stay stable under inserts.
type: decision
tags: [api, http, pagination]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
verified_at: 2026-10-01
---
List endpoints return `next_cursor`; offset pagination is not offered.

## Context
Offsets skipped rows when items were inserted.

## Decision
Opaque base64 cursors over `(created_at, id)`.

## Alternatives
Keyset parameters exposed internals.

## Consequences
Clients cannot jump to page N.
