# Strata — engine repository

This repository builds **Strata**, a drop-in operating layer for Claude Code (knowledge base, script library, project memory, quality gates, gardener, plugin).

- The design is in `docs/spec/` and is authoritative. Settled decisions: `docs/spec/13-decisions.md`.
- The build is driven by `PROMPT.md`. If asked to build, run, or continue the build, read `PROMPT.md` and follow it exactly — including its continuity rules (`projects/strata-v1/handoff.md` first after any compaction).
- Go source lives only in `engine/`. The Claude Code plugin lives in `plugin/`. Use the root `justfile` for build, install, test, and check.
- Never merge PRs, push to `main`, or tag releases without the user's explicit go-ahead.

Once Strata is built (Phase 11), `strata init` adds a managed protocol block below this line; keep the content above it.
