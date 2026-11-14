# 05 — Projects

Projects are Strata's working, episodic and *institutional* memory. Each project is one folder per
epic-sized body of work. It records the work's origin (the request, verbatim), its contract
(requirements), its design, its proof (tests), its rationale (decisions, attributed to user or
Claude), its history (the chronicle) and its outcome. A later session or person can then resume
the work, or understand *why it was done that way* before changing it.

## 1. Principles

- **A project is an epic.** One project is a coherent body of work with its own goal, of the
  size of a Jira epic. Examples: a new feature, a bug-fixing session, an audit, a research
  question, a multi-part initiative. Individual tasks live in its `plan.md`, not in their own
  projects. Work that one project *produces* but that has its own goal becomes a new, linked
  project. For example, an audit's remediation is a separate project with
  `spawned_from: <audit-slug>`. Trivial one-shots log to `_adhoc`.
- **One file, one job; one home per fact.** Each file answers one question (§2). Status lives
  only in `project.md`, requirements only in `requirements.md`, and rationale only in
  `decisions.md`. Other files reference these by stable ID and never restate them.
- **Declared mutation modes.** Every file states how it may change (§8.2). The user's words are
  never edited, accepted decisions are superseded rather than rewritten, and approved contracts
  change only through a decision.
- **Attribution.** Every judgement records who made it (`by: user | claude | joint`). Key
  decisions Claude makes are surfaced to the user for ratification (§3.10).
- **Chronicle captures negative knowledge.** What was tried and failed is as valuable as what
  worked.
- **Link, don't copy.** KB standards, code and external material are referenced, not pasted.
- **Version control is a per-instance choice** (§5): `local` (the default), `records` or `all`.
  `strata init` asks which mode to use. Projects may override the mode.
- **Projects feed the KB.** Closing a project distills durable facts, gotchas and decisions
  into `knowledge/`. Decision shards are versioned, so the "why" survives even in `local` mode.
- **Searchable and addressable.** Project files and log entries are indexed (`kind:project`).
  IDs are addressable as `<slug>:<ID>`, for example `strata show ci-flake:D-2`.

## 2. Layout

```
projects/
├── <slug>/
│   ├── project.md       # IDENTITY & STATUS — the index; the one home of status      §3.1
│   ├── log.md           # HISTORY — append-only chronicle                            §3.2
│   ├── handoff.md       # RESUME POINT — overwritten at each handoff                 §3.3
│   ├── plan.md          # SEQUENCE — milestones and tasks                            §3.4
│   ├── request.md       # ORIGIN — verbatim ask, restatement, clarifications         §3.5
│   ├── sources.md       # EVIDENCE — registry of research & inputs (S-n)             §3.6
│   ├── sources/         #   the material itself; untrusted data
│   ├── requirements.md  # CONTRACT — stories, acceptance criteria, NFRs              §3.7
│   ├── spec.md          # DESIGN — engineering specification & standards applied     §3.8
│   ├── test-plan.md     # PROOF — strategy + AC→test traceability with results       §3.9
│   ├── decisions.md     # RATIONALE — decision record (D-n)                          §3.10
│   ├── findings.md      # FINDINGS — audit/research findings (F-n)                   §3.11
│   ├── outcome.md       # RESULTS & LEARNINGS — written at close                     §3.12
│   ├── out/             # deliverables (indexed by filename only)
│   └── scratch/         # throwaway working files (never indexed, never tracked)
├── _adhoc/              # standing project for work not tied to a named project
└── _archive/<slug>/     # closed projects
```

Which files a project carries depends on its profile (§7). The reading order for a cold reader
is: project → request → requirements → decisions → spec → test-plan / findings → outcome.

| File | Answers | Mutation (§8.2) | Primary author |
|---|---|---|---|
| `project.md` | What is this, and where does it stand? | living (frontmatter via CLI) | Claude / CLI |
| `log.md` | What happened, including what failed? | append-only | Claude |
| `handoff.md` | Where do I pick up? | overwrite | Claude |
| `plan.md` | In what order are we doing it? | living | Claude |
| `request.md` | What was asked, and what did we take it to mean? | verbatim-frozen + amendments | user's words; Claude's restatement |
| `sources.md` | What did we read, and how far can we trust it? | append-only | either |
| `requirements.md` | What must be true when we're done? | controlled | Claude drafts, user approves |
| `spec.md` | How will it be built, and to what standards? | controlled | Claude drafts, user approves |
| `test-plan.md` | How do we prove each criterion, and did we? | living strategy + matrix status | Claude |
| `decisions.md` | What did we choose, who chose it, and why? | append-only (status lines mutate) | either, attributed |
| `findings.md` | What did the audit or research find? | append-only (status lines mutate) | Claude |
| `outcome.md` | What did we get, and what did we learn? | frozen at close + `## Later` | Claude; user may annotate |

