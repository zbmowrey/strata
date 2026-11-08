---
name: strata-search
description: How to retrieve from Strata efficiently — the retrieval ladder (search → show → links → code), the query language and filters, batching ids into one show, context for files, and finding scripts and past project work. Use before answering questions about this codebase, editing an unfamiliar area, debugging, or running an ad-hoc command.
tags: [strata, search]
---

# Searching Strata

## Ladder — stop at the first rung that answers

1. `strata search '<terms>'` — ranked one-line hits (shards, scripts, playbooks, projects).
2. `strata show <id> <id> …` — **one** call with every id you need, in order.
   `strata show <id>#<section>` · `--brief` (title, summary, first paragraph) ·
   `--budget 1500` (cap tokens) · `--meta` (frontmatter only).
3. `strata show <id> --links 1` or `strata related <id>` — the neighborhood.
4. `strata context <path>…` — knowledge and scripts whose `applies_to` matches files.
5. Code and the web — then capture what you learned.

`strata map [domain] --depth 3` shows the shape of the knowledge base.

## Query language

Whitespace = AND · `OR` (uppercase) · `"exact phrase"` · `-term` excludes · `term*` prefix.

| Filter | Example |
|---|---|
| `kind:` | `kind:script`, `kind:project`, `kind:playbook` (repeat = OR) |
| `type:` | `type:gotcha`, `type:decision`, `type:standard` |
| `tag:` / `-tag:` | `tag:pytest -tag:legacy` |
| `domain:` / `topic:` | `domain:testing` |
| `status:` / `trust:` | `status:any`, `trust:>=verified` |
| `side:` / `runtime:` | `side:read-only`, `side:<=writes-local`, `runtime:go` |
| `updated:` `created:` `verified:` | `updated:>30d`, `updated:2026-01-01..2026-06-30` |
| `stale:` / `path:` / `project:` / `meta.<k>:` | `stale:true`, `path:knowledge/infra/**` |

Every filter has a flag (`--kind`, `--tag`, …); `--explain` shows scoring; `--limit N`.

## Recipes

```
strata search 'connection leak OR hang type:gotcha'          # known traps
strata search 'deploy config kind:script side:read-only'     # a script for the job
strata search 'flaky retries kind:project'                   # did we try this before?
strata show pytest-fixture-teardown postgres-connection-pool --brief
strata context tests/conftest.py app/db.py
```

No results → loosen (drop filters, synonyms, `term*`), then fall back to code. If the
answer came from outside the knowledge base, `strata capture` it. Prefer scripts over
improvising: `strata run <id> -- <args>`.
