---
type: gotcha
captured: 2026-11-08T00:45:57-05:00
session: 825a4ac1-dbb7-59f6-8fc4-5ea5702044b9
project: _adhoc
sources: []
---
Subagents share the parent session id in hook payloads, so the instance Stop hook's loop guard and nudge state are shared between the main session and its subagents.
