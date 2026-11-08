// Slash command arguments → engine argv (docs/spec/08-claude-integration.md
// §5.2). Pure functions: the hooks run what these return.

/** Splits command arguments like a shell: whitespace, "double" and 'single' quotes. */
export function tokenize(args: string): string[] {
  const out: string[] = []
  const re = /"((?:[^"\\]|\\.)*)"|'([^']*)'|(\S+)/g
  for (const m of args.matchAll(re)) {
    out.push(m[1]?.replace(/\\(.)/g, '$1') ?? m[2] ?? m[3] ?? '')
  }
  return out
}

/** A planned engine call: its arguments and whether it answers JSON. */
export type Plan =
  { kind: 'engine'; args: string[]; json: boolean } | { kind: 'usage'; text: string }

const usage = (text: string): Plan => ({ kind: 'usage', text })
const json = (args: string[]): Plan => ({ kind: 'engine', args, json: true })
const text = (args: string[]): Plan => ({ kind: 'engine', args, json: false })

/** /capture <text> — into the current project's chronicle when one is active. */
export function captureArgv(args: string, project: string | null): Plan {
  const body = args.trim()
  if (body === '') {
    return usage('Usage: /capture <text> — e.g. /capture "pytest -n auto leaks DB connections"')
  }
  const words = tokenize(body)
  const t = words.length === 1 ? (words[0] ?? body) : body
  return text(project === null ? ['capture', t] : ['capture', t, '--project', project])
}

/** /strata resume [slug] (/resume is a Claude Code built-in) */
export function resumeArgv(args: string): Plan {
  const [slug, ...rest] = tokenize(args)
  if (rest.length > 0) {
    return usage('Usage: /strata resume [slug]')
  }
  return text(slug === undefined ? ['resume'] : ['resume', slug])
}

const PROJECT_USAGE =
  'Usage: /project new <slug> "<title>" [--goal "<goal>"] | list [--all] | close <slug> | handoff <slug>'

/** /project <new|list|close|handoff> … */
export function projectArgv(args: string): Plan {
  const [sub, ...rest] = tokenize(args)
  switch (sub) {
    case 'new':
      return rest.length >= 2 ? text(['project', 'new', ...rest]) : usage(PROJECT_USAGE)
    case 'list':
      return json(['project', 'list', ...rest.filter(a => a === '--all')])
    case 'close':
    case 'handoff':
      return rest.length === 1 && rest[0] !== undefined
        ? text(['project', sub, rest[0]])
        : usage(PROJECT_USAGE)
    default:
      return usage(PROJECT_USAGE)
  }
}

/** /bench [retrieval|protocol] */
export function benchArgv(args: string): Plan {
  const [kind = 'retrieval', ...rest] = tokenize(args)
  if ((kind !== 'retrieval' && kind !== 'protocol') || rest.length > 0) {
    return usage('Usage: /bench [retrieval|protocol]')
  }
  return json(['bench', kind])
}

/** What /garden was asked to do. */
export type GardenMode = 'run' | 'plan-only' | 'finish' | 'abort' | 'usage'

/** /garden [--plan-only | finish | abort] */
export function gardenMode(args: string): GardenMode {
  const words = tokenize(args)
  if (words.length === 0) {
    return 'run'
  }
  if (words.length > 1) {
    return 'usage'
  }
  switch (words[0]) {
    case '--plan-only':
      return 'plan-only'
    case 'finish':
      return 'finish'
    case 'abort':
      return 'abort'
    default:
      return 'usage'
  }
}

/** Input of the strata_search model tool. */
export type SearchInput = { query: string; filters?: string[]; limit?: number }

/** strata_search → `strata search <query> [filters…] --limit N --format json`. */
export function searchArgv(input: SearchInput): string[] {
  const q = [input.query, ...(input.filters ?? [])].filter(s => s.trim() !== '').join(' ')
  const limit = Math.min(Math.max(Math.trunc(input.limit ?? 10), 1), 50)
  return ['search', q, '--limit', String(limit)]
}

/** Input of the strata_show model tool. */
export type ShowInput = { ids: string[]; links?: number; mode?: 'full' | 'brief' | 'meta' | 'body' }

/** strata_show → `strata show <ids…> [--links N] [--brief|--meta|--body]` (markdown). */
export function showArgv(input: ShowInput): string[] {
  const out = ['show', ...input.ids]
  const links = Math.min(Math.max(Math.trunc(input.links ?? 0), 0), 2)
  if (links > 0) {
    out.push('--links', String(links))
  }
  if (input.mode !== undefined && input.mode !== 'full') {
    out.push(`--${input.mode}`)
  }
  return out
}
