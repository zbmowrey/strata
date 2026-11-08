---
id: hook-payload-fields
title: "Hook stdin payload fields"
summary: "Payloads carry session_id, cwd, hook_event_name, transcript_path, tool_name/tool_input/tool_response; Stop adds stop_hook_active."
type: reference
tags: [claude-code, hooks]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/hooks/input.go", at: 2026-11-08 }
  - { kind: file, ref: "engine/testdata/hooks/README.md", at: 2026-11-08 }
---
Recorded from 2.1.319: `session_id`, `cwd`, `hook_event_name`, `transcript_path`, `tool_name`, `tool_input`, `tool_response`, `tool_use_id`, `duration_ms`; `agent_id`/`agent_type` for subagent tool calls; `stop_hook_active` + `last_assistant_message` on Stop/SubagentStop; `source` on SessionStart. Unknown fields must be ignored (decoder does).
