---
id: id-collision-kind-rank
title: "Cross-kind id collisions resolve by kind rank"
summary: "When two kinds share an id, shard < hub < question < script < playbook < inbox < project keeps the bare id; the other becomes <kind>:<id>."
type: decision
tags: [engine, index, ids]
aliases: [kind-rank]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/index/writer.go", at: 2026-11-08 }
applies_to: ["engine/internal/index/**"]
---
Resolution is order-independent and reversible: deleting the holder gives the bare id back to the best-ranked prefixed doc (unshadow). `strata show script:<id>` addresses the loser.
