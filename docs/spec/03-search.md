# 03 — Search and Index

Retrieval is the most frequently exercised path in Strata. It must be fast enough to be reflexive, accurate enough to trust, and frugal with context.

## 1. Retrieval ladder (how Claude is expected to use it)

Each rung costs roughly 5–10× the one above it. Stop at the first rung that answers the question.

1. `strata search <query>` → ranked one-line results (~60–120 tokens per hit).
2. `strata show <id> [<id>…]` → full content of exactly the shards needed, **all in one call, in the order given**.
3. `strata show <id>#<heading>` → just one section when the shard is long.
4. `strata show <ids> --links 1 --budget 4000` → the shards plus their 1-hop neighborhood, within a token budget.
5. `strata related <id>` / `strata map <domain>` → when you don't know what exists.
6. Outside sources (code, web) — and then **capture** what was learned.

## 2. Indexed corpora

| `kind` | Source | Title / summary from |
|---|---|---|
| `shard` | `knowledge/**/*.md` excluding `_inbox`, `_questions`, `_hubs` | frontmatter |
| `hub` | `knowledge/_hubs/*.md` | frontmatter |
| `inbox` | `knowledge/_inbox/*.md` | first line / frontmatter |
| `question` | `knowledge/_questions/*.md` | `## Question` first line |
| `script` | `scripts/**/script.yaml` (+ source docstring) | manifest `name`, `summary` |
| `playbook` | `.claude/skills/*/SKILL.md`, plugin skills | skill `name`, `description` |
| `project` | `projects/*/project.md`, `plan.md`, `handoff.md`, `log.md` entries (each dated log entry indexed as its own document `project:<slug>/log#<date>`) | project frontmatter / entry heading |
| `private` | `knowledge/_private/**` | frontmatter; never leaves the machine (see 11) |

Default search scope: all kinds except `archived` status and `private` (`--private` includes it).

## 3. Index schema (SQLite, WAL)

```sql
CREATE TABLE files (path TEXT PRIMARY KEY, size INT, mtime_ns INT, hash BLOB, kind TEXT);
CREATE TABLE docs (
  rowid INTEGER PRIMARY KEY,
  id TEXT UNIQUE NOT NULL,          -- shard id / script id / playbook name / project slug / composite
  kind TEXT NOT NULL, path TEXT NOT NULL, anchor TEXT,
  domain TEXT, topic TEXT, type TEXT, status TEXT, trust TEXT,
  title TEXT NOT NULL, summary TEXT, created TEXT, updated TEXT, verified_at TEXT, review_after_days INT,
  side_effect TEXT, runtime TEXT,   -- scripts
  bytes INT, lines INT, tokens_est INT,
  meta_json TEXT                    -- full frontmatter for --meta and filters on arbitrary keys
);
CREATE VIRTUAL TABLE docs_fts USING fts5(
  title, aliases, summary, tags, headings, body,
  content='', contentless_delete=1, tokenize='porter unicode61 remove_diacritics 2');  -- contentless_delete: needed for incremental refresh
CREATE VIRTUAL TABLE names_tri USING fts5(name, tokenize='trigram');  -- ids, titles, aliases: fuzzy/substring
CREATE TABLE tags (doc INT, tag TEXT);              CREATE INDEX tags_tag ON tags(tag);
CREATE TABLE aliases (alias TEXT PRIMARY KEY, doc INT);
CREATE TABLE links (src INT, dst_id TEXT, anchor TEXT, kind TEXT);  -- kind: wikilink|related|supersedes|scripts|gates|verify
CREATE INDEX links_dst ON links(dst_id);
CREATE TABLE sections (doc INT, ord INT, heading TEXT, slug TEXT, start_byte INT, end_byte INT);
CREATE TABLE applies_to (doc INT, glob TEXT);
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);  -- schema version, corpus stats, last refresh
```

Body text is stored contentless in FTS (files are the source of truth); `show` reads files directly, using `sections` offsets for `#heading` slices. Hash + mtime guarantee `show` never serves content that differs from disk.

## 4. Ranking

Deterministic, explainable (`--explain` prints the score breakdown per hit).

1. **Exact short-circuit.** If the whole query equals an id or alias (case-insensitive), that document ranks first with score ∞.
2. **Lexical.** FTS5 `bm25()` with column weights (configurable): `title 10, aliases 8, summary 5, tags 4, headings 3, body 1`.
3. **Fuzzy assist.** If lexical results < `limit`, or the query contains a token with no FTS hits, trigram matches over `names_tri` are merged with a discounted score (0.5×) — catches typos and partial ids.
4. **Synonym expansion.** Query tokens are expanded via `.strata/synonyms.txt` (`k8s, kubernetes`, `pg, postgres, postgresql`) as FTS `OR` groups, original token weighted 1.0, synonyms 0.7.
5. **Multipliers** (applied to the normalized lexical score):

| Signal | Multiplier |
|---|---|
| status `active` / `draft` / `deprecated` / `superseded` | 1.0 / 0.8 / 0.5 / 0.3 |
| trust `user-stated` / `verified` / `observed` / `inferred` | 1.15 / 1.1 / 1.0 / 0.9 |
| stale | 0.85 |
| kind boost (config; default `shard 1.0, playbook 1.0, script 1.0, hub 0.6, project 0.9, inbox 0.8, question 0.7`) | as configured |
| filter-target kind (when `--kind` given) | n/a (filtered) |

6. **Tie-break:** score desc, then `updated` desc, then id asc. Output is fully deterministic for a given corpus and query.

Normalization: BM25 scores are min-max normalized within the candidate set (top 200) before multipliers, so multipliers have consistent effect. Scores printed are rounded to 2 decimals.

## 5. Query language

