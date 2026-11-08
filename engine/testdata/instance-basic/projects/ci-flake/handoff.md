---
updated: 2026-11-01T18:00:00Z
session: c3d4
---
## State
Teardown fix landed for `db_session`; two other fixtures look suspicious.
## Next
1. Finish find-leaked-fixtures (strata run find-leaked-fixtures -- --path tests/).
## Open threads
- Is the redis fixture also leaking?
## Don't redo
- Raising --timeout (log 2026-10-21).
