# 01 — Architecture

## 1. Two kinds of repository

| | Engine repository | Instance repository |
|---|---|---|
| What | `github.com/zbmowrey/strata` — this repo | Any repo where `strata init` has been run |
| Contains | Go engine source, the Claude Code plugin, scaffold templates, specs | Knowledge, scripts, projects, config — that repo's content |
| Versioned by | semver tags (`v0.x.y`) | its own history; pins a minimum engine version in `.strata/config.toml` |

The engine repository is **also** an instance (dogfooding, D-015): it has its own `knowledge/`, `scripts/`, `.strata/`, and a Strata-managed block in its `CLAUDE.md`.

## 2. Engine repository layout

```
strata/
├── README.md  LICENSE  PROMPT.md  CLAUDE.md
├── justfile                      # build, install, test, check, bench, release, plugin targets
├── .github/workflows/            # ci.yml (gates + tests + bench), release.yml
├── .claude-plugin/
│   └── marketplace.json          # marketplace listing; entry "strata" → ./plugin
├── docs/spec/                    # this specification
├── engine/                       # ALL Go source lives here (single Go module)
│   ├── go.mod                    # module github.com/zbmowrey/strata/engine
│   ├── cmd/strata/main.go        # thin: wires cobra root, calls internal/cli
│   ├── internal/
│   │   ├── cli/                  # cobra commands; argument parsing and output only
│   │   ├── config/               # .strata/config.toml load/validate/defaults
│   │   ├── corpus/               # walking, file classification, change detection
│   │   ├── doc/                  # frontmatter + body scanner, wikilinks, headings, sections
│   │   ├── schema/               # JSON-schema-equivalent validation for each kind
│   │   ├── index/                # SQLite FTS5 store, migrations, incremental refresh
│   │   ├── search/               # query language parser, ranking, result assembly
│   │   ├── render/               # line/table/json/jsonl/md renderers, budgets, truncation
│   │   ├── graph/                # link graph, BFS expansion, orphan/broken-link analysis
│   │   ├── knowledge/            # shard create/move/supersede, hubs, inbox, questions
│   │   ├── scripts/              # manifests, catalogs, build cache, run wrapper
│   │   ├── projects/             # project lifecycle, chronicle append, gitignore block
│   │   ├── gates/                # gate config, runner, baselines, stage selection
│   │   ├── hooks/                # Claude Code hook adapters (stdin JSON → actions → stdout/exit)
│   │   ├── garden/               # deterministic gardener: analyses → plan.json
│   │   ├── telemetry/            # JSONL event writer/reader, stats
│   │   ├── bench/                # retrieval bench, protocol eval harness
│   │   ├── forge/                # gh / tea adapters (PRs, ready-for-review)
│   │   ├── scaffold/             # go:embed templates for init/upgrade; CLAUDE.md block
│   │   ├── perms/                # .claude/settings.json allowlist generator
│   │   ├── doctor/               # environment + instance health checks
│   │   ├── fsutil/               # atomic writes, file locks, managed-block editing
│   │   └── version/              # build info, min-version checks, migrations registry
│   ├── testdata/                 # fixtures, golden outputs, fuzz corpora
│   └── tools/corpusgen/          # synthetic 10k+ corpus generator for perf tests
├── plugin/                       # Claude Code plugin (TypeScript hooks module + md assets)
│   ├── .claude-plugin/plugin.json
│   ├── hooks/hooks.json          # { "modules": ["./register.ts"] }
│   ├── hooks/register.ts         # thin: every action shells out to `strata`
│   ├── types/index.d.ts          # $.state contract
│   ├── agents/*.md               # gardener, scout, scribe, scriptsmith, verifier
│   ├── skills/*/SKILL.md         # strata-capture, strata-scriptsmith, strata-garden, strata-project
│   └── *.test.ts                 # claude plugin test
├── knowledge/  scripts/  .strata/  .claude/   # dogfood instance content
└── projects/                     # local-only (gitignored), incl. the build's own project
```

