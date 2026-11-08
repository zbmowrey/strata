---
id: bm25-normalize-divide-by-best
title: "BM25 scores are normalized by the best score"
summary: "Search divides each BM25 score by the query's best score; min–max normalization was tried and replaced."
type: decision
tags: [engine, search, ranking]
aliases: [ranking]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/search/rank.go", at: 2026-11-08 }
  - { kind: file, ref: "docs/spec/03-index-and-search.md", at: 2026-11-08 }
applies_to: ["engine/internal/search/**"]
---
Min–max collapsed single-hit queries to 0. Divide-by-best keeps the top hit at 1.0, then synonyms (×0.7), fuzzy (max of trigram Dice and OSA edit similarity ≥ 0.6) and status/trust/stale/kind multipliers apply; exact id/alias short-circuits first. Ties: score desc, updated desc, id asc.
