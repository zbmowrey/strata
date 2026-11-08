// The strata plugin: a thin client over the `strata` engine
// (docs/spec/08-claude-integration.md §5). Claude's hook events (edit gates,
// stop, session start…) are handled by the engine's `strata hook` commands
// in .claude/settings.json; this module adds the status line, the pane, the
// slash commands and the search/show model tools.
import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { StrataEngine } from '../types'

import {
  benchArgv,
  captureArgv,
  gardenMode,
  projectArgv,
  resumeArgv,
  searchArgv,
  showArgv,
} from './commands'
import type { Plan, SearchInput, ShowInput } from './commands'
import { EXIT, PLUGIN_VERSION, call, describe, sameRelease } from './engine'
import { abortGarden, finishGarden, previewGarden, startGarden } from './garden'
import { PANE, registerPane } from './pane'
import { renderBench, renderDoctor, renderProjects, renderSearch } from './render'
import { refreshAll, sessionId } from './state'
import { formatStatus, loadStatus } from './status'

type $ = EngineInterface

// $.state values (the scan reads each atom from a const of the file that reads it).
const engineAtom = atom({ plugin: 'strata', key: 'engine' } as const, null)
const gardenAtom = atom({ plugin: 'strata', key: 'garden' } as const, null)
const lastGardenAtom = atom({ plugin: 'strata', key: 'lastGarden' } as const, null)

/** The slash commands (docs/spec/08 §5.2). */
export const COMMANDS = [
  {
    name: 'garden',
    description: 'Run the gardener: plan → worktree → PR',
    argumentHint: '[--plan-only | finish | abort]',
  },
  {
    name: 'capture',
    description: 'Capture something non-obvious into the Strata inbox',
    argumentHint: '<text>',
  },
  {
    name: 'promote',
    description: 'Ask the gardener whether a shard should move down the ladder now',
    argumentHint: '<id>',
  },
  {
    name: 'bench',
    description: 'Run the Strata retrieval bench or protocol evals',
    argumentHint: '[retrieval|protocol]',
  },
  {
    name: 'project',
    description: 'Strata project lifecycle',
    argumentHint: '<new|list|close|handoff> …',
  },
  {
    name: 'strata',
    description:
      'Open the Strata pane; /strata doctor checks health; /strata resume shows projects',
    argumentHint: '[doctor | resume [slug]]',
  },
] as const

export const TOOLS = [
  {
    name: 'strata_search',
    description:
      'Search the repository knowledge base (shards, scripts, playbooks, projects) before acting. Returns ids with one-line summaries; read them with strata_show.',
    inputSchema: {
      type: 'object',
      properties: {
        query: {
          type: 'string',
          description: 'Words plus inline filters, e.g. "flaky tests type:gotcha"',
        },
        filters: {
          type: 'array',
          items: { type: 'string' },
          description: 'Extra filters such as "kind:script"',
        },
        limit: { type: 'integer', minimum: 1, maximum: 50 },
      },
      required: ['query'],
    },
  },
  {
    name: 'strata_show',
    description: 'Read knowledge documents by id (batch several ids in one call).',
    inputSchema: {
      type: 'object',
      properties: {
        ids: { type: 'array', items: { type: 'string' }, minItems: 1 },
        links: {
          type: 'integer',
          minimum: 0,
          maximum: 2,
          description: 'Append the link neighborhood',
        },
        mode: { type: 'string', enum: ['full', 'brief', 'meta', 'body'] },
      },
      required: ['ids'],
    },
    isDeferred: false,
  },
] as const

/** Checks the engine is present and of the plugin's release. */
export async function checkEngine($: $): Promise<StrataEngine> {
  const res = await call<{ version?: unknown }>($, ['version'])
  if (!res.ok) {
    return res.exitCode === null
      ? { kind: 'missing', message: describe(res) }
      : { kind: 'error', message: describe(res) }
  }
  const v = typeof res.data.version === 'string' ? res.data.version : ''
  return sameRelease(v, PLUGIN_VERSION)
    ? { kind: 'ok', version: v }
    : { kind: 'mismatch', version: v, plugin: PLUGIN_VERSION }
}

/** Refreshes the status line (counts only: one cheap engine call). */
async function refreshStatus($: $): Promise<void> {
  const engine = await read($, engineAtom)
  if (engine !== null && engine.kind !== 'ok') {
    $.ui.status(formatStatus(engine, null))
    return
  }
  const st = await loadStatus($, await sessionId($))
  $.ui.status(formatStatus(engine, typeof st === 'string' ? null : st))
}

/** Runs a planned engine call and renders its answer. */
async function runPlan($: $, plan: Plan, render?: (data: unknown) => string): Promise<string> {
  if (plan.kind === 'usage') {
    return plan.text
  }
  const res = await call<unknown>($, plan.args, {
    text: !plan.json,
    timeoutMs: 600_000,
    accept: [EXIT.partial],
  })
  if (!res.ok) {
    return describe(res)
  }
  if (render !== undefined) {
    return render(res.data)
  }
  return typeof res.data === 'string' ? res.data.trimEnd() : JSON.stringify(res.data)
}

/** The current project for /capture: the only active one, else none (_adhoc). */
async function currentProject($: $): Promise<string | null> {
  const res = await call<{ projects?: { slug?: unknown }[] }>($, ['resume'])
  const ps = res.ok ? (res.data.projects ?? []) : []
  const slug = ps.length === 1 ? ps[0]?.slug : undefined
  return typeof slug === 'string' ? slug : null
}

