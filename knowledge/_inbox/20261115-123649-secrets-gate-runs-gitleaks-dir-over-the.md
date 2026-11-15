---
type: gotcha
captured: 2026-11-15T12:36:49-05:00
session: a097b4df-26bb-4df3-b101-cb4e18673675
project: _adhoc
sources: []
---
secrets gate runs 'gitleaks dir .' over the whole working tree including gitignored trees (.venv, large data dirs); in hockey-pool it took 3m43s and timed out (advisory) until a .gitleaks.toml allowlisted data/lake and .venv. strata init could scan only tracked+untracked-not-ignored files (git ls-files -co --exclude-standard) or generate a .gitleaks.toml from .gitignore.
