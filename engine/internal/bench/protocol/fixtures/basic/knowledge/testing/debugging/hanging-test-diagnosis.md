---
id: hanging-test-diagnosis
title: Find which fixture keeps the process alive
summary: Dump thread stacks with py-spy to see which non-daemon thread keeps a finished suite alive.
type: howto
tags: [testing, debugging]
status: active
trust: observed
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: session, ref: "projects/ci-flake/log.md", at: 2026-06-01 }
---
Run `py-spy dump --pid <pid>` while the suite hangs and look for non-daemon threads.

1. Start the suite and wait for the hang.
2. Run `py-spy dump`.
3. Map the thread to its fixture; usually [[pytest-fixture-teardown]].
