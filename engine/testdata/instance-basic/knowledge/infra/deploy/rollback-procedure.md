---
id: rollback-procedure
title: Roll back a bad release
summary: Re-deploy the previous image tag with helm rollback; never revert migrations in the same step.
type: howto
tags: [deploy, incident]
aliases: [rollback]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
verified_at: 2026-10-01
---
Run `helm rollback app <revision>`; database migrations are rolled back separately per [[migration-rollback]].
