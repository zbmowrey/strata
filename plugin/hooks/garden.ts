// /garden orchestration (docs/spec/07-gardener.md §3): the engine prepares the
// worktree and plan (`garden start`), the strata:gardener subagent executes
// it, the engine pushes and opens the PR (`garden finish`).
import type { AgentSpawnArgs, AgentSpawnResult } from 'claude-code'

import type { StrataGardenRun } from '../types'

import { call, describe } from './engine'
import type { ProcessHost } from './engine'
import { parseGardenStart, renderFinish, renderPlan } from './render'
import type { GardenStart } from './render'

/** The agent type the plugin's agents/gardener.md defines. */
export const GARDENER = 'strata:gardener'

/** The gardener's task prompt for a started run. */
export function gardenerPrompt(start: GardenStart): string {
  return [
    `You are running a Strata garden cycle in the worktree ${start.worktree} (branch ${start.branch}, based on ${start.base}).`,
    `The plan is ${start.planFile} (${start.tasks} tasks: ${start.counts}). Mechanical fixes are already committed.`,
    'Your working directory is already the worktree: run each command on its own (no `cd`, no `&&`/`;` chains, no pipes) — you run in the background and cannot ask for permission, so compound commands are denied.',
    'Work task by task under the autonomy matrix (docs/spec/07-gardener.md §5) using the strata-garden skill:',
    'one commit per task category in conventional style (`garden(<area>): …`) with task ids in the body;',
    'escalations become questions under knowledge/_questions/ in this branch.',
    'Then verify: `strata check --all` must be green and `strata bench retrieval` must not regress beyond tolerance; drop or fix offending commits.',
    'Do not push, open a PR, merge, or touch the main working tree — the plugin does that when you finish.',
    'Your final answer must be the PR digest (docs/spec/07-gardener.md §6 format) and nothing else.',
  ].join('\n')
}

/** The part of `$` the garden flow uses. */
export type GardenHost = ProcessHost & {
  agent: { spawn: (input: AgentSpawnArgs) => Promise<AgentSpawnResult> }
  fs: { write: (path: string, text: string) => Promise<void> }
}

/** The outcome of starting /garden. */
export type GardenStarted = { text: string; run: StrataGardenRun | null }

/** /garden: start the run and hand it to the gardener. */
export async function startGarden($: GardenHost): Promise<GardenStarted> {
  const res = await call<unknown>($, ['garden', 'start'], { timeoutMs: 600_000 })
  if (!res.ok) {
    return { text: describe(res), run: null }
  }
  const start = parseGardenStart(res.data)
  if (start.tidy) {
    return { text: 'Garden is tidy: nothing to do.', run: null }
  }
  const spawned = await $.agent.spawn({
    subagentType: GARDENER,
    description: `garden ${start.branch}`,
    prompt: gardenerPrompt(start),
    cwd: start.worktree,
  })
  if (spawned.deny !== undefined || spawned.agentId === undefined) {
    return {
      text:
        `Garden prepared ${start.worktree} but the gardener could not start: ${spawned.deny ?? 'no agent id'}.\n` +
        `Ask Claude to run the ${GARDENER} agent there with the plan ${start.planFile}, then /garden finish (or /garden abort).`,
      run: null,
    }
  }
  const skipped =
    start.skipped.length > 0 ? ` (${start.skipped.length} skipped: open garden PR)` : ''
  return {
    text: `Gardener started on ${start.branch}: ${start.tasks} tasks${skipped}. The PR opens when it finishes.`,
    run: {
      agentId: spawned.agentId,
      worktree: start.worktree,
      branch: start.branch,
      tasks: start.tasks,
    },
  }
}

/** The digest file handed to `garden finish` (gitignored inside the worktree). */
export function digestPath(worktree: string): string {
  return `${worktree.replace(/\/+$/, '')}/.strata/garden/digest.md`
}

/** Pushes the branch and opens the PR with the gardener's digest. */
export async function finishGarden(
  $: GardenHost,
  run: StrataGardenRun | null,
  digest: string,
): Promise<string> {
  const args = ['garden', 'finish', '--title', `Garden: ${run?.branch ?? 'run'}`]
  if (run !== null && digest.trim() !== '') {
    const path = digestPath(run.worktree)
    await $.fs.write(path, digest.trim() + '\n')
    args.push('--body-file', path)
  }
  const res = await call<unknown>($, args, { timeoutMs: 600_000 })
  return res.ok ? renderFinish(res.data) : describe(res)
}

/** /garden abort. */
export async function abortGarden($: ProcessHost): Promise<string> {
  const res = await call<unknown>($, ['garden', 'abort'], { text: true })
  return res.ok ? 'Garden run aborted: worktree and marker removed.' : describe(res)
}

/** /garden --plan-only. */
export async function previewGarden($: ProcessHost): Promise<string> {
  const res = await call<unknown>($, ['garden'], { timeoutMs: 120_000 })
  return res.ok ? renderPlan(res.data) : describe(res)
}
