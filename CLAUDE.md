# Strata — engine repository

This repository builds **Strata**, a drop-in operating layer for Claude Code (knowledge base, script library, project memory, quality gates, gardener, plugin).

- The design is in `docs/spec/` and is authoritative. Settled decisions: `docs/spec/13-decisions.md`.
- The build is driven by `PROMPT.md`. If asked to build, run, or continue the build, read `PROMPT.md` and follow it exactly — including its continuity rules (`projects/strata-v1/handoff.md` first after any compaction).
- Go source lives only in `engine/`. The Claude Code plugin lives in `plugin/`. Use the root `justfile` for build, install, test, and check.
- Never merge PRs, push to `main`, or tag releases without the user's explicit go-ahead.

Once Strata is built (Phase 11), `strata init` adds a managed protocol block below this line; keep the content above it.

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
bump `updated`/`verified_at`. Never overwrite a `trust: user-stated` shard on your own:
record the conflict with `strata questions ask "<question>" --blocking <id>` and tell the
user. Precedence: user-stated > verified > observed > inferred.
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
