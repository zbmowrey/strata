// The /strata pane (docs/spec/08-claude-integration.md §5.4): active
// projects, inbox, open questions, last gate state, garden, bench.
import { atom, read } from 'claude-code'
import type { On } from 'claude-code'

import type { StrataSnapshot } from '../types'

import { refreshAll } from './state'
import { clock, formatStatus } from './status'

export const PANE = 'strata'

// $.state values (the scan reads each atom from a const of the file that reads it).
const engineAtom = atom({ plugin: 'strata', key: 'engine' } as const, null)
const snapshotAtom = atom({ plugin: 'strata', key: 'snapshot' } as const, null)
const gardenAtom = atom({ plugin: 'strata', key: 'garden' } as const, null)
const lastGardenAtom = atom({ plugin: 'strata', key: 'lastGarden' } as const, null)

/** The pane's lines, section by section (pure: the tests read these). */
export function paneSections(
  snap: StrataSnapshot | null,
  garden: { branch: string } | null,
  lastGarden: string | null,
): { title: string; lines: string[] }[] {
  if (snap === null) {
    return [{ title: 'Strata', lines: ['Loading…'] }]
  }
  const st = snap.status
  const gates =
    st === null
      ? 'unknown'
      : st.gates === 'blocked'
        ? `✗ ${st.blocking} blocking`
        : st.gates === 'ok'
          ? '✓ clean'
          : '– no run yet'
  const gardenLines: string[] = []
  if (garden !== null) {
    gardenLines.push(`gardener working on ${garden.branch}`)
  } else if (st?.gardenRun != null) {
    gardenLines.push(
      `run in progress since ${clock(st.gardenRun.started)} (${st.gardenRun.branch})`,
    )
  }
  gardenLines.push(lastGarden ?? 'no run in this session — /garden --plan-only previews the plan')
  return [
    {
      title: `Active projects (${snap.projects.length})`,
      lines:
        snap.projects.length === 0
          ? ['none']
          : snap.projects.map(p => `${p.slug} — Next: ${p.next || '(no handoff)'}`),
    },
    {
      title: `Inbox (${st?.inbox ?? snap.inbox.length})`,
      lines: snap.inbox.length === 0 ? ['empty'] : snap.inbox.map(i => `${i.id} — ${i.title}`),
    },
    {
      title: `Open questions (${snap.questions.length})`,
      lines:
        snap.questions.length === 0
          ? ['none']
          : snap.questions.map(q => `${q.path} — ${q.question}`),
    },
    { title: 'Last gate run', lines: [gates] },
    { title: 'Garden', lines: gardenLines },
    { title: 'Bench', lines: ['/bench retrieval runs the golden queries'] },
  ]
}

/** Draws the pane; `refresh` reloads the snapshot. */
export function registerPane(on: On): void {
  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    const { Box, Text, Button } = $.ui.resolve(e)
    const [snap, engine, garden, last] = await Promise.all([
      read($, snapshotAtom),
      read($, engineAtom),
      read($, gardenAtom),
      read($, lastGardenAtom),
    ])
    const sections = paneSections(snap, garden, last)
    return (
      <Box flexDirection="column">
        <Text weight="semibold">{formatStatus(engine, snap?.status ?? null)}</Text>
        {snap?.error != null && <Text color="red">{snap.error}</Text>}
        {sections.map(sec => (
          <Box flexDirection="column" key={sec.title}>
            <Text weight="semibold">{sec.title}</Text>
            {sec.lines.map(line => (
              <Text dimColor wrap="truncate-end">
                {'  '}
                {line}
              </Text>
            ))}
          </Box>
        ))}
        <Box>
          <Button
            key="garden"
            label="Run /garden"
            onPress={() => void $.command.run({ command: 'garden', args: '' })}
          />
          <Button key="refresh" label="Refresh" onPress={() => void refreshAll($)} />
        </Box>
      </Box>
    )
  })
}
