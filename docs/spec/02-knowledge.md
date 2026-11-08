# 02 — Knowledge

The knowledge arm is Strata's semantic memory: atomic, linked, searchable markdown files ("shards") under `knowledge/`.

## 1. Shards

A shard is **one idea**: one fact, one gotcha, one decision, one standard, one procedure. If a shard needs a second H2 section that could stand on its own, it is two shards.

- Path: `knowledge/<domain>/<topic>/<id>.md`. `domain` and `topic` are **derived from the path**, never stored in frontmatter (no drift). One level of `topic` is required; deeper nesting is allowed but discouraged.
- **Filename stem is the id.** Ids are globally unique across `knowledge/` (lint-enforced). This is what makes `[[id]]` resolve identically in Strata and in Obsidian.
- Id format: lowercase kebab-case, `^[a-z0-9]+(-[a-z0-9]+)*$`, 3–64 chars, descriptive (`pytest-fixture-teardown`, not `note-17`).
- Moving a shard between folders never changes its id. Renaming an id uses `strata mv <old> <new>`, which rewrites every inbound link and adds `<old>` to `aliases`.

## 2. Frontmatter schema

```yaml
---
id: pytest-fixture-teardown           # required; == filename stem
title: DB fixtures need explicit teardown   # required; ≤ 80 chars
summary: Session-scoped DB fixtures leak connections unless torn down; tests then hang at exit.  # required; one line, ≤ 160 chars; shown in search results
type: gotcha                           # required; see §3
tags: [testing, pytest, database]      # required (≥ 1); Obsidian-native
aliases: [fixture-leak, hanging-tests] # optional; Obsidian-native; searched with high weight
status: active                         # required; draft|active|superseded|deprecated|archived
trust: observed                        # required; user-stated|verified|observed|inferred (see 11)
created: 2026-11-07                    # required; ISO date
updated: 2026-11-07                    # required; ISO date; bumped on every content change
sources:                               # required for trust ≥ observed; see §2.1
  - { kind: session, ref: "projects/ci-flake/log.md#2026-11-07", at: 2026-11-07 }
verified_at: 2026-11-07                # optional; last time the claim was confirmed true
review_after: 180d                     # optional; duration; defaults by type (§7)
verify: { script: check-fixture-teardown, args: ["--all"] }  # optional; script that re-verifies the claim
applies_to: ["tests/**/conftest.py"]   # optional; globs of code this shard is about (drives `strata context` and the pre-edit advisory)
scripts: [find-leaked-fixtures]        # optional; scripts that implement/enforce this
gates: [semgrep:fixture-teardown]      # optional; gates that enforce this
supersedes: [old-fixture-advice]       # optional
superseded_by: null                    # set when status=superseded
occurrences: 2                         # optional; gotcha/correction recurrence count — promotion signal
sensitive: false                       # optional; true only valid under knowledge/_private/
---
```

### 2.1 Sources

`kind` ∈ `user` (the human said so; `ref` = short quote or context), `url`, `file` (repo path + optional line/anchor), `commit` (sha), `session` (project log anchor), `script` (script id + invocation that verified it), `doc` (external doc title/version). `at` is the date observed. Sources are what make correction possible: a shard without a source can't be re-verified.

### 2.2 Validation

`strata lint` validates every shard against the schema (compiled into the engine; instance-level extra required fields allowed via `[knowledge] extra_required`). Errors are blocking at the edit stage for the touched file and at commit/CI for all files. Unknown frontmatter keys are a warning (allows user extensions; Obsidian adds its own keys).

## 3. Types

| Type | Holds | Normative? | Default `review_after` |
|---|---|---|---|
| `fact` | A true statement about this repo/system/domain | no | 180d |
| `gotcha` | A trap, its symptom, cause, and fix | no | 365d |
| `howto` | A short procedure (≤ 10 steps); longer → playbook | no | 180d |
| `decision` | A choice made, alternatives, and why (ADR) | **yes** | never (superseded instead) |
| `standard` | A rule the codebase follows + how it is checked | **yes** | 365d |
| `reference` | Distilled external material (API notes, vendor docs) | no | 90d |
| `glossary` | A domain term and its precise meaning | no | 365d |
| `correction` | A recorded mistake + the right answer (often promoted to gotcha) | no | 365d |

**Normative** types (`decision`, `standard`) direct behavior. Only `trust: user-stated` or `trust: verified` normative shards are binding; an `inferred` standard is a proposal and is rendered with a `[proposed]` marker in search output ([11](11-security.md) §2).

## 4. Body conventions

- First paragraph: the claim, stated plainly, in ≤ 3 sentences. Search snippets and `show --brief` use it.
- Optional sections, in this order, only when they add value: `## Why`, `## How to apply`, `## Evidence`, `## See also`.
- `gotcha`: `## Symptom`, `## Cause`, `## Fix`.
- `decision`: `## Context`, `## Decision`, `## Alternatives`, `## Consequences`.
- `standard`: `## Rule`, `## Rationale`, `## Enforcement` (names the gate/script; "none yet" is a promotion signal).
- Inline `[[wikilinks]]` are first-class edges in the link graph; `[[id#Heading]]` and `[[id|label]]` are supported.
- Code blocks are allowed but keep them short; long code belongs in a script.

## 5. Size limits (the sharding forcing function)

