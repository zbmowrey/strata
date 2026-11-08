# 04 — Scriptorium

The Scriptorium is Strata's hard procedural memory: a catalogued library of deterministic, tested, side-effect-classified scripts that Claude finds and runs instead of improvising.

## 1. When a script is required

Claude must look for a script (`strata search '<need> kind:script'`) whenever an operation is **repeatable** and **can be deterministic**: transforms, audits, formatting, code generation, data fetch/normalize, release steps, environment checks, report generation, anything Claude has done (or will do) more than once.

| Situation | Action |
|---|---|
| A script fits | Run it (`strata run <id> -- args` or its direct invocation). |
| A script nearly fits; change is additive and backward-compatible; tests cover current behavior | Extend it (new flag/option), add tests, keep existing behavior byte-identical. |
| A script nearly fits but the change alters semantics, interface, output format, or side-effect class — or callers can't be enumerated | **Create a new script**, reusing shared code from `scripts/lib/`. Never fork by copy-paste: extract the common part into `lib/` first. |
| No script exists and the operation will plausibly recur | Create one (`strata new script`). |
| Genuinely one-off | Run ad hoc; if it recurs, the `bash` repetition detector will flag it for the gardener. |

## 2. Layout

```
scripts/
├── <domain>/<name>/
│   ├── script.yaml          # manifest (required)
│   ├── main.go | main.py    # entry point (runtime-specific)
│   ├── *_test.go | test_*.py
│   └── testdata/            # fixtures, golden outputs
├── lib/go/<pkg>/            # shared Go packages (module github.com/<org>/<repo>/scripts)
├── lib/py/strata_lib/       # shared Python package (installed in the uv project)
├── go.mod  go.sum           # one Go module for all Go scripts
├── pyproject.toml uv.lock   # one uv project for all Python scripts
└── CATALOG.md               # GENERATED
```

Script id = `<name>`; names are globally unique across domains (lint-enforced), kebab-case, verb-first (`find-leaked-fixtures`, `normalize-csv`, `check-migrations`).

## 3. Manifest (`script.yaml`)

```yaml
id: find-leaked-fixtures
summary: Detect pytest fixtures that acquire resources without teardown.   # ≤ 160 chars
description: |
  Parses conftest.py and test modules with the Python AST; reports fixtures that open
  connections/files/sockets without a yield-teardown or finalizer.
status: active               # draft|active|deprecated
replaced_by: null            # set when deprecated
runtime: python              # go|python|rust|typescript|bash
runtime_justification: >     # REQUIRED when runtime != go (see §5)
  Needs the Python `ast` module to parse Python sources faithfully; no Go equivalent.
entry: main.py
usage: find-leaked-fixtures [--path DIR] [--format text|json]
examples:
  - cmd: strata run find-leaked-fixtures -- --path tests/
    explain: Scan the whole test tree, human-readable output.
  - cmd: strata run find-leaked-fixtures -- --format json | jq '.[] | .file'
    explain: Machine-readable for piping.
inputs:  [{ name: --path, type: dir, default: ".", desc: "root to scan" }]
outputs: [{ kind: stdout, format: "text|json", desc: "one finding per line / JSON array" }]
exit_codes: { 0: no findings, 1: findings present, 2: usage or runtime error }
side_effect: read-only       # read-only|writes-local|network|destructive
writes: []                   # globs it may write (required unless read-only)
network: []                  # hosts it may contact (required if network)
idempotent: true
deterministic: true          # same inputs → same outputs (no time/random/network dependence)
requires: [uv]               # external tools on PATH
env: []                      # env vars read (names only; never values)
timeout: 60s
tags: [testing, pytest]
aliases: [fixture-leaks]
applies_to: ["tests/**"]
shards: [pytest-fixture-teardown]   # knowledge this implements
tests: [test_main.py]
owner: gardener              # gardener|user — user-owned scripts are never modified autonomously
```

Validated by `strata lint` against the compiled schema. The manifest is the single source of the script's documentation; `CATALOG.md` and `strata show <script-id>` render from it.

## 4. Side-effect classes

| Class | Meaning | Permission handling ([08](08-claude-integration.md) §7) | `strata run` behavior |
|---|---|---|---|
| `read-only` | Reads files/state; writes only to stdout/stderr | auto-allowed | runs |
| `writes-local` | Writes files inside the repo (declared `writes` globs) | auto-allowed | runs; post-run check that only declared globs changed (git status diff) — violation = error + telemetry |
| `network` | Contacts declared hosts; may write locally | prompt (not allowlisted) | runs; prints hosts first |
| `destructive` | Deletes/overwrites data, mutates external systems, irreversible | never allowlisted | requires `--yes`; prints a summary of what will happen first |

