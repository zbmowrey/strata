---
id: jwt-clock-skew
title: JWT validation needs clock skew leeway
summary: Tokens issued by the IdP were rejected as not yet valid until a 30 second leeway was added.
type: gotcha
tags: [api, auth, jwt]
status: active
trust: observed
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: session, ref: "projects/ci-flake/log.md", at: 2026-06-01 }
applies_to: ["app/auth/**"]
---
Token validation rejected fresh tokens because the IdP clock ran ahead.

## Symptom
Intermittent 401 responses right after login.

## Cause
`nbf` was a few seconds in the future.

## Fix
Validate with `leeway=30`.
