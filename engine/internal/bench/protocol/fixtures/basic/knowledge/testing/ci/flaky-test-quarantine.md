---
id: flaky-test-quarantine
title: Quarantine flaky tests instead of retrying
summary: Flaky tests are marked quarantine and tracked; automatic retries are not allowed in CI.
type: decision
tags: [ci, testing, flaky]
aliases: [no-retries]
status: active
trust: user-stated
created: 2026-06-01
updated: 2026-10-01
sources:
  - { kind: user, ref: "agreed in planning", at: 2026-06-01 }
---
Flaky tests get the `quarantine` marker and an issue; CI never retries failed tests.

## Context
Retries hid real races for months.

## Decision
Quarantine plus an owner, fixed within two weeks.

## Alternatives
`pytest-rerunfailures` was rejected because it masks real races.

## Consequences
The quarantine list is reviewed weekly.
