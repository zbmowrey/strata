import { expect, test } from 'claude-code/testing'
import type { On } from 'claude-code'

type Answer = { exitCode: number; stdout: string; stderr?: string }

/** A slash command as the person types it. */
const cmd = (command: string, args: string) => ({
  command,
  args,
  origin: { kind: 'composer' as const },
  presentation: { isFullscreen: false, columns: 100 },
})

const STATUS = JSON.stringify({
  v: 1,
  counts: { shards: 40, scripts: 5, playbooks: 2, inbox: 2, questions: 1, stale: 1 },
  session: 'sess-1',
  gates: 'blocked',
  blocking: 2,
})

const DEFAULTS: Record<string, Answer> = {
  version: { exitCode: 0, stdout: '{"v":1,"version":"0.1.0-dev"}\n' },
  status: { exitCode: 0, stdout: STATUS },
  resume: {
    exitCode: 0,
    stdout: JSON.stringify({
      v: 1,
      projects: [{ slug: 'ci-flake', title: 'CI', next: '1. Finish find-leaked-fixtures' }],
    }),
  },
  questions: {
    exitCode: 0,
    stdout: JSON.stringify({
      v: 1,
      questions: [{ id: 'q-1', question: 'Which floor?', path: 'knowledge/_questions/q-1.md' }],
    }),
  },
  search: {
    exitCode: 0,
    stdout: JSON.stringify({
      v: 1,
      total: 1,
      results: [
        {
          id: '20261102-helm-diff',
          kind: 'inbox',
          title: 'helm diff misses ConfigMaps',
          path: 'knowledge/_inbox/x.md',
        },
      ],
    }),
  },
  capture: { exitCode: 0, stdout: 'knowledge/_inbox/20261108-010203-pytest-leaks.md\n' },
  show: { exitCode: 0, stdout: '# pytest-fixture-teardown\nBody.\n' },
  doctor: {
    exitCode: 2,
    stdout: JSON.stringify({
      v: 1,
      status: 'fail',
      checks: [{ name: 'lint', status: 'fail', message: '1 error', fix: 'strata lint' }],
    }),
  },
}

/** Fakes the engine: answers by subcommand, records every argv. */
function engine(
  on: On,
  over: Record<string, Answer | 'missing'> = {},
): { calls: string[][]; lines: (string | undefined)[] } {
  const calls: string[][] = []
  const lines: (string | undefined)[] = []
  on('ui.status', (_, e) => {
    lines.push(e.text)
    return { value: undefined }
  })
  on('process.run', (_, e) => {
    calls.push([...e.argv])
    const sub = e.argv[1] ?? ''
    const a = over[sub] ??
      DEFAULTS[sub] ?? {
        exitCode: 4,
        stdout: '',
        stderr: `strata: unknown command "${sub}" (hint: see strata --help)\n`,
      }
    if (a === 'missing') {
      return { deny: 'spawn strata ENOENT' }
    }
    return {
      value: {
        exitCode: a.exitCode,
        stdout: a.stdout,
        stderr: a.stderr ?? '',
        isStdoutTruncated: false,
        isStderrTruncated: false,
      },
    }
  })
  on('session.id', () => ({ value: 'sess-1' }))
  on('command.register', (_, e) => ({ value: { command: e.name } }))
  on('tool.register', (_, e) => ({ value: { tool: `mcp__strata__${e.name}` } }))
  on('ui.open', () => ({ value: { isPlaced: true } }))
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  on('ui.toast', () => ({ value: undefined }))
  return { calls, lines }
}

const start = { cwd: '/repo', surface: 'terminal', isInteractive: true } as const

test('session start: version check and status line', async ($, on) => {
  const { calls, lines: seen } = engine(on)
  await $.session.start(start)
  expect(calls[0]).toEqual(['strata', 'version', '--format', 'json'])
  expect(calls).toContainEqual(['strata', 'status', '--session', 'sess-1', '--format', 'json'])
  expect(seen.at(-1)).toBe('⛬ 40 · inbox 2 · ? 1 · gates ✗2')
})

