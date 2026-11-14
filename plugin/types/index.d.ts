// The strata plugin's state contract: what its pane and status line keep in
// $.state (docs/spec/08-claude-integration.md §5.4).

/** Engine health as the plugin last saw it. */
export type StrataEngine =
  | { kind: 'ok'; version: string }
  | { kind: 'missing'; message: string }
  | { kind: 'mismatch'; version: string; plugin: string }
  | { kind: 'error'; message: string }

/** `strata status --format json`, the counts the status line shows. */
export type StrataStatus = {
  shards: number
  scripts: number
  playbooks: number
  inbox: number
  questions: number
  stale: number
  /** The session's last gate outcome (hook state). */
  gates: 'ok' | 'blocked' | 'unknown'
  blocking: number
  /** A /garden run in progress (the run marker), if any. */
  gardenRun: { branch: string; worktree: string; started: string } | null
}

export type StrataProject = { slug: string; title: string; next: string }
export type StrataQuestion = { id: string; question: string; path: string }
export type StrataInboxItem = { id: string; title: string; path: string }

/** Everything the pane draws, refreshed together. */
export type StrataSnapshot = {
  status: StrataStatus | null
  projects: StrataProject[]
  inbox: StrataInboxItem[]
  questions: StrataQuestion[]
  error: string | null
  refreshedAt: number
}

/** A /garden run waiting for its gardener subagent. */
export type StrataGardenRun = {
  agentId: string
  worktree: string
  branch: string
  tasks: number
  /** Lease fence from `garden start`; `garden finish --fence` refuses a stale one. */
  fence?: number
}

declare module 'claude-code' {
  // eslint-disable-next-line @typescript-eslint/consistent-type-definitions -- declaration merging into the API's PluginState needs an interface
  interface PluginState {
    strata: {
      engine: StrataEngine | null
      snapshot: StrataSnapshot | null
      garden: StrataGardenRun | null
      lastGarden: string | null
    }
  }
}
