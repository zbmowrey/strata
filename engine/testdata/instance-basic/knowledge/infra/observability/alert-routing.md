---
id: alert-routing
title: Alert routing rules
summary: Pages route by service label to the owning team; warnings go to the team channel only.
type: reference
tags: [alerts, observability]
status: active
trust: observed
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: url, ref: "https://wiki.example.com/ops", at: 2026-06-01 }
---
Alertmanager routes `severity=page` by the `service` label to the owning rotation; `severity=warning` posts to the team channel.
