# Recorded Claude Code hook payloads

Captured from a live headless session (`claude -p`, Claude Code 2.1.319, 2026-11-07)
with a dump hook registered for every event Strata uses. Paths and user names are
anonymized (`/work/repo`, `/home/user`); everything else is verbatim.

`pre-compact.json` is synthesized from the documented PreCompact fields
(`trigger`, `custom_instructions`): compaction cannot be triggered in `-p` mode.

Observed shape (used by `internal/hooks`): every payload carries `session_id`, `cwd`,
`hook_event_name`, `transcript_path`; tool events add `tool_name`, `tool_input`,
`tool_response`, `tool_use_id`, `duration_ms`; subagent tool events add `agent_id`
and `agent_type`; Stop/SubagentStop carry `stop_hook_active` and
`last_assistant_message`; SessionStart carries `source` (startup|resume|clear|compact).
