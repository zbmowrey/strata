---
name: strata-scriptsmith
description: The Strata script creation protocol — when a script is required, run vs extend vs new, scaffolding with strata new script, manifest fields and side-effect classes, runtime choice, shared lib reuse, tests and the --format json contract, gates, catalog and linking. Use when an operation is repeatable and deterministic, before improvising a command you have run before, or when writing or changing anything under scripts/.
tags: [strata, scripts]
---

# Writing Strata scripts

## Find first

`strata search '<need> kind:script'` · `strata scripts list` · then `strata run <id> -- <args>`.

| Situation | Action |
|---|---|
| A script fits | Run it. |
| Nearly fits; change additive, backward-compatible, current behavior tested | Extend: new flag, existing output byte-identical, new tests. |
| Change alters semantics, interface, output, side-effect class, or callers unknown | New script; move shared code into `scripts/lib/<runtime>/` first — never copy-paste. |
| Nothing fits and it will recur | `strata new script <domain>/<name> [--runtime go\|python]` |
| Genuinely one-off | Run ad hoc (repeats are detected and become garden tasks). |

## Manifest (`script.yaml`) — scaffold placeholders fail lint until filled

`id, summary, description, status, runtime, entry, usage, examples, inputs, outputs,
exit_codes, side_effect, writes, network, idempotent, deterministic, requires, env, timeout,
tags, applies_to, shards, tests, owner`.

Side-effect class = the **maximum** of any code path:
`read-only` (stdout only) · `writes-local` (repo files matching declared `writes` globs,
checked after each run) · `network` (declared hosts; prompts) · `destructive` (needs `--yes`).

Runtime: Go by default. Python, Rust, TypeScript or Bash need `runtime_justification`
(≥ 40 chars, a real reason: Python for pandas/AST/PDF/ML; Bash only for ≤ 40 lines of glue).

## Conventions

`--help` from `usage`; `--format text|json` with a stable JSON shape; errors on stderr;
exit codes per manifest; no prompts unless destructive; no undeclared network; no secrets
read from repo files; no shell interpolation (use the lib exec helpers). Go:
`scripts/lib/go` (`cli`, `out`, `repo`, `exec`, `fsx`); Python: `strata_lib` (`cli`, `out`,
`repo`, `proc`, `fsx`).

## Done means

1. Manifest complete, `strata lint` clean, `side_effect` honest.
2. Tests: happy path, ≥ 1 error path, the `--format json` contract (golden file);
   `deterministic: true` → a test proving identical output across two runs.
3. `strata check <script dir>` green (format, lint, types, complexity, security, tests).
4. Linked both ways: script `shards:` ↔ shard `scripts:`.
5. `strata scripts catalog`; `strata search '<summary terms>'` finds it.
