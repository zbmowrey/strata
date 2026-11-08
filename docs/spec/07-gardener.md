# 07 — Gardener

The gardener is Strata's metabolism: the process that consolidates, repairs, reorganizes, and hardens the instance — autonomously — and delivers every change as a reviewable pull request. It comes to the human only for genuine conflict, risk, or ambiguity (D-010).

## 1. Two phases

| Phase | Who | Cost | Trigger |
|---|---|---|---|
| **Deterministic** (`strata garden`) | the Go engine | free (no model calls), ≤ 5 s | every `/garden`; also its safe subset (`--auto`) at SessionStart when the index refreshes, and in CI |
| **Judgment** (`/garden`) | the `strata:gardener` subagent, driven by the plugin command | tokens | on demand via `/garden`; optionally on a schedule the user sets up |

The deterministic phase does every mechanical fix itself and produces a **plan** (`.strata/garden/plan.json`) of tasks that need judgment. The judgment phase executes the plan.

## 2. Deterministic phase

`strata garden [--plan] [--auto] [--format json]`:

**Auto-applied mechanical fixes** (`--auto` applies them in the working tree; within `/garden` they are applied in the garden worktree):

- Rebuild/refresh the index; prune telemetry past retention.
- Regenerate hubs, script catalogs, the `.gitignore` projects block, the permission allowlist.
- Fix broken wikilinks with an unambiguous target (renamed id present in `aliases`; single trigram match ≥ 0.9).
- Normalize frontmatter (key order, date formats, tag case, trailing whitespace) — content-preserving.
- Remove fixed findings from gate baselines; delete empty baselines older than 14 days.

**Analyses → plan tasks:**

| Task type | Detection | Default risk |
|---|---|---|
| `file-inbox` | each `_inbox/` item | low |
| `merge-duplicates` | title/alias trigram similarity ≥ 0.8 **or** body MinHash Jaccard ≥ 0.6 between two shards | low (medium if either is `user-stated`) |
| `split-oversize` | size-limit warnings/errors | low |
| `fix-orphan` | shard with no inbound links and not in a hub section | low |
| `reverify-stale` | stale per [02](02-knowledge.md) §7; includes `verify.script` if present | low (high if verification fails on `user-stated`) |
| `archive-cold` | cold per [02](02-knowledge.md) §7 | low |
| `fill-gap` | zero-result queries recurring ≥ 2× in 30 days | low |
| `distill-project` | project closed, or `_adhoc` log entries > 30 days old | low |
| `promote` | see §4 | varies |
| `scriptify` | `bash` telemetry: same normalized command hash ≥ 3× across ≥ 2 sessions | low |
| `fix-broken-link` | ambiguous broken links | medium |
| `resolve-conflict` | two active shards with contradicting claims (detected heuristically: same `applies_to`/tags + negation/antonym patterns; and confirmed by the agent) | high |
| `apply-answer` | `_questions/` items newly `answered` | low |
| `stale-project` | active project, no log activity in 30 days | → question |
| `deprecated-script-removal` | deprecated script, 0 runs in 90 days | low |
| `bench-grow` | resolved `fill-gap` tasks → new bench queries | low |

Each task: `{id, type, risk, targets[], evidence{…}, suggested_action}`. Plan output is deterministic and diffable.

## 3. Judgment phase (`/garden`)

The plugin's `/garden` command runs:

1. **Lock.** Acquire `.strata/garden.lock` (one garden at a time per repo); abort with a message if held.
2. **Plan.** `strata garden --plan --format json`. If the plan is empty and no mechanical fixes were applied, report "garden is tidy" and stop.
3. **Worktree.** Create a git worktree at `.strata/worktrees/garden-<YYYYMMDD-HHMM>` on a new branch `garden/<YYYYMMDD-HHMM>` from the default branch's latest remote head. The human's working tree is never touched (D-011).
4. **Execute.** Spawn the `strata:gardener` subagent in the worktree with the plan. It works task by task under the autonomy matrix (§5), using the same CLI, scripts, and gates as any session.
5. **Commit.** One commit per task category, conventional style: `garden(knowledge): file 7 inbox items`, `garden(scripts): add find-leaked-fixtures`, `garden(gates): promote fixture-teardown rule (advisory)`. Each body lists task ids and evidence. Attribution trailers per the repository's convention.
6. **Verify.** `strata check --all` (all blocking gates green) and `strata bench retrieval` (MRR must not drop by more than `[garden] bench_tolerance`, default 0.02; recall@5 must not drop). Failures → fix or drop the offending commit (revert within the branch) and record why in the digest.
7. **PR.** Push the branch; open a **draft** PR via the forge adapter with the digest (§6) as the body. When verification is green and all commits are in, **mark the PR ready for review** (`gh pr ready` / Forgejo equivalent). The human reviews and merges; the gardener **never merges** (D-011).
8. **Escalate.** Questions raised (§5) are written to `knowledge/_questions/` in the branch and listed at the top of the PR body.
9. **Clean up.** Remove the worktree; release the lock; emit a summary to the session (and status line).