`engine/` is the single home for Go source (user requirement). The `justfile` at the repo root drives everything (§11).

## 3. Instance layout (what `strata init` creates)

All paths are configurable in `.strata/config.toml`; these are defaults.

```
<repo>/
├── CLAUDE.md                     # user content + <!-- strata:begin --> managed block <!-- strata:end -->
├── knowledge/                    # Obsidian-compatible vault (open this folder or the repo root)
│   ├── <domain>/<topic>/<id>.md  # shards; filename stem == id (globally unique)
│   ├── _hubs/<domain>.md         # GENERATED domain maps (hand-written intro above marker preserved)
│   ├── _inbox/<ts>-<slug>.md     # captures awaiting filing
│   ├── _questions/<id>.md        # escalations awaiting a human answer
│   ├── _archive/<domain>/...     # archived shards (excluded from default search)
│   └── _private/                 # sensitive tier — gitignored, local-only (see 11)
├── scripts/
│   ├── <domain>/<name>/          # script.yaml + source + tests
│   ├── lib/go/  lib/py/          # shared code (DRY)
│   ├── go.mod                    # Go scripts module (if any Go scripts)
│   ├── pyproject.toml            # uv project for Python scripts (if any)
│   └── CATALOG.md                # GENERATED; per-domain CATALOG.md files when > 50 scripts
├── projects/                     # local-only working memory (see 05)
├── .claude/
│   ├── settings.json             # hooks (managed keys) + generated permission allowlist
│   └── skills/<name>/SKILL.md    # playbooks (indexed as kind:playbook)
└── .strata/
    ├── config.toml               # instance config (committed)
    ├── gates.toml                # gate definitions (committed)
    ├── tools.toml                # pinned tool versions (committed)
    ├── baselines/<gate>.json     # ratchet baselines (committed)
    ├── bench/retrieval.yaml      # golden queries (committed)
    ├── bench/baseline.json       # last accepted bench metrics (committed)
    ├── evals/protocol/*.yaml     # protocol eval scenarios (committed)
    ├── synonyms.txt              # query expansion (committed)
    ├── index.db  index.db-wal    # GENERATED (gitignored)
    ├── telemetry/*.jsonl         # local only (gitignored)
    ├── session/<session_id>.json # hook session state (gitignored)
    ├── garden/plan.json  garden.lock   # gitignored
    └── bin/                      # compiled script cache (gitignored)
```

Underscore-prefixed directories in `knowledge/` are system directories; shards never live in them except `_archive/` and `_private/`.

## 4. Components and data flow

```
                ┌─────────────── Claude Code session ────────────────┐
                │  CLAUDE.md protocol   plugin (commands, agents,     │
                │                       skills, status, pane, tools)  │
                └──────┬──────────────────────┬─────────────────────┬─┘
             Bash /    │ hooks (stdin JSON)   │ $.process.run        │ edits files
             tools     ▼                      ▼                      ▼
        ┌──────────────────────────── strata (Go binary) ─────────────────────────┐
        │ cli ─▶ search ─▶ index (SQLite FTS5) ◀─ refresh ◀─ corpus ◀─ doc/schema │
        │  │                     ▲                                                │
        │  ├─▶ knowledge / scripts / projects  (write paths: atomic, locked)      │
        │  ├─▶ gates ─▶ external tools (gofumpt, golangci-lint, ruff, semgrep…)   │
        │  ├─▶ hooks adapters ─▶ gates / search / projects / telemetry            │
        │  ├─▶ garden (analyses over index + telemetry + git) ─▶ plan.json        │
        │  ├─▶ forge (gh | tea) ─▶ PRs                                            │
        │  └─▶ telemetry (append JSONL)                                           │
        └─────────────────────────────────────────────────────────────────────────┘
                ▲                                    │
                └──────── markdown + yaml in git ◀───┘   (source of truth)
```