test('session start: version mismatch', async ($, on) => {
  const { lines: seen } = engine(on, {
    version: { exitCode: 0, stdout: '{"v":1,"version":"0.2.0"}' },
  })
  await $.session.start(start)
  expect(seen.at(-1)).toBe('⛬ strata engine 0.2.0 ≠ plugin 0.1.0')
})

test('session start: engine missing', async ($, on) => {
  const { lines: seen } = engine(on, { version: 'missing' })
  await $.session.start(start)
  expect(seen.at(-1)).toBe('⛬ strata engine not found')
})

test('slash commands map to engine argv', async ($, on) => {
  const { calls } = engine(on)
  await $.session.start(start)
  const cap = await $.command.run(cmd('capture', 'pytest -n auto leaks connections'))
  expect(cap.text).toBe('Captured → knowledge/_inbox/20261108-010203-pytest-leaks.md')
  expect(calls).toContainEqual([
    'strata',
    'capture',
    'pytest -n auto leaks connections',
    '--project',
    'ci-flake',
  ])
  const res = await $.command.run(cmd('resume', ''))
  expect(calls).toContainEqual(['strata', 'resume'])
  expect(res.text).toBeDefined()
  const doc = await $.command.run(cmd('strata', 'doctor'))
  expect(doc.text).toBe('strata doctor: fail\n✗ lint               1 error → strata lint')
  const bad = await $.command.run(cmd('project', 'delete x'))
  expect(bad.text).toContain('Usage: /project')
  const bench = await $.command.run(cmd('bench', ''))
  expect(bench.text).toBe('strata: unknown command "bench" (hint: see strata --help)')
})

test('engine errors surface with hints', async ($, on) => {
  engine(on, {
    capture: {
      exitCode: 5,
      stdout: '',
      stderr: 'strata: not a strata instance (hint: run `strata init` here, or pass --root)\n',
    },
  })
  await $.session.start(start)
  const out = await $.command.run(cmd('capture', 'x'))
  expect(out.text).toBe(
    'strata: not a strata instance (hint: run `strata init` here, or pass --root) — run `strata init` in this repository',
  )
})

test('model tools: strata_search and strata_show', async ($, on) => {
  const { calls } = engine(on)
  on('tool.call', () => ({ result: 'core' }))
  await $.session.start(start)
  const found = await $.tool.call({ tool: 'mcp__strata__strata_search', query: 'helm', limit: 3 })
  expect(found.result).toBe('20261102-helm-diff [inbox] — helm diff misses ConfigMaps')
  expect(calls).toContainEqual(['strata', 'search', 'helm', '--limit', '3', '--format', 'json'])
  const shown = await $.tool.call({
    tool: 'mcp__strata__strata_show',
    ids: ['pytest-fixture-teardown'],
    links: 1,
  })
  expect(shown.result).toBe('# pytest-fixture-teardown\nBody.\n')
  expect(calls).toContainEqual(['strata', 'show', 'pytest-fixture-teardown', '--links', '1'])
})

test('/garden: tidy, plan-only, and the gardener hand-off', async ($, on) => {
  const startJSON = JSON.stringify({
    v: 1,
    tidy: false,
    worktree: '/repo/.strata/worktrees/garden-1',
    branch: 'garden/1',
    base: 'main',
    plan_file: '/repo/.strata/worktrees/garden-1/.strata/garden/plan.json',
    counts: { 'fix-orphan': 2 },
    tasks: 2,
    skipped: [],
  })
  const { calls } = engine(on, {
    garden: { exitCode: 0, stdout: startJSON },
  })
  // The kit has no core to start a subagent (core alone sets agentId), so the
  // spawn is refused here: the plugin falls back to instructions.
  const spawned: { subagentType: string; cwd: string }[] = []
  on('agent.spawn', (_, e) => {
    // The kit raises it as the Agent tool's input (subagent_type), not AgentSpawnInput.
    const raw = e as unknown as Record<string, unknown>
    spawned.push({
      subagentType: typeof raw.subagent_type === 'string' ? raw.subagent_type : e.subagentType,
      cwd: e.cwd ?? '',
    })
    return { deny: 'no subagents in tests' }
  })
  const written: string[] = []
  on('fs.write', (_, e) => {
    written.push(e.path)
    return { value: undefined }
  })
  await $.session.start(start)
  const out = await $.command.run(cmd('garden', ''))
  expect(out.text).toContain('the gardener could not start: no subagents in tests')
  expect(calls).toContainEqual(['strata', 'garden', 'start', '--format', 'json'])
  expect(spawned).toEqual([
    { subagentType: 'strata:gardener', cwd: '/repo/.strata/worktrees/garden-1' },
  ])
  expect(written).toEqual([])
  const usage = await $.command.run(cmd('garden', 'sideways'))
  expect(usage.text).toBe('Usage: /garden [--plan-only | finish | abort]')
})

