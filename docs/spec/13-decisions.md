# 13 — Decision Log

Decisions made during design (2026-11-07). Each is binding for the build unless new evidence is recorded here as a superseding decision. Format: context → decision → consequences. After Phase 11, these migrate into the Strata repo's own KB as `type: decision` shards (`trust: user-stated` where the user made the call).

| ID | Decision | Made by |
|---|---|---|
| D-001 | **Per-repository, drop-in.** No global store. Multi-repo use is via a hub repo that federates read-only (panopticon pattern). | user |
| D-002 | **Go engine; performance-first runtimes.** Go for the engine and default scripts (typed, compiler-checked, maintainable). Python only with written justification (library ecosystem). Rust for truly latency-critical paths. TypeScript only where JS is required (plugin). | user |
| D-003 | **No CGO.** Pure-Go SQLite (`modernc.org/sqlite`) for static cross-compiled binaries. | design |
| D-004 | **No daemon in v1.** Short-lived CLI with per-call freshness check meets the latency budget; a daemon adds lifecycle complexity. Revisit only if budgets can't be met. | design |
| D-005 | **Lexical retrieval, no embeddings in v1.** BM25 + aliases + synonyms + trigram + link graph. Embeddings only if the retrieval bench shows a recall problem lexical fixes can't solve. | design |
| D-006 | **No auto-injection of KB results into prompts.** Retrieval is deliberate. Sole exception: once-per-file, ≤ 3-line, switchable pre-edit advisory from `applies_to`. | user (advisory exception: design — flagged for user veto) |
| D-007 | **Markdown + YAML frontmatter is the source of truth;** all indexes, hubs, catalogs, allowlists, and managed blocks are derived and regenerable. | user + design |
| D-008 | **Hooks block for code quality; advise for knowledge.** Structural lint of knowledge files (schema, size, links) counts as code quality. Knowledge capture is a nudge (one Stop nudge per session, switchable). | user |
| D-009 | **Obsidian-native knowledge.** Filename stem = id; `[[wikilinks]]`; native `tags`/`aliases`. | user |
| D-010 | **Autonomous gardener.** Acts on its own; escalates only true conflict, risk, or ambiguity (autonomy matrix, [07](07-gardener.md) §5). | user |
| D-011 | **Gardener delivers via PRs it never merges.** Works in a git worktree on a `garden/` branch; opens a draft PR; marks it ready for review when verified; the user reviews and merges. | user |
| D-012 | **No migration tooling.** Migrating existing systems (e.g., panopticon) is one-off engineering. | user |
| D-013 | ~~**`projects/` is local-only;** version control is a rare, explicit per-project opt-in (`track:` globs → generated `.gitignore` re-includes).~~ **Superseded by D-029.** | user (mechanism: design) |
| D-014 | **`strata show` takes many ids and returns them in one response, in the order given.** | user |
| D-015 | **Dogfooding.** The engine repo is itself a Strata instance and passes its own strictest gates. | design |
| D-016 | **One implementation of logic.** Hooks and the plugin are thin callers of the `strata` binary (`--format json`). | design |
| D-017 | **Enforcement hooks live in the instance repo** (`.claude/settings.json` via `strata init`), not only in the plugin, so enforcement travels with the repo. The plugin provides UX/commands/agents/skills and avoids double-registering hooks. | design |
| D-018 | **`/garden` is the interface; scheduling is opt-in.** The user can ask Claude to schedule `/garden`; Strata installs no schedules itself. *Amended by D-031* (opt-in, debt-driven auto-launch at SessionStart). | user |
| D-019 | **Generated files are guarded.** They carry a GENERATED header and the pre-edit hook refuses manual edits, pointing to the source. | design |
| D-020 | **Engine source lives in `engine/`;** a root `justfile` provides build/install/test/check/release targets. | user |
| D-021 | **Public repo `zbmowrey/strata`, MIT license.** | user |
| D-022 | **The build is executed by a single Claude session from `PROMPT.md`,** after the user clears this design conversation; subagents may parallelize within phases. Work lands on a `build/v1` branch with a PR. | user (branch strategy: design) |
| D-023 | **Name: Strata.** Knowledge settles in layers and hardens toward determinism (the hardening ladder). CLI `strata`. | user |
| D-024 | **Trust levels with precedence** (`user-stated > verified > observed > inferred`); only user-stated/verified normative shards bind. | design |
| D-025 | **Ratcheting baselines** let Strata drop into existing repos without a wall of errors: baselined findings don't block; baselines only shrink. | design |
| D-026 | **A project is an epic.** One project per coherent, goal-bearing body of work (feature, bug-fixing session, audit, research, initiative); tasks live in its plan; work a project produces that has its own goal (e.g. an audit's remediation) is a new project linked by `spawned_from`. ([05](05-projects.md) §1) | user |
| D-027 | **Standard project record family.** Every project carries project, request (verbatim), decisions, log, handoff and outcome; profiles add requirements, spec, test-plan, findings, sources. Stories and their acceptance criteria share one file (`requirements.md`). Stable IDs; declared mutation modes; extensible via registered kinds. ([05](05-projects.md) §2–§9) | user (taxonomy: design) |
| D-028 | **Status lives only in `project.md`** (`status`, `phase`, `health`); its history is the log; no separate status file. | user |
| D-029 | **Project version control is a per-instance mode** — `local` (default) \| `records` \| `all` — chosen at `strata init` (Claude asks the user before running it), overridable per project; `records` tracks low-churn record files and keeps session-churn files local to avoid merge conflicts in large teams. Supersedes D-013. ([05](05-projects.md) §5) | user (modes: design) |
| D-030 | **Key Claude decisions require user ratification.** Decisions are attributed (`by: user\|claude\|joint`); those marked `key` (architectural or pivotal) made by Claude start `ratified: pending` and are surfaced at checkpoints; close lists any left pending. ([05](05-projects.md) §3.10) | user |
| D-031 | **Debt-driven garden auto-launch (2026-11-14).** With `[garden] auto_launch = "on"` (opt-in per instance; the engine repo opts in), the SessionStart hook launches the gardener as a **detached headless process** (`strata garden launch`: a supervisor running `claude -p` with `acceptEdits` and a generated allowlist scoped to the garden worktree) when garden debt is due, rather than asking the session's Claude to start a subagent. Why: plugin agents ignore `permissionMode`, and a subagent's edit rights depend on the parent session's mode; headless gives an explicit model, explicit permissions and a real pid. It runs under a fenced, heartbeat lease that lets a later session resume a lost run. Amends D-018. ([07](07-gardener.md) §7, §9) | user (mechanism: design) |
| D-032 | **The gardener runs on Opus at medium effort (2026-11-14),** pinned whatever model the calling session uses (`[garden] model`, `[garden] effort`). Gardening needs judgment: merges, rewording, escalation calls and promotions become knowledge that later agents trust. Fable is unnecessary, and Haiku/Sonnet risk subtle knowledge-base damage. ([07](07-gardener.md) §3, §9.4) | user |
| D-033 | **Orphan = no inbound links other than generated hubs, past a minimum age (2026-11-14)** (`[garden] orphan_min_age_days`, default 30). Replaces the earlier wording ("not in a hub section"), which never fires because every shard is listed in a hub, and the v0.1 implementation, which flagged every shard. ([07](07-gardener.md) §2) | user (default: design, confirmed by user) |
