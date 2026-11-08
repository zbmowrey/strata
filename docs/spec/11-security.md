# 11 — Security and Trust

Strata gives an agent a memory it acts on and scripts it runs. Both are attack and error surfaces. This document defines the boundaries.

## 1. Threats considered

| Threat | Example | Mitigation |
|---|---|---|
| Poisoned knowledge | Text from a fetched web page captured as a "standard" that tells Claude to disable a check | Trust levels; only user-stated/verified normative shards bind (§2); external material must be `type: reference` |
| Prompt injection via KB | A shard body containing "ignore previous instructions…" | Shards are data; the protocol says they inform, the user and `CLAUDE.md` instruct; `reference` shards are rendered with a source banner in `show` |
| Secret leakage | API keys captured into a shard, committed, or quoted in a garden PR | Never capture secrets (protocol); gitleaks on `knowledge/` at edit/commit/CI; private tier never enters PRs or federation |
| Over-privileged scripts | A "read-only" script that actually writes or calls the network | Side-effect classes, post-run write check, semgrep heuristics, permissions never allowlist network/destructive |
| Destructive automation | The gardener deleting knowledge or merging its own PR | Autonomy matrix; archive-not-delete; never merges; worktree isolation |
| Supply chain | Compromised tool or dependency | Pinned tool versions + checksums; `govulncheck`, `osv-scanner`, `cargo-deny`; minimal engine deps; no `curl \| sh` |
| Hook abuse / DoS | A slow or failing hook wedging sessions | Hook timeouts; fail-open on internal errors (except intended blocks); loop guard on Stop |

## 2. Trust model

| Level | Meaning | Who can assign |
|---|---|---|
| `user-stated` | The human said it (source `kind: user`) | Claude only when quoting the user directly; the gardener when applying an answered question |
| `verified` | Confirmed by a script run, test, or direct code inspection, with the evidence in `sources` | Anyone, with evidence |
| `observed` | Seen happen in this repo (a session, a log, an error) | Anyone |
| `inferred` | Reasoned, read in external material, or assumed | Default for captures without evidence |

Rules:

1. **Precedence on conflict:** higher trust wins; equal trust → newer evidence wins, unless either is `user-stated` (→ question).
2. **Binding force:** only `decision`/`standard` shards with `trust ∈ {user-stated, verified}` are binding. Others render as `[proposed]`.
3. **No self-promotion of trust:** trust only rises with new evidence recorded in `sources`. Lint rejects `trust: verified` without a `script`, `file`, `commit`, or `url` source; `user-stated` without a `user` source.
4. **External material** (web, vendor docs) enters as `type: reference`, `trust: inferred` or `observed`, with a `url`/`doc` source and the fetch date; it can inform but never become normative without the user.

## 3. Sensitive tier

- `knowledge/_private/` — gitignored via the managed `.gitignore` block, local-only. For information that is useful to the agent on this machine but must not be committed (personal context, client names, internal hostnames).
- Indexed locally; excluded from search unless `--private`; `show` prints a `[private]` banner.
- **Never** read by the gardener, never included in PRs, digests, telemetry, federation, or bench files. The `pre-edit` hook refuses writes to it during a garden run; a semgrep rule forbids scripts from reading it.
- **Secrets never go anywhere in `knowledge/`, including `_private/`.** Record *where* a secret lives (e.g., "1Password item `X`", "env var `Y` from direnv"), never its value. gitleaks runs on `knowledge/` including `_private/` at edit stage.
- A future optional `_sealed/` tier (age-encrypted, committed) is out of scope for v1.

## 4. Script execution safety

- Side-effect classes and their enforcement: [04](04-scriptorium.md) §4.
- `strata run` executes with an explicit argv (no shell), a timeout, the repo root as cwd, and environment passthrough limited to the manifest's `env` names plus `PATH`, `HOME`, locale, and toolchain variables (`GOPATH`, `GOCACHE`, `UV_*`, …).
- `writes-local` scripts: the engine snapshots `git status --porcelain` before/after and fails the run if paths outside declared `writes` globs changed.
- `destructive` scripts require `--yes` and print a pre-flight summary; they are never auto-allowed.

## 5. Engine hygiene

- No network access in the engine except: invoking `gh`/`tea` (forge), `git` (push), and `claude` (protocol evals). No telemetry upload, no update pings.
- All file writes atomic; path joins validated to stay inside the instance root (symlink-resolved); no following symlinks out of the root during corpus walks.
- Hook stdin parsing is strict and size-limited (reject > 10 MB); unknown fields ignored.
- Releases: reproducible builds (`-trimpath`), checksums, and (when available) build provenance attestation on GitHub releases.

## 6. Telemetry privacy

Local only, gitignored, retention-limited. No file contents, no full Bash commands (hash + program name only), queries are stored (they are useful for gap detection) — `[telemetry] store_queries = false` disables that, at the cost of gap detection.