## 3. Files

Each standard below covers: purpose · required when · mutation · template · acceptable / not
acceptable · create / update · validation. The rules common to every file are in §8.

### 3.1 `project.md` — identity, status, index

**Purpose.** The front door. In 30 seconds it tells a reader what this is, why it exists, where
it stands and what to read next. It is **the only home of status**. History of status is in
the log, and the narrative of *now* is in the handoff. There is no separate status file.

**Mutation.** The frontmatter is living and is changed through CLI verbs, each of which writes a
log line. The body changes only when the project's identity changes (a rescope), which needs
a D-n.

```markdown
---
id: ci-flake                  # slug == folder name
title: Eliminate CI flakiness in the integration suite
type: bugfix                  # feature|bugfix|audit|research|initiative (§7)
size: M                       # S|M|L|XL — rough effort; drives spec requirement
status: active                # active|paused|done|archived
phase: build                  # define|design|build|verify|close (§4)
health: amber                 # green|amber|red
health_reason: teardown hang root cause unconfirmed
goal: Integration suite passes 50 consecutive CI runs with no retries.
requested_by: user
created: 2026-11-07
updated: 2026-11-13
branch: fix/ci-flake
prs: [42]
spawned_from: ""              # parent project slug (e.g. the audit that produced this)
related: []                   # other project slugs
tags: [ci, testing]
shards: []                    # filled by distillation at close
vcs: ""                       # per-project override of [projects] vcs (§5); empty = inherit
track: []                     # extra globs to version-control (§5)
extra: []                     # extension files (§9)
---
## Why
Two or three sentences: the problem or opportunity and who it matters to.

## Scope
In: … · Out: … (one line each; detail in requirements.md)
```

**Acceptable:** a short why/scope summary and links. **Not acceptable:** task lists (they go in
plan), progress narrative (log/handoff), requirement detail, or rationale (decisions).

**Create / Update.** `strata project new` creates it. `strata project set <slug>
phase=verify health=green --reason "…"` changes status fields, and each change writes
`note: phase build→verify (reason)` to the log. Legal status transitions are listed in §4.

**Validation.** Schema check; `id` == folder name; a `health` other than green requires
`health_reason`; `goal` equals requirements.md `## Goal` when that file exists;
`spawned_from`/`related` slugs resolve (live or archived).

### 3.2 `log.md` — the chronicle

Append-only. One entry per meaningful step, not per tool call. It is written via
`strata project log`, which timestamps, attributes the session and appends atomically, or by
direct append.

```markdown
## 2026-11-07 21:40 · s:3f9a
- tried: `pytest -x --timeout 30` on the integration suite
- failed: hang persists at session teardown; timeout never fires (it's after the last test)
- learned: hang is in fixture finalization, not in a test → capture as gotcha
- decided: D-2 fix teardown rather than raising the timeout
- next: write `find-leaked-fixtures` to scan for the same pattern elsewhere
```

Entry prefixes are a fixed vocabulary, lint-enforced as a warning: `tried`, `failed`,
`worked`, `learned`, `decided`, `blocked`, `next`, `note`. A `decided:` line that has a D-n
entry cites it, while minor tactical decisions may stand alone. `learned:` and `decided:` lines
are the gardener's distillation candidates and seed `outcome.md` at close. The CLI writes
`note:` lines for status, phase and health transitions, approvals and ratifications, so the log
is the project's status history.

**Not acceptable:** tool-output dumps, transcripts, secrets. Long material goes in `scratch/`
and is summarized in the log.

### 3.3 `handoff.md`

Overwritten, not appended, at each handoff point: end of session, before compaction (the
PreCompact hook nudges), or when switching projects. Fixed shape, ≤ 40 lines:

```markdown
---
updated: 2026-11-07T22:15:00-05:00
session: 3f9a
---
## State
One paragraph: where things stand.
## Next
1. Concrete next action (with the exact command if there is one).
## Awaiting user
- D-4 (open): pick retry policy · D-7 (key, by claude): ratify · requirements.md: approve
## Open threads
- Unresolved question / uncertainty.
## Don't redo
- Things already ruled out (with log anchors).
```

`## Awaiting user` lists everything only the user can unblock: `open`/`assumed` decisions,
key Claude decisions awaiting ratification, and unapproved requirements or spec.
`strata project handoff` pre-fills it, and the section is omitted when empty.
`strata project handoff` also pre-fills `## Don't redo` from `failed:` log lines since the
last handoff.

### 3.4 `plan.md` — sequence

**Purpose.** The current intended order of work. **Mutation.** Living: it is rewritten whenever
the plan changes, and the log records why it changed.

