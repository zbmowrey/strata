---
type: gotcha
captured: 2026-11-14T11:38:41-05:00
session: 17c2c802-c35e-441e-b738-5a19d648a2dc
project: garden-autonomy
sources:
  - { kind: file, ref: "engine/internal/garden/knowledge.go", at: 2026-11-14 }
  - { kind: doc, ref: "docs/spec/13-decisions.md#D-033", at: 2026-11-14 }
---
In a young knowledge base, garden fix-orphan floods the plan. orphans() already counted only non-hub inbound links, which is correct, but it had no age gate. Shards rarely link to each other in their first weeks, so on 2026-11-14 31 of 31 shards were flagged (31 of 35 plan tasks). The old spec 07 §2 wording ("not in a hub section") never fires, because every shard is in a generated hub. Fixed per D-033: shards younger than [garden] orphan_min_age_days (default 30) are skipped, and this repo's plan dropped from 35 tasks to 6.