test("/garden: the gardener's digest finishes the run", async ($, on) => {
  // A run core started (agentId set): its final answer (the digest) opens the PR.
  const { calls } = engine(on, {
    garden: { exitCode: 0, stdout: '{"v":1,"pr":{"number":41,"url":"https://x/41"},"ready":true}' },
  })
  const run = {
    agentId: 'agent-7',
    worktree: '/repo/.strata/worktrees/garden-1',
    branch: 'garden/1',
    tasks: 2,
  }
  on('state.get', (_, e) => ({
    value: { value: e.key === 'garden' ? run : undefined, version: 1 },
  }))
  on('state.set', () => ({ value: { isSet: true, version: 2 } }))
  const written: string[] = []
  on('fs.write', (_, e) => {
    written.push(e.path)
    return { value: undefined }
  })
  on('turn.complete', () => ({ text: '' }))
  await $.session.start(start)
  const done = {
    answer: '## Garden\nDigest',
    durationMs: 1,
    isAborted: false,
    turnId: 't',
    reason: 'answer',
  } as const
  await $.turn.complete({ ...done, agentId: 'someone-else' })
  expect(written).toEqual([])
  await $.turn.complete({ ...done, agentId: 'agent-7' })
  expect(written).toEqual(['/repo/.strata/worktrees/garden-1/.strata/garden/digest.md'])
  expect(calls).toContainEqual([
    'strata',
    'garden',
    'finish',
    '--title',
    'Garden: garden/1',
    '--body-file',
    '/repo/.strata/worktrees/garden-1/.strata/garden/digest.md',
    '--format',
    'json',
  ])
})

test('/garden reports tidy and previews the plan', async ($, on) => {
  engine(on, { garden: { exitCode: 0, stdout: '{"v":1,"tidy":true}' } })
  await $.session.start(start)
  expect((await $.command.run(cmd('garden', ''))).text).toBe('Garden is tidy: nothing to do.')
  expect((await $.command.run(cmd('garden', '--plan-only'))).text).toBe('Garden is tidy: no tasks.')
})

const PANE = {
  plugin: 'strata',
  component: 'Pane',
  requestId: 'strata',
  props: {
    title: 'Strata',
    isFocused: false,
    bodyColumns: 80,
    placement: 'dock',
    scroll: { offset: 0, bodyRows: 40 },
    view: {},
  },
} as const

test('the pane renders on terminal and desktop', async ($, on) => {
  engine(on)
  on('clock.now', () => ({ value: 1000 }))
  await $.session.start(start)
  await $.command.run(cmd('strata', ''))
  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await $.ui.mount({ ...PANE, surface })
    expect((await ui.find({ type: 'Text', text: /⛬ 40/ }))?.text).toBe(
      '⛬ 40 · inbox 2 · ? 1 · gates ✗2',
    )
    expect(await ui.find({ type: 'Text', text: /ci-flake — Next: 1\. Finish/ })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /Which floor\?/ })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /helm diff misses ConfigMaps/ })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /✗ 2 blocking/ })).toBeDefined()
    expect(await ui.find({ type: 'Button', key: 'refresh' })).toBeDefined()
    await ui.unmount()
  }
})
