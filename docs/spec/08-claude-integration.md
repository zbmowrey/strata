# 08 — Claude Integration

How Strata reaches Claude: the `CLAUDE.md` protocol (soft, explains *why*), hooks (hard, deterministic), and the Claude Code plugin (commands, agents, skills, status line, pane, model tools).

**Verification requirement for the builder:** Claude Code's hook payloads, plugin manifest, and mod (function-hooks) API evolve. Before implementing §4–§6, confirm the current contracts — load the `plugin-authoring` skill and grep its `types/claude-code.d.ts`, and consult the Claude Code hooks documentation (the `claude-code-guide` agent can fetch it). Record what you confirmed as `reference` shards in the Strata repo's own KB. Where this spec and the current platform disagree, follow the platform and update this spec.

## 1. Division of responsibility

| Layer | Owns | Lives in | Why there |
|---|---|---|---|
| `CLAUDE.md` managed block | The protocol: what to do and why | instance repo | Travels with the repo; read every session |
| Hooks (`.claude/settings.json`) | Enforcement: gates, resume, advisories | instance repo, written by `strata init` | Anyone who clones gets enforcement, plugin or not (D-017) |
| Plugin `strata` | UX + procedures: slash commands, subagents, skills, status line, pane, model tools | engine repo `plugin/`, installed per user | Generic across repos; versioned with the engine |
| Engine `strata` | All logic | binary on PATH | One implementation; hooks and plugin are thin callers (D-016) |

The plugin does **not** register enforcement hooks if the repo already has them (it checks `.claude/settings.json` for `strata hook` commands) — no double execution.

## 2. The `CLAUDE.md` managed block

`strata init` inserts this block (rendered from `engine/internal/scaffold/templates/claude-block.md`, with instance paths substituted) between markers. `strata upgrade` replaces only the content between markers; user content outside is never touched. The block must stay under ~80 lines: it is loaded into every session.

```markdown
<!-- strata:begin v1 — managed by `strata upgrade`; edit outside these markers -->
## Strata — how to work in this repository

This repo runs Strata: a searchable knowledge base (`knowledge/`), a script library
(`scripts/`), local project memory (`projects/`), and blocking quality gates. Use them.

### 1. Search before you act
Before answering questions about this codebase, editing an unfamiliar area, debugging,
or running an ad-hoc command, search:
  strata search '<terms>'                 # ranked one-line hits across knowledge, scripts, playbooks, projects
  strata show <id> [<id> …]               # fetch everything you need in ONE call, in order
  strata show <id>#<section>              # one section of a long shard
  strata context <path>                   # knowledge that applies to a file
Climb only as far as needed: search → show → show --links 1 → code/web. Batch ids into a
single `show`. If you used outside sources, capture what you learned (step 3).

### 2. Scripts before improvisation
If an operation is repeatable and can be deterministic, find a script first:
  strata search '<need> kind:script'      then   strata run <id> -- <args>
- Fits → run it. Nearly fits and the change is additive → extend it with tests.
- Nearly fits but changing it is risky (semantics, interface, side effects) → write a new
  script that reuses scripts/lib/. Nothing fits and it will recur → `strata new script`.
- Default runtime is Go; any other runtime needs a written justification in script.yaml.

### 3. Capture as you go
When you learn something non-obvious and reusable — a gotcha, a decision and its reason,
how a thing really works, a correction from the user — capture it immediately:
  strata capture "<what, why, evidence>" [--type gotcha|fact|decision|correction] [--project <slug>]
Don't capture what the code or git history already says. Never capture secrets.
When the user corrects you, capture it with --type correction.

### 4. Correct the record
If a shard is wrong, fix it in place now: correct the claim, add the evidence to `sources`,
bump `updated`/`verified_at`. Never overwrite a `trust: user-stated` shard on your own —
raise it with the user. Precedence: user-stated > verified > observed > inferred.
Only user-stated/verified `decision` and `standard` shards are binding.

### 5. Gates are law
Edits are checked as you make them; failing checks block. Fix the root cause. Don't
suppress a finding without a rule id and a reason; never weaken gate config to pass.
  strata check --changed        strata gates explain <gate>/<rule>

### 6. Projects hold multi-session work
For work spanning sessions: `strata project new <slug>`; keep `plan.md` current; log
tried/failed/learned/decided steps (`strata project log <slug> --tried "…"`); update
`handoff.md` before stopping or when context is about to be compacted. `strata resume`
shows where active work stands.

### 7. Leave it better
Don't hand-edit generated files (hubs, catalogs, managed blocks). `/garden` runs the
gardener, which consolidates knowledge and opens a PR for review.
<!-- strata:end -->
```