```markdown
---
updated: 2026-11-09
---
## M-1 · Reproduce reliably (US-1) — done
- [x] soak workflow running 50× (T-1)
## M-2 · Fix teardown (US-1, D-2) — current
- [ ] explicit teardown in db_session
- [ ] leak-check assertion (AC-1.2)
## M-3 · Nightly coverage (US-2)
```

**Acceptable:** milestones (`M-n`) and checkbox tasks tagged with US/D/F ids; one `current`
marker. **Not acceptable:** narrative (log), rationale (decisions), or status beyond checkboxes
and markers. **Validation:** each milestone cites ≥ 1 US or F (warning); references resolve.

### 3.5 `request.md` — origin

**Purpose.** The permanent record of what was *actually asked*, unaltered, next to how it was
*understood*. Every later "was this in scope?" question is settled here.

**Mutation.** `## Original` and each `## Amendment` block are verbatim-frozen. The
`## Restatement` may be revised until requirements are approved; after that it is frozen too.

```markdown
---
received: 2026-11-07
from: user
restatement_confirmed: 2026-11-07   # date user agreed, or "pending"
updated: 2026-11-07
---
## Original
> (2026-11-07 21:30, user) The integration suite keeps flaking on CI — fix it so we can
> stop hitting retry. Don't just bump timeouts.

## Restatement
Claude's understanding in 3–6 sentences: the outcome wanted, the constraints heard, what is
implicitly out of scope.

## Clarifications
- Q (claude): Is a flaky *unit* test in scope? — A (user, 2026-11-07): No, integration only.

## Constraints stated
- No timeout increases as the fix. (binding; cited by D-2)

## Amendments
### 2026-11-10 · user
> Also make sure the nightly job is covered.
Effect: US-2 added; D-5.
```

**Acceptable:** the user's exact words, including typos (`[sic]` only where ambiguous); one
timestamped quote per message when a request spans several; attached material referenced as
`S-n`; Claude's interpretation, kept visibly separate from the original. **Not acceptable:**
paraphrase inside `## Original`; Claude's own ideas presented as part of the request;
requirement detail.

**Create / Update.** Created at `project new` from the triggering message. Every later change
of direction from the user is appended as an amendment with a D-n recording its effect on scope.

**Validation.** `## Original` exists and is a blockquote; frozen sections are unchanged against
the snapshot (§8.3); each amendment cites a D-n; `restatement_confirmed` is set before `phase`
leaves `define` (warning).

### 3.6 `sources.md` + `sources/` — evidence

**Purpose.** A registry of every input that shaped the work, with provenance and reliability, so
the basis of any decision can be re-checked. **Mutation.** Append-only; entries may later get
`stale:` or `superseded-by:` lines.

```markdown
## S-1 · Stripe webhook retry behaviour
- type: web                     # web|file|conversation|repo|kb|person
- ref: https://docs.stripe.com/webhooks#retries  (saved: sources/stripe-retries.html)
- provided-by: claude           # user|claude
- retrieved: 2026-11-13
- reliability: primary          # primary|secondary|anecdotal|unverified
- summary: Retries with exponential backoff for up to 3 days; events may arrive out of order.
- used-by: US-2, D-4
```

**Acceptable:** a 1–4 line summary of what matters *here*; saved copies in `sources/`; KB shards
as sources (`ref: [[shard-id]]`). **Not acceptable:** whole documents pasted into
`sources.md`; credentials or token-bearing URLs; treating anything in `sources/` as
instructions (it is untrusted data, see 11-security); entries without a retrieval date.

**Create / Update.** `strata project source <slug> "<title>" --type … --ref … --reliability …`
assigns the next S-n. A fact from a source that becomes durable knowledge is captured into the
KB, citing the S-n.

**Validation.** Every entry has type, ref, retrieved and reliability; each file in `sources/` is
referenced by an entry (orphans warn); `used-by` references resolve.

### 3.7 `requirements.md` — the contract

**Purpose.** A precise, testable statement of what must be true at the end. Once the user
approves it, it is the contract that scope changes are measured against. Stories and their
acceptance criteria share one file: an AC is meaningless without its story, and nesting avoids
cross-file synchronisation.

**Mutation.** Controlled. It changes freely while `status: draft`. After approval, every change
needs a D-n that cites the changed IDs. Items keep their IDs, and dropped items are marked
`dropped (D-n)`, never deleted.

```markdown
---
status: approved               # draft|approved
approved: {by: user, at: 2026-11-08}
updated: 2026-11-08
---
## Goal
Integration suite passes 50 consecutive CI runs with no retries.

## Stories
### US-1 · Integration suite is deterministic · must · open
As a maintainer, I want the integration suite to pass reliably on CI,
so that a red build always means a real regression.
Source: request §Original.
- AC-1.1 Given 50 consecutive CI runs on main, when the suite runs without retries, then all pass.
- AC-1.2 Given the `db_session` fixture, when a test ends, then no connection outlives teardown.

## Non-functional requirements
- NFR-1 (performance) Suite wall-time does not increase by more than 10% vs baseline.

## Out of scope
- Unit-test flakiness (request §Clarifications).
```

