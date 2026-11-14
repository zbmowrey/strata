# 07 — Gardener

The gardener is Strata's metabolism: the process that consolidates, repairs, reorganizes, and hardens the instance — autonomously — and delivers every change as a reviewable pull request. It comes to the human only for genuine conflict, risk, or ambiguity (D-010).

## 1. Two phases

| Phase | Who | Cost | Trigger |
|---|---|---|---|
| **Deterministic** (`strata garden`) | the Go engine | free (no model calls), ≤ 5 s | every `/garden`; also its safe subset (`--auto`) at SessionStart when the index refreshes, and in CI |
| **Judgment** (`/garden`) | the `strata:gardener` subagent, driven by the plugin command | tokens | on demand via `/garden`; optionally on a schedule the user sets up; or auto-launched as a detached headless run at SessionStart when garden debt is due and `[garden] auto_launch = "on"` (§9, D-031) |

The deterministic phase does every mechanical fix itself and produces a **plan** (`.strata/garden/plan.json`) of tasks that need judgment. The judgment phase executes the plan.

## 2. Deterministic phase

`strata garden [--plan] [--auto] [--format json]`:

- no flag: run the analyses and write the plan; mechanical fixes are not applied (safe in the human's tree).
- `--auto`: apply the mechanical fixes only. At SessionStart the hook runs it **incrementally** — only when the index refresh found changes, and only for what those paths can affect (per-file normalization of changed shards, hubs when shards changed, catalogs + allowlist when scripts changed, the projects block when a `project.md` changed) — so the hook stays within its 300 ms budget.
- `--plan`: apply the mechanical fixes and write the plan (what `/garden` runs inside its worktree). Requires the run lease (§9.2); exit 6 when another run holds it.

Sessions the gardener starts set `STRATA_GARDEN=1`; the `pre-edit` hook then refuses writes under `knowledge/_private/`, and the analyses never read the private tier.

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
| `fix-orphan` | shard with no inbound links other than from generated hubs, created more than `[garden] orphan_min_age_days` (default 30) days ago (D-033) | low |
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

Each task: `{id, type, risk, targets[], evidence{…}, suggested_action, escalate?}`. Plan output is deterministic and diffable: tasks sorted by the table order above, then id; ids are `<type>:<targets joined by +>` (qualified with `#<detail>` when one target has several tasks of a type, e.g. `fix-broken-link:api-retries#old-id`). Telemetry-derived targets are `query:<normalized query>` and `bash:<hash>`; project targets are `project:<slug>`. A type listed in `[garden] escalate` gets `escalate: true` and risk `question`.

Implementation notes (v0.1): duplicate titles use trigram Dice over lowercase titles via an inverted index (trigrams shared by > 400 titles are skipped), plus "alias equals the other's id"; bodies use 64-permutation MinHash over word 3-shingles with 16×4 LSH banding; a pair whose titles differ by negation is a `resolve-conflict` candidate, not a duplicate. `archive-cold` and `deprecated-script-removal` are only judged when telemetry reaches back over the whole window (a new instance has no usage history). `fill-gap` needs the zero-result query in telemetry (`[telemetry] store_queries`); `bench-grow` = a former gap query that now has results and is absent from `.strata/bench/retrieval.yaml`. Link repair rewrites only wikilinks with exactly one candidate at Dice ≥ 0.9 over ids; frontmatter link fields become tasks.

## 3. Judgment phase (`/garden`)

The plugin's `/garden` command runs:

1. **Lease.** Acquire the run lease `.strata/garden.lock` (one garden at a time per repo, §9.2). A live lease → stop with a message; an expired one → resume that run instead of starting a new one (§9.3).
2. **Plan.** `strata garden --plan --format json`. If the plan is empty and no mechanical fixes were applied, report "garden is tidy" and stop.
3. **Worktree.** Create a git worktree at `.strata/worktrees/garden-<YYYYMMDD-HHMM>` on a new branch `garden/<YYYYMMDD-HHMM>` from the default branch's latest remote head. The human's working tree is never touched (D-011).
4. **Execute.** Spawn the `strata:gardener` subagent in the worktree with the plan; it is pinned to `[garden] model` (default Opus, effort `[garden] effort`, default `medium`; D-032) whatever model the calling session runs. It records each task's outcome with `strata garden task` (§9.3), which also refreshes the lease heartbeat. It works task by task under the autonomy matrix (§5), using the same CLI, scripts, and gates as any session.
5. **Commit.** One commit per task category, conventional style: `garden(knowledge): file 7 inbox items`, `garden(scripts): add find-leaked-fixtures`, `garden(gates): promote fixture-teardown rule (advisory)`. Each body lists task ids and evidence. Attribution trailers per the repository's convention.
6. **Verify.** `strata check --all` (all blocking gates green) and `strata bench retrieval` (MRR must not drop by more than `[garden] bench_tolerance`, default 0.02; recall@5 must not drop). Failures → fix or drop the offending commit (revert within the branch) and record why in the digest.
7. **PR.** Push the branch; open a **draft** PR via the forge adapter with the digest (§6) as the body. When verification is green and all commits are in, **mark the PR ready for review** (`gh pr ready` / Forgejo equivalent). The human reviews and merges; the gardener **never merges** (D-011).
8. **Escalate.** Questions raised (§5) are written to `knowledge/_questions/` in the branch and listed at the top of the PR body.
9. **Clean up.** Remove the worktree; release the lock; emit a summary to the session (and status line).

**Engine helpers.** Steps 1–3 and 7–9 are engine commands so the plugin stays a thin client (lease, resume and launch are specified in §9):

- `strata garden start [--launcher manual|headless] [--format json]` — acquires the lease (§9.2; live → exit 6; expired → exit 6 with the hint `strata garden resume`), fetches `origin/<default>` when a remote exists, adds the worktree and branch, runs `garden --plan` inside it, drops tasks whose evidence paths are touched by open garden PRs (`forge.FindOpen(branch_prefix)`), and commits the mechanical fixes as `garden(fixes): …`. Output: `{"v":2,"run":id,"fence":n,"tidy":bool,"worktree","branch","base","plan_file","counts":{…},"tasks":n,"skipped":[ids],"open_prs":[{number,url,branch}]}`. When tidy (no tasks, no fixes) it cleans up itself.
- `strata garden finish --fence N [--title T] [--body-file F] [--no-ready] [--format json]` — from the main tree; refused with exit 7 when `N` is not the lease's current fence (§9.2). First looks for an open PR on the branch and reuses it, so a retried finish is safe. If the branch has no commits beyond the base it cleans up and reports `nothing to propose`; otherwise pushes the branch, opens a **draft** PR via the forge adapter with the digest as the body, marks it ready unless `--no-ready`, removes the worktree and the marker. Missing forge CLI → pushes and prints the compare URL. Output: `{"v":1,"pr":{number,url},"compare_url":…,"ready":bool}`.
- `strata garden abort` — removes the worktree, deletes the unpushed local branch, removes the lease and marks the run `aborted` in history. Explicit only: nothing calls it automatically on a run that has commits.
- Hooks treat any instance rooted under `.strata/worktrees/garden-*` (or `STRATA_GARDEN=1`) as a gardener session (private-tier guard).

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

**Auto-launch (D-031, amends D-018).** An instance may opt in with `[garden] auto_launch = "on"` (default `"off"`; the engine repo sets it on). The SessionStart hook then launches a detached, headless garden run when garden debt is due (§9). This is not a schedule: nothing runs while no session starts, it is driven by measured debt, and it fires at most `[garden] launch_per_day` times a day. Its output is still a PR the human reviews (D-011).

## 8. Forge adapter

`internal/forge` abstracts PR operations: `CreateDraft`, `MarkReady`, `Comment`, `FindOpen(prefix)`. Implementations: `gh` (GitHub) and `tea` (Forgejo/Gitea), selected by `[garden] forge = "auto"|"github"|"forgejo"` (auto = inspect the `origin` remote host). The adapter shells out to the CLI (no tokens handled by Strata). Missing CLI → the gardener pushes the branch and prints the compare URL instead.

Implementation notes (Phase 9): `gh` runs `gh pr create --draft --head --base --title --body`, `gh pr ready`, `gh pr comment --body`, and `gh pr list --state open --json number,url,headRefName,files` (prefix-filtered in Go; `files` lets the gardener skip tasks touched by an open garden PR). `tea` runs `tea pulls create --draft …--description` (Gitea/Forgejo drafts are `WIP: `-prefixed titles), `tea pulls edit --ready <index>`, `tea comment <index> <body>`, and `tea pulls list --fields index,url,head --output json` (no file list). A missing CLI is the typed `forge.ErrNoCLI`; the caller then pushes and prints `forge.CompareURL(remote, base, branch)` (`https://<host>/<owner>/<repo>/compare/<base>...<branch>`, from https, ssh:// and scp-style remotes).

## 9. Autonomous runs: debt, lease, resume, launch

A garden run must be able to start without a human, survive being lost (crash, killed terminal, sleep, reboot), and never run twice at once. Progress lives on disk and in git, never only in an agent's context. Decisions: D-031 (detached headless launch), D-032 (model), D-033 (orphans).

### 9.1 Garden debt

Garden debt is computed cheaply enough for the SessionStart budget (no forge calls, no plan run): the inbox directory scan, the counts in the last written plan (`.strata/garden/plan.json`), and the run history (`.strata/garden/history.jsonl`, one line per attempt: `{run, attempt, launcher, model, started, ended, outcome, pr, tasks_done, tasks_left}`; local, gitignored).

Debt is **due** when any of:

| Signal | Default |
|---|---|
| oldest `_inbox/` item older than `[garden] debt_inbox_days` | 3 days |
| actionable plan tasks (excluding `escalate`/question tasks) ≥ `[garden] debt_tasks` | 10 |
| last finished run older than `[garden] debt_days`, and inbox or actionable tasks > 0 | 7 days |

A plan generated before the last finished run is spent and contributes no tasks. An instance with no finished run is judged on the first two signals only, so a single fresh capture never triggers a garden.

`strata status --format json` reports `garden_debt: {due, reasons[], inbox, inbox_oldest_days, tasks, last_run_days}`. The session-start line and the status line show it (08 §4.1, §5.3).

### 9.2 The run lease

`.strata/garden.lock` is a lease, not a bare marker (JSON, `v: 2`):

```json
{"v":2,"run":"20261114-1210","fence":3,"attempt":2,
 "owner":{"pid":48211,"host":"zm-mbp","launcher":"headless","model":"opus","session":"…"},
 "started":"…","heartbeat_at":"…","ttl":"30m",
 "branch":"garden/20261114-1210","worktree":"/abs/.strata/worktrees/garden-20261114-1210-a2",
 "base":"main","base_ref":"…","plan_hash":"…"}
```

- **Acquire.** Exclusive create (`O_EXCL`). Present and live → exit 6. Present and expired → takeover (below) or exit 6 with the `strata garden resume` hint.
- **Live** means `now − heartbeat_at < ttl` (`[garden] lease_ttl`, default 30 min). On the same host, a dead `owner.pid` also makes the lease expired, without waiting for the ttl.
- **Heartbeat.** Every `strata` command whose instance root is the run's worktree refreshes `heartbeat_at` (atomic temp-file + rename) when its fence matches. The heartbeat therefore measures *progress*: a hung agent stops producing it, no matter how long its process lives. The headless supervisor (§9.4) does not heartbeat on its own behalf.
- **Fence.** A number that increases on every takeover. The worktree carries its fence in `.strata/garden-run.json`. Every gardener write through the engine (`garden task`, `garden --plan`, `garden finish --fence N`) is refused with **exit 7** when its fence is not the lease's current fence. Commits go through the worktree's git pre-commit hook (`strata check --staged`), which applies the same fence check, so a stale gardener's commit is aborted. Resume also removes the stale worktree (§9.3). A gardener that wakes up after its lease was taken over cannot write, so two gardeners never commit to the same branch.
- **Takeover.** The taker exclusively creates `garden.lock.takeover`. Only the winner re-reads the lease, confirms it is still expired with the same fence, writes the new lease (`fence+1`, `attempt+1`, new owner) by rename, and removes the takeover file. A takeover file older than 60 s is debris and may be removed.
- **Scope.** The lease is local to one clone (gitignored). Across machines and clones, the open-PR check in `garden start` remains the guard: tasks touching paths in an open garden PR are dropped.

### 9.3 Durable progress and resume

Run state lives outside the worktree, in `.strata/garden/runs/<run>/`: `plan.json` (the plan at start), `progress.json` (`{task_id: {status: pending|in_progress|done|skipped|escalated, tries, note}}`), the run log, `verify.json` (the last verification result) and any salvage patches. Losing the worktree loses only uncommitted edits. The PR digest (§6) is **rendered by the engine** at finish from `progress.json` notes, the branch's commits and raised questions, plus an optional summary the gardener writes to `.strata/garden-summary.md` in the worktree. The gardener needs no write access outside its worktree.

- `strata garden task start|done|skip|escalate <task-id> [--note …]` updates `progress.json` and heartbeats the lease. Tasks must be **idempotent**: before acting, the gardener checks whether the target state already holds (inbox item already filed, link already fixed), so redoing an interrupted task is safe.
- `strata garden resume [--force]` (the hook and `launch` call it for an expired lease; `--force` lets a human take over a live one):
  1. Take over the lease (§9.2). If the old owner's pid is alive on this host, SIGTERM it and wait up to 10 s.
  2. Create a fresh worktree from the run's branch (`…-a<attempt>`). The branch is the durable record; the old worktree may be missing.
  3. **Salvage** uncommitted changes from the old worktree, if it still exists: apply the diff to the new worktree and run `edit`/`stop`-stage gates on those files. Clean → commit `garden(resume): salvage <n> files`. Not clean → save `runs/<run>/salvage-a<attempt>.patch` and record it in the digest. Then remove the old worktree.
  4. Reset any `in_progress` task to `pending` and increment its `tries`. A task with `tries ≥ 2` is **poison**: mark it `skipped`, raise a question citing it, and list it in the digest.
  5. Emit the same JSON as `garden start`, plus `"resumed": true` and the remaining tasks. The gardener continues from there.
- **Already pushed or PR'd.** `finish` reuses an existing open PR for the branch. A run whose PR exists and whose tasks are all settled is just finished.
- **Give up.** When `attempt > [garden] max_attempts` (default 3), the run is marked `stuck` in history with `.strata/garden/last-failure` (last log tail, poison tasks, salvage files). Auto-launch stops for this run, and session start shows `garden stuck — strata garden status`. A human resumes (`garden resume --force`) or aborts.
- `strata doctor --fix` never deletes a resumable run. Expired lease with commits or salvageable changes → report "resumable: strata garden resume". Nothing to salvage → clear the lease and remove the worktree.

`strata garden status [--format json]` shows the lease (owner, fence, attempt, heartbeat age), progress counts, the last history lines, and the debt.

### 9.4 Headless launch

`strata garden launch [--resume] [--dry-run] [--format json]` starts a detached supervisor (`strata garden supervise`, new session/process group, output to `runs/<run>/run.log`) and returns immediately. The supervisor:

1. Exits with outcome `deferred` (recorded in history, no lease taken) when ≥ `[garden] max_open_prs` (default 1) garden PRs are already open, so unreviewed PRs never pile up.
2. Runs `garden start --launcher headless`, or `garden resume` with `--resume`. Tidy → records `tidy` and exits.
3. Runs the agent: `claude -p <prompt> --model <[garden] model> --permission-mode acceptEdits --allowedTools <generated>`, with effort `[garden] effort`. The environment carries `STRATA_GARDEN=1` and the run id. The prompt is rendered from an engine template: work the plan in `runs/<run>/plan.json` as the `strata:gardener` agent, under the autonomy matrix, recording each task with `strata garden task`. **Flag names and the effort mechanism are verified against the current Claude Code CLI at build time** (08's verification requirement). Unattended `bypassPermissions` is refused by Claude Code, so this permission mode is the supported form.
4. **Allowlist (generated, minimal):** `Read`, `Glob`, `Grep`; `Edit`/`Write` scoped to the run's worktree; `Bash(strata …)` for the commands the autonomy matrix needs; `Bash(git -C <worktree> …)` for read and commit subcommands only. No `git push` and no forge CLI: publishing is the supervisor's job.
5. Kills the agent when the lease heartbeat is older than `lease_ttl` (hung) or the run exceeds `[garden] max_run` (default 90 min). Records outcome `timeout`. A later session resumes the run.
6. On agent exit 0 with no `pending` tasks: runs verification (§3 step 6). Green → `garden finish --fence <n>` with the rendered digest; records outcome `pr` and the PR number. Red → writes `runs/<run>/verify.json`, records `failed-verify` and publishes nothing. The resumed run's prompt starts with the verification failures, so the gardener fixes them or drops the offending commit (§3 step 6). On any other exit: records `failed` and leaves the lease to expire, so the next session start resumes it.

The supervisor never merges (D-011), and it is the only process that pushes.

### 9.5 SessionStart decision

The `session-start` hook (08 §4.1) runs this within its 300 ms budget. Launching forks the supervisor and does not wait for it.

1. Skip entirely in gardener sessions (`STRATA_GARDEN=1` or a root under `.strata/worktrees/garden-*`), when `STRATA_NO_AUTOLAUNCH=1` (set by `bench protocol` and CI), and when `auto_launch = "off"`. With it off, the debt line is still printed when debt is due.
2. Lease live → print `garden ▸ running <age> (<n>/<m> tasks) — strata garden status`.
3. Lease expired, run not stuck → `garden launch --resume`; print `garden ▸ resuming run <id> (attempt <k>)`.
4. Run stuck → print the stuck line; launch nothing.
5. Debt due and fewer than `launch_per_day` launches today (history) → `garden launch`; print `garden ▸ launched (<model>, <reasons>)`.
6. Otherwise print nothing about the garden.

### 9.6 Failure modes

| Failure | Result |
|---|---|
| Two sessions start at once | One wins the exclusive create; the other sees a live lease. |
| Two sessions find the same expired lease | One wins the takeover file; the other sees a live lease. |
| Agent hangs (no strata calls) | Heartbeat ages past `lease_ttl`; the supervisor kills it; the next session resumes. |
| Machine sleeps or reboots mid-run | Lease expires; pid is gone; resume salvages the worktree and continues. |
| Old gardener wakes after a takeover | Every write is fenced out (exit 7); it can no longer commit or publish. |
| Worktree deleted by hand | Resume recreates it from the branch; only uncommitted edits are lost. |
| Same task crashes the run twice | Poison: skipped, escalated as a question, listed in the digest. |
| Run keeps failing | Stuck after `max_attempts`; auto-launch stops for it; a human decides. |
| Push succeeded, PR creation failed | `finish` retries; an existing PR is reused. |
| PRs awaiting review | Launch defers while `max_open_prs` are open. |
| Forge CLI missing | As §8: push, print the compare URL, record it in history. |
