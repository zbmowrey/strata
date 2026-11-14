# 00 — Overview

> Strata is a drop-in, per-repository operating layer for Claude Code: a living knowledge base, a library of deterministic scripts, a set of blocking quality gates, and a gardener that keeps all of it organized — wired together so that what Claude learns today becomes something the machine *enforces* tomorrow.

This document is the entry point to the specification. Every other document in `docs/spec/` elaborates one part of what is sketched here. Where documents disagree, the more specific document wins; where a specific document is silent, this one governs. Design decisions and their rationale are recorded in [13-decisions.md](13-decisions.md) — do not relitigate them during the build without new evidence.

## 1. The problem

An AI coding agent working in a repository repeatedly pays for the same things:

- **Re-discovery.** Facts learned in one session (a quirk of the build, a decision and its reason, the right way to run a migration) are gone in the next. The agent re-reads code, re-runs experiments, and re-asks the human.
- **Non-determinism.** Operations that should be repeatable and exact (formatting, data transforms, release steps, audits) are re-improvised from scratch each time, with slightly different results and a non-zero error rate.
- **Soft rules.** Instructions in `CLAUDE.md` are suggestions. They slip under long contexts, after compaction, and in subagents.
- **Rot.** Hand-maintained knowledge stores decay: duplicates, contradictions, stale facts, broken links, and nobody to tend them.

Strata exists to make each of those costs paid **once**.

## 2. The core loop

```
   retrieve ──▶ act ──▶ verify ──▶ capture ──▶ consolidate ──▶ harden
      ▲                                                          │
      └──────────────────────────────────────────────────────────┘
```

| Step | What happens | Strata component |
|---|---|---|
| retrieve | Search knowledge, scripts, playbooks, and project history before acting | `strata search` / `strata show` ([03](03-search.md)) |
| act | Prefer running an existing deterministic script over improvising | Scriptorium ([04](04-scriptorium.md)) |
| verify | Every edit passes formatters, linters, type checks, scans, tests | Gates + hooks ([06](06-gates.md), [08](08-claude-integration.md)) |
| capture | Record non-obvious, reusable knowledge the moment it is learned; low friction | `strata capture`, project logs ([02](02-knowledge.md), [05](05-projects.md)) |
| consolidate | File the inbox, merge duplicates, split oversized shards, distill project logs into facts | Gardener ([07](07-gardener.md)) |
| harden | Push knowledge down the hardening ladder toward determinism | Gardener promotions ([07](07-gardener.md)) |

## 3. The hardening ladder

The defining idea of Strata. Knowledge is not merely stored; it **sediments** — each layer more solid, more deterministic, and cheaper to apply than the one above it.

| Rung | Form | Lives in | Example |
|---|---|---|---|
| 1. Observation | Inbox note, project log entry | `knowledge/_inbox/`, `projects/<slug>/log.md` | "pytest hung; DB fixture not torn down" |
| 2. Shard | Atomic fact / gotcha / decision | `knowledge/<domain>/<topic>/<id>.md` | "Fixture X requires explicit teardown — why, how" |
| 3. Playbook | Procedure needing judgment (a Claude Code skill) | `.claude/skills/<name>/SKILL.md` | "Debugging a hanging test suite" |
| 4. Script | Deterministic, tested tool | `scripts/<domain>/<name>/` | `find-leaked-fixtures` |
| 5. Gate | Blocking rule run by hooks / pre-commit / CI | `.strata/gates.toml`, semgrep rules, lint config | semgrep rule flagging fixtures without teardown |

Promotion is driven by evidence (recurrence counts, telemetry, gate-failure patterns) and executed by the gardener. A rung is never skipped silently: a gate links back to the shard that explains *why* it exists, and the shard links forward to the script or gate that now enforces it.

## 4. The arms

Strata is organized like an organism. Each arm has one job, one owner directory, and one spec.

