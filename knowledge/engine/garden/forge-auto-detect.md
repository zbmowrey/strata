---
id: forge-auto-detect
title: "Forge auto-detection"
summary: "[garden] forge = auto maps a github.com origin to gh and any other host to tea; a missing CLI prints a compare URL."
type: fact
tags: [garden, forge]
status: active
trust: verified
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/forge/forge.go", at: 2026-11-08 }
applies_to: ["engine/internal/forge/**"]
---
`FindOpen` filters open PRs by branch prefix in Go (gh `--head` is exact-match only). Forgejo drafts use a `WIP: ` title prefix. Only gh returns touched files. tea JSON keys are unconfirmed against a live Forgejo.
