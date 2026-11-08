---
id: image-tagging
title: Images are tagged with the commit SHA
summary: Container images use the full git SHA as the tag; latest is never deployed.
type: standard
tags: [deploy, docker]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
verified_at: 2026-10-01
---
Every image is tagged `ghcr.io/acme/app:<sha>`.

## Rule
Never deploy a mutable tag such as `latest`.

## Rationale
Mutable tags make rollbacks ambiguous; see [[rollback-procedure]].

## Enforcement
none yet