| Arm | Analogy | Job | Spec |
|---|---|---|---|
| **Knowledge** | semantic memory | Atomic, searchable facts, standards, gotchas, decisions, references | [02](02-knowledge.md) |
| **Search** | recall | Sub-50 ms ranked retrieval over every corpus, batched multi-id fetch, link-graph traversal | [03](03-search.md) |
| **Scriptorium** | procedural memory (hard) | Catalogued, tested, side-effect-classified scripts | [04](04-scriptorium.md) |
| **Playbooks** | procedural memory (soft) | Claude Code skills for procedures that need judgment; indexed alongside shards | [02](02-knowledge.md) §9, [08](08-claude-integration.md) |
| **Projects** | episodic memory / working memory | One folder per epic: request, requirements, design, tests, decisions, chronicle, outcome; version control per instance mode | [05](05-projects.md) |
| **Gates** | immune system | Deterministic, blocking quality enforcement at edit, stop, commit, and CI time, with ratcheting baselines | [06](06-gates.md) |
| **Gardener** | metabolism / sleep | Autonomous consolidation, repair, and promotion; delivers changes as PRs | [07](07-gardener.md) |
| **Telemetry** | proprioception | Local event log feeding the gardener and the bench | [01](01-architecture.md) §8 |
| **Bench & evals** | fitness tests | Retrieval quality metrics and protocol-adherence evals | [10](10-evals.md) |
| **Trust & safety** | boundaries | Provenance, trust precedence, sensitive tier, side-effect permissions | [11](11-security.md) |
| **Claude integration** | nervous system | `CLAUDE.md` protocol, hooks, the Claude Code plugin (commands, agents, skills, status line, pane) | [08](08-claude-integration.md) |

## 5. Design principles

1. **Markdown is truth; everything else is derived.** Shards, script manifests, project files and questions are human-readable files in git. The search index, hubs, catalogs, permission allowlists, and `.gitignore` blocks are *generated* and can be deleted and rebuilt at any time. Nothing is maintained by hand in two places.
2. **Context is the scarce resource.** Every read path is two-step: compact, ranked one-line results first; full content only for the ids actually needed, fetched in one batched call. Output sizes are budgeted and measured.
3. **Deterministic beats clever.** If an operation is repeatable and can be exact, it becomes a script. If a rule can be checked by a tool, it becomes a gate. Judgment is reserved for what tools cannot decide.
4. **Hard enforcement for code, soft guidance for knowledge.** Code-quality hooks block. Knowledge hooks advise. (Decision D-008.)
5. **Small, atomic, linked.** One shard, one idea. Size limits are enforced by lint, which makes sharding a property of the system rather than a habit.
6. **Self-healing by default, human by exception.** The gardener acts autonomously and escalates only genuine conflict, risk, or ambiguity. All autonomous change arrives as a reviewable PR.
7. **Engine vs. content.** The engine (the `strata` binary and the plugin) is generic and versioned. Each repository's knowledge, scripts, and config are its own. Nothing repository-specific ever lives in the engine.
8. **Fast enough to be reflexive.** If retrieval is slower than guessing, the agent will guess. Performance budgets are requirements, benchmarked in CI ([01](01-architecture.md) §7).
9. **Obsidian-native.** The knowledge tree is a valid Obsidian vault: filenames are ids, `[[wikilinks]]` resolve, `tags` and `aliases` are native frontmatter.
10. **Built to its own standard.** Strata's own code passes the strictest gates it ships, and the Strata repository is itself a Strata instance (dogfooding).

## 6. What Strata is not

