import { expect, test } from 'claude-code/testing'

import {
  benchArgv,
  captureArgv,
  gardenMode,
  projectArgv,
  resumeArgv,
  searchArgv,
  showArgv,
  tokenize,
} from '../hooks/commands'
import { argv, decode, describe as describeFail, firstJSON, sameRelease } from '../hooks/engine'
import { gardenerPrompt, digestPath } from '../hooks/garden'
import { paneSections } from '../hooks/pane'
import { COMMANDS } from '../hooks/register'
import {
  parseGardenStart,
  renderDoctor,
  renderFinish,
  renderPlan,
  renderProjects,
  renderSearch,
} from '../hooks/render'
import { clock, formatStatus, parseStatus, statusArgs, thousands } from '../hooks/status'

const RAW_STATUS = {
  v: 1,
  counts: { shards: 1204, scripts: 37, playbooks: 9, inbox: 3, questions: 1, stale: 4 },
  session: 's1',
  gates: 'ok',
  blocking: 0,
}

test('status text formatting', () => {
  const ok = parseStatus(RAW_STATUS)
  expect(thousands(1204)).toBe('1,204')
  expect(thousands(7)).toBe('7')
  expect(formatStatus({ kind: 'ok', version: '0.1.0' }, ok)).toBe(
    '⛬ 1,204 · inbox 3 · ? 1 · gates ✓',
  )
  const blocked = parseStatus({ ...RAW_STATUS, gates: 'blocked', blocking: 4 })
  expect(formatStatus(null, blocked)).toBe('⛬ 1,204 · inbox 3 · ? 1 · gates ✗4')
  const running = parseStatus({
    ...RAW_STATUS,
    garden_run: {
      branch: 'garden/20261108-0102',
      worktree: '/r/.strata/worktrees/g',
      started: '2026-11-08T01:02:00-05:00',
    },
    garden_live: true,
  })
  expect(formatStatus(null, running)).toBe(
    '⛬ 1,204 · inbox 3 · ? 1 · gates ✓ · garden running since 01:02',
  )
  const lost = parseStatus({
    ...RAW_STATUS,
    garden_run: { started: 'x' },
    garden_debt: { due: true },
  })
  expect(formatStatus(null, lost)).toBe(
    '⛬ 1,204 · inbox 3 · ? 1 · gates ✓ · garden lost (strata garden resume)',
  )
  expect(formatStatus(null, parseStatus({ ...RAW_STATUS, garden_debt: { due: true } }))).toBe(
    '⛬ 1,204 · inbox 3 · ? 1 · gates ✓ · garden due',
  )
  expect(formatStatus({ kind: 'missing', message: 'x' }, ok)).toBe('⛬ strata engine not found')
  expect(formatStatus({ kind: 'mismatch', version: '0.2.0', plugin: '0.1.0' }, ok)).toBe(
    '⛬ strata engine 0.2.0 ≠ plugin 0.1.0',
  )
  expect(formatStatus({ kind: 'error', message: 'x' }, null)).toBe(
    '⛬ strata ✗ (run /strata doctor)',
  )
  expect(formatStatus(null, null)).toBe('⛬ strata')
  const empty = parseStatus('nonsense')
  expect(empty.shards).toBe(0)
  expect(empty.gates).toBe('unknown')
  expect(clock('no time')).toBe('no time')
  expect(statusArgs('abc')).toEqual(['status', '--session', 'abc'])
  expect(statusArgs(null)).toEqual(['status'])
})

