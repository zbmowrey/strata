---
id: deprecation-header
title: Deprecated endpoints send a Sunset header
summary: Endpoints scheduled for removal send Deprecation and Sunset headers for 90 days.
type: fact
tags: [api, versioning]
status: superseded
trust: observed
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: session, ref: "projects/ci-flake/log.md", at: 2026-06-01 }
superseded_by: api-versioning
---
Deprecated endpoints send `Deprecation: true` and a `Sunset` date.