If a previous garden PR is still open: the new run bases on the default branch, and skips tasks whose targets are touched by the open PR (to avoid conflicting PRs), noting them in the digest.

## 4. Promotion (hardening the ladder)

| From → to | Signal | Action |
|---|---|---|
| inbox → shard | always (filing) | create/merge shard |
| correction → gotcha | correction `occurrences` ≥ 2 | convert to `gotcha` with Symptom/Cause/Fix |
| shard → playbook | `howto` exceeding size limits, or ≥ 3 shards consistently `show`n together (telemetry co-access) | draft a skill linking the shards |
| shard/howto → script | `scriptify` signal; or a `howto` whose steps are all commands | write the script per [04](04-scriptorium.md) §9 |
| gotcha/standard → gate | `occurrences` ≥ 3; or `standard` with `## Enforcement: none yet`; or recurring gate-adjacent failures | write the rule (semgrep/lint config/strata lint rule), add to `gates.toml`, link `gates:` both ways |
| gate advisory → blocking | the codebase has had zero findings for the gate for 7 days, or all findings are auto-fixable in the same PR | flip `blocking = true` |

Every promotion links both directions (shard ↔ script/gate) so the ladder is navigable.

## 5. Autonomy matrix

### 5.1 Acts autonomously

- Filing the inbox; merging duplicates (non-`user-stated`); splitting oversize shards; fixing orphans and links; regenerating derived files.
- Re-verifying stale shards and updating `verified_at`, or correcting `observed`/`inferred` shards when evidence (script output, code, sources) contradicts them.
- Archiving cold shards (never deleting them).
- Distilling project logs into shards.
- Writing **new** scripts (all gates green, tests included); extending `owner: gardener` scripts additively.
- Adding gates under the ratchet rule (§5.2); flipping advisory → blocking when the codebase is clean.
- Growing the bench with resolved gap queries.
- Removing deprecated scripts with zero usage.

### 5.2 The ratchet rule for new gates

A gardener-added gate may be `blocking` immediately only if the current codebase produces zero findings, or every finding is auto-fixed within the same PR. Otherwise it lands as `blocking = false` (advisory) or with a baseline, and is flipped later (§4).

### 5.3 Escalates (raises a question; does not act)

- Any change that would overwrite, contradict, or delete a `user-stated` shard.
- Conflicts between two sources of equal or higher trust that no script/code inspection can settle.
- Deleting anything other than exact duplicates or zero-usage deprecated scripts.
- Modifying a `user`-owned script, or changing any script's interface, output contract, or side-effect class.
- Anything involving a `network` or `destructive` script's behavior.
- Anything touching `knowledge/_private/` (the gardener does not read the private tier at all).
- Closing or pausing a project.
- A new gate that would block and whose findings cannot be mechanically fixed.
- Any task where the agent's own confidence is low after investigation — ambiguity is a valid reason to ask.

Instances may tighten (never loosen beyond this) via `[garden] escalate = [...]` in config.

## 6. Digest (PR body)

```markdown
## Garden 2026-11-08 06:00

**Needs you:** 1 question → knowledge/_questions/q-20261108-fixture-scope.md

| Area | Changes |
|---|---|
| Knowledge | filed 7 inbox items (3 new shards, 4 merged), merged 2 duplicates, split 1, archived 4 cold |
| Corrections | 2 shards corrected from evidence (links) |
| Scripts | + find-leaked-fixtures (read-only, Python — justification: AST) |
| Gates | + semgrep:fixture-teardown (advisory; 3 existing findings) |
| Bench | MRR 0.84 → 0.86 · recall@5 0.95 → 0.96 · +3 queries |

### Learned this cycle
- [[pytest-fixture-teardown]] — Session-scoped DB fixtures leak connections unless torn down.
…
### Skipped / deferred
- merge `a` + `b`: both user-stated → question raised.
```

## 7. Scheduling

`/garden` is the interface. Strata does not install schedules itself. The user may ask Claude to create a scheduled task (cloud routine or local cron/loop) that runs `/garden`; the `strata-garden` skill documents how. The deterministic subset (`strata garden --auto`) is cheap enough to run at SessionStart (configurable; default on) and in CI (report-only).

## 8. Forge adapter

`internal/forge` abstracts PR operations: `CreateDraft`, `MarkReady`, `Comment`, `FindOpen(prefix)`. Implementations: `gh` (GitHub) and `tea` (Forgejo/Gitea), selected by `[garden] forge = "auto"|"github"|"forgejo"` (auto = inspect the `origin` remote host). The adapter shells out to the CLI (no tokens handled by Strata). Missing CLI → the gardener pushes the branch and prints the compare URL instead.
