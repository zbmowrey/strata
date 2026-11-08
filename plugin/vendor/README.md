`claude-code.d.ts` is the mod API declaration file written by Claude Code 2.1.319
(the plugin-authoring skill's `types/claude-code.d.ts`). It is vendored so `tsc -p plugin`
and eslint type-check the hooks module without a running engine; when a session loads the
plugin, the engine lays the same file under `.claude-plugin/types/` (gitignored there).
Refresh it after a Claude Code update that changes the API.
