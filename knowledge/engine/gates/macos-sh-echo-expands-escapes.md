---
id: macos-sh-echo-expands-escapes
title: "macOS /bin/sh echo expands backslash escapes"
summary: "On macOS /bin/sh, echo turns \\n into a newline; fake tools in tests must use printf '%s\\n'."
type: gotcha
tags: [tooling, macos, testing]
status: active
trust: observed
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/gates/runner_test.go", at: 2026-11-08 }
---
## Symptom
Recorded tool output in a fake script came out mangled.
## Cause
`/bin/sh` is a POSIX echo that interprets escapes.
## Fix
`printf '%s\n' …`. Also set `cmd.WaitDelay`, or a child holding the pipe stops timeouts from firing.