## 3. Hook wiring (written by `strata init` into `.claude/settings.json`)

`strata init` merges these into the repo's `.claude/settings.json`, preserving all other keys; entries it owns are identified by a command starting with `strata hook`. Commands reference the binary on PATH (with a `$CLAUDE_PROJECT_DIR`-relative fallback check performed by the hook itself).

| Event | Matcher | Command | Blocking? | Timeout |
|---|---|---|---|---|
| `SessionStart` | (all sources) | `strata hook session-start` | no | 10 s |
| `PreToolUse` | `Edit\|Write\|MultiEdit\|NotebookEdit` | `strata hook pre-edit` | only for generated-file / private-tier guards | 5 s |
| `PostToolUse` | `Edit\|Write\|MultiEdit\|NotebookEdit` | `strata hook post-edit` | **yes** (code-quality + structural lint) | 30 s |
| `PostToolUse` | `Bash` | `strata hook post-bash` | no (telemetry only) | 2 s |
| `Stop` | — | `strata hook stop` | **yes** (bounded; [06](06-gates.md) §2.1) + one optional knowledge nudge | 120 s |
| `SubagentStop` | — | `strata hook stop --subagent` | yes (same bound) | 120 s |
| `PreCompact` | — | `strata hook pre-compact` | no (advisory) | 5 s |

No `UserPromptSubmit` hook is installed (no auto-injection; D-006).

## 4. Hook behaviors

All hook adapters: read JSON from stdin (fields such as `session_id`, `cwd`, `hook_event_name`, `tool_name`, `tool_input`, `tool_response`, `stop_hook_active` — verify current names); never crash (any internal error → exit 0 with a one-line stderr warning, except where blocking is the intended outcome); write a `hook` telemetry event with duration; keep per-session state in `.strata/session/<session_id>.json`.

### 4.1 `session-start`
Refresh the index; run `garden --auto` if enabled; then print (stdout → added to context), ≤ 25 lines:
```
strata ▸ 1,204 shards · 37 scripts · 9 playbooks · index fresh (12 ms)
Active projects:
  ci-flake — Next: 1. write find-leaked-fixtures (strata new script testing/find-leaked-fixtures)
Inbox 3 · Open questions 1 (strata questions list) · Stale 4 · Garden PR #41 awaiting review
⚠ doctor: semgrep 1.80 < pinned 1.95 (strata doctor)
```

### 4.2 `pre-edit`
1. **Guards (blocking, exit 2):** the target is a generated file (header marker or known path) → "generated by strata; edit <source> and run <cmd>"; the target is under `knowledge/_private/` while the session is a gardener run → refuse.
2. **Advisory (non-blocking, configurable `[hooks] pre_edit_context = true`):** the first time in a session a given path is edited, if `strata context <path>` has hits, emit ≤ 3 lines (`ℹ strata: 2 shards apply to this file: pytest-fixture-teardown — …`). Never repeated for the same path in a session. This is the only automatic retrieval Strata performs, and it is cheap, narrow, and switchable (D-006).