A script's class is the **maximum** effect of any code path. Changing a script's class is a semantic change (→ new script or explicit user approval).

## 5. Runtimes

Default: **Go**. Strictly typed, compiler-checked, fast to start once built, single toolchain shared with the engine (D-002).

Other runtimes require `runtime_justification` (lint-enforced, non-empty, ≥ 40 chars):

| Runtime | Acceptable justifications | Toolchain |
|---|---|---|
| Python | Mature library with no adequate Go equivalent (pandas/polars, numpy/scipy, the Python AST, PDF/OCR, ML); data-science notebooks-turned-scripts | `uv` project at `scripts/`; Python ≥ 3.12 |
| Rust | Truly latency-critical or memory-critical hot path where Go is measured insufficient | cargo workspace at `scripts/rust/` |
| TypeScript | Must use a JS-only ecosystem (browser automation, a JS SDK, the Claude Code plugin API) | `pnpm` + `tsx` at `scripts/ts/` |
| Bash | Glue ≤ 40 lines with no logic beyond sequencing commands | `shellcheck` + `shfmt`; `set -euo pipefail` mandatory |

Gates per runtime are defined in [06-gates.md](06-gates.md) §4.

## 6. Shared code (DRY)

- Any logic used by two scripts lives in `scripts/lib/<runtime>/`. Lint (`dupl` for Go, a jscpd-style duplicate detector across runtimes) flags duplicated blocks ≥ 50 tokens between scripts.
- `lib/go` starter packages created by `strata init --scripts`: `cli` (flag parsing conventions, `--format text|json`, exit-code helpers), `out` (stable JSON/text writers), `repo` (repo root discovery, path safety), `exec` (safe subprocess with timeouts, no shell), `fsx` (atomic writes).
- `lib/py/strata_lib` mirrors the same conventions.
- Conventions all scripts follow: `--help` is generated from the manifest `usage`; `--format json` for machine output; errors on stderr; exit codes per manifest; no interactive prompts unless `destructive`; no network unless declared; never read secrets from files in the repo.

## 7. Running and building

`strata run <id> [-- args…]`:

1. Resolve manifest; verify `requires` tools exist (fail with install hint).
2. Enforce side-effect policy (§4).
3. Go: use the cached binary at `.strata/bin/<id>` if its recorded source hash (script dir + `lib/go` + `go.sum`) is current; otherwise `go build` into the cache first (first run pays compile cost; subsequent runs start in ~5 ms). `strata scripts build [--all]` prebuilds.
4. Python: `uv run --project scripts python <entry>` (uv's cache makes warm starts fast).
5. Apply `timeout`; stream stdout/stderr; propagate exit code.
6. Emit `script_run` telemetry.

Direct invocation (`go run`, `uv run …`) also works; `strata run` is preferred because it enforces policy and records usage.

## 8. Catalogs

`scripts/CATALOG.md` is generated by `strata scripts catalog` (and by the gardener):

- ≤ 50 scripts: one table — `id | summary | side effect | runtime | usage`.
- > 50 scripts: the root catalog lists domains with counts and links; each `scripts/<domain>/CATALOG.md` holds the table for that domain. (This is the "multiple indices" sharding of the manifest; the search index is the primary discovery path regardless.)

## 9. Creating a script (the scriptsmith protocol)

`strata new script <domain>/<name> [--runtime go]` scaffolds the directory, a manifest with all required keys (placeholders that fail lint until filled), an entry point using `lib` conventions, and a failing test. Done means:

1. Manifest complete and lint-clean; `side_effect` honest.
2. Tests cover the happy path, at least one error path, and the `--format json` contract (golden file).
3. All gates for its runtime pass (format, lint, type check, complexity, security, tests).
4. `deterministic: true` scripts have a test proving identical output across two runs.
5. Linked from the shard(s) it implements (`scripts:` key) and vice versa (`shards:`).
6. Catalog regenerated; `strata search` finds it by its summary terms.

## 10. Deprecation

Set `status: deprecated` and `replaced_by: <id>`. `strata run` on a deprecated script prints a warning naming the replacement. The gardener removes deprecated scripts with zero runs in 90 days (via PR).
