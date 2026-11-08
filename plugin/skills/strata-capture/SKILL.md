---
name: strata-capture
description: What to capture into the Strata knowledge base and how to write a good shard — capture triggers, the capture command, shard anatomy (frontmatter, types, sections, sources, trust, size), corrections, and good vs bad examples. Use when you learned something non-obvious, the user corrected you, or you are writing or editing a shard.
tags: [strata, knowledge]
---

# Capturing knowledge

## When

Capture **immediately** when you learn something non-obvious and reusable:
a gotcha (symptom → cause → fix), a decision and its reason, how something really works,
a fact you had to dig for, a correction from the user. Do **not** capture what the code or
git history already says, transient state, or secrets (record *where* a secret lives).

```
strata capture "pytest hangs at exit: session-scoped db fixture never closes its pool; add teardown" \
  --type gotcha --tags pytest,database --source file:tests/conftest.py --project ci-flake
strata capture "user: deploys happen Tuesdays only, never Fridays" --type correction \
  --shard deploy-schedule --source "user:said in session"
```

Captures land in `knowledge/_inbox/` and are searchable at once; the gardener files them.
`--type correction --shard <id>` also bumps that shard's `occurrences`.

## Fix the record in place

A shard is wrong → correct it now: fix the claim, add the evidence to `sources`, bump
`updated` (and `verified_at` if you confirmed it). Never overwrite `trust: user-stated` —
tell the user. Precedence: user-stated > verified > observed > inferred.

## Shard anatomy

`strata new shard <domain>/<topic>/<id> --type <type> --title "…" --summary "…"`, then edit.

- One idea per shard; filename stem == `id` (kebab-case, globally unique).
- Required: `id, title (≤ 80), summary (≤ 160, one line), type, tags (≥ 1), status, trust,
  created, updated`; `sources` (kind ∈ user/url/file/commit/session/script/doc, `ref`, `at`)
  for trust ≥ observed.
- Useful: `aliases` (other names people search), `applies_to` (code globs → pre-edit
  advisory), `scripts:`/`gates:` (enforcement), `verify: {script: …}`, `review_after`.
- Body: first paragraph = the claim in ≤ 3 sentences. Then only sections that add value:
  `## Why`, `## How to apply`, `## Evidence`, `## See also` (`[[id]]` links).
- Type sections — gotcha: Symptom/Cause/Fix · decision: Context/Decision/Alternatives/
  Consequences · standard: Rule/Rationale/Enforcement (`none yet` = promotion signal).
- Limits: body ≤ 120 lines, ≤ 6 KB. Longer → split into linked shards; long procedures →
  playbook; long code → script.
- `strata lint <path>` must be clean.

## Good vs bad

Good: *"Session-scoped DB fixtures leak connections unless torn down; tests then hang at
exit."* — specific, causal, sourced, tagged, `applies_to: ["tests/**/conftest.py"]`.

Bad: *"Be careful with fixtures."* (vague) · a pasted 300-line log (state the finding, cite
the log) · *"The API uses JWT"* when `auth/jwt.go` says so plainly (the code already tells).
