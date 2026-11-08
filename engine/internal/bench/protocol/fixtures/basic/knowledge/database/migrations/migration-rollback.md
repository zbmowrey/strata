---
id: migration-rollback
title: Roll back a migration safely
summary: Run alembic downgrade one revision at a time and verify the schema before redeploying.
type: howto
tags: [database, migrations, alembic]
aliases: [alembic-downgrade]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
scripts: [check-migrations]
verified_at: 2026-10-01
---
Run `alembic downgrade -1`, check `alembic current`, then redeploy the previous image per [[rollback-procedure]].
