# 09 — CLI Reference

Binary: `strata`. All commands run from anywhere inside an instance (repo root discovered by walking up to the directory containing `.strata/config.toml`; `--root` overrides).

## 1. Global flags

| Flag | Meaning |
|---|---|
| `--root DIR` | Instance root (default: discovered) |
| `--format line\|table\|json\|jsonl\|md` | Output format (command-specific default) |
| `--no-color` / `NO_COLOR` | Disable ANSI color (auto-disabled when stdout is not a TTY) |
| `--no-refresh` | Skip index freshness check |
| `--strict-fresh` | Wait for the index lock instead of reading the current snapshot |
| `--quiet` / `-q` | Suppress non-essential stderr |
| `--verbose` / `-v` | Debug logging to stderr |

## 2. Exit codes (contract)

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Runtime error |
| 2 | Blocking failure (gates failed; hook wants to block) — matches Claude Code hook semantics |
| 3 | Partial result (e.g., `show` with some ids missing) |
| 4 | Usage error (bad flags, query parse error) |
| 5 | Not an instance / config invalid |
| 6 | Lock held (garden lease live, index strict mode) |
| 7 | Fenced: the caller's garden fence is not the lease's current fence ([07](07-gardener.md) §9.2) |

## 3. Commands

### Retrieval
| Command | Synopsis |
|---|---|
| `search` | `strata search <query…> [--kind K]… [--type T] [--tag T]… [--limit N] [--explain] [--private] [--archived] [--scope local\|fleet]` |
| `show` | `strata show <id[#section]>… [--stdin] [--links 0-2] [--budget TOKENS] [--meta\|--body\|--brief]` |
| `related` | `strata related <id> [--depth 1\|2] [--limit N]` |
| `context` | `strata context <path>… [--limit N]` |
| `map` | `strata map [domain] [--depth 1-3]` |

### Knowledge
| Command | Synopsis |
|---|---|
| `new shard` | `strata new shard <domain>/<topic>/<id> --type T [--title …] [--summary …]` → scaffolded file path |
| `capture` | `strata capture "<text>" [--type T] [--tags a,b] [--source kind:ref] [--project slug] [--shard id]` |
| `mv` | `strata mv <old-id> <new-id>` (rewrites links, adds alias) |
| `supersede` | `strata supersede <old-id> <new-id>` |
| `archive` | `strata archive <id>…` |
| `lint` | `strata lint [paths…] [--fix] [--kind K]` |
| `hubs` | `strata hubs` (regenerate) |
| `questions` | `strata questions list\|show <id>\|ask "<q>" [--blocking id…]\|answer <id>` |

### Scripts
| Command | Synopsis |
|---|---|
| `new script` | `strata new script <domain>/<name> [--runtime go\|python\|…]` |
| `run` | `strata run <id> [--yes] [-- args…]` |
| `scripts list` | `strata scripts list [--domain D] [--side S]` |
| `scripts build` | `strata scripts build [<id>…\|--all]` |
| `scripts catalog` | `strata scripts catalog` (regenerate) |

### Playbooks
`strata new playbook <name>` · indexed automatically; found via `search kind:playbook`.

### Projects
`strata project new <slug> "<title>" --type feature\|bugfix\|audit\|research\|initiative --goal "…" [--spawned-from <slug>]` · `project list [--all]` · `project show <slug>` · `project set <slug> key=value… [--reason "…"]` · `project log <slug> [--tried\|--failed\|--worked\|--learned\|--decided\|--blocked\|--next\|--note] "<text>"` · `project handoff <slug>` · `project decide <slug> "<title>" --by user\|claude\|joint [--key] [--status …] [--supersedes D-n]` · `project ratify <slug> D-n [--reject]` · `project approve <slug> requirements\|spec` · `project source <slug> "<title>" --type … --ref … --reliability …` · `project add <slug> <file\|kind>` · `project renumber <slug> <ID> --to <ID>` · `project pause\|activate\|close [--force] <slug>` · `project track <slug> <glob>…` · `resume [<slug>]` · `gitignore sync`. See [05](05-projects.md).

### Gates
`strata check [--changed\|--staged\|--all\|<paths…>] [--stage edit\|stop\|commit\|ci] [--fix]` · `gates list` · `gates explain <gate>[/<rule>]` · `gates baseline <gate> [--accept]`.

### Gardener
`strata garden [--plan] [--auto] [--format json]` (deterministic phase; judgment phase is `/garden` in the plugin). Run control ([07](07-gardener.md) §3, §9): `garden start [--launcher manual|headless]` · `garden finish --fence N […]` · `garden abort` · `garden resume [--force]` · `garden task start|done|skip|escalate <task-id> [--note …]` · `garden status` · `garden launch [--resume] [--dry-run]` (`garden supervise` is internal).

### Claude integration
`strata hook <session-start\|pre-edit\|post-edit\|post-bash\|stop\|pre-compact> [--subagent]` (reads stdin JSON) · `strata perms sync`.

