# Changelog

All notable changes to Strata. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/). The engine (`strata` binary) and the
Claude Code plugin are versioned together.

## [Unreleased]

## [0.1.1] - 2026-11-08

Fixes found by installing 0.1.0 into a fresh repository through the public path.

### Fixed

- **Plugin:** `session.start` threw because `/resume` is a Claude Code built-in, and the refusal
  aborted every registration after it (`/project`, `/strata`, the status line, the
  `strata_search`/`strata_show` tools, the version check). Resume is now `/strata resume [slug]`.
  Each command and tool registers on its own, so a refused name is logged and skipped.
- **Gates fail closed:** a tool that exits non-zero without reporting a finding is now a blocking
  `tool-failed` error, not a pass. Before this, semgrep's exit 7 on a missing `--config` path (it
  still prints valid, empty SARIF) made the `sast` gate pass everything.
- **`strata init`** creates `.strata/semgrep/rules.yml` (empty starter rules) for the security
  profile's `sast` gate, and `strata upgrade` restores it when gates.toml reads the directory
  and it is missing.
- **Gate cache:** `config_files` entries may name directories (every file under them is
  hashed). The security profile's `sast` gate declares `.strata/semgrep/`, so rule changes
  invalidate cached results.
- **Version:** `doctor`, `init`/`upgrade` and `bench protocol` report the module version for
  `go install …@vX.Y.Z` builds, as `strata version` already did, instead of `0.1.0-dev`.

### Added

- `strata doctor` checks that the Claude Code plugin is installed and matches the engine
  (warning only: the hooks enforce without it), as docs/spec/09-cli.md §4 specifies.
- Quickstart: the trust-dialog step (Claude Code ignores `.claude/settings.json` permissions in
  untrusted workspaces) and the macOS quarantine note for browser-downloaded binaries.

## [0.1.0] - 2026-11-08

First release: the full v1 design in `docs/spec/` (Phases 0–12 of the build plan).

### Added

- **Knowledge base** (`knowledge/`): Markdown + YAML frontmatter shards with a schema, size
  limits, trust levels and lifecycle; `strata new shard|playbook`, `capture` (inbox; `--type correction` records corrections),
  `mv`, `supersede`, `archive`, generated hubs, and `questions ask|answer|apply|list|show` for
  escalations to a human. `strata lint [--fix]` validates everything.
- **Retrieval**: SQLite FTS5 index refreshed incrementally on every call (p95 ≈ 11 ms freshness
  check at 10k documents); `strata search` (BM25 with field weights, synonyms, trigram/edit
  fuzzy matching, any-term relaxation for natural-language questions, filters such as
  `kind:` `tag:` `domain:` `stale:`), `strata show` (many ids, one call, in order), `context`,
  `related`, `map`, fleet search across repositories.
- **Scriptorium** (`scripts/`): manifest-described Go and Python scripts with declared side
  effects, built and cached, run through `strata run` (policy-enforced: read-only scripts may
  not write); generated catalogs; shared libraries.
- **Projects** (`projects/`, local-only): chronicle logs with typed entries, handoffs,
  `strata resume`, and opt-in tracking of selected files via a managed `.gitignore` block.
- **Gates**: a language-aware gate runner (Go, Python, TypeScript, Rust, shell, docs, security)
  with output parsers, fix-mode gates, results cache, baselines with a ratchet, suppression
  rules, and per-module runs; `strata check [--changed|--staged|--all]`, `strata gates`.
- **Claude Code hooks** (`strata hook …`, wired by `strata init`): session-start orientation,
  pre-edit guards for generated files and managed blocks plus a once-per-file advisory,
  post-edit gates that block on findings, a bounded Stop gate with a once-per-session knowledge
  checkpoint, pre-compact handoff reminders. Hooks fail open.
- **Gardener**: `strata garden` (deterministic fixes and a plan of 16 task types) and
  `strata garden start|finish|abort` for `/garden`: a worktree, the `strata:gardener` agent,
  verification, then a draft → ready pull request through `gh` or `tea`. It never merges.
- **Plugin** (`plugin/`): `/garden`, `/capture`, `/promote`, `/bench`, `/resume`, `/project`,
  `/strata` (pane); status line; `strata_search`/`strata_show` model tools; agents (gardener,
  scout, scribe, scriptsmith, verifier) and skills.
- **Lifecycle**: `strata init` (idempotent, `--dry-run`, profile detection), `upgrade`,
  `doctor [--fix]`, `perms sync`, `status`, telemetry (`telemetry stats|prune`, local only).
- **Evals**: `strata bench retrieval` (recall@k, MRR, nDCG, latency, output size vs baseline),
  `strata bench protocol` (headless Claude Code sessions scored on tool-call traces; 12-scenario
  standard suite), `strata bench perf` (budgets from the spec).

### Measured at release

- Performance (10k-document corpus, Apple M-series): version 3.4 ms · freshness 10.9 ms ·
  search 18.8 ms · show ×10 17.0 ms · refresh after 10 edits 34.5 ms · rebuild 1.6 s ·
  hook post-edit 8.1 ms · hook session-start 25 ms · garden plan 2.2 s — all within budget.
- Retrieval bench (engine repo, 34 queries): MRR 0.944, recall@5 0.956, nDCG@10 0.947.
- Protocol suite (claude-opus-5-5, 3 runs each): 12/12 scenarios pass (11 at 3/3).

[Unreleased]: https://github.com/zbmowrey/strata/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/zbmowrey/strata/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/zbmowrey/strata/releases/tag/v0.1.0
