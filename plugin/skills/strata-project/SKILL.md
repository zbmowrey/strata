---
name: strata-project
description: Strata project lifecycle for multi-session work — creating a project, keeping plan.md current, logging the chronicle (tried/failed/worked/learned/decided/blocked/next/note), writing handoff.md before stopping or compaction, resuming, pausing and closing. Use when work will span sessions, when resuming work, or before context compaction.
tags: [strata, projects]
---

# Projects: multi-session memory

Projects live in `projects/<slug>/` (local-only, gitignored): `project.md` (goal),
`plan.md`, `log.md` (append-only chronicle), `handoff.md` (where things stand).

## Start

```
strata resume                                   # what's active, each project's Next
strata project new ci-flake "Eliminate CI flakiness" --goal "50 green runs, no retries"
```

Keep `plan.md` a short checklist; edit it as the plan changes.

## Log as you go — negative knowledge matters most

```
strata project log ci-flake --tried "pytest -x --timeout 30 on the integration suite"
strata project log ci-flake --failed "raising the timeout only hides the hang"
strata project log ci-flake --learned "db_session fixture never closes its pool"
strata project log ci-flake --decided "explicit teardown instead of a longer timeout"
strata project log ci-flake --next "write find-leaked-fixtures"
```

Prefixes: tried, failed, worked, learned, decided, blocked, next, note. No slug → `_adhoc`.
Lines from one session within 30 minutes group into one dated entry. Durable lessons also
go to the knowledge base (`strata capture … --project ci-flake`).

## Handoff — before stopping, and when context is about to be compacted

`strata project handoff <slug>` rewrites `handoff.md`, pre-filling *Don't redo* from
`failed:` lines. Then make it accurate:

```markdown
## State      — what is done and verified, in 2–4 lines
## Next       — numbered, concrete first steps (commands, ids)
## Open threads
## Don't redo — dead ends and why
```

A fresh session must be able to continue from `strata resume` alone.

## Lifecycle

`strata project list` (stale flagged) · `strata project show <slug>` ·
`strata project pause <slug>` / `strata project activate <slug>` ·
`strata project close <slug>` (done → the gardener distills it into shards, then archives
it). Version a project file only on explicit request: `strata project track <slug> <glob>`.