Story shapes by profile:
- **feature / initiative:** "As a ⟨role⟩, I want ⟨capability⟩, so that ⟨benefit⟩."
- **bugfix:** one story per defect with a *repro* AC (given ⟨conditions⟩ the defect occurs) and
  a *fixed* AC (given the same conditions, ⟨correct behaviour⟩).
- **audit:** stories define coverage, e.g. "every service's auth path is reviewed". ACs make it
  checkable, e.g. "each finding has severity, evidence and a recommendation".
- **research:** "As ⟨user⟩, I need to know ⟨question⟩ so that I can decide ⟨decision⟩." ACs
  define a sufficient answer, e.g. "cites ≥ 2 primary sources; states confidence".

Each story carries priority `must|should|could|wont` and status `open|done|dropped`.
**Acceptable:** observable, measurable ACs (Given/When/Then or a crisp checkable statement).
**Not acceptable:** unmeasurable ACs ("fast", "intuitive", "robust") without a threshold;
implementation detail (spec); tasks (plan); rationale for scope choices (decisions, cited
here).

**Create / Update.** Drafted once the request's restatement is confirmed, then presented for
approval. `strata project approve <slug> requirements` sets `status`/`approved`, records a
D-n and logs it. Every later change follows the order: new D-n → edit → bump `updated`.

**Validation.** Every US has ≥ 1 AC; vague words without a number are flagged (warning); IDs
are unique and stable; `project/controlled-edit` (§8.3); at close, every `must` AC is
`passing` or `waived` in test-plan.

### 3.8 `spec.md` — engineering specification and standards

**Purpose.** How the requirements will be met: design, interfaces, data, constraints, risks and
the standards that apply. It is what a reviewer reads before the diff, and what a future
changer reads before touching the design.

**Mutation.** Controlled, as in §3.7. When the implementation diverges, the spec is updated with
a D-n, because a stale spec is worse than none.

```markdown
---
status: draft                 # draft|approved
approved: null
updated: 2026-11-13
---
## Context            — one paragraph; cites US/NFR ids and S-n
## Design
### Overview          — five lines a reviewer can hold in their head
### Components        — what changes where (paths), new vs modified
### Interfaces        — CLI/API/schema signatures, formats, error behaviour
### Data              — models, migrations, retention
### Flows             — main-path sequences (mermaid allowed)
## Standards applied  — [[kb-standard]] links + project-specific standards (each with D-n)
## Constraints        — from request §Constraints stated and NFRs
## Alternatives considered — one line each → D-n
## Risks & mitigations — table: risk | likelihood | impact | mitigation
## Security & privacy
## Rollout, migration & rollback
## Observability
## Open issues        — each is a D-n with status open
```

**Acceptable:** design at reviewer level; diagrams; signatures; links to code. **Not
acceptable:** large code blocks (link the file); restated KB standards (link them); "we chose X"
without a D-n; requirement changes (requirements.md + D-n).

**Validation.** Alternatives and open issues cite D-n; standards links resolve; `Context` cites
requirement ids; `project/controlled-edit`.

### 3.9 `test-plan.md` — strategy and proof

**Purpose.** How each criterion is proven, and the evidence that it was. **Mutation.** The
strategy is living. Matrix rows are append-only, and their `status`/`evidence` columns update as
tests run.

```markdown
---
updated: 2026-11-12
---
## Strategy
- Levels: unit (fixtures), integration (suite), soak (50× CI loop).
- Environments: local docker-compose; GitHub Actions ubuntu-24.04.
- Data: seeded fixture DB `testdata/seed.sql`.

## Traceability
| AC | Test | Level | Method | Location | Status | Evidence |
|---|---|---|---|---|---|---|
| AC-1.1 | T-1 | soak | auto | `.github/workflows/soak.yml` | passing | run 1188 (50/50), 2026-11-12 |
| AC-1.2 | T-2 | unit | auto | `tests/fixtures_test.py::test_no_leak` | passing | CI #1187 |
| NFR-1 | T-4 | integration | auto | `just bench-it` | waived (D-6) | — |

## Manual checks
### T-3 · Nightly job green after deploy
Steps: … · Expected: … · Performed: 2026-11-12 by user — pass.
```

Allowed statuses are `planned`, `passing`, `failing`, `flaky` and `waived (D-n)`.
**Acceptable:** links to tests and CI runs; short evidence summaries; manual-check procedures.
**Not acceptable:** pasted test output (link it, or use scratch); `passing` without evidence;
waivers without a decision.

**Validation.** Every AC and NFR has ≥ 1 row; T-ids are unique; `waived` cites a D-n; at close,
no `must` AC is `planned`, `failing` or `flaky`.

