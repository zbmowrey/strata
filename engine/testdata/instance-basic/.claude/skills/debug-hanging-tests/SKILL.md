---
name: debug-hanging-tests
description: Procedure for diagnosing a test suite that hangs at exit — find the live thread, map it to a fixture, fix teardown.
tags: [testing, debugging]
shards: [pytest-fixture-teardown, hanging-test-diagnosis]
---
# Debugging a hanging test suite

1. Confirm the hang happens after the last test (summary printed).
2. `py-spy dump --pid <pid>`; look for non-daemon threads.
3. Map the thread to a fixture; check [[pytest-fixture-teardown]].
4. Run `strata run find-leaked-fixtures -- --path tests/`.
