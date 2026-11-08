---
id: pytest-markers-registered
title: All pytest markers must be registered
summary: Every custom pytest marker is declared in pyproject.toml; unknown markers fail the run.
type: standard
tags: [testing, pytest]
status: active
trust: verified
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: file, ref: "README.md", at: 2026-06-01 }
applies_to: ["tests/**", "pyproject.toml"]
verified_at: 2026-10-01
---
Custom markers are declared under `[tool.pytest.ini_options] markers` and the suite runs with `--strict-markers`.

## Rule
No test may use an undeclared marker.

## Rationale
Typos in marker names silently deselect tests.

## Enforcement
`--strict-markers` in `addopts`; see [[ci-pipeline-stages]].
