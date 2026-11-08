---
id: hook-exit-2-feeds-stderr
title: "PostToolUse exit 2 sends stderr to Claude"
summary: "A PostToolUse hook that exits 2 blocks: its stderr is shown to Claude, which then fixes the problem."
type: fact
tags: [claude-code, hooks]
aliases: [post-edit-blocking]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/hooks/edit.go", at: 2026-11-08 }
  - { kind: file, ref: "engine/testdata/hooks/README.md", at: 2026-11-08 }
---
Verified live with Claude Code 2.1.319 (`claude -p`): `strata hook post-edit` exiting 2 with findings on stderr made Claude fix the file. Any other non-zero exit is a non-blocking error; exit 0 is silent.

Recorded payloads: `engine/testdata/hooks/`.
