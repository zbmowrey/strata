---
id: ci-pipeline-stages
title: CI runs lint, unit, integration, then deploy
summary: The CI pipeline has four ordered stages; integration only runs after lint and unit tests pass.
type: fact
tags: [ci, testing]
aliases: [pipeline]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
applies_to: [".github/workflows/**"]
verified_at: 2026-10-01
---
The GitHub Actions workflow runs `lint`, `unit`, `integration`, and `deploy` in that order.

## See also
- [[ci-cache-keys]]
- [[deploy-flow]]
