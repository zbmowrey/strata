---
id: background-task-notice-early
title: "Background task completion notice can precede a long claude -p run's end"
summary: "A background Bash completion notice for a long 'claude -p' run may arrive early; confirm via output file or side effects before acting."
type: gotcha
tags: [claude-code, headless, background-tasks]
status: active
trust: observed
created: 2026-11-14
updated: 2026-11-14
sources:
  - { kind: session, ref: "825a4ac1-dbb7-59f6-8fc4-5ea5702044b9", at: 2026-11-08 }
---
Don't take a background Bash completion notice for a long `claude -p` run as proof the run has finished.

## Symptom

The notice arrives while the headless run is still working; acting on it (cleanup, abort, re-run) can kill or race a live run.

## Cause

Observed while driving long headless runs from a background Bash task; the exact trigger was not pinned down.

## Fix

Before acting, confirm completion from the run's output file or its side effects (e.g. entries in a fake `gh` log).
