# 10 — Evals and Testing

Strata is only as good as (a) whether retrieval finds the right thing, (b) whether Claude actually follows the protocol, and (c) whether the engine is correct and fast. Each has its own measurement.

## 1. Retrieval bench (`strata bench retrieval`)

Golden queries in `.strata/bench/retrieval.yaml`:

```yaml
- q: "tests hang at exit"
  relevant: { pytest-fixture-teardown: 3, debug-hanging-tests: 2, find-leaked-fixtures: 1 }   # graded relevance
  filters: ""            # optional inline filters
- q: "how do we deploy"
  relevant: { deploy-flow: 3 }
```

Metrics: recall@1/3/5/10, MRR, nDCG@10, latency p50/p95, output bytes p50/p90. Stored as `.strata/bench/baseline.json` when accepted (`--update-baseline`). CI and the gardener fail on regression beyond tolerance (MRR −0.02, recall@5 any drop, latency +20%, bytes +20%).

Growth: queries with zero results that the gardener resolves are added automatically (`bench-grow` task) with the resolving shard as relevance 3. A fresh instance starts with whatever the user/Claude adds; the engine repo ships its own bench over its own KB plus a synthetic-corpus bench.

## 2. Protocol evals (`strata bench protocol`)

Do sessions actually follow the protocol? Each scenario sets up a fixture instance, runs a real headless Claude Code session, and asserts over the tool-call trace.

```yaml
# .strata/evals/protocol/kb-first.yaml
id: kb-first
fixture: fixtures/basic          # copied to a temp dir (git-initialized) per run
setup: []                        # optional shell steps (strata capture …, file edits)
prompt: "Why do the integration tests hang at the end of the run?"
assert:
  - first_of: [strata_search, "Bash(strata search*)"]     # retrieval happens before any Read/Grep/WebSearch
    before: [Read, Grep, Glob, WebSearch, WebFetch]
  - called: "Bash(strata show*)"
  - answer_mentions: ["teardown"]
budget: { max_turns: 12, max_cost_usd: 0.50 }
runs: 3                           # pass if ≥ 2 of 3
```

Runner: copies the fixture to a temp dir, `strata init`s it if needed, runs `claude -p "<prompt>" --output-format stream-json --plugin-dir <plugin>` (plus a permission mode that allows the fixture's actions) with the instance's hooks, parses tool-use events, evaluates assertions. Assertions: `called`, `not_called`, `first_of … before …`, `called_with` (arg regex), `file_created` (glob + optional frontmatter match), `file_changed`, `gate_clean` (final `strata check --all` passes), `answer_mentions`, `max_calls` (e.g., `show` called ≤ 1 time — tests batching).

**Standard suite** (shipped in the engine; runnable against any instance via `--suite standard`):

| Scenario | Checks |
|---|---|
| `kb-first` | search precedes code reads for a question the KB answers |
| `show-batching` | multiple ids fetched in a single `show` |
| `script-reuse` | a task an existing script performs is done via `strata run`, not ad hoc |
| `script-create` | a repeatable operation with no script → new script with manifest, tests, gates green |
| `script-new-not-mutate` | near-fit script whose change is semantic → new script, original untouched |
| `capture-gotcha` | discovering a non-obvious trap → `strata capture --type gotcha` |
| `capture-correction` | user correction → capture with `--type correction` |
| `correct-shard` | a shard contradicted by code → shard fixed in place with source |
| `respect-user-stated` | a `user-stated` shard contradicted → asks / raises question; does not overwrite |
| `gate-response` | a post-edit gate failure → fixed, not suppressed |
| `project-continuity` | a session resumes an active project from `handoff.md` without redoing `failed:` items |
| `no-generated-edit` | attempts to edit a hub are redirected to the source |

Results: pass rate per scenario and overall, stored in `.strata/bench/protocol.json` with model id and date. This is the stress test of the framework itself: when a scenario fails, the fix is usually in the `CLAUDE.md` block wording, hook messages, or skill text — and the change is validated by re-running the suite. Costs tokens; run on demand and before releases, not in every CI run.

## 3. Engine test strategy

| Layer | Approach |
|---|---|
| Unit | Table-driven tests per package; `go-cmp` diffs |
| Parsers | Fuzz targets: frontmatter/doc scanner, query parser, hook stdin decoder, gate output parsers, gitignore glob validator. Fuzz corpora committed under `testdata/fuzz/` |
| Golden | CLI output (all formats) for a fixture instance; `-update` flag regenerates; reviewed in PRs |
| Integration | Temp-dir instances: init → capture → lint → search → show → garden plan; git operations against temp repos (incl. `git check-ignore` assertions for the projects block) |
| Hooks | Feed recorded hook payloads (fixtures captured from a real session during the build) to `strata hook …`; assert stdout/stderr/exit |
| Concurrency | Parallel `search` + `refresh` + `capture` under `-race`; lock contention tests |
| Property | Ranking determinism (same input → same order); `show` content == file content; index rebuild == incremental refresh result |
| Performance | `go test -bench` for every budgeted operation on the synthetic corpus (`tools/corpusgen --n 10000 --seed 1`); `strata bench perf` compares to `engine/testdata/perf-baseline.json`; CI fails on > 20% regression |
| Forge | Adapter tests with fake `gh`/`tea` executables on PATH that record argv |

Coverage floor: 80% per `internal/` package (gate-enforced). Mutation testing (e.g., `gremlins`) is a nice-to-have for `search` and `gates`, run manually.

## 4. Plugin tests

`claude plugin test plugin/` with `*.test.ts`: status formatting, command → argv mapping, engine-missing/version-mismatch handling, pane render on `terminal` and `desktop`, model-tool schemas. A fake `strata` on PATH returns canned JSON.
