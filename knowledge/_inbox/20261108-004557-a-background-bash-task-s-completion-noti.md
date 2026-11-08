---
type: gotcha
captured: 2026-11-08T00:45:57-05:00
session: 825a4ac1-dbb7-59f6-8fc4-5ea5702044b9
project: _adhoc
sources: []
---
A background Bash task's completion notice for a long 'claude -p' run can arrive before the run has finished; confirm via the output file or side effects (e.g. the fake gh log) before acting on it, or you may abort a live run.
