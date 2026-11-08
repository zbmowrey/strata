---
id: api-versioning
title: Version the API in the URL path
summary: Breaking changes ship under a new /v2 path; additive changes stay in the current version.
type: decision
tags: [api, versioning]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
verified_at: 2026-10-01
---
Breaking changes get a new path prefix such as `/v2`.

## Context
Header-based versioning confused caches.

## Decision
Path versioning, one prior version supported for six months.

## Alternatives
Media-type versioning.

## Consequences
Routers mount both versions during a migration.
