---
id: fork-worktrees-branch-from-main
title: "Worktree forks start from main"
summary: "Agent worktrees are created from main, not the current branch; a fork must reset to the intended base commit first."
type: gotcha
tags: [process, agents, git]
status: active
trust: observed
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "docs/spec/12-build-plan.md", at: 2026-11-08 }
---
## Symptom
A fork's worktree lacked the latest build/v1 commits.
## Cause
The worktree tool branches from the default branch.
## Fix
First command in every fork: `git checkout -B <branch> <base-sha>` and verify `git log -1`.