### Bench, telemetry
`strata bench retrieval [--update-baseline]` · `strata bench protocol [--scenario id]… [--model …]` · `strata bench perf` (engine repo) · `strata telemetry stats [--since 30d]` · `strata telemetry prune`.

`telemetry stats` reports event counts, distinct sessions, the top 10 bash programs (`first_token`), search count and zero-result rate, hook latency p50/p95 per hook, and pass/fail/other per gate (`--format json`: `{"v":1,"since":…,"stats":{…}}`). `telemetry prune` deletes events older than `[telemetry] retention` and reports the count on stderr only.

### Lifecycle
`strata status [--session id] [--format json]` — cheap snapshot for status lines (counts, last edit/stop gate result of the session, active `/garden` run). `strata init [--profile go,python,ts,…] [--projects-vcs local|records|all] [--ci] [--scripts] [--no-hooks] [--dry-run]` · `strata upgrade [--dry-run]` · `strata doctor [--fix]` · `strata version [--format json]` · `strata completion <shell>`.

`init` is idempotent: re-running it adds missing pieces and never overwrites user-owned files (it prints what it skipped). `--dry-run` prints the full change list. Without `--projects-vcs` and with no configured value, `init` prompts on a TTY and otherwise defaults to `local` with a notice; Claude must ask the user before running `init` ([05](05-projects.md) §5.1).

## 4. `strata doctor` checks

Engine version ≥ `min_version`; plugin installed and version-matched (from Claude Code's `plugins/installed_plugins.json`; a warning, since the hooks enforce without the plugin); hooks present in `.claude/settings.json`; `CLAUDE.md` managed block present and current; required tools present at pinned versions (`tools.toml`); index healthy; git pre-commit hook installed; `.gitignore` managed blocks current; no disabled gates without reasons; lint summary (counts by severity); open questions; garden lease: live, resumable (expired with commits or salvageable changes → "resumable: strata garden resume"), or debris (expired, nothing to salvage → offer to clear); a `stuck` run is reported with `.strata/garden/last-failure`. `--fix` applies safe fixes (regenerate managed blocks, install missing git hook, clear stale lock) and prints install commands for missing tools (it never installs system tools itself).

## 5. Output conventions

- Human formats are stable enough to read, not to parse; `json`/`jsonl` are versioned contracts (`"v": 1` at top level) covered by golden tests.
- No output to stdout on success for mutating commands except the affected paths/ids (one per line) — scriptable.
- Color only on TTY; widths adapt to `$COLUMNS`; never emit lines longer than the terminal width in `line` format.

## 6. Configuration reference (`.strata/config.toml`)

```toml
[engine]
min_version = "0.1.0"

[paths]
knowledge = "knowledge"
scripts = "scripts"
projects = "projects"
skills = [".claude/skills"]

[knowledge]
max_body_lines = 120
max_body_bytes = 6144
max_summary = 160
extra_required = []
review_after = { fact = "180d", gotcha = "365d", howto = "180d", reference = "90d", standard = "365d", glossary = "365d", correction = "365d" }
cold_after = "180d"

[search]
default_limit = 10
weights = { title = 10, aliases = 8, summary = 5, tags = 4, headings = 3, body = 1 }
kind_boost = { shard = 1.0, playbook = 1.0, script = 1.0, hub = 0.6, project = 0.9, inbox = 0.8, question = 0.7 }
synonyms = ".strata/synonyms.txt"

[scripts]
default_runtime = "go"
catalog_split_at = 50

[projects]
stale_after = "30d"
adhoc_retention = "30d"
vcs = "local"                # local | records | all (05 §5)
max_tracked_source = "1MiB"
assumed_stale_after = "7d"
# [projects.profiles.<name>] required = […], optional = […]   (05 §9)

[gates]
stop_max_blocks = 3
edit_budget = "2s"

[hooks]
pre_edit_context = true
stop_nudge = "once"          # once | off
session_start_garden_auto = true

[garden]
forge = "auto"               # auto | github | forgejo
branch_prefix = "garden/"
bench_tolerance = 0.02
escalate = []                # extra task types to always escalate
auto_launch = "off"          # off | on — debt-driven headless launch at SessionStart (D-031)
model = "opus"               # gardener model (D-032); effort below
effort = "medium"
lease_ttl = "30m"            # heartbeat age after which a run is considered lost
max_run = "90m"              # supervisor kills the agent after this
max_attempts = 3             # resumes before a run is marked stuck
max_open_prs = 1             # launch defers while this many garden PRs are open
launch_per_day = 1
orphan_min_age_days = 30     # D-033
debt_inbox_days = 3          # debt is due when any threshold is crossed (07 §9.1)
debt_tasks = 10
debt_days = 7

[telemetry]
enabled = true
retention = "180d"

[federation]
members = []
```
