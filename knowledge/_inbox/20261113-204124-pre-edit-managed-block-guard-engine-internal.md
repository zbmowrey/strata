---
type: gotcha
captured: 2026-11-13T20:41:24-05:00
session: 702370e4-5951-41a8-8deb-05b242994163
project: project-records
sources: []
---
pre-edit managed-block guard (engine/internal/hooks/edit.go guard) fires on ANY file containing marker lines, including docs/spec/05-projects.md which quotes the projects .gitignore block as an example; a Write is allowed only if that quoted block survives verbatim. Also its message recommends 'strata project sync', which does not exist — the command is 'strata gitignore sync' (edit.go managedBlocks).