### 3.10 `decisions.md` — the decision record

**Purpose.** The "why we did it that way" file. Every material choice is recorded with its
context, options, rationale, consequences and owner. Consult it before changing anything the
project produced.

**Mutation.** Append-only. Only the `status`, `ratified` and `superseded-by` lines of an existing
entry may change. To change a decision, add a new D-n that supersedes it.

Questions and assumptions live here too: an open question is a decision not yet made, and an
assumption is a decision made implicitly.

| status | meaning |
|---|---|
| `open` | question raised; shapes or blocks work; answer pending |
| `assumed` | Claude proceeded on an unconfirmed assumption; needs confirmation |
| `proposed` | a recommendation awaiting the user |
| `accepted` | in force |
| `rejected` | considered and declined (kept so it is not re-litigated) |
| `superseded` | replaced; `superseded-by: D-m` |

**When to record a D-n:** the user made a call; alternatives were genuinely weighed; someone
could later reasonably ask "why?"; scope changed; an approved requirement or spec changed; a
test was waived; Claude proceeded on an assumption. Minor tactical choices stay as `decided:`
log lines.

**Key decisions** (`key: true`) are those that are:
- **architectural:** structure, interfaces, data model, dependencies, technology choice, or
  cross-cutting patterns;
- **pivotal:** they change scope, direction or approach; are costly to reverse; deviate from
  an approved requirement or spec or from a KB standard; or carry security, cost or
  data-loss risk.

A key decision with `by: claude` starts `ratified: pending`. Pending ratifications appear in
`strata resume`, in `strata project show`, and under handoff `## Awaiting user`. They are
batched to the user at natural checkpoints: phase change, PR open, close. The user confirms with
`strata project ratify <slug> D-n` or reverses with `--reject`, which adds a superseding D-n.
Non-key Claude decisions are recorded but need no ratification. Every user decision is
recorded, key or not.

```markdown
---
updated: 2026-11-10
---
## D-2 · Fix teardown, don't raise timeouts · accepted
- date: 2026-11-08
- by: user                    # user|claude|joint
- key: true
- ratified: n/a               # by: claude + key → pending | yes (user, <date>) | no (→ D-m)
- context: Hang occurs in fixture finalization after the last test; timeouts never fire (log 2026-11-07 21:40).
- options:
  - (a) raise pytest timeout to 120 s — cheap; hides the leak.
  - (b) explicit teardown in `db_session` — fixes the cause; touches 14 tests.
- decision: (b).
- rationale: user constraint "don't just bump timeouts" (request §Constraints stated); (a) masks a real leak.
- consequences: 14 tests updated; new leak-check assertion AC-1.2.
- affects: US-1, AC-1.2, spec §Design, tests/conftest.py
- evidence: S-2, log 2026-11-07 21:40
- supersedes: —

## D-5 · Nightly job is in scope · accepted
- date: 2026-11-10 · by: user · key: false · context: request §Amendments 2026-11-10 · decision: add US-2.
```

Short decisions may collapse onto one line, but `by`, `key`, `context`, `decision` and a
rationale (stated, or obvious from the context) must remain. **Acceptable:** concise options with
honest trade-offs; verbatim user words when the user decided; links to evidence. **Not
acceptable:** rewriting an accepted entry; deleting rejected options; rationale that only
restates the decision.

**Create / Update.** `strata project decide <slug> "<title>" --by user|claude [--key] [--status
…] [--supersedes D-n]` assigns the next D-n, appends a templated entry, flips the superseded
entry's status line, and logs `decided: D-n …`.

**Validation.** IDs are unique and monotonic; `accepted`, `rejected` and `superseded` entries
carry the required fields; supersession resolves both ways; accepted entries are unchanged
against the snapshot apart from the mutable lines; at close, no `open` or `assumed` entries
remain and every key Claude decision is ratified or listed under outcome `## Left pending`.

**Distillation.** At close, each accepted decision whose relevance outlives the project becomes
a KB `decision` shard. It is `trust: user-stated` when `by: user` and `verified` when it is a
ratified Claude decision, and it cites `projects/<slug>/decisions.md#D-n`.

### 3.11 `findings.md` — audit and research findings

**Purpose.** The discrete results of an audit or investigation. Each finding is citable, so
follow-up projects (remediation) can reference `<audit-slug>:F-n`. **Mutation.** Append-only;
`status` and `remediated-by` lines may change.

```markdown
## F-3 · Session tokens logged at debug level · high · open
- area: services/auth/middleware.go:88
- evidence: log sample in scratch/auth-debug.txt (redacted); S-2
- impact: tokens exposed to anyone with log access.
- recommendation: redact at the logger; add a gitleaks rule for the pattern.
- remediated-by: auth-hardening:US-2      # set by the remediation project
```

