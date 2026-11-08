---
id: deploy-flow
title: How a release reaches production
summary: Merges to main build an image, deploy to staging, run smoke tests, then promote to production.
type: howto
tags: [deploy, release]
aliases: [release-process]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
scripts: [check-deploy-config]
verified_at: 2026-10-01
---
A merge to `main` triggers the release workflow.

1. Build and push the image tagged with the commit SHA.
2. Deploy to staging with [[helm-values-layering]].
3. Run smoke tests.
4. Promote with `make promote`; see [[deploy-freeze-window]].