- **Not a global memory.** Strata is per-repository. A "hub" repository may federate child repositories (the panopticon pattern; [01](01-architecture.md) §10), but there is no machine-wide store.
- **Not an auto-injector.** Strata never stuffs retrieval results into every prompt. Claude retrieves deliberately, guided by the protocol. (Decision D-006.) The one narrow exception — an optional, once-per-file advisory on first edit — is configurable ([08](08-claude-integration.md) §4.2).
- **Not a vector database.** Retrieval is lexical (BM25 over FTS5) with aliases, synonyms, trigram fuzzing, and graph expansion. Semantic embeddings are a possible future extension, gated on bench evidence of a recall problem. (Decision D-005.)
- **Not a migration tool.** There is no importer for existing systems. Migrating an existing knowledge store is a one-off engineering effort. (Decision D-012.)
- **Not a task tracker.** Projects hold working state for the agent, not tickets for a team.

## 7. Glossary

| Term | Meaning |
|---|---|
| **Engine** | The `strata` Go binary. Generic, versioned, contains no repository content. |
| **Instance** | A repository into which Strata has been initialized (`strata init`). |
| **Shard** | One markdown file with frontmatter in `knowledge/`, holding one atomic idea. |
| **Hub** | A generated per-domain map of content listing its shards. |
| **Inbox** | `knowledge/_inbox/` — low-friction captures awaiting filing by the gardener. |
| **Question** | An escalation from the gardener (or Claude) awaiting a human answer; `knowledge/_questions/`. |
| **Script** | A catalogued, tested, deterministic program in `scripts/` with a `script.yaml` manifest. |
| **Playbook** | A Claude Code skill (`.claude/skills/*/SKILL.md`), indexed as `kind:playbook`. |
| **Project** | A local-only working folder in `projects/<slug>/` for multi-session work. |
| **Chronicle** | The append-only `log.md` of a project: what was tried, failed, decided, learned. |
| **Gate** | A deterministic check (formatter, linter, type checker, scanner, test) with a stage and a blocking mode. |
| **Baseline** | A recorded set of pre-existing gate violations that may only shrink (the ratchet). |
| **Gardener** | The two-phase maintenance process: deterministic `strata garden` + judgment-driven `/garden`. |
| **Trust level** | Provenance class of a shard: `user-stated` > `verified` > `observed` > `inferred`. |
| **Side-effect class** | A script's declared impact: `read-only` < `writes-local` < `network` < `destructive`. |
| **Kind** | Corpus type in the index: `shard`, `script`, `playbook`, `project`, `question`, `inbox`. |

## 8. Document map

| Doc | Contents |
|---|---|
| [01-architecture.md](01-architecture.md) | Repositories, layouts, components, data flow, config, concurrency, performance budgets, dependencies, telemetry, federation |
| [02-knowledge.md](02-knowledge.md) | Shard schema, types, sizing, hubs, inbox, questions, archive, writing rules, playbooks |
| [03-search.md](03-search.md) | Index schema, refresh, ranking, query language, `search`/`show`/`related`/`context`/`map`, output formats |
| [04-scriptorium.md](04-scriptorium.md) | Script layout, manifest, runtimes, shared libraries, catalogs, run/build, reuse-vs-create rules |
| [05-projects.md](05-projects.md) | Project record family and per-file standards, profiles, lifecycle, decisions and ratification, vcs modes, `.gitignore` generation |
| [06-gates.md](06-gates.md) | Gate model, stages, per-language toolchains, baselines/ratchet, suppressions, tool pinning |
| [07-gardener.md](07-gardener.md) | Deterministic and judgment phases, task catalog, autonomy matrix, worktrees, PR flow, digest |
| [08-claude-integration.md](08-claude-integration.md) | `CLAUDE.md` protocol (full text), hook adapters, the plugin (commands, agents, skills, status, pane, tools), permissions |
| [09-cli.md](09-cli.md) | Complete command reference, flags, exit codes, config reference |
| [10-evals.md](10-evals.md) | Retrieval bench, protocol evals, engine test strategy, performance benchmarks |
| [11-security.md](11-security.md) | Trust model, sensitive tier, prompt-injection posture, side-effect enforcement, supply chain |
| [12-build-plan.md](12-build-plan.md) | Phased build with acceptance criteria |
| [13-decisions.md](13-decisions.md) | Decision log (ADR-style) |
