---
id: subagents-share-session-state
title: "Subagents share the parent's hook session state"
summary: "Subagent hook payloads carry the parent session_id, so Stop-hook loop guard and nudge state are shared with the main session."
type: gotcha
tags: [engine, hooks, claude-code, subagents]
status: active
trust: observed
created: 2026-11-14
updated: 2026-11-14
sources:
  - { kind: session, ref: "825a4ac1-dbb7-59f6-8fc4-5ea5702044b9", at: 2026-11-08 }
  - { kind: file, ref: "engine/internal/hooks/stop.go", at: 2026-11-14 }
---
Per-session hook state is not per-agent: a subagent and its parent session read and write the same state.

## Symptom

The Stop hook's loop guard count and the once-per-session knowledge nudge behave as if the main session and its subagents were one session (e.g. a nudge consumed by a subagent never reaches the main session).

## Cause

Claude Code sends the parent's `session_id` in subagent hook payloads (only `agent_id`/`agent_type` distinguish them), and Strata keys hook state by `SessionID` (`update(root, h.in.SessionID, …)` in `stop.go`).

## Fix

When state must be per-agent, key it by `session_id` plus `agent_id`. See also [[hook-payload-fields]], [[stop-hook-decision-block]].
