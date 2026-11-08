---
id: mod-model-tool-names
title: "Mod model tools are named mcp__strata__<name>"
summary: "Tools registered with $.tool.register appear as mcp__strata__<name> and are answered by a tool.call hook returning { result }."
type: fact
tags: [claude-code, plugins]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "plugin/hooks/register.ts", at: 2026-11-08 }
---
`strata_search` / `strata_show` are exposed as `mcp__strata__strata_search` and `mcp__strata__strata_show`. Protocol eval matchers treat `strata_search` and the mcp name as the same tool.
