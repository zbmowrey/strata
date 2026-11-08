---
id: k8s-resource-limits
title: Every container declares requests and limits
summary: All Kubernetes containers set CPU and memory requests and limits; the admission policy rejects others.
type: standard
tags: [k8s, standards]
aliases: [resource-limits]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
applies_to: ["deploy/helm/**/templates/*.yaml"]
verified_at: 2026-10-01
---
Each container spec declares `resources.requests` and `resources.limits`.

## Rule
No container without both requests and limits.

## Rationale
Unbounded pods evicted neighbors during load tests.

## Enforcement
The `require-limits` admission policy.