**Invariant:** every write the engine makes to source-of-truth files is (a) atomic (write temp + rename), (b) under an advisory file lock where concurrent writers are possible, and (c) leaves the file schema-valid. Generated files carry a header comment `<!-- GENERATED by strata — edit <source> instead -->` and the pre-edit hook refuses manual edits to them (D-019).

## 5. Process model

- `strata` is a short-lived CLI process. No daemon in v1 (D-004). Every invocation that reads the index performs a **freshness check** (§6) first, so the index is never stale from the caller's point of view.
- Hook invocations are the same binary (`strata hook <event>`), which reads the hook JSON from stdin.
- The plugin never re-implements logic: it calls `strata ... --format json` via `$.process.run` and renders results (D-016).

## 6. Index freshness and concurrency

- **Change detection:** parallel directory walk (bounded worker pool, `GOMAXPROCS`), compare `(path, size, mtime_ns)` against the `files` table. Changed or new files are re-read and hashed (xxh3); a hash match updates only mtime. Deleted paths are removed. Target: freshness check on an unchanged 10k-file corpus ≤ 15 ms warm.
- **Writer exclusivity:** SQLite in WAL mode; refresh runs in a single write transaction guarded by `flock` on `.strata/index.lock`. Readers never block. If the lock is held, a reader proceeds on the current snapshot unless `--strict-fresh`.
- **Schema versioning:** `PRAGMA user_version` holds the index schema version; mismatch triggers a full rebuild (the index is disposable).
- **Corruption:** any SQLite error on open → delete and rebuild, log a telemetry event.
- **Fallback:** if SQLite is unavailable or the rebuild fails, `search` degrades to a parallel in-process scan with the same ranking signals minus BM25 statistics, and prints a warning to stderr.

## 7. Performance budgets (requirements, benchmarked in CI)

Measured on the synthetic corpus from `engine/tools/corpusgen` (10,000 shards averaging 3 KB, 300 scripts, 50 playbooks, 2,000 links) on an Apple-silicon laptop; CI uses relative regression thresholds (≤ 20% regression vs. stored baseline fails).

| Operation | Budget (p95) |
|---|---|
| Process start → first output (`strata version`) | 10 ms |
| Freshness check, no changes | 15 ms |
| `search` (warm, incl. freshness) | 50 ms |
| `show` 10 ids | 30 ms |
| `show --links 1` (≤ 40 docs) | 60 ms |
| Incremental refresh, 10 changed files | 80 ms |
| Full rebuild, 10k shards | 3 s |
| `hook post-edit` overhead excluding external tools | 25 ms |
| `hook session-start` total | 300 ms |
| `garden --plan` (deterministic analyses) | 5 s |

## 8. Telemetry

Local, append-only JSONL under `.strata/telemetry/YYYY-MM.jsonl`; gitignored; retention configurable (default 180 days, pruned by the gardener). Never transmitted anywhere. Each event: `{ts, v, session, event, ...fields}`.

| Event | Fields | Consumer |
|---|---|---|
| `search` | query, filters, n_results, top_ids[≤5], zero (bool), ms | gaps list, bench growth, hit counts |
| `show` | ids, missing, bytes | hit counts, cold-shard detection |
| `script_run` | id, exit, ms, side_effect | usage, flaky-script detection |
| `gate` | gate, stage, files_n, result, rules[{id,count}] | recurring-failure promotion |
| `hook` | event, ms, outcome | performance, protocol evals |
| `capture` | kind, type, project | capture rate |
| `bash` | first_token, normalized_hash, length | repeated-command → scriptify candidates |
| `correction` | shard_id? | mistake ledger |

`bash` events never store the full command — only the program name and a hash of the normalized command (numbers/paths/quoted strings replaced with placeholders), enough to detect repetition without retaining content.

## 9. Configuration

