<div align="center">

# ⛬ Strata

**Your repository's long-term memory, toolbelt, and immune system for Claude Code.**

*Knowledge settles in layers — and hardens into code.*

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/zbmowrey/strata/actions/workflows/ci.yml/badge.svg)](https://github.com/zbmowrey/strata/actions/workflows/ci.yml)
![Version](https://img.shields.io/badge/version-0.1.3-blue)
![Go](https://img.shields.io/badge/engine-Go-00ADD8?logo=go&logoColor=white)
![Claude Code](https://img.shields.io/badge/Claude%20Code-plugin-D97757)

</div>

> [!NOTE]
> **Strata 0.1.3** implements the full v1 design in [`docs/spec/`](docs/spec/). The output below is real,
> captured from the release build; numbers are from the release benchmarks.

---

## The problem

Every session, your agent starts over. It re-reads the same code to rediscover the same quirks. It improvises the same shell pipelines with slightly different bugs. It forgets the decision you explained last Tuesday — and the reason behind it. The rules in your `CLAUDE.md` slip under a long context, and the notes you *did* keep slowly rot into duplicates and contradictions.

**Strata makes each of those costs get paid once.**

## What it is

Strata is a drop-in, per-repository operating layer for [Claude Code](https://claude.com/claude-code). Run `strata init` in any repo and you get:

| | |
|---|---|
| 🧠 **Knowledge** | Atomic markdown "shards" with frontmatter — facts, gotchas, decisions, standards — in an Obsidian-compatible vault. Size-limited so each shard holds exactly one idea. |
| ⚡ **Search** | A Go binary with a SQLite FTS5 index: ranked results in **< 50 ms across 10,000+ shards**, one line per hit, then `strata show a b c` fetches exactly what's needed, in one call. |
| 📜 **Scriptorium** | A catalogued library of deterministic, tested scripts, each with a manifest declaring usage and **side-effect class** (read-only → destructive). Claude runs these before it starts improvising. |
| 📓 **Projects** | Local-only working memory for multi-session work: plan, an append-only chronicle of what was *tried and failed*, and a handoff note that survives context compaction. |
| 🛡️ **Gates** | Formatters, strict linters, type checkers, complexity limits, SAST, secret scanning, and tests, run **as Claude edits**. Failures block, and ratcheting baselines let you adopt Strata in a messy repo without being buried in errors. |
| 🌱 **Gardener** | Autonomous upkeep: files the inbox, merges duplicates, re-verifies stale facts, distills finished projects, and promotes knowledge into scripts and gates. It delivers everything as a PR for you to review. |
| 🔌 **Plugin** | `/garden`, `/capture`, `/bench`, `/strata resume`, a status line, a side pane, and specialist subagents (gardener, scout, scribe, scriptsmith, verifier). |

## The big idea: the hardening ladder

Most memory systems only store what the agent learns. Strata keeps pushing it toward determinism, one rung at a time.

```
  1. observation   "tests hung — DB fixture never torn down"          _inbox/ · project log
        ▼
  2. shard         gotcha: symptom · cause · fix · evidence            knowledge/
        ▼
  3. playbook      "debugging a hanging suite" (a Claude Code skill)   .claude/skills/
        ▼
  4. script        find-leaked-fixtures (tested, read-only)            scripts/
        ▼
  5. gate          semgrep rule — blocks the bug from ever landing     .strata/gates.toml
```

Recurrence, telemetry, and gate-failure patterns drive each promotion. A mistake made three times becomes a check that runs on every edit.

## The loop

```
   retrieve ──▶ act ──▶ verify ──▶ capture ──▶ consolidate ──▶ harden
      ▲                                                          │
      └──────────────────────────────────────────────────────────┘
```

- **Retrieve:** search before acting.
- **Act:** run a script when one exists.
- **Verify:** gates on every edit.
- **Capture:** record what was learned, at near-zero friction.
- **Consolidate and harden:** the gardener's job.

## A taste

```console
$ strata search 'tests hang at exit'
debug-hanging-tests            playbook      1.00  Procedure for diagnosing a test suite that hangs at exit —…
pytest-fixture-teardown        shard/gotcha  0.96  DB fixtures need explicit teardown — Session-scoped DB fix…
hanging-test-diagnosis         shard/howto   0.30  Find which fixture keeps the process alive — Dump thread s…
ci-flake:plan                  project       0.26  ci-flake plan — 1. Find the hang at session teardown. 2. W…
ci-flake:log#2026-10-21-10-00  project       0.24  ci-flake log 2026-10-21 10:00 — tried: `pytest -x --timeou…

$ strata show pytest-fixture-teardown
═══ pytest-fixture-teardown · shard/gotcha · knowledge/testing/pytest/pytest-fixture-teardown.md
trust: observed · updated 2026-10-01 · links: 3 out / 4 in
Session-scoped database fixtures that open a connection without a `yield` teardown leak it; pytest then hangs after the last test.

## Symptom
The suite prints its summary and never exits. `--timeout` does not fire because no test is running.
…

$ strata scripts list
check-deploy-config   read-only     go      Validate Helm values files for required resources and image tags.
find-leaked-fixtures  read-only     python  Detect pytest fixtures that acquire resources without teardown.
render-release-notes  writes-local  go      Render release notes from conventional commits since the last tag.
…

$ strata resume
ci-flake — Eliminate CI flakiness in the integration suite (active, last activity 2026-10-22, 15d ago)
  Next:
    1. Finish find-leaked-fixtures (strata run find-leaked-fixtures -- --path tests/).
```

(From the repository's test fixture, `engine/testdata/instance-basic`.)

## Quickstart

```bash
# 1. Install the engine (Go 1.26+; the module pins its toolchain)
go install github.com/zbmowrey/strata/engine/cmd/strata@v0.1.3
#    …or download a binary for macOS / Linux / Windows from the GitHub release
#    (gh release download v0.1.3 -R zbmowrey/strata -p 'strata_*_darwin_arm64.tar.gz').
#    The binaries are not notarized: if you download one with a browser on macOS, clear the
#    quarantine flag before running it: xattr -d com.apple.quarantine ./strata

# 2. Drop Strata into a repository
cd your-repo
strata init        # idempotent; --dry-run shows every change first
strata doctor      # tools, hooks, index, managed blocks
```

```console
$ strata init
.strata/config.toml
.strata/synonyms.txt
.strata/bench/retrieval.yaml
knowledge/_inbox/.gitkeep
knowledge/_questions/.gitkeep
scripts/.gitkeep
.strata/gates.toml
.strata/tools.toml
CLAUDE.md
.gitignore
.claude/settings.json
knowledge/_hubs/index.md
scripts/CATALOG.md
.git/hooks/pre-commit

$ strata doctor
✓ engine             strata 0.1.0 ≥ min_version 0.1.0
✓ managed            CLAUDE.md block, .gitignore blocks, hooks and allowlist current
✓ git-hook           pre-commit runs strata check --staged
✓ index              1 file indexed
✓ gates              11 gates configured
✓ tool:golangci-lint golangci-lint 2.15.0
…
✓ lint               1 file checked: 0 errors, 0 warnings

$ strata capture --type gotcha "The staging DB rejects connections without sslmode=require; local defaults to disable"
knowledge/_inbox/20261108-010459-the-staging-db-rejects-connections-witho.md

$ strata search 'staging database ssl'
20261108-010459-the-staging-db-rejects-…  inbox/gotcha  0.48  The staging DB rejects connections without sslm…
```

`init` detects your languages and writes gates for them (Go, Python, TypeScript, Rust, shell, docs,
security); `doctor` prints the install command for any missing tool.

```text
# 3. Install the Claude Code plugin (inside Claude Code)
/plugin marketplace add zbmowrey/strata
/plugin install strata@strata
```

Open Claude Code in the repository once, interactively, and accept the workspace trust dialog.
Until you do, Claude Code ignores the permission allowlist in `.claude/settings.json`, and every
`strata search` asks for permission. `strata doctor` then also reports the plugin as installed.

Start a session. The hooks `strata init` wrote into `.claude/settings.json` make Claude search
before it acts, block it on failing gates as it edits, and ask once per session for a knowledge
checkpoint. Run `/garden` whenever you like: it works in a separate worktree and opens a pull
request for you to review — it never merges.

## Design principles

1. **Markdown is truth; everything else is derived.** The index, hubs, catalogs, and permission allowlists are all regenerable from source.
2. **Context is the scarce resource.** Retrieval is two-step and budgeted; output size is benchmarked like latency.
3. **Deterministic beats clever.** If it repeats, script it. If it can be checked, gate it.
4. **Hard enforcement for code, gentle nudges for knowledge.**
5. **Self-healing by default, human by exception.** The gardener asks only about real conflict, risk, or ambiguity, and it never merges its own PRs.
6. **Fast enough to be reflexive.** If retrieval is slower than guessing, the agent will guess.
7. **Built to its own standard.** Strata is developed inside a Strata instance, under its strictest gates.

## Under the hood

- **Engine:** one static Go binary with no CGO. It uses pure-Go SQLite FTS5 with BM25 field weighting, synonym expansion, trigram fuzzing, link-graph expansion, and trust- and staleness-aware ranking. All of it is deterministic, and `--explain` shows how each score was computed.
- **Trust model:** `user-stated > verified > observed > inferred`. Only user-stated or verified standards and decisions are binding, and external material can inform but never instruct.
- **Safety:** script side-effect classes generate your permission allowlist. A local-only private tier is never committed, never gardened, and never sent anywhere. Secret scanning covers the knowledge base too.
- **Evals:** a retrieval bench (MRR, nDCG, recall@k, latency, bytes) and **protocol evals**, which run real headless Claude Code sessions and assert that the agent actually followed the rules.

## Documentation

| | |
|---|---|
| [Overview](docs/spec/00-overview.md) | Vision, loop, ladder, arms, glossary |
| [Architecture](docs/spec/01-architecture.md) | Layouts, components, performance budgets |
| [Knowledge](docs/spec/02-knowledge.md) · [Search](docs/spec/03-search.md) | Shard schema · index, ranking, query language |
| [Scriptorium](docs/spec/04-scriptorium.md) · [Projects](docs/spec/05-projects.md) | Scripts and manifests · working memory |
| [Gates](docs/spec/06-gates.md) · [Gardener](docs/spec/07-gardener.md) | Quality enforcement · autonomous upkeep |
| [Claude integration](docs/spec/08-claude-integration.md) · [CLI](docs/spec/09-cli.md) | Protocol, hooks, plugin · command reference |
| [Evals](docs/spec/10-evals.md) · [Security](docs/spec/11-security.md) | Bench and protocol evals · trust and safety |
| [Build plan](docs/spec/12-build-plan.md) · [Decisions](docs/spec/13-decisions.md) | Phases · decision log |

## Measured (v0.1.0)

| | |
|---|---|
| Performance, 10k-document corpus (p95) | search 19 ms · show ×10 17 ms · freshness check 11 ms · refresh after 10 edits 35 ms · full rebuild 1.6 s · post-edit hook 8 ms · session-start hook 25 ms · `garden --plan` 2.2 s — all within the [budgets](docs/spec/01-architecture.md) |
| Retrieval bench (this repository, 34 queries) | MRR 0.944 · recall@5 0.956 · nDCG@10 0.947 |
| Protocol evals (claude-opus-5-5, 3 headless runs each) | 12 / 12 scenarios pass — searches before reading code, batches `show`, reuses scripts, captures gotchas and corrections, fixes gate failures instead of suppressing them, resumes projects from the handoff, respects user-stated knowledge |

Strata is built inside a Strata instance: this repository's own `CLAUDE.md`, hooks, gates and
knowledge base are the dogfood.

## Development

```bash
just build          # bin/strata
just check          # every gate (gofumpt, golangci-lint strict, govulncheck, semgrep, gitleaks, typos, shellcheck, plugin lint, strata lint)
just test           # go test -race, coverage ≥ 80% per internal package
just bench          # performance budgets on the synthetic corpus
just plugin-check   # claude plugin validate + tests
strata bench retrieval / strata bench protocol   # retrieval bench and protocol evals
```

See [CHANGELOG.md](CHANGELOG.md) for what shipped.

## License

[MIT](LICENSE) © 2026 Zachary Mowrey