Severity is one of `critical|high|medium|low|info`. Status is one of
`open|accepted-risk (D-n)|remediating|resolved|wontfix (D-n)`. **Not acceptable:**
unredacted secrets as evidence; findings without evidence or a location; remediation work done
inside the audit project (spawn a project instead). A remediation plan is an `out/`
deliverable of the audit; executing it is a new project with `spawned_from:`.

### 3.12 `outcome.md` — results and learnings

**Purpose.** The closing record: did it work, what exactly was delivered, how it differed from
the plan and why, and what we know now that we didn't before. **Mutation.** Drafted by
`strata project close` (results from test-plan, learnings from `learned:` lines), finalized
with the user, then frozen. Later observations are appended under `## Later`.

```markdown
---
result: met                   # met|partial|not-met|abandoned
closed: 2026-11-13
updated: 2026-11-13
---
## Result        — one paragraph against the goal, with headline evidence
## Acceptance    — AC | result | evidence (generated from test-plan)
## Delivered     — PRs, commits, releases, out/ files, spawned projects
## Deviations    — each cites a D-n
## Learnings     — worked / failed / surprised
## Follow-ups    — new project candidates, issues
## Distilled     — shard ids produced at close
## Left pending  — key Claude decisions not ratified, with reason
## Later         — dated post-close notes
```

**Not acceptable:** a re-narration of the log; unlinked claims of success; learnings that are
only "it went well". **Validation:** `result` is set; Acceptance matches test-plan; each
deviation cites a D-n; Distilled links resolve after the gardener's distill task.

## 4. Lifecycle

```
strata project new <slug> "<title>" --type <profile> --goal "…"   → active, phase define
strata project set <slug> phase=<p> health=<h> --reason "…"      (logged)
strata project approve <slug> requirements|spec                    (D-n + log)
strata project pause <slug> / activate <slug>                      → paused / active
strata project resume [<slug>]                                     prints handoff (+ plan headings)
strata project close <slug>                                        → done; drafts outcome.md; queues `distill`
(gardener distills, links shards in project.md)                    → archived (moved to _archive/)
```

Phases are `define → design → build → verify → close`. They are advisory: projects loop
back, and the log records it.

| Phase | Files born or finalized |
|---|---|
| define | request (restatement confirmed), sources, requirements (approved) |
| design | spec (approved), test-plan, decisions |
| build | plan, log, handoff, decisions |
| verify | test-plan results, findings (audit) |
| close | outcome; distillation |

Two transitions are checked:
- **Leaving `define`** for `feature` and `initiative` profiles warns unless requirements are
  approved and the restatement is confirmed.
- **`close` refuses** unless:
  - `outcome.md` has a `result`;
  - every `must` AC is `passing` or `waived (D-n)`;
  - no decision is `open` or `assumed`;
  - every key Claude decision is ratified or listed under `## Left pending`.

`--force` closes anyway and records a log line saying why.

Status transitions: active ⇄ paused; active|paused → done → archived. Lifecycle details
unchanged from v1:
- `strata resume` (also used by the SessionStart hook) prints, for each `active` named
  project, most recently active first: title, phase and health, last activity and its age, the
  handoff `## Next`, and the *count* of awaiting-user items. Output is ≤ 15 lines per project
  and ≤ 3 projects; the rest are summarized as a count.
- `strata project log` groups lines from the same session into the current entry while that
  entry is < 30 minutes old. Writes take a per-project lock (`projects/<slug>/.lock`).
- `_adhoc` is always present; captures and logs without `--project` go there. The gardener
  distills `_adhoc/log.md` periodically and truncates entries older than 30 days after
  distillation.
- A project with no log entry for `[projects] stale_after` (default 30 days) is flagged. The
  gardener asks whether to pause or close it, and never closes a project itself, because closing
  is a scope decision. It also flags `assumed` decisions older than 7 days.

## 5. Version control (`vcs` mode)

How much of `projects/` is committed is chosen per instance, because the right answer depends on
the team:

| Mode | Tracked | Use when |
|---|---|---|
| `local` (default) | nothing (per-project `track:` globs only) | solo work where the folder is personal; or as a cautious default |
| `records` | the record files: `project.md`, `request.md`, `sources.md`, `requirements.md`, `spec.md`, `test-plan.md`, `decisions.md`, `findings.md`, `outcome.md`, and `out/` | teams that want the "why" in review and history, but not session churn |
| `all` | everything except `scratch/` and lock files | solo or small teams wanting the complete record in git |

`log.md`, `handoff.md` and `plan.md` are session-churn files: they are rewritten or appended
many times per session and are per-person by nature. In a large team they are the source of
merge conflicts, which is why `records` leaves them local.

