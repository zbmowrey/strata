---
type: fact
captured: 2026-11-14T12:04:33-05:00
session: 17c2c802-c35e-441e-b738-5a19d648a2dc
project: strata-v1
sources: []
---
Subagent permissions and models, per the Claude Code docs (sub-agents, permission-modes), read via a summarizing fetch on 2026-11-14. (1) Plugin agents ignore permissionMode, hooks and mcpServers in frontmatter. (2) A subagent inherits the parent's mode when the parent is acceptEdits/auto/bypass, and frontmatter permissionMode applies only under a default/dontAsk parent. (3) Model precedence: the Agent tool's model param, then frontmatter model, then CLAUDE_CODE_SUBAGENT_MODEL, then the parent model; CLAUDE_CODE_SUBAGENT_MODEL_FORCE=1 overrides both. plugin/agents/gardener.md uses model: inherit, so /garden runs on whatever model the main session uses. The docs say a background subagent's permission prompt is shown in the main session; the gotcha background-agent-no-permission-prompts (2026-11-08) observed it denied instead. Unresolved.