| Limit | Default | Severity |
|---|---|---|
| Body lines (excluding frontmatter, blank lines, code fences' contents counted at 0.5) | 120 | error |
| Body bytes | 6,144 | error |
| `summary` length | 160 chars | error |
| `title` length | 80 chars | error |
| H2 sections | 6 | warning |
| Outbound links | 30 | warning |

Limits are configurable per instance but the defaults are deliberate. An oversize shard at the edit stage produces a blocking lint error with the hint: *"split into focused shards; see `strata new shard --help`"*. (Shard lint is part of the knowledge arm but blocks like code — it is a **structural** check, not a knowledge-capture nudge; D-008.)

## 6. Hubs

`knowledge/_hubs/<domain>.md` is generated by `strata hubs` (and by the gardener):

```markdown
---
id: hub-testing
title: Testing
summary: Map of everything known about testing in this repository.
type: index
...
---
Hand-written intro is preserved here.

<!-- strata:hub:begin — GENERATED, do not edit below -->
## pytest  (12)
- [[pytest-fixture-teardown]] — Session-scoped DB fixtures leak connections unless torn down…
...
<!-- strata:hub:end -->
```

Topics are sorted by shard count desc, shards by `updated` desc. Superseded/archived shards are excluded. A root `knowledge/_hubs/index.md` lists domains with counts and one-line descriptions (taken from each hub's `summary`). `strata map` renders the same tree in the terminal.

## 7. Lifecycle and staleness

```
draft ──▶ active ──▶ superseded (superseded_by set; kept, de-ranked)
                 └──▶ deprecated (no replacement; kept, de-ranked)
                 └──▶ archived (moved to _archive/; excluded from default search)
```

- **Stale** = `today > (verified_at or updated) + review_after`. Stale shards are flagged in search output (`⚠ stale`) and become gardener tasks: if `verify.script` exists, the gardener runs it and bumps `verified_at` on success or opens a correction task on failure; otherwise it re-checks against sources.
- **Cold** = no `search`/`show` hits in telemetry for 180 days and no inbound links. Cold shards are archive candidates (never deleted).
- **Deletion** is reserved for exact duplicates (after merge) and content that was wrong with no historical value — and is always via the gardener PR, never silent.

## 8. Inbox, questions, archive

### 8.1 Inbox (`knowledge/_inbox/`)

`strata capture "text" [--type gotcha|fact|correction|…] [--tags a,b] [--source …] [--project slug]` writes `_inbox/<YYYYMMDD-HHMMSS>-<slug>.md` with minimal frontmatter (`type`, `captured`, `session`, `project`, `sources`) and the raw text. Captures have **no size or schema requirements** beyond that — capture must be nearly free. Inbox items are searchable (`kind:inbox`) so a fresh capture is retrievable immediately. The gardener files them into shards (merge into an existing shard, create a new one, or discard with reason).

`--type correction` additionally increments `occurrences` on any shard named with `--shard <id>` and emits a `correction` telemetry event.

### 8.2 Questions (`knowledge/_questions/`)

Escalations that need a human. One file per question:

```yaml
---
id: q-20261107-fixture-scope
status: open            # open|answered|withdrawn
raised_by: gardener     # gardener|claude
raised: 2026-11-07
blocking: [pytest-fixture-teardown]   # shards/tasks waiting on the answer
---
## Question
Shard [[x]] (user-stated) says A; script `y` output shows B. Which is correct?
## Options
1. A is still correct — `y` is wrong.  2. B is correct — update [[x]].
## Answer
<!-- human writes here; then: strata questions answer <id> -->
```

`strata questions list` shows open questions; the SessionStart summary shows the count; the plugin status line shows the count; garden PR bodies list newly raised ones. When answered, the gardener applies the answer and records it as a `trust: user-stated` source.

### 8.3 Archive (`knowledge/_archive/`)

Mirrors the domain tree. Archived shards keep their ids (links still resolve; `show` marks them archived). Search excludes them unless `--archived` or `status:archived`.

## 9. Playbooks (Claude Code skills)

Procedures that need judgment live as Claude Code skills in `.claude/skills/<name>/SKILL.md` (repo-specific) and in the plugin (Strata's own). Strata indexes them as `kind:playbook` using the skill's `name` and `description` frontmatter plus body, so `strata search` finds them alongside shards and scripts. Strata does not add required frontmatter to skills (they must remain valid skills); optional `tags`, `aliases`, `scripts`, `shards` keys are read if present. `strata new playbook <name>` scaffolds one.

## 10. Writing rules (what goes in, what stays out)

**Capture** when the knowledge is: non-obvious; likely to matter again; true beyond this one moment; and not cheaply derivable from the code, git history, or tool output.

**Do not capture**: what the code says plainly; what `git log` records; ephemeral task state (that belongs in the project log); secrets (ever — they go nowhere in `knowledge/`, not even `_private/`; use the secret manager and record only *where* the secret lives).

**Correct in place.** When a shard is wrong: fix it now, add the evidence to `sources`, bump `updated` and `verified_at`. If the correct version is a different claim with its own history, create the new shard and mark the old one `superseded`. Never silently overwrite a `user-stated` shard — raise a question ([07](07-gardener.md) §5).

**One owner per fact.** If a fact appears in two shards, one of them links to the other instead of repeating it. Duplicate detection ([07](07-gardener.md) §3) enforces this.

## 11. Commands (summary; full reference in 09)

`strata new shard <domain>/<topic>/<id>` · `strata capture` · `strata mv` · `strata supersede <old> <new>` · `strata archive <id>` · `strata lint [--fix]` · `strata hubs` · `strata questions list|show|answer` · `strata map`.
