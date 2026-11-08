# Log — ci-flake

## 2026-10-21 10:00 · s:a1b2
- tried: `pytest -x --timeout 30` on the integration suite
- failed: hang persists at session teardown; timeout never fires
- learned: hang is in fixture finalization, not in a test

## 2026-10-22 15:30 · s:c3d4
- decided: add explicit teardown to `db_session` rather than raising the timeout
- next: write find-leaked-fixtures to scan for the same pattern elsewhere
