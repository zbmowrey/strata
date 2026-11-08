---
id: rate-limit-headers
title: Rate limits are reported in headers
summary: Responses include RateLimit-Limit, RateLimit-Remaining and RateLimit-Reset headers.
type: fact
tags: [api, http, rate-limiting]
status: active
trust: observed
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: session, ref: "projects/ci-flake/log.md", at: 2026-06-01 }
---
Every response carries `RateLimit-Limit`, `RateLimit-Remaining`, and `RateLimit-Reset`.
