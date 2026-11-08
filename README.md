<div align="center">

# ⛬ Strata

**Your repository's long-term memory, toolbelt, and immune system for Claude Code.**

*Knowledge settles in layers — and hardens into code.*

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Status](https://img.shields.io/badge/status-spec%20complete%20·%20engine%20in%20construction-orange)
![Go](https://img.shields.io/badge/engine-Go-00ADD8?logo=go&logoColor=white)
![Claude Code](https://img.shields.io/badge/Claude%20Code-plugin-D97757)

</div>

> [!IMPORTANT]
> **Strata is pre-alpha.** The full design is in [`docs/spec/`](docs/spec/) and the build is under way.
> Commands below describe the v0.1.0 target; the README will be updated with real output as each piece ships.

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
| 🔌 **Plugin** | `/garden`, `/capture`, `/bench`, `/resume`, a status line, a side pane, and specialist subagents (gardener, scout, scribe, scriptsmith, verifier). |

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

## A taste (v0.1.0 target)

```console
$ strata search 'tests hang at exit'
pytest-fixture-teardown   shard/gotcha   0.94  DB fixtures need explicit teardown — Session-scoped DB fixtures leak…
find-leaked-fixtures      script/ro      0.81  Detect pytest fixtures without teardown across tests/
debug-hanging-tests       playbook       0.66  Procedure for diagnosing a test suite that hangs at exit
ci-flake:log#2026-11-07   project        0.52  tried: pytest -x --timeout … failed: hang persists at teardown

$ strata show pytest-fixture-teardown find-leaked-fixtures --links 1 --budget 4000
$ strata run find-leaked-fixtures -- --path tests/
$ strata capture "pg pool must be closed in the session fixture finalizer" --type gotcha
```

## Quickstart (v0.1.0 target)

```bash
# 1. Install the engine
go install github.com/zbmowrey/strata/engine/cmd/strata@latest

# 2. Drop Strata into a repository
cd your-repo
strata init            # knowledge/, scripts/, projects/, .strata/, hooks, CLAUDE.md block
strata doctor          # verifies tools, hooks, index, managed blocks
```

```text
# 3. Install the Claude Code plugin (inside Claude Code)
/plugin install strata --marketplace zbmowrey/strata
```

Start a session. Claude now searches before it acts, reuses and writes scripts, captures what it learns, and can't leave a lint error behind. Run `/garden` whenever you like, or ask Claude to schedule it.

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

## Status and roadmap

- [x] Design and full specification
- [ ] Engine: documents, schema, lint
- [ ] Index and search (performance budgets met)
- [ ] Knowledge write paths, Scriptorium, Projects
- [ ] Gates, hooks, `init` / `doctor`
- [ ] Gardener and forge integration
- [ ] Claude Code plugin
- [ ] Dogfooding, retrieval bench, protocol evals
- [ ] v0.1.0 release

## License

[MIT](LICENSE) © 2026 Zachary Mowrey
