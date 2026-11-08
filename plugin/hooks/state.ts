// The plugin's $.state (declared in ../types) and the snapshot loader.
import { atom, read, update } from 'claude-code'
import type { EngineInterface } from 'claude-code'

import type { StrataInboxItem, StrataProject, StrataQuestion, StrataSnapshot } from '../types'

import { EXIT, call } from './engine'
import type { ProcessHost } from './engine'
import { formatStatus, loadStatus } from './status'

const engineAtom = atom({ plugin: 'strata', key: 'engine' } as const, null)
const snapshotAtom = atom({ plugin: 'strata', key: 'snapshot' } as const, null)

type Raw = Record<string, unknown>
const list = (v: unknown, key: string): Raw[] => {
  const o = typeof v === 'object' && v !== null ? (v as Raw)[key] : undefined
  return Array.isArray(o) ? (o.filter(x => typeof x === 'object' && x !== null) as Raw[]) : []
}
const s = (v: unknown): string => (typeof v === 'string' ? v : '')

/** Loads everything the pane shows, in parallel. */
export async function loadSnapshot(
  $: ProcessHost,
  session: string | null,
  now: number,
): Promise<StrataSnapshot> {
  const [status, resume, questions, inbox] = await Promise.all([
    loadStatus($, session),
    call<unknown>($, ['resume']),
    call<unknown>($, ['questions', 'list']),
    call<unknown>($, ['search', 'kind:inbox', '--limit', '10'], { accept: [EXIT.partial] }),
  ])
  const projects: StrataProject[] = resume.ok
    ? list(resume.data, 'projects').map(p => ({
        slug: s(p.slug),
        title: s(p.title),
        next: s(p.next).split('\n')[0] ?? '',
      }))
    : []
  const qs: StrataQuestion[] = questions.ok
    ? list(questions.data, 'questions').map(q => ({
        id: s(q.id),
        question: s(q.question),
        path: s(q.path),
      }))
    : []
  const items: StrataInboxItem[] = inbox.ok
    ? list(inbox.data, 'results').map(i => ({ id: s(i.id), title: s(i.title), path: s(i.path) }))
    : []
  return {
    status: typeof status === 'string' ? null : status,
    projects,
    inbox: items,
    questions: qs,
    error: typeof status === 'string' ? status : null,
    refreshedAt: now,
  }
}

/** This session's id (ties the engine's gate state to it), or null. */
export async function sessionId($: EngineInterface): Promise<string | null> {
  try {
    return await $.session.id()
  } catch {
    return null
  }
}

/** Reloads everything the pane shows, and the status line with it. */
export async function refreshAll($: EngineInterface): Promise<void> {
  const snap = await loadSnapshot($, await sessionId($), await $.clock.now())
  await update($, snapshotAtom, () => snap)
  $.ui.status(formatStatus(await read($, engineAtom), snap.status))
}
