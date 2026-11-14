---
id: leading-dash-negates-term
title: A leading dash negates a search term
summary: "In strata search, 'claude -p' excludes docs containing 'p'; quote it (\"claude -p\") to search for the flag."
type: gotcha
tags: [engine, search, query]
status: active
trust: verified
created: 2026-11-14
updated: 2026-11-14
verified_at: 2026-11-14
sources:
  - { kind: session, ref: "825a4ac1-dbb7-59f6-8fc4-5ea5702044b9", at: 2026-11-08 }
  - { kind: file, ref: "engine/internal/search/query.go", at: 2026-11-14 }
---
Searching for a CLI flag like `claude -p` silently excludes every document containing `p` instead of matching the flag.

## Symptom

`strata search 'claude -p'` returns few or unrelated hits.

## Cause

The query lexer (`lexOne` in `engine/internal/search/query.go`) treats `-` followed by a non-space character as negation of the next term.

## Fix

Quote the phrase: `strata search '"claude -p"'`.
