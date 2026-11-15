---
type: gotcha
captured: 2026-11-15T11:42:55-05:00
session: a097b4df-26bb-4df3-b101-cb4e18673675
project: _adhoc
sources: []
---
osv-scanner (deps gate) finds 'No package sources found' inside a garden worktree: it walks from / and appears to honor the main repo's .gitignore, which ignores .strata/worktrees/. deps passes in the main checkout. Seen 2026-11-15 in hockey-pool.
