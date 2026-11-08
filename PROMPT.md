# PROMPT — Build Strata v0.1.0

You are building **Strata**, a drop-in, per-repository operating layer for Claude Code: a fast searchable knowledge base, a catalogued script library, local project memory, blocking quality gates, an autonomous gardener, and a Claude Code plugin tying them together. The design is complete and lives in `docs/spec/`. Your job is to build it — to a standard you would be proud to have the system itself enforce.

You were given full freedom to build the most optimal version of this tool. The spec says *what* and much of *how*; where it leaves room, choose the best engineering option, justify it, and record it.

---

## 1. Read first (in this order)

1. `docs/spec/00-overview.md` — vision, the loop, the hardening ladder, principles, glossary.
2. `docs/spec/13-decisions.md` — settled decisions. Do not relitigate them without new evidence.
3. `docs/spec/12-build-plan.md` — the phases and acceptance criteria you will execute.
4. `docs/spec/01-architecture.md` — layouts, components, performance budgets, dependencies.
5. The rest of `docs/spec/` (02–11) — read each fully before the phase that implements it; re-read the relevant sections at the start of each phase.

## 2. Mission and definition of finished

Execute Phases 0–12 of `docs/spec/12-build-plan.md`. Strata v0.1.0 is finished when:

- every phase's acceptance criteria are met and recorded in the build log;
- `just check` and `just test` are green, with no baselines and no unjustified suppressions;
- all performance budgets in `01-architecture.md` §7 are met and recorded;
- the engine repository is itself a clean Strata instance (`strata doctor` clean);
- the protocol eval standard suite passes (each scenario ≥ 2 of 3 runs) with results recorded;
- the README reflects reality, and its quickstart has been executed verbatim in a fresh temp repo;
- the release is prepared, and the user has been asked before tagging/publishing.

## 3. Authority — what you may do without asking

- Create and switch to branch `build/v1` from `main`. Commit freely on it — small, coherent commits; at least one per phase; conventional messages (`feat(search): …`, `test(index): …`, `docs(spec): …`). Follow the commit attribution conventions your harness provides.
- Push `build/v1` to `origin` and open a **draft** PR into `main` early (after Phase 1). Keep its description updated with phase progress. Mark it ready for review when Phase 11 is complete.
- Install developer tools via `go install`, `uv tool install`, `pnpm`, or Homebrew (non-sudo).
- Create scratch GitHub repositories **only** if needed to test `/garden` PR flow end-to-end — prefer a fake forge (fake `gh` on PATH) first, and ask before creating any real repo.
- Spawn subagents (≤ 4 concurrent) for parallel workstreams per the build plan's parallelization guidance.

**Ask the user first** before: merging anything, pushing to `main`, force-pushing, tagging or publishing a release, any `sudo` install, creating real remote repositories, deleting anything outside `build/v1` changes and scratch dirs, or changing a decision in `13-decisions.md`.

## 4. Continuity — you will lose context; plan for it

This build is long. Your context will be compacted, possibly many times. Strata's own philosophy applies to its construction:

- In Phase 0, create `projects/strata-v1/` (gitignored, local) with `project.md`, `plan.md`, `log.md`, `handoff.md` exactly as specified in `docs/spec/05-projects.md`. `plan.md` holds the phase checklist.
- **Log as you go** in `log.md` using the fixed prefixes (`tried:`, `failed:`, `worked:`, `learned:`, `decided:`, `blocked:`, `next:`, `note:`). Record every non-obvious finding, every spec deviation, every measured benchmark number.
- **Rewrite `handoff.md`** at the end of each phase, before any long-running subagent fan-out, and whenever you sense the context is getting long. It must be sufficient for a fresh session to continue with zero other context.
- **After any compaction or restart:** read `PROMPT.md`, then `projects/strata-v1/handoff.md`, then `plan.md`, then the current phase's spec sections — before doing anything else.
- Once the engine can do it (Phase 6+), switch to using `strata project log` / `strata project handoff` for these files. Once the KB exists (Phase 11), the `learned:`/`decided:` lines become shards.

## 5. Engineering standard (non-negotiable)

- **Quality is gate-enforced, not aspirational.** The strict profiles in `docs/spec/06-gates.md` apply to this repo from Phase 1. Fix root causes; never weaken a gate to pass. Suppressions need a rule id and a reason.
- **Tests first where it's cheap, always before "done."** Table-driven unit tests, golden tests for every output format, fuzz targets for every parser, integration tests on temp-dir instances, benchmarks for every budgeted operation.
- **Small packages with one job.** Respect the package map in `01-architecture.md` §2. `internal/cli` only parses and renders; logic lives in its domain package. Cyclomatic ≤ 15, cognitive ≤ 20, functions ≤ 80 lines.
- **DRY via shared packages,** not copy-paste: `fsutil` (atomic writes, locks, managed blocks) and `render` are shared by everything.
- **Determinism:** stable sort orders, no map-iteration-order leaks into output, injectable clocks for anything time-dependent.
- **Errors carry context** (`%w`), user-facing errors carry a hint, exit codes follow `09-cli.md` §2 exactly.
- **Performance is a requirement:** measure against `01-architecture.md` §7 on the synthetic corpus; record numbers; investigate any budget miss before moving on.
- **Dependencies are a liability:** add one only when it clearly beats writing it; check maintenance status and license; record the choice in the log.

## 6. Verify platform facts; don't trust memory

Claude Code's hook payloads, plugin manifest, and mod API change over time. Before implementing hooks (Phase 8) and the plugin (Phase 10):

- Load the `plugin-authoring` skill and grep its `types/claude-code.d.ts` for the APIs you use (`tool.call`, `session.start`, `$.process.run`, `$.ui.status`, `$.ui.open`, `$.command.register`, `$.tool.register`, agents).
- Confirm hook stdin fields and exit-code/JSON-output semantics against current Claude Code documentation (the `claude-code-guide` agent can fetch it). Capture real payloads from a live session into `engine/testdata/hooks/`.
- Likewise confirm third-party facts (e.g., `modernc.org/sqlite` FTS5/trigram support, golangci-lint v2 config schema) by spike or docs, not assumption.

Where the platform and the spec disagree, follow the platform, update the spec in the same commit, and log it.

## 7. Spec deviations

The spec is authoritative but not infallible. If implementation reveals a better design or a flaw:

- Small, local improvements (internal structure, naming, an extra flag): do it, update the spec section, log `decided:`.
- Anything that changes user-visible behavior, a decision in `13-decisions.md`, a performance budget, or the autonomy/security model: stop and ask the user, with a concise recommendation.

## 8. Working loop for each phase

1. Re-read the phase's spec sections and acceptance criteria. Update `plan.md` with a task breakdown.
2. Define or confirm interfaces first; write the acceptance tests (failing) where practical.
3. Implement in small steps, running the relevant `just` targets continuously.
4. Run `just check`, `just test`, and (from Phase 3) `just bench`. All green.
5. Verify acceptance criteria one by one; record evidence (commands run, numbers) in `log.md`.
6. Commit, push, update the PR description, rewrite `handoff.md`.
7. Give the user a short progress note: phase completed, notable decisions, anything needing their input. Then continue to the next phase without waiting unless something is blocked on them.

## 9. When you finish

Post a final summary to the user: what was built, measured performance vs. budgets, protocol-eval pass rates, known gaps and recommended next steps, the PR link, and the exact commands to install the binary and the plugin. Ask whether to tag and publish v0.1.0.