test('command → CLI argv mapping', () => {
  expect(argv(['search', 'x'])).toEqual(['strata', 'search', 'x', '--format', 'json'])
  expect(argv(['resume'], true)).toEqual(['strata', 'resume'])
  expect(tokenize(`new ci-flake "Fix CI flakes" 'a b' c\\"d`)).toEqual([
    'new',
    'ci-flake',
    'Fix CI flakes',
    'a b',
    'c\\"d',
  ])
  expect(captureArgv('pytest -n auto leaks connections', null)).toEqual({
    kind: 'engine',
    args: ['capture', 'pytest -n auto leaks connections'],
    json: false,
  })
  expect(captureArgv('"quoted text"', 'ci-flake')).toEqual({
    kind: 'engine',
    args: ['capture', 'quoted text', '--project', 'ci-flake'],
    json: false,
  })
  expect(captureArgv('  ', null).kind).toBe('usage')
  expect(resumeArgv('')).toEqual({ kind: 'engine', args: ['resume'], json: false })
  expect(resumeArgv('ci-flake')).toEqual({
    kind: 'engine',
    args: ['resume', 'ci-flake'],
    json: false,
  })
  expect(resumeArgv('a b').kind).toBe('usage')
  expect(projectArgv('new ci-flake "Fix CI" --goal "green"')).toEqual({
    kind: 'engine',
    args: ['project', 'new', 'ci-flake', 'Fix CI', '--goal', 'green'],
    json: false,
  })
  expect(projectArgv('list --all')).toEqual({
    kind: 'engine',
    args: ['project', 'list', '--all'],
    json: true,
  })
  expect(projectArgv('close ci-flake')).toEqual({
    kind: 'engine',
    args: ['project', 'close', 'ci-flake'],
    json: false,
  })
  expect(projectArgv('handoff')).toEqual(expect.objectContaining({ kind: 'usage' }))
  expect(projectArgv('delete x').kind).toBe('usage')
  expect(projectArgv('new onlyslug').kind).toBe('usage')
  expect(benchArgv('')).toEqual({ kind: 'engine', args: ['bench', 'retrieval'], json: true })
  expect(benchArgv('protocol')).toEqual({ kind: 'engine', args: ['bench', 'protocol'], json: true })
  expect(benchArgv('perf').kind).toBe('usage')
  expect(gardenMode('')).toBe('run')
  expect(gardenMode('--plan-only')).toBe('plan-only')
  expect(gardenMode('finish')).toBe('finish')
  expect(gardenMode('abort')).toBe('abort')
  expect(gardenMode('now please')).toBe('usage')
  expect(gardenMode('later')).toBe('usage')
  expect(searchArgv({ query: 'flaky tests', filters: ['type:gotcha', ' '], limit: 500 })).toEqual([
    'search',
    'flaky tests type:gotcha',
    '--limit',
    '50',
  ])
  expect(searchArgv({ query: 'x' })).toEqual(['search', 'x', '--limit', '10'])
  expect(showArgv({ ids: ['a', 'b'], links: 1, mode: 'brief' })).toEqual([
    'show',
    'a',
    'b',
    '--links',
    '1',
    '--brief',
  ])
  expect(showArgv({ ids: ['a'], links: 9, mode: 'full' })).toEqual(['show', 'a', '--links', '2'])
})

const run = (exitCode: number, stdout: string, stderr = '') => ({
  exitCode,
  stdout,
  stderr,
  isStdoutTruncated: false,
  isStderrTruncated: false,
})

test('JSON and exit-code error handling', () => {
  expect(decode(run(0, '{"v":1,"a":2}\n'))).toEqual({ ok: true, data: { v: 1, a: 2 }, exitCode: 0 })
  expect(decode(run(0, 'path/x.md\n'), { text: true })).toEqual({
    ok: true,
    data: 'path/x.md\n',
    exitCode: 0,
  })
  expect(decode(run(0, 'not json')).ok).toBe(false)
  expect(decode(run(3, '{"v":1}\n'), { accept: [3] }).ok).toBe(true)
  const notInstance = decode(
    run(
      5,
      '',
      'strata: not a strata instance (no .strata/config.toml found above /x) (hint: run `strata init` here, or pass --root)\n',
    ),
  )
  expect(notInstance).toEqual({
    ok: false,
    exitCode: 5,
    message: 'not a strata instance (no .strata/config.toml found above /x)',
    hint: 'run `strata init` here, or pass --root',
  })
  if (!notInstance.ok) {
    expect(describeFail(notInstance)).toContain('run `strata init` in this repository')
  }
  const locked = decode(
    run(6, '{"v":1,"error":{"message":"garden lock held","hint":"wait","code":6}}\n'),
  )
  expect(locked).toEqual({ ok: false, exitCode: 6, message: 'garden lock held', hint: 'wait' })
  expect(decode(run(1, '', ''))).toEqual({ ok: false, exitCode: 1, message: 'exit 1', hint: '' })
  expect(decode(run(2, '', 'boom\nmore'))).toEqual({
    ok: false,
    exitCode: 2,
    message: 'boom',
    hint: '',
  })
  expect(firstJSON('{"a":1}\n{"error":{}}')).toEqual({ a: 1 })
  expect(firstJSON('')).toBeUndefined()
  expect(sameRelease('0.1.0-dev', '0.1.0')).toBe(true)
  expect(sameRelease('v0.1.0', '0.1.0')).toBe(true)
  expect(sameRelease('0.2.0', '0.1.0')).toBe(false)
})

