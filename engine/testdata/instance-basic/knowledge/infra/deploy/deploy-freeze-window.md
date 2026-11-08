---
id: deploy-freeze-window
title: No production deploys on Friday afternoons
summary: Production promotions are blocked from Friday 14:00 to Monday 08:00 local time.
type: decision
tags: [deploy, policy]
aliases: [friday-freeze]
status: active
trust: user-stated
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: user, ref: "agreed in planning", at: 2026-06-01 }
---
Promotions to production are frozen from Friday 14:00 until Monday 08:00.

## Context
Weekend incidents had no on-call coverage.

## Decision
The promote job checks the window and refuses.

## Alternatives
Requiring a second approver on Fridays was rejected.

## Consequences
Hotfixes need an explicit override label.