### 4.3 `post-edit`
Run `edit`-stage gates on the touched file(s) ([06](06-gates.md) §2): fix-mode gates first, then checks. Blocking findings → exit 2 with the formatted findings on stderr (Claude receives them and must fix). Clean → exit 0 silently. Markdown in `knowledge/` → `strata lint` on that file (schema, size, links) is part of this stage.

### 4.4 `post-bash`
Record a `bash` telemetry event (program name + normalized hash only; [01](01-architecture.md) §8). Also records `strata run`/`strata search` usage counts for protocol evals. Silent.

### 4.5 `stop`
1. Run `stop`-stage gates on changed files (cached results reused). Blocking findings → block with findings (bounded by the loop guard).
2. **Knowledge nudge** (`[hooks] stop_nudge = "once"|"off"`, default `once`): if gates are clean and this session made code changes or had gate failures or a `correction`-like event, *and* no `capture`/`project log` occurred this session, block **once** with: "Knowledge checkpoint: capture anything non-obvious you learned (strata capture …) and update the project handoff if this is multi-session work — or reply that there is nothing to capture." Never fires twice per session. This is the soft side of D-008: one nudge, trivially satisfiable.

### 4.6 `pre-compact`
Print a reminder (advisory) to update `handoff.md` for active projects; include the last 5 `log.md` entry headlines so the post-compaction context retains the thread.

## 5. The plugin (`plugin/`)

The plugin is a Claude Code mod (function-hooks module in TypeScript) plus markdown assets. It is a thin client: every operation runs `strata … --format json` via `$.process.run` and renders the result.

### 5.1 Manifest and marketplace

- `plugin/.claude-plugin/plugin.json`: `{ "name": "strata", "version": <engine version>, "description": "…", "types": "./types/index.d.ts" }`.
- `plugin/hooks/hooks.json`: `{ "modules": ["./register.ts"] }`.
- Repo root `.claude-plugin/marketplace.json` lists `strata` with `source: "./plugin"`.
- Install: `/plugin install strata --marketplace zbmowrey/strata`.
- Version of plugin and engine move together; the plugin checks `strata version --format json` at `session.start` and shows a status warning on mismatch.

### 5.2 Slash commands

| Command | Behavior |
|---|---|
| `/garden [--plan-only]` | Runs the gardener judgment phase ([07](07-gardener.md) §3): plan → worktree → `strata:gardener` agent → PR. `--plan-only` prints the plan. |
| `/capture <text>` | `strata capture` with the current project; confirms the inbox id. |
| `/promote <id>` | Asks the gardener agent to evaluate promoting a specific shard down the ladder now. |
| `/bench [retrieval\|protocol]` | Runs the bench; renders metrics vs. baseline. |
| `/resume [slug]` | Prints `strata resume`. |
| `/project <new\|list\|close\|handoff> …` | Project lifecycle shortcuts. |
| `/strata` | Opens the Strata pane (§5.4); `/strata doctor` runs doctor. |

### 5.3 Status line

`$.ui.status(...)`, updated at `session.start`, after each `strata` invocation the plugin observes, and after hook-relevant tool calls: `⛬ 1,204 · inbox 3 · ? 1 · gates ✓` (gates ✗N when the last edit/stop gate run had blocking findings; `garden PR #41` when one is open).

### 5.4 Pane

`/strata` opens a pane (`$.ui.open` + `ui.render`): sections for Active projects (with handoff Next), Inbox, Open questions (with links to files), Last gate run (findings), Garden (last run, open PR, plan size), Bench (MRR trend). Read-only plus buttons: "Run /garden", "Refresh". State in `$.state` declared in `types/index.d.ts`.

### 5.5 Model tools (optional, default on)

Register `strata_search` and `strata_show` via `$.tool.register` as typed tools (structured input: query + filters; ids array + options), returning the engine's JSON rendered compactly. Benefits: no Bash permission prompts, schema-validated arguments, easy telemetry. The CLI remains canonical; the protocol text works with either.

### 5.6 Subagents (`plugin/agents/*.md`, namespaced `strata:<name>`)