Free text plus inline filters; every inline filter has an equivalent flag.

```
strata search 'fixture teardown kind:shard type:gotcha tag:pytest -tag:legacy updated:>2026-01-01'
strata search '"connection leak" OR hang  domain:testing  trust:>=verified'
strata search 'deploy  kind:script  side:read-only'
strata search 'path:knowledge/infra/**  status:any'
```

| Filter | Values | Flag |
|---|---|---|
| `kind:` | shard, hub, script, playbook, project, inbox, question (repeatable = OR) | `--kind` |
| `type:` | shard types | `--type` |
| `tag:` / `-tag:` | any tag (repeatable = AND) | `--tag`, `--not-tag` |
| `domain:` / `topic:` | path-derived | `--domain`, `--topic` |
| `status:` | lifecycle value, or `any` | `--status` |
| `trust:` | value or `>=value` | `--trust` |
| `side:` | side-effect class or `<=class` (scripts) | `--side` |
| `runtime:` | go, python, … (scripts) | `--runtime` |
| `updated:` / `created:` / `verified:` | `>DATE`, `<DATE`, `DATE..DATE`, `>30d` | `--updated` … |
| `stale:` | true/false | `--stale` |
| `path:` | doublestar glob | `--path` |
| `project:` | slug (project docs) | `--project` |
| `meta.<key>:` | arbitrary frontmatter key equality | `--meta key=value` |

Syntax: whitespace = AND; `OR` (uppercase) between terms; `"…"` phrases; `-term` excludes; `term*` prefix. Parse errors exit 4 with a caret-pointed message.

## 6. Commands

### 6.1 `strata search <query…>`

Flags: `--limit N` (default 10, max 200), `--format line|table|json|jsonl|md`, `--explain`, `--private`, `--archived`, `--scope local|fleet`, `--no-refresh`, `--strict-fresh`.

Default `line` format (one hit per line, terminal-width aware; never wraps):

```
pytest-fixture-teardown   shard/gotcha   0.94  DB fixtures need explicit teardown — Session-scoped DB fixtures leak…
find-leaked-fixtures      script/ro      0.81  Detect pytest fixtures without teardown across tests/
debug-hanging-tests       playbook       0.66  Procedure for diagnosing a test suite that hangs at exit
ci-flake:log#2026-11-07   project        0.52  tried: pytest -x --timeout … failed: …
⚠ 1 stale · 3 more results (use --limit)
```

Script rows show the side-effect class abbreviation (`ro`, `wl`, `net`, `DESTR`). Normative shards with insufficient trust show `[proposed]`.

`json` emits `{"query":…, "results":[{"id","kind","type","score","title","summary","path","status","trust","stale","side_effect"}], "total":N, "ms":N}`.

Zero results print suggestions (nearest ids by trigram, and "try removing filter X") and log a `zero` telemetry event (gap tracking).

### 6.2 `strata show <id> [<id>…]`

- Accepts any number of ids (also via `--stdin`, one per line). Returns **all of them in one response, in the order given**, deduplicated (first occurrence wins).
- `id#heading-slug` returns only that section (plus the shard's title line).
- Composite ids from search (`project-slug:log#date`, `fleet-member:id`) are accepted verbatim.
- Missing ids: found documents are still printed; missing ones are reported at the end and on stderr with did-you-mean suggestions; exit code 3.
- `--links N` (0–2): after the requested documents, append the BFS neighborhood (outbound wikilinks, then inbound, ordered by link count then rank) — deduplicated against what was already printed.
- `--budget TOKENS`: hard cap on total output (tokens ≈ bytes/4). Requested ids always print (truncated at a section boundary if needed, with a `… [truncated: N more tokens — show <id>#<section>]` marker); linked documents are dropped from the tail first.
- `--meta` (frontmatter only), `--body` (body only), `--brief` (title + summary + first paragraph).
- `--format md|json`. Default `md`:

```
═══ pytest-fixture-teardown · shard/gotcha · knowledge/testing/pytest/pytest-fixture-teardown.md
trust: observed · updated 2026-11-07 · verified 2026-11-07 · links: 3 out / 5 in
<body>

═══ find-leaked-fixtures · script/read-only · scripts/testing/find-leaked-fixtures/
<rendered manifest: summary, usage, examples, side effects>
```

### 6.3 `strata related <id> [--depth 1|2] [--limit N]`

Neighbors in the link graph (outbound, inbound, shared tags weighted by IDF), as `line` rows with the relationship (`→`, `←`, `#tag`).

### 6.4 `strata context <path…>`

Documents whose `applies_to` globs match the given code paths, plus scripts whose manifest `applies_to` match, ranked by glob specificity. Used by the pre-edit advisory hook ([08](08-claude-integration.md) §4.2) and directly by Claude before working in an unfamiliar area.

### 6.5 `strata map [domain] [--depth N]`

Tree of domains → topics → counts (and, at depth 3, shard ids with summaries). Equivalent to reading hubs without opening files.

## 7. Output budgets (measured by the bench)

| Command | Target output |
|---|---|
| `search` (10 hits, line) | ≤ 1,500 bytes |
| `show` 1 typical shard | ≤ 3,500 bytes |
| `map` (depth 2) | ≤ 2,500 bytes for a 1k-shard corpus |
| `context` (typical) | ≤ 800 bytes |

The bench records bytes per command; regressions > 20% fail CI.

## 8. Correctness requirements

- Results must never include a document whose file no longer exists (freshness check guarantees this; tests assert it).
- `show` output must equal the file's current content byte-for-byte for the body region (no stale index content).
- Ranking is deterministic: golden tests pin result order for a fixture corpus across platforms.
- The query parser is fuzz-tested (`go test -fuzz`) — it must never panic and must round-trip `String()`.
