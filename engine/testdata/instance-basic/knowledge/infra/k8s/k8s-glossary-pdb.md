---
id: k8s-glossary-pdb
title: PodDisruptionBudget
summary: A PodDisruptionBudget caps how many replicas voluntary disruptions may take down at once.
type: glossary
tags: [k8s, glossary]
aliases: [pdb]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
verified_at: 2026-10-01
---
A PDB limits voluntary evictions (drains, upgrades) so at least `minAvailable` replicas stay up.
