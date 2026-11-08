# 12 — Build Plan

Phased build of Strata v0.1.0. Each phase ends with **all acceptance criteria met and `just check` green**, then a commit (or several) on the build branch. Phases are ordered by dependency; within a phase, independent workstreams may be parallelized with subagents once interfaces are fixed.

Global Definition of Done (applies to every phase):

- All gates for the touched languages pass at blocking level with no baselines and no unjustified suppressions.
- New code has tests; `internal/` package coverage ≥ 80%; parsers have fuzz targets.
- Public behavior matches the spec; where implementation forced a spec change, the spec is updated in the same commit and the change is recorded in [13-decisions.md](13-decisions.md).
- The build project's `log.md` and `handoff.md` are current (see PROMPT.md).

---

## Phase 0 — Orientation and toolchain

- Read all of `docs/spec/`. Create the local build project folder `projects/strata-v1/` by hand following [05](05-projects.md) (the engine doesn't exist yet): `project.md`, `plan.md` (this phase list with checkboxes), `log.md`, `handoff.md`.
- Verify toolchain; install what's missing (ask the user before any system-level install that needs sudo): Go (≥ 1.24; the user has 1.26), `just`, `golangci-lint` v2, `gofumpt`, `govulncheck`, `gitleaks`, `semgrep`, `typos`, `shellcheck`, `shfmt`, `osv-scanner`, Node LTS + `pnpm` (plugin), `uv` (Python script support tests), `gh`.
- Confirm platform contracts ([08](08-claude-integration.md) preamble): hook stdin/exit semantics, plugin manifest, mod API (`plugin-authoring` skill; `$.process.run`, `$.ui.status`, `$.ui.open`, `$.command.register`, `$.tool.register`, agents). Confirm `modernc.org/sqlite` FTS5 + trigram availability with a 20-line spike. Record findings in the build log (they become `reference` shards in Phase 11).

**Accept:** toolchain table in `log.md` with versions; spike results recorded; any spec deltas noted.

## Phase 1 — Skeleton and guardrails

- `engine/` Go module, `cmd/strata` with cobra root, `version` command (ldflags build info), `internal/` package stubs per [01](01-architecture.md) §2.
- Root `justfile` with all targets from [01](01-architecture.md) §11 (release can be a stub until Phase 12).
- Gate configs for the engine repo: `.golangci.yml` (strict profile, [06](06-gates.md) §4.1), `typos.toml`, `.semgrep/` custom rules, `.gitleaks.toml`, plugin `tsconfig`/eslint/prettier placeholders.
- CI: `.github/workflows/ci.yml` running `just check`, `just test`, `just bench` (perf job may be allowed to skip until Phase 3).
- git pre-commit hook via `just hooks` (installs a hook that runs `just check-staged`).

**Accept:** `just build && ./bin/strata version` works; `just check` and `just test` green on the skeleton; CI workflow passes on push.

## Phase 2 — Documents, schema, lint

- `internal/doc`: frontmatter split, YAML decode (goccy), body scan for headings (with slugging identical to Obsidian's anchor rules), wikilinks (`[[id]]`, `[[id#h]]`, `[[id|label]]`), code-fence awareness, line/byte counts. Fuzzed.
- `internal/schema`: validators for shard, inbox, question, hub, script manifest, project, playbook (lenient), with precise error positions.
- `internal/config`: config load/defaults/validation.
- `internal/corpus`: root discovery, walker, kind classification.
- `strata lint [--fix]` with all rules from [02](02-knowledge.md) §2.2, §5 and [04](04-scriptorium.md) §3, plus link resolution (needs an id table — build in memory here; index comes next).
- Fixture instance `engine/testdata/instance-basic/` (≈ 40 shards across 4 domains, 5 scripts, 2 playbooks, 1 project) used by all later phases.

**Accept:** lint golden tests over valid and invalid fixtures; fuzz targets run ≥ 60 s each without failure; `strata lint` on the fixture is clean.

## Phase 3 — Index and search

- `internal/index`: schema ([03](03-search.md) §3), migrations via `user_version`, incremental refresh, locking, corruption recovery, fallback scan.
- `internal/search`: query parser (fuzzed, round-trippable), filters, ranking ([03](03-search.md) §4) with `--explain`, synonyms, trigram assist.
- `internal/render`: line/table/json/jsonl/md, terminal width, budgets/truncation.
- Commands: `search`, `show` (multi-id, ordered, dedup, `#section`, `--links`, `--budget`, missing-id handling, exit 3), `related`, `context`, `map`.
- `engine/tools/corpusgen`; benchmarks for every budget in [01](01-architecture.md) §7; `perf-baseline.json`.

**Accept:** golden ranking tests; property tests (rebuild == incremental; show == file); all perf budgets met on the 10k synthetic corpus (record numbers in the log); fallback scan path tested.

## Phase 4 — Knowledge write paths

- `new shard`, `capture`, `mv`, `supersede`, `archive`, `hubs`, `questions` (list/show/ask/answer).
- `internal/fsutil`: atomic writes, flock, managed-block editor (marker-delimited regions in md/gitignore/json-adjacent files) — shared by hubs, CLAUDE.md block, gitignore, perms.

**Accept:** integration test: capture → appears in search as `inbox` immediately; `mv` rewrites all inbound links (golden); hubs regenerate deterministically and preserve hand-written intros.

## Phase 5 — Scriptorium

- Manifest parsing/validation, `new script` scaffolds (Go and Python templates + `lib` starters), `scripts list|build|catalog`, `run` with side-effect policy, build cache, timeout, env filtering, writes-local post-check, telemetry.
- Catalog generation incl. per-domain split.

**Accept:** fixture scripts run via `strata run` in Go and Python; writes-local violation detected in test; catalog golden test; a scaffolded script fails lint until filled, passes after.

## Phase 6 — Projects

- `project` subcommands, chronicle append, handoff template with Don't-redo prefill, `resume`, `gitignore sync` with the managed block ([05](05-projects.md) §5).

**Accept:** `git check-ignore` assertions over a temp repo for tracked/untracked project files; resume output golden; log entries indexed as individual documents.

## Phase 7 — Gates

- `gates.toml` model, stage selection, `{targets}` expansion, parallel runner, fix-before-check ordering, parsers ([06](06-gates.md) §6), findings formatter, results cache, baselines + ratchet, suppressions gate, `check`, `gates list|explain|baseline`.
- Profiles (Go, Python, TS, Rust, shell, docs, security) as embedded templates for `init`.

**Accept:** parser golden tests from recorded tool outputs; ratchet test (new finding blocks, fixed finding shrinks baseline, adding requires `--accept`); cache hit test; the engine repo's own `just check` can be expressed as `strata check --all` (dogfood switch happens in Phase 11).

## Phase 8 — Hooks and init

- `strata hook` adapters for all events ([08](08-claude-integration.md) §4) with per-session state, loop guard, knowledge nudge, pre-edit guards/advisory, telemetry.
- `init` (idempotent, `--dry-run`, profile detection, settings.json merge, CLAUDE.md block, gitignore blocks, git pre-commit hook, optional CI workflow), `upgrade`, `doctor [--fix]`, `perms sync`.
- Record real hook payloads from a live session into `engine/testdata/hooks/` and test against them.

**Accept:** `strata init` on an empty temp repo and on the fixture produces a working instance (doctor clean); re-running init changes nothing; hook golden tests pass; a live smoke test in a scratch repo shows: post-edit blocks a bad Go file, Stop blocks then releases after the bound, session-start prints the summary.

## Phase 9 — Gardener (deterministic) and forge

- `internal/garden`: mechanical fixes and all analyses ([07](07-gardener.md) §2): duplicate detection (trigram + MinHash), staleness, cold, orphans, gaps, scriptify (bash telemetry), promotions, distill; `plan.json` contract.
- `internal/telemetry`: writer, reader, stats, prune.
- `internal/forge`: gh + tea adapters with fake-binary tests.

**Accept:** plan golden test over a fixture seeded with every task type; mechanical fixes idempotent; `garden --plan` ≤ 5 s on the synthetic corpus.

## Phase 10 — Plugin

- Load the `plugin-authoring` skill first. Build `plugin/` per [08](08-claude-integration.md) §5: manifest, marketplace entry, `register.ts` (commands, status, pane, model tools, version check, no duplicate hooks), agents, skills, types contract, tests.
- `/garden` end-to-end: lock → plan → worktree → `strata:gardener` agent → commits → `strata check --all` + bench → draft PR → ready.

**Accept:** `just plugin-check` green; `/garden` run against a scratch GitHub repo (or a fake forge) produces a correct draft → ready PR with a digest; status line and pane render on terminal.

## Phase 11 — Dogfood and evals

- `strata init` the engine repo itself. Seed its KB with real knowledge from the build (confirmed platform contracts, gotchas hit, decisions — convert `projects/strata-v1/log.md` `learned:`/`decided:` lines), its own scripts if any emerged, retrieval bench (≥ 30 queries).
- `internal/bench`: retrieval bench + protocol eval runner; implement the standard suite ([10](10-evals.md) §2) with fixtures.
- Run the protocol suite; iterate on `CLAUDE.md` block wording, hook messages, and skills until every scenario passes ≥ 2/3.

**Accept:** engine repo is a clean Strata instance (`strata doctor` clean, `strata check --all` green); retrieval bench baseline recorded; protocol suite results recorded with model id.

## Phase 12 — Release

- goreleaser config; `just release`; README updated to reflect reality (install, quickstart, real command output, status badge); CHANGELOG; tag `v0.1.0` (ask the user before tagging/publishing a release).

**Accept:** `go install github.com/zbmowrey/strata/engine/cmd/strata@v0.1.0` works on a clean machine path; `/plugin install strata --marketplace zbmowrey/strata` installs the plugin; README quickstart verified by actually running it in a fresh temp repo.

---

## Parallelization guidance

- Phases 0–3 are sequential (foundation).
- After Phase 3, interfaces (`doc`, `schema`, `index`, `render`, `fsutil`) are frozen; Phases 4, 5, 6, 7 can proceed in parallel workstreams (separate subagents, separate packages, one integrator).
- Phase 8 depends on 4–7; 9 depends on 4–8; 10 depends on 8–9; 11–12 last.
- When parallelizing: ≤ 4 concurrent builder subagents; each gets a brief naming its package(s), the spec sections, the frozen interfaces, and the acceptance tests; the main session integrates, runs `just check`, and commits.
