---
id: request-id-propagation
title: Propagate the request id
summary: Every inbound request id is forwarded to downstream calls and logged on every line.
type: standard
tags: [api, http, observability]
aliases: [request-id]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
verified_at: 2026-10-01
---
The `X-Request-Id` header is accepted or generated, forwarded downstream, and logged per [[log-format-json]].

## Rule
No outbound call without the request id.

## Rationale
Traces across services need one key.

## Enforcement
none yet
