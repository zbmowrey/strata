---
id: managed-block-guard-any-file
title: "Managed-block edit guard fires on any file containing marker lines"
summary: "The pre-edit guard checks every file with marker lines (even docs quoting them); its .gitignore hint names a nonexistent 'strata project sync'."
type: gotcha
tags: [engine, hooks, managed-blocks, gitignore]
status: active
trust: verified
created: 2026-11-14
updated: 2026-11-14
verified_at: 2026-11-14
sources:
  - { kind: session, ref: "702370e4-5951-41a8-8deb-05b242994163", at: 2026-11-13 }
  - { kind: file, ref: "engine/internal/hooks/edit.go", at: 2026-11-14 }
  - { kind: file, ref: "engine/internal/cli/project.go", at: 2026-11-14 }
---
The pre-edit guard (`guard` in `engine/internal/hooks/edit.go`) protects managed blocks in *any* file that contains the marker lines, not just the files Strata manages.

## Symptom

Editing `docs/spec/05-projects.md`, which quotes the projects `.gitignore` managed block as an example, is blocked; a full-file Write passes only if the quoted block survives verbatim. The block message suggests `strata project sync`, which fails.

## Cause

The guard matches on marker presence, not file path. The `managedBlocks` table in `edit.go` maps `projects.GitignoreMarkers` to `strata project sync`, but the real command is `strata gitignore sync` (`engine/internal/cli/project.go`).

## Fix

Keep quoted marker blocks byte-identical when rewriting such docs (or edit around them with Edit). To regenerate the `.gitignore` block, run `strata gitignore sync`.
