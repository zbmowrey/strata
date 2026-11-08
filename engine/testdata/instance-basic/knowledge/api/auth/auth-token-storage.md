---
id: auth-token-storage
title: Store refresh tokens hashed
summary: Refresh tokens are stored as SHA-256 hashes; the raw token is only ever held by the client.
type: decision
tags: [api, auth, security]
status: active
trust: user-stated
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: user, ref: "agreed in planning", at: 2026-06-01 }
---
Only hashes of refresh tokens are persisted.

## Context
A database dump would otherwise grant sessions.

## Decision
Store `sha256(token)`, compare in constant time.

## Alternatives
Encryption at rest still exposed tokens to anyone with the key.

## Consequences
Tokens cannot be shown again after issuance.