- **Setting the mode.** `[projects] vcs = "local"|"records"|"all"` in `.strata/config.toml`.
  `strata init` asks for it (§5.1). A project overrides with `vcs:` in its `project.md`, for
  example one high-stakes project kept fully in `all` inside a `records` instance. Per-project
  `track:` globs add individual files on top of either.
- **Never tracked**, in any mode: `scratch/`, `.lock`, and `sources/` files over
  `[projects] max_tracked_source` (default 1 MiB). Large sources are kept local and recorded
  by ref.
- **ID collisions in shared modes.** Two branches can both append `D-7`. Lint reports duplicate
  IDs as errors, and `strata project renumber <slug> D-7 --to D-8` rewrites the newer entry and
  every in-project reference to it. On a rebase/merge conflict, keep both entries and renumber.
- **Changing the mode later** means editing the config and running `strata gitignore sync`.
  Moving away from a tracked mode does not delete committed files: `sync` prints the
  `git rm --cached` command for the user to run.

### 5.1 Asking at init

`strata init` resolves the mode in this order:
1. the `--projects-vcs local|records|all` flag;
2. an existing config value (re-running init never changes it);
3. on an interactive TTY, a prompt showing the table above, defaulting to `local`;
4. otherwise `local`, with a printed notice of how to change it.

When Claude runs `strata init` for a user, the `strata-project` skill and the install
instructions require Claude to **ask the user first** which mode they want. Claude shows the
three options with the guidance above and passes the answer as `--projects-vcs`; it does not
silently take the default.

### 5.2 The generated `.gitignore` block

`strata gitignore sync` (run by `project` commands, `init`, `doctor` and the gardener)
regenerates a managed block in the repo's `.gitignore` from the mode, the per-project `vcs:`
overrides and the `track:` globs. In `local` mode, with one project opting in two files
(`track: [out/report.md, plan.md]`):

```gitignore
# >>> strata:projects (GENERATED — edit `track:` in projects/<slug>/project.md)
/projects/**
!/projects/**/
!/projects/ci-flake/out/report.md
!/projects/ci-flake/plan.md
# <<< strata:projects
```

In `records` mode the body between the markers becomes:

```
/projects/**
!/projects/**/
!/projects/*/project.md
!/projects/*/request.md
!/projects/*/sources.md
!/projects/*/requirements.md
!/projects/*/spec.md
!/projects/*/test-plan.md
!/projects/*/decisions.md
!/projects/*/findings.md
!/projects/*/outcome.md
!/projects/*/out/**
/projects/*/scratch/**
/projects/**/.lock
```

Why this shape: git cannot re-include a file whose parent directory is excluded. `/projects/**`
ignores everything; `!/projects/**/` re-includes all directories, which commits nothing by
itself; the `!` lines then re-include exactly the selected files; and the trailing patterns
re-exclude what must never be tracked. In `all` mode the block re-includes `/projects/**` and
keeps the exclusions. The marker comment names the active mode. The generator validates globs,
rejects any that would track `scratch/`, and has tests that assert `git check-ignore` results
for every mode against a fixture repo.

## 6. Commands (summary; full reference in 09)

- **Lifecycle:** `strata project new|list|show|set|pause|activate|resume|close` and
  `strata resume`.
- **Records:** `strata project log|handoff|decide|ratify|approve|source|add|renumber`.
- **Version control:** `strata project track` and `strata gitignore sync`.

`project show <slug>` renders the generated index and status report: the fields from
`project.md`, the files present for its profile (missing ones flagged), counts per ID type,
AC coverage (passing / waived / outstanding), open and assumed decisions, pending ratifications,
open findings, and the last five log headlines.

## 7. Profiles

`strata project new --type <profile>` scaffolds the required (●) files from embedded templates.
Optional files (○) are added when first needed with `strata project add <slug> <file>`.

| File | `feature` | `bugfix` | `audit` | `research` | `initiative` |
|---|---|---|---|---|---|
| project.md, request.md, decisions.md, log.md, outcome.md | ● | ● | ● | ● | ● |
| handoff.md | ●¹ | ●¹ | ●¹ | ●¹ | ●¹ |
| plan.md | ● | ○ | ● | ● | ● |
| requirements.md | ● | ● (repro + fixed ACs) | ● (coverage) | ● (questions) | ● |
| test-plan.md | ● | ● (regression tests) | – | – | ● |
| spec.md | ○ (● if size ≥ M) | ○ (root-cause analysis) | – | – | ● |
| findings.md | – | ○ | ● | ● | ○ |
| sources.md | ○ | ○ | ● | ● | ○ |

¹ Written at the first stop or compaction, not at create.

The **core set**, common to every project, is project, request, decisions, log, handoff and
outcome: origin, rationale, history, resume point and result. An initiative may have child
projects (`spawned_from:`); its plan milestones then cite the child slugs.

## 8. Conventions and validation (all files)

