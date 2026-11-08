package scaffold

import "embed"

// ScriptTemplates holds the Scriptorium templates (docs/spec/04-scriptorium.md
// §6, §9): per-runtime script skeletons under go/ and python/, the shared
// lib starters under lib/, and module files (go.mod, pyproject.toml). Files
// carry a .tmpl suffix so they are never compiled or linted as engine code.
//
//go:embed all:templates/scripts
var ScriptTemplates embed.FS

// ScriptTemplateRoot is the directory of ScriptTemplates.
const ScriptTemplateRoot = "templates/scripts"
