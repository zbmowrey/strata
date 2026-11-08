---
id: k8s-liveness-probe-timeout
title: Liveness probes killed slow-starting pods
summary: A 5 second liveness timeout restarted pods during cold start; use a startupProbe instead.
type: gotcha
tags: [k8s, probes]
status: active
trust: observed
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: session, ref: "projects/ci-flake/log.md", at: 2026-06-01 }
---
Pods restarted in a loop during deploys because the liveness probe ran before the app warmed up.

## Symptom
CrashLoopBackOff only on fresh nodes.

## Cause
Cold JIT plus cache warmup exceeded the liveness timeout.

## Fix
Add a `startupProbe` with a generous failure threshold.