`.strata/config.toml` — all keys optional; defaults compiled into the engine; `strata doctor` validates unknown keys. Full reference in [09-cli.md](09-cli.md) §6. Key sections: `[engine] min_version`, `[paths]`, `[knowledge]` (size limits, required fields, ttl defaults), `[search]` (weights, default limit, synonyms file), `[scripts]`, `[projects]`, `[gates]`, `[garden]` (autonomy overrides, branch prefix, forge), `[hooks]` (advisory toggles), `[telemetry]`, `[federation]`.

## 10. Federation (hub repositories)

A hub repo (the panopticon pattern) lists child instances:

```toml
[federation]
members = [
  { name = "api",  path = "~/code/api" },
  { name = "web",  path = "~/code/web" },
]
```

`strata search --scope fleet` fans out to each member's index (each refreshed in parallel), merges by normalized score, and prefixes ids as `api:<id>`. `strata show api:<id>` resolves across members. Writes never cross repositories; the gardener operates on one repository at a time. Federation is read-only aggregation by design.

## 11. Build, install, and release (engine repo `justfile`)

| Target | Does |
|---|---|
| `just build` | `go build` with `-trimpath -ldflags "-s -w -X version..."` → `./bin/strata` |
| `just install` | build, then install to `$(go env GOPATH)/bin` (or `PREFIX`) and print the path |
| `just test` | `go test -race -shuffle=on ./...` with coverage profile |
| `just check` | every gate (format check, lint, vuln, semgrep, gitleaks, typos, tsc/eslint for plugin, strata lint) — identical to CI |
| `just fmt` | apply all formatters |
| `just bench` | perf benchmarks vs. baseline + retrieval bench |
| `just plugin-check` | `claude plugin validate plugin/` + `tsc` + `claude plugin test plugin/` |
| `just release VERSION` | tag, goreleaser (darwin/linux × arm64/amd64), checksums, GitHub release |
| `just doctor` | run `strata doctor` against this repo |

End users install with `go install github.com/zbmowrey/strata/engine/cmd/strata@latest` (or a release binary / Homebrew tap later) and add the plugin with `/plugin install strata --marketplace zbmowrey/strata`.

## 12. Dependencies (engine)

Keep the dependency graph small and audited. Proposed (verify current versions and health during the build; record choices as decision shards):

| Need | Choice | Notes |
|---|---|---|
| CLI | `github.com/spf13/cobra` | completions, help; flags via pflag |
| SQLite + FTS5 | `modernc.org/sqlite` (pure Go) | must confirm FTS5 + trigram tokenizer are compiled in; fallback `github.com/ncruces/go-sqlite3` |
| YAML | `github.com/goccy/go-yaml` | `gopkg.in/yaml.v3` is unmaintained |
| TOML | `github.com/pelletier/go-toml/v2` | |
| Hashing | `github.com/zeebo/xxh3` | change detection |
| Glob | `github.com/bmatcuk/doublestar/v4` | `**` globs for applies_to, gates |
| Fuzzy | in-house (trigram + Damerau-Levenshtein on ids) | no dependency |
| Markdown | in-house scanner (frontmatter, headings, wikilinks, code fences) | fuzz-tested; no full CommonMark parser needed |
| Testing | stdlib `testing`, `github.com/google/go-cmp` | golden files under `testdata/` |

No CGO (D-003). Static binaries for all targets.

## 13. Error model

- User-facing errors: one line `strata: <what failed>: <why> (hint: <fix>)` on stderr; `--format json` emits `{"error": {...}}` on stdout as well.
- Exit codes are a contract ([09-cli.md](09-cli.md) §2), including hook-specific semantics.
- Every internal error is wrapped with context (`fmt.Errorf("...: %w", err)`); `errorlint` and `wrapcheck` gates enforce this.
- No panics escape `main`; a recover wrapper converts them to exit 1 with a bug-report hint.