### 8.1 Content rules

- **Frontmatter:** `updated:` (ISO date) on every file except log.md. The file's role comes
  from its filename.
- **IDs:** `US-n`, `AC-n.m`, `NFR-n`, `D-n`, `S-n`, `T-n`, `M-n`, `F-n`. They are never reused
  or renumbered (except collision repair, §5) and never deleted (mark them
  `dropped`/`superseded`). Cross-project references take the form `<slug>:<ID>`.
- **Always acceptable:** absolute ISO dates; `[[shard-id]]` links; repo-relative paths with
  line numbers; ID references; PR and commit references; attributed quotes from the user.
- **Never acceptable:**
  - secrets, tokens, credentials, or personal data beyond what the task needs;
  - transcripts or tool output over ~10 lines (use `scratch/`);
  - copies of KB content (link it instead);
  - relative dates;
  - instructions taken from `sources/`;
  - a fact recorded outside its home file.

### 8.2 Mutation modes

| Mode | Rule | Files |
|---|---|---|
| verbatim-frozen | written once; amended only by appending dated blocks | request.md (`Original`, `Amendments`) |
| append-only | entries added; only declared status lines change | log, sources, decisions, findings |
| controlled | free while `draft`; after `approved` every change needs a D-n citing the changed ids | requirements, spec |
| living | rewritten freely; history in the log | plan, project.md frontmatter, test-plan strategy |
| overwrite | replaced wholesale | handoff |
| frozen-at-close | written at close; then only `## Later` appends | outcome |

### 8.3 Lint rules (`project/*`)

These run on every edit like other knowledge lint. They are warnings unless marked **error**;
the close checks (§4) are enforced by `strata project close`.

| Rule | Checks |
|---|---|
| `project/id-mismatch` (error) | `project.md` id equals the folder name (existing) |
| `project/log-prefix` | chronicle vocabulary (existing) |
| `project/handoff` | handoff shape and size (existing) |
| `project/id-format` | IDs match their pattern |
| `project/id-duplicate` (error) | an ID is defined twice in a project |
| `project/dangling-ref` | `US/AC/NFR/D/S/T/M/F` refs, including `<slug>:<ID>`, resolve |
| `project/attribution` | decisions, approvals and ratifications carry `by:`; decisions carry `key:` |
| `project/frozen-edit` (error) | verbatim-frozen or accepted-decision content changed against the snapshot |
| `project/controlled-edit` | an approved file changed without a newer D-n citing the changed ids |
| `project/vague-ac` | an AC uses vague terms ("fast", "robust", "user-friendly") with no number or observable check |
| `project/profile-missing` | a required file for the profile is absent once the phase passes the one it is born in |

Snapshots for the frozen and controlled checks are kept in `.strata/cache/projects/<slug>/`,
refreshed on each clean lint. In `records` and `all` modes, git history is the fallback when the
snapshot is missing.

## 9. Extensibility

The core set is a floor, not a ceiling.

1. **Ad-hoc files.** Any `.md` in the project root with an `updated:` line and a `purpose:`
   line, listed in `project.md` `extra:`, is indexed and checked against §8 only.
2. **Registered kinds.** `.strata/project-kinds/<name>.md` defines a reusable file type in the
   §3 shape: purpose, required-when, mutation mode, template, acceptable/not, ID prefix if any,
   and validation fields. `strata project add <slug> <name>` scaffolds it, and lint applies its
   declared mode and fields.
3. **Template overrides.** `.strata/templates/project/<file>.md` replaces the embedded template
   for that instance.
4. **Custom profiles.** `[projects.profiles.<name>]` lists required and optional files, for
   example a `migration` profile adding a `runbook`.
5. **Promotion.** When an `extra:` file name recurs in three or more projects, the gardener asks
   whether to register it as a kind, or to propose it for the engine's core set.

Candidate kinds: `runbook`, `threat-model`, `experiments` (hypotheses and results), `comms`
(stakeholder updates), `glossary`, `migration`.

## 10. Fit with the rest of Strata

- **Search.** Every project file is indexed as `kind:project` with its role as a subkind, so
  `teardown kind:project role:decisions` answers "did we decide this before?".
  `strata show <slug>:<ID>` returns one entry.
- **Code → why.** Commit messages and PR bodies made for a project carry the trailers
  `Project: <slug>` and, where relevant, `Decisions: D-2, D-5`. This lets a reader walk from
  `git blame` to the request, requirements and rationale.
- **Gardener.** At close it distills decisions and learnings. It also flags stale projects and
  stale `assumed` decisions, and proposes extension promotion.
- **CLAUDE.md block.** Section 6 instructs Claude to:
  - open a project for epic-sized work;
  - record the request verbatim;
  - give every user call, assumption and key decision a D-n;
  - surface key Claude decisions for ratification.
