---
type: gotcha
captured: 2026-11-15T11:42:55-05:00
session: a097b4df-26bb-4df3-b101-cb4e18673675
project: _adhoc
sources: []
---
Garden worktrees have no project virtualenv (.venv is gitignored), so Python gates (pyright) report import errors instead of real findings; verification in the worktree is meaningless until 'uv sync' runs there. Seen 2026-11-15 in hockey-pool's first /garden. Consider: garden start runs the profile's env setup (uv sync / pnpm install) in the worktree, or gates run with the main tree's env.