async function garden($: $, args: string): Promise<string> {
  switch (gardenMode(args)) {
    case 'plan-only':
      return previewGarden($)
    case 'abort': {
      await update($, gardenAtom, () => null)
      return abortGarden($)
    }
    case 'finish': {
      const run = await read($, gardenAtom)
      await update($, gardenAtom, () => null)
      return finishGarden($, run, '')
    }
    case 'usage':
      return 'Usage: /garden [--plan-only | finish | abort]'
    case 'run': {
      const started = await startGarden($)
      await update($, gardenAtom, () => started.run)
      return started.text
    }
  }
}

async function promote($: $, args: string): Promise<string> {
  const id = args.trim()
  if (id === '' || /\s/.test(id)) {
    return 'Usage: /promote <id>'
  }
  const spawned = await $.agent.spawn({
    subagentType: 'strata:gardener',
    description: `promote ${id}`,
    prompt:
      `Evaluate whether ${id} should be promoted down the ladder now (docs/spec/07-gardener.md §4: ` +
      'correction → gotcha, shard → playbook, howto → script, gotcha/standard → gate). Read it with ' +
      '`strata show`, check its signals (occurrences, enforcement, telemetry), and answer with a ' +
      'recommendation and the exact next step. Do not change files: promotions land through /garden.',
  })
  return spawned.deny === undefined
    ? `The gardener is evaluating ${id}; its recommendation will appear when it finishes.`
    : `Could not start the gardener: ${spawned.deny}`
}

async function command($: $, name: string, args: string): Promise<string> {
  switch (name) {
    case 'garden':
      return garden($, args)
    case 'capture': {
      const out = await runPlan($, captureArgv(args, await currentProject($)))
      return out.startsWith('strata:') || out.startsWith('Usage') ? out : `Captured → ${out}`
    }
    case 'promote':
      return promote($, args)
    case 'bench':
      return runPlan($, benchArgv(args), renderBench)
    case 'project': {
      const plan = projectArgv(args)
      return runPlan($, plan, plan.kind === 'engine' && plan.json ? renderProjects : undefined)
    }
    default:
      return strata($, args)
  }
}

async function strata($: $, args: string): Promise<string> {
  const [sub, ...rest] = args.trim().split(/\s+/)
  if (sub === 'resume') {
    // /resume is a Claude Code built-in, so resume lives under /strata.
    return runPlan($, resumeArgv(rest.join(' ')))
  }
  if (args.trim() === 'doctor') {
    const res = await call<unknown>($, ['doctor'], { accept: [EXIT.blocking], timeoutMs: 120_000 })
    return res.ok ? renderDoctor(res.data) : describe(res)
  }
  await $.ui.open({ id: PANE, title: 'Strata' })
  void refreshAll($)
  return 'Strata pane opened.'
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    // Each registration stands alone: a refused name (e.g. one Claude Code
    // later makes a built-in) must not take the status line and tools with it.
    const refused: string[] = []
    for (const c of COMMANDS) {
      await $.command
        .register({ name: c.name, description: c.description, argumentHint: c.argumentHint })
        .catch(() => refused.push(`/${c.name}`))
    }
    for (const t of TOOLS) {
      await $.tool
        .register({ ...t, inputSchema: { ...t.inputSchema } })
        .catch(() => refused.push(t.name))
    }
    if (refused.length > 0) {
      $.ui.log(`strata: not registered: ${refused.join(', ')} (see claude --debug)`)
    }
    const engine = await checkEngine($)
    await update($, engineAtom, () => engine)
    await refreshStatus($)
    return next(e)
  })

  for (const c of COMMANDS) {
    on('command.run', { command: c.name }, async ($, e) => ({
      text: await command($, c.name, e.args),
    })).catch(() => ({ text: `strata: /${c.name} failed inside the plugin (see claude --debug)` }))
  }

  on('tool.call', { tool: 'mcp__strata__strata_search' }, async ($, e) => {
    const input = e as unknown as SearchInput
    const res = await call<unknown>($, searchArgv(input), { accept: [EXIT.partial] })
    return { result: res.ok ? renderSearch(res.data) : describe(res) }
  }).catch(() => ({
    result: 'strata_search failed inside the plugin; use `strata search` via Bash.',
  }))

  on('tool.call', { tool: 'mcp__strata__strata_show' }, async ($, e) => {
    const input = e as unknown as ShowInput
    const res = await call<string>($, showArgv(input), { text: true, accept: [EXIT.partial] })
    return { result: res.ok ? res.data : describe(res) }
  }).catch(() => ({ result: 'strata_show failed inside the plugin; use `strata show` via Bash.' }))

  // The status line follows edits and strata invocations (gate state, counts).
  on('tool.call', async ($, e, next) => {
    const ran = await next(e)
    const touches =
      e.tool === 'Edit' || e.tool === 'Write' || (e.tool === 'Bash' && /\bstrata\b/.test(e.command))
    if (touches && e.agentId === undefined) {
      void refreshStatus($)
    }
    return ran
  }).catch(($, e, next) => next(e))

  // The gardener's final answer is the PR digest: finish the run with it.
  on('turn.complete', async ($, e, next) => {
    const run = await read($, gardenAtom)
    if (run === null || e.agentId !== run.agentId) {
      return next(e)
    }
    await update($, gardenAtom, () => null)
    const outcome =
      e.reason === 'answer'
        ? await finishGarden($, run, e.answer)
        : `Gardener ended (${e.reason}); the worktree ${run.worktree} is kept — /garden finish or /garden abort.`
    await update($, lastGardenAtom, () => outcome)
    $.ui.toast(outcome)
    void refreshStatus($)
    return next(e)
  })

  registerPane(on)
}
