---
name: strata-garden
description: The Strata gardener's operating manual — how to execute a garden plan (.strata/garden/plan.json) task by task under the autonomy matrix, commit per category, verify, raise questions and write the PR digest; also how to schedule /garden. Use when running /garden, acting as the strata:gardener agent, or setting up recurring gardening.
tags: [strata, garden]
---

# Gardening a Strata instance

`/garden` = `strata garden start` (lock, worktree, plan, mechanical fixes committed) →
you work the plan **in that worktree** → `strata garden finish --body-file <digest>` (push,
draft PR, mark ready, clean up). `strata garden abort` throws the run away.
Never merge. Never read `knowledge/_private/`. Never touch the human's working tree.

## Task recipes (plan order)

| Type                        | Do                                                                                                                                                                                                                 |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `file-inbox`                | `strata search` the claim; merge into an existing shard or `strata new shard …`; delete the inbox file.                                                                                                            |
| `merge-duplicates`          | Keep the better-sourced shard; fold the other's evidence into it; `strata supersede <loser> <keeper>` (delete only an exact duplicate). Either side `user-stated` → question.                                      |
| `split-oversize`            | Split into focused shards linked both ways; the original id keeps the core claim.                                                                                                                                  |
| `fix-orphan`                | Link it from related shards' See also, or merge it into one.                                                                                                                                                       |
| `reverify-stale`            | `verify_script` in evidence → `strata run <script>`; pass → bump `verified_at`; fail → correct (`observed`/`inferred`) or question (`user-stated`). No script → re-check sources/code.                             |
| `archive-cold`              | `strata archive <id>` (never delete).                                                                                                                                                                              |
| `fill-gap`                  | Find the answer in code/docs; write the shard; note the query for `bench-grow`.                                                                                                                                    |
| `distill-project`           | Read the chronicle; durable facts/gotchas/decisions → shards with `kind: session` sources; list them in `project.md` `shards:`. Never close a project yourself.                                                    |
| `promote`                   | `evidence.to`: `gotcha` (correction ≥ 2), `gate` (ratchet rule below), `script` (via scriptsmith), `playbook` (`strata new playbook <name>` linking the shards). Link both ways (`scripts:`/`gates:` ↔ `shards:`). |
| `scriptify`                 | Write the script for the repeated command (scriptsmith protocol).                                                                                                                                                  |
| `fix-broken-link`           | Point the link at the right id or remove it.                                                                                                                                                                       |
| `resolve-conflict`          | Confirm from code/sources; correct the weaker claim; equal trust → question.                                                                                                                                       |
| `apply-answer`              | Apply the human's answer (under `## Answer`) to the shards it concerns.                                                                                                                                            |
| `stale-project`             | Always a question: pause, close or continue?                                                                                                                                                                       |
| `deprecated-script-removal` | Delete the script dir; repoint references to `replaced_by`; `strata scripts catalog`.                                                                                                                              |
| `bench-grow`                | Add the query with its relevant ids to `.strata/bench/retrieval.yaml`.                                                                                                                                             |

## Autonomy

**Act:** filing, merging non-user-stated duplicates, splits, orphans, links, derived files;
re-verifying and correcting `observed`/`inferred` shards from evidence; archiving cold
shards; distilling projects; **new** scripts (gates green, tests included) and additive
changes to `owner: gardener` scripts; gates under the ratchet rule; advisory → blocking
when the codebase is clean; growing the bench; removing zero-use deprecated scripts.

**Ratchet rule:** a new gate is `blocking` only if the codebase has zero findings or every
finding is auto-fixed in this PR; otherwise `blocking = false` or a baseline.

**Ask (do not act):** anything that would overwrite, contradict or delete a `user-stated`
shard; equal-trust conflicts; deleting anything but exact duplicates / zero-use deprecated
scripts; `user`-owned scripts, interfaces, output contracts, side-effect classes;
`network`/`destructive` script behavior; `_private/`; closing or pausing a project; a
blocking gate whose findings can't be fixed mechanically; low confidence.
Tasks with `escalate: true` are always questions.

```
strata questions ask "merge a + b? both user-stated and they disagree on X" --blocking a,b
```

## Commits

One per category, conventional, body = task ids + evidence, repo attribution trailers:
`garden(knowledge): file 7 inbox items`, `garden(scripts): add find-leaked-fixtures`,
`garden(gates): add semgrep fixture-teardown rule (advisory)`, `garden(bench): +3 queries`.

## Verify before finishing

`strata check --all` green; `strata bench retrieval`: MRR drop ≤ `[garden] bench_tolerance`
(0.02), recall@5 not lower. Failure → fix, or `git revert` the offending commit and record
why under Skipped / deferred.

## Digest (PR body)

The engine renders the PR body from your `strata garden task … --note` lines, grouped by
outcome, plus `.strata/garden-summary.md` if you write one. In an interactive `/garden`
your final report becomes the body instead. Either way it follows this shape:

```markdown
## Garden 2026-11-08 06:00

**Needs you:** 1 question → knowledge/_questions/q-20261108-fixture-scope.md

| Area        | Changes                                                                            |
| ----------- | ---------------------------------------------------------------------------------- |
| Knowledge   | filed 7 inbox items (3 new shards, 4 merged), merged 2 duplicates, archived 4 cold |
| Corrections | 2 shards corrected from evidence                                                   |
| Scripts     | + find-leaked-fixtures (read-only, Go)                                             |
| Gates       | + semgrep:fixture-teardown (advisory; 3 existing findings)                         |
| Bench       | MRR 0.84 → 0.86 · recall@5 0.95 → 0.96 · +3 queries                                |

### Learned this cycle

- [[pytest-fixture-teardown]] — Session-scoped DB fixtures leak connections unless torn down.

### Skipped / deferred

- merge `a` + `b`: both user-stated → question raised.
```

## Scheduling

Strata installs no schedules. When asked, create a scheduled task (cloud routine or local
cron/loop) whose prompt is `/garden`. `strata garden --auto` (mechanical fixes only)
already runs at SessionStart.

With `[garden] auto_launch = "on"` (opt-in, D-031) the SessionStart hook also launches a
detached headless run (`strata garden launch`) when garden debt is due, and resumes a lost
run. `strata garden status` shows the lease, progress and history; `strata garden resume
--force` or `strata garden abort` settle a run marked stuck.
