---
id: git-hook-env-leaks
title: "Git hooks export GIT_INDEX_FILE to child processes"
summary: "Commands run from a git hook inherit GIT_DIR/GIT_INDEX_FILE; git calls on other repos must drop them."
type: gotcha
tags: [engine, git, gates]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/garden/session.go", at: 2026-11-08 }
applies_to: ["engine/internal/garden/**"]
---
## Symptom
Garden tests failed only under the pre-commit hook: `fatal: .git/index: index file open failed: Not a directory`.
## Cause
The hook's `GIT_INDEX_FILE` leaked into `git worktree add` in a temp repo.
## Fix
`cleanGitEnv` strips GIT_DIR, GIT_INDEX_FILE, GIT_WORK_TREE, GIT_PREFIX, GIT_OBJECT_DIRECTORY, GIT_COMMON_DIR.
