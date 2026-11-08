---
id: http-timeouts
title: Outbound HTTP calls need explicit timeouts
summary: httpx has no default timeout in our client wrapper; a hung partner API exhausted all workers.
type: gotcha
tags: [api, http, timeouts]
status: active
trust: observed
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: session, ref: "projects/ci-flake/log.md", at: 2026-06-01 }
applies_to: ["app/clients/**"]
---
Calls without a timeout blocked workers indefinitely during a partner outage.

## Symptom
All workers busy, zero CPU.

## Cause
The wrapper passed `timeout=None`.

## Fix
Use `timeout=httpx.Timeout(10, connect=3)` everywhere.
