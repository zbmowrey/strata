---
id: fts5-contentless-delete
title: "FTS5 tables need contentless_delete=1"
summary: "Incremental refresh deletes rows from contentless FTS5 tables, which requires contentless_delete=1 (SQLite ≥ 3.43)."
type: fact
tags: [engine, sqlite, index]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/index/schema.go", at: 2026-11-08 }
applies_to: ["engine/internal/index/**"]
---
modernc.org/sqlite v1.62.0 bundles SQLite 3.54.1: FTS5 porter/unicode61, trigram and `contentless_delete=1` are available. Without the option, `DELETE FROM docs_fts WHERE rowid = ?` fails on a contentless table.
