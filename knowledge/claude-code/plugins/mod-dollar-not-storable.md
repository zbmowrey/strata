---
id: mod-dollar-not-storable
title: "Mod API: $ cannot be stored"
summary: "In a mod, $ cannot be kept in a variable; pass it only to top-level functions of the plugin's own files."
type: fact
tags: [claude-code, plugins]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "plugin/hooks/register.ts", at: 2026-11-08 }
  - { kind: file, ref: "plugin/hooks/state.ts", at: 2026-11-08 }
---
Verified while building `plugin/`: `$` may only be passed to functions declared at top level in the same file or imported from the plugin. `read`/`update` accept only atoms defined as `const` in the same file, so each file declares its own atom consts. Blockable hooks (`command.run`, `tool.call`) need `.catch`.
