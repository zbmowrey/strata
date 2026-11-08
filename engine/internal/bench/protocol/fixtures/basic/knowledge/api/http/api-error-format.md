---
id: api-error-format
title: Errors use RFC 9457 problem details
summary: All API errors return application/problem+json with type, title, status, and detail.
type: standard
tags: [api, http, errors]
aliases: [problem-details]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
applies_to: ["app/api/**"]
verified_at: 2026-10-01
---
Error responses follow RFC 9457.

## Rule
Every 4xx and 5xx response is `application/problem+json`.

## Rationale
Clients parse one error shape.

## Enforcement
Contract tests in `tests/api/test_errors.py`.
