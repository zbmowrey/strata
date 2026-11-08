---
name: incident-rollback
description: Roll back a bad production release under incident pressure, including database migration considerations.
---
# Incident rollback

1. Freeze promotions.
2. Roll back the release per [[rollback-procedure]].
3. Decide whether the migration must be downgraded ([[migration-rollback]]).
4. Write the incident note.
