---
id: helm-values-layering
title: Helm values are layered base, env, then secrets
summary: Charts merge values.yaml, values-<env>.yaml, then secret references, in that order.
type: fact
tags: [k8s, helm, deploy]
aliases: [values-files]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
applies_to: ["deploy/helm/**"]
verified_at: 2026-10-01
---
The chart applies `values.yaml`, then `values-<env>.yaml`, then the external secrets manifest.

## See also
- [[k8s-resource-limits]]
