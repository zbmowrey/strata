// The status line: `⛬ 1,204 · inbox 3 · ? 1 · gates ✓` (docs/spec/08 §5.3),
// from `strata status [--session <id>] --format json`.
import type { StrataEngine, StrataStatus } from '../types'

import { call } from './engine'
import type { ProcessHost } from './engine'

/** Groups thousands: 1204 → "1,204". */
export function thousands(n: number): string {
  return String(Math.trunc(n)).replace(/\B(?=(\d{3})+(?!\d))/g, ',')
}

/** HH:MM of an RFC 3339 timestamp, as written (the engine's local clock). */
export function clock(ts: string): string {
  const m = /T(\d{2}:\d{2})/.exec(ts)
  return m?.[1] ?? ts
}

/** The status line text for an engine state and its counts. */
export function formatStatus(engine: StrataEngine | null, status: StrataStatus | null): string {
  switch (engine?.kind) {
    case 'missing':
      return '⛬ strata engine not found'
    case 'mismatch':
      return `⛬ strata engine ${engine.version} ≠ plugin ${engine.plugin}`
    case 'error':
      return '⛬ strata ✗ (run /strata doctor)'
    default:
      break
  }
  if (status === null) {
    return '⛬ strata'
  }
  const parts = [
    `⛬ ${thousands(status.shards)}`,
    `inbox ${status.inbox}`,
    `? ${status.questions}`,
    status.gates === 'blocked' ? `gates ✗${status.blocking}` : 'gates ✓',
  ]
  if (status.gardenRun !== null) {
    parts.push(
      status.gardenRun.live
        ? `garden running since ${clock(status.gardenRun.started)}`
        : 'garden lost (strata garden resume)',
    )
  } else if (status.gardenDue) {
    parts.push('garden due')
  }
  return parts.join(' · ')
}

type Raw = Record<string, unknown>

const num = (v: unknown): number => (typeof v === 'number' && Number.isFinite(v) ? v : 0)
const str = (v: unknown): string => (typeof v === 'string' ? v : '')
const obj = (v: unknown): Raw | null =>
  typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as Raw) : null

/** Reads `strata status --format json` leniently: missing fields read as empty. */
export function parseStatus(raw: unknown): StrataStatus {
  const r = obj(raw) ?? {}
  const c = obj(r.counts) ?? {}
  const run = obj(r.garden_run)
  const gates = r.gates === 'ok' || r.gates === 'blocked' ? r.gates : 'unknown'
  return {
    shards: num(c.shards),
    scripts: num(c.scripts),
    playbooks: num(c.playbooks),
    inbox: num(c.inbox),
    questions: num(c.questions),
    stale: num(c.stale),
    gates,
    blocking: num(r.blocking),
    gardenRun:
      run === null
        ? null
        : {
            branch: str(run.branch),
            worktree: str(run.worktree),
            started: str(run.started),
            live: r.garden_live === true,
          },
    gardenDue: obj(r.garden_debt)?.due === true,
  }
}

/** The argv of a status call (the session id ties the gate state to this session). */
export function statusArgs(session: string | null): string[] {
  return session === null || session === '' ? ['status'] : ['status', '--session', session]
}

/** Loads the counts, or the reason they could not be read. */
export async function loadStatus(
  $: ProcessHost,
  session: string | null,
): Promise<StrataStatus | string> {
  const res = await call<unknown>($, statusArgs(session))
  return res.ok ? parseStatus(res.data) : res.message
}