| Agent | Purpose | Tools | Notes |
|---|---|---|---|
| `gardener` | Executes garden plans under the autonomy matrix | all | Loads `strata-garden` skill; never merges; escalates per [07](07-gardener.md) §5.3 |
| `scout` | KB-first read-only research: answers a question using search → show → code, returns ids + conclusion | read-only + Bash(strata search/show/context) | Terse returns with shard ids, not dumps |
| `scribe` | Turns raw findings/logs into well-formed shards (schema, size, links) | Read, Write, Edit, Bash(strata …) | Used by gardener for filing/distillation |
| `scriptsmith` | Writes or extends scripts per [04](04-scriptorium.md) §9 | all | Must leave all gates green |
| `verifier` | Runs gates/tests/bench and reviews a change against standards shards | read-only + Bash(strata check, tests) | Returns pass/fail + findings |

### 5.7 Skills (`plugin/skills/*/SKILL.md`)

`strata-capture` (what/how to capture; shard anatomy; examples of good and bad shards) · `strata-scriptsmith` (the script creation protocol) · `strata-garden` (the gardener's operating manual: task handling per type, autonomy matrix, commit/PR conventions, scheduling `/garden`) · `strata-project` (project lifecycle, chronicle and handoff discipline) · `strata-search` (retrieval ladder, query language, batching). Skills are themselves indexed as playbooks.

### 5.8 Implementation notes (v0.1)

- Status line comes from `strata status --session <id> --format json` (session id from `$.session.id()`); while a `/garden` run is active it shows `garden running since HH:MM` instead of an open-PR number. Refreshed at session start, after main-loop Edit/Write, after Bash calls running `strata`, and when a garden run ends.
- `/garden` runs `strata garden start`, then `$.agent.spawn({ subagentType: 'strata:gardener', cwd: <worktree>, prompt })`; a `turn.complete` hook for that agent writes its final answer (the digest) to `<worktree>/.strata/garden/digest.md` and runs `strata garden finish --body-file …`. If the spawn is refused, the command prints instructions for the main agent. `/garden finish` and `/garden abort` are manual fallbacks.
- `/capture` files into the single active project (else `_adhoc`); `strata_show` returns the engine's markdown; `/promote` spawns `strata:gardener` in evaluate-only mode; the pane's Bench section shows a hint until bench data exists.
- Agent frontmatter `tools` takes tool names only, so per-command Bash limits (scout, verifier) are stated in the agent prompt.
- The mod API types are vendored at `plugin/vendor/claude-code.d.ts` so `tsc` runs without the engine; `no-misused-promises` runs with `checksVoidReturn: false` (that check took minutes per file on `on(...)` registrations).

## 6. Plugin quality bar

TypeScript strict profile ([06](06-gates.md) §4.3); `claude plugin validate plugin/` clean; `tsc -p plugin` clean; `claude plugin test plugin/` covering: status text formatting, command → CLI argv mapping, JSON error handling (engine missing, engine error, version mismatch), pane rendering on `terminal` and `desktop` surfaces.

## 7. Permissions (`strata perms sync`)

Generates a managed set of `permissions.allow` entries in `.claude/settings.json` (identified by a marker list in `.strata/perms.lock` so only Strata-owned entries are rewritten):

- Always: `Bash(strata search:*)`, `Bash(strata show:*)`, `Bash(strata context:*)`, `Bash(strata related:*)`, `Bash(strata map:*)`, `Bash(strata capture:*)`, `Bash(strata check:*)`, `Bash(strata lint:*)`, `Bash(strata resume:*)`, `Bash(strata project:*)`, `Bash(strata questions list:*)`.
- Per script with `side_effect` `read-only` or `writes-local`: `Bash(strata run <id>:*)`.
- Never: `network` or `destructive` scripts, `strata garden` (runs via `/garden` with its own flow), `strata gates baseline --accept`.
