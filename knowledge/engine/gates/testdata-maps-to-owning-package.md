---
id: testdata-maps-to-owning-package
title: "Package-scope gates map testdata to its owner"
summary: "A changed file under testdata/ tests the package that owns it; module-level testdata/ runs ./... ."
type: fact
tags: [engine, gates, go]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/gates/targets.go", at: 2026-11-08 }
applies_to: ["engine/internal/gates/**"]
---
The go tool ignores `testdata` directories, so `go test ./x/testdata` fails as "package failed". `owningPackage` cuts at the `testdata` component.
