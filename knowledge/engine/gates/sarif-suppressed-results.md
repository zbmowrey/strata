---
id: sarif-suppressed-results
title: "SARIF results with suppressions are not findings"
summary: "semgrep --sarif still lists nosemgrep-suppressed results, marked with a suppressions array; the parser skips them."
type: gotcha
tags: [engine, gates, semgrep]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/gates/parsers.go", at: 2026-11-08 }
applies_to: ["engine/internal/gates/**"]
---
## Symptom
`strata check` blocked on a line carrying `// nosemgrep: <rule> -- reason`.
## Cause
SARIF output keeps suppressed results with `suppressions: [{kind: inSource}]`.
## Fix
`parseSARIF` drops results with suppressions.
