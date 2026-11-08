# 05 — Projects

Projects are Strata's working and episodic memory: a local-only folder per piece of multi-session work holding the plan, an append-only chronicle, and a handoff note so any session (or any context window after compaction) can resume exactly where the last one stopped.

## 1. Principles

- **Local-only by default.** `projects/` is gitignored. Versioning a project file is a rare, explicit, per-project opt-in (§5). (D-013)
- **Chronicle captures negative knowledge.** What was tried and failed is as valuable as what worked; fact stores lose it, project logs keep it.
- **Projects feed the KB.** Closing a project triggers distillation: durable facts, gotchas, and decisions move into `knowledge/`; the rest is archived.
- **Searchable.** Project files and individual log entries are indexed (`kind:project`), so "did we try this before?" is one search.

## 2. Layout

```
projects/
├── <slug>/
│   ├── project.md     # identity + status (frontmatter) + one-paragraph goal
│   ├── plan.md        # the current plan; rewritten freely as it evolves
│   ├── log.md         # chronicle; append-only dated entries
│   ├── handoff.md     # resume point; overwritten at each handoff
│   ├── scratch/       # throwaway working files (never indexed)
│   └── out/           # deliverables produced by the project (indexed by filename only)
├── _adhoc/            # a standing project for sessions not tied to a named project
└── _archive/<slug>/   # closed projects
```

## 3. Files

### 3.1 `project.md`

```yaml
---
id: ci-flake                 # slug == folder name
title: Eliminate CI flakiness in the integration suite
status: active               # active|paused|done|archived
goal: Integration suite passes 50 consecutive CI runs with no retries.
created: 2026-11-07
updated: 2026-11-07
tags: [ci, testing]
shards: []                   # shards produced by distillation (filled at close)
track: []                    # globs (relative to the project dir) to version-control; rare (§5)
---
Free-form context: why this project exists, constraints, links.
```

### 3.2 `log.md` — the chronicle

Append-only. One entry per meaningful step (not per tool call). Written via `strata project log` (which timestamps, attributes the session, and appends atomically) or by direct append.

```markdown
## 2026-11-07 21:40 · s:3f9a
- tried: `pytest -x --timeout 30` on the integration suite
- failed: hang persists at session teardown; timeout never fires (it's after the last test)
- learned: hang is in fixture finalization, not in a test → capture as gotcha
- decided: add explicit teardown to `db_session` rather than raising the timeout
- next: write `find-leaked-fixtures` to scan for the same pattern elsewhere
```

Entry prefixes are a fixed vocabulary (lint-enforced as a warning): `tried`, `failed`, `worked`, `learned`, `decided`, `blocked`, `next`, `note`. `learned:` and `decided:` lines are the gardener's distillation candidates.

### 3.3 `handoff.md`

Overwritten (not appended) at each handoff point: end of session, before compaction (PreCompact hook nudge), or when switching projects. Fixed shape, ≤ 40 lines:

```markdown
---
updated: 2026-11-07T22:15:00-05:00
session: 3f9a
---
## State
One paragraph: where things stand.
## Next
1. Concrete next action (with the exact command if there is one).
## Open threads
- Unresolved question / uncertainty.
## Don't redo
- Things already ruled out (with log anchors).
```

### 3.4 `plan.md`

Free-form markdown; Claude rewrites it whenever the plan changes (git-style history is unnecessary: the log records why it changed).

## 4. Lifecycle

```
strata project new <slug> "<title>" --goal "…"   → active
strata project pause <slug>                       → paused
strata project resume [<slug>]                    → prints handoff (+ plan headings) for one or all active projects
strata project close <slug>                       → done; queues a `distill` garden task
(gardener distills, links shards in project.md)   → archived (moved to _archive/)
```

- `strata resume` (alias used by the SessionStart hook) prints, for each `active` named project (not `_adhoc`), most recently active first: title, last activity and its age, and the `## Next` section of its handoff — compact (≤ 15 lines per project, ≤ 3 projects; the rest are summarized as a count line).
- `strata project log` groups lines from the same session into the current entry while it is < 30 minutes old; otherwise it starts a new `## YYYY-MM-DD HH:MM · s:<session4>` entry (`s:cli` when no session id is known). Writes take a per-project lock (`projects/<slug>/.lock`).
- `strata project activate <slug>` reverses `pause`.
- `_adhoc` is always present; captures and logs without `--project` go there. The gardener distills `_adhoc/log.md` periodically and truncates entries older than 30 days after distillation.
- A project with no log entry for `[projects] stale_after` (default 30 days) is flagged by `strata project list` and becomes a gardener task (ask: pause/close?). The gardener never closes a project autonomously — it raises a question (closing is a scope decision).

## 5. Version-control opt-in (`track`)

Default: everything under `projects/` is ignored. A project may list globs in `track:` (relative to its folder), e.g. `track: [out/report.md, plan.md]`. `strata gitignore sync` (run by `project` commands, `doctor`, and the gardener) regenerates a managed block in the repo's `.gitignore`:

```gitignore
# >>> strata:projects (GENERATED — edit `track:` in projects/<slug>/project.md)
/projects/**
!/projects/**/
!/projects/ci-flake/out/report.md
!/projects/ci-flake/plan.md
# <<< strata:projects
```

Why this shape: git cannot re-include a file whose parent directory is excluded. `/projects/**` ignores everything; `!/projects/**/` re-includes all directories (directories alone are never committed, so this tracks nothing by itself); the explicit `!` lines then re-include exactly the opted-in files. The generator validates globs, rejects any that would track `scratch/`, and has tests that assert `git check-ignore` results against a fixture repo.

## 6. Commands (summary; full reference in 09)

`strata project new|list|show|log|handoff|pause|resume|close|track` · `strata resume` · `strata gitignore sync`.

`strata project handoff <slug>` opens a templated `handoff.md` rewrite: it pre-fills `## Don't redo` from `failed:` log lines since the last handoff, so the handoff is never written from memory alone.