test('renderers', () => {
  expect(renderPlan({ tasks: [] })).toBe('Garden is tidy: no tasks.')
  const plan = renderPlan(
    {
      counts: { 'fix-orphan': 2, 'file-inbox': 1 },
      tasks: [
        { id: 'file-inbox:a', risk: 'low' },
        { id: 'fix-orphan:b', risk: 'low' },
        { id: 'fix-orphan:c', risk: 'low' },
      ],
    },
    2,
  )
  expect(plan).toContain('Garden plan: 3 tasks — fix-orphan 2 · file-inbox 1')
  expect(plan).toContain('… 1 more')
  expect(
    renderDoctor({
      status: 'warn',
      checks: [
        { name: 'tool:typos', status: 'warn', message: 'missing', fix: 'brew install typos-cli' },
        { name: 'managed', status: 'warn', message: 'stale', fix: 'x', fixed: true },
      ],
    }),
  ).toBe(
    'strata doctor: warn\n⚠ tool:typos         missing → brew install typos-cli\n✓ managed            stale',
  )
  expect(renderProjects({ projects: [] })).toContain('No projects')
  expect(
    renderProjects({
      projects: [
        { slug: 'a', status: 'active', title: 'T', next: '1. do x\n2. y' },
        { slug: 'b', status: 'done', title: 'B' },
      ],
    }),
  ).toBe('a (active) — Next: 1. do x\nb (done) — B')
  expect(renderSearch({ results: [], suggestions: ['flaky'] })).toBe(
    'No results. Did you mean: flaky?',
  )
  expect(renderSearch({ results: [] })).toBe('No results.')
  expect(
    renderSearch({
      total: 5,
      results: [
        { id: 'x', kind: 'shard', type: 'gotcha', summary: 'S', stale: true },
        { id: 'y', kind: 'script', title: 'T' },
      ],
    }),
  ).toBe('x [shard/gotcha] ⚠ stale — S\ny [script] — T\n(2 of 5; strata_show <ids> to read)')
  expect(renderFinish({ pr: { number: 41, url: 'https://x/41' }, ready: true })).toBe(
    'Garden PR #41 ready for review: https://x/41',
  )
  expect(renderFinish({ pr: { number: 41, url: 'https://x/41' }, ready: false })).toContain(
    '(draft)',
  )
  expect(renderFinish({ compare_url: 'https://x/compare' })).toContain(
    'open the PR: https://x/compare',
  )
  expect(renderFinish({ v: 1 })).toBe('Garden: nothing to propose.')
  const start = parseGardenStart({
    tidy: false,
    worktree: '/r/.strata/worktrees/garden-1',
    branch: 'garden/1',
    base: 'main',
    plan_file: '/p.json',
    tasks: 3,
    counts: { 'fix-orphan': 3 },
    skipped: ['a'],
  })
  expect(start.fence).toBe(0)
  expect(parseGardenStart({ fence: 3 }).fence).toBe(3)
  expect(gardenerPrompt(start)).toContain('/r/.strata/worktrees/garden-1')
  expect(gardenerPrompt(start)).toContain('3 tasks: fix-orphan 3')
  expect(digestPath('/r/w/')).toBe('/r/w/.strata/garden/digest.md')
  expect(paneSections(null, null, null)[0]?.lines).toEqual(['Loading…'])
})

test('no slash command shadows a Claude Code built-in (registration would be refused)', () => {
  const builtins = [
    'resume',
    'help',
    'clear',
    'compact',
    'config',
    'plugin',
    'review',
    'status',
    'init',
    'doctor',
  ]
  for (const c of COMMANDS) {
    expect(builtins).not.toContain(c.name)
  }
})
