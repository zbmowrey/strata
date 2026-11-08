---
id: bench-tests-load-independent
title: "Benchmark tests must not depend on machine load"
summary: "Tests that compare against a just-recorded latency baseline flake under parallel runs; relax cost fields in the test baseline."
type: gotcha
tags: [testing, bench]
status: active
trust: observed
created: 2026-11-08
updated: 2026-11-08
sources:
  - { kind: file, ref: "engine/internal/cli/benchretrieval_test.go", at: 2026-11-08 }
---
## Symptom
`TestBenchRetrieval` failed only in `just test` (race + shuffle, all packages).
## Cause
Latency regression vs a baseline recorded seconds earlier on an idle core.
## Fix
The test rewrites `latency_p95_ms` in the baseline before comparing.
