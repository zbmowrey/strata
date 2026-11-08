---
id: agent-frontmatter-tools-names-only
title: "Agent frontmatter tools take names only"
summary: "Plugin agent .md frontmatter `tools` lists tool names; per-command Bash patterns are not allowed, so limits live in the prompt."
type: fact
tags: [claude-code, plugins, agents]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "plugin/agents/scout.md", at: 2026-11-08 }
  - { kind: file, ref: "plugin/agents/verifier.md", at: 2026-11-08 }
---
Frontmatter: `name`, `description`, `tools` (comma-separated names), `model`, optional `disallowedTools`, `skills`, `effort`, `permissionMode`. Scout and verifier use `Bash` with `disallowedTools: Write, Edit, NotebookEdit` and state allowed commands in the prompt.
