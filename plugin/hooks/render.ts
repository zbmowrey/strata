// Compact text for the engine's JSON answers (command output and model tools).

type Raw = Record<string, unknown>

const asObj = (v: unknown): Raw =>
  typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as Raw) : {}
const asArr = (v: unknown): unknown[] => (Array.isArray(v) ? v : [])
const asStr = (v: unknown): string => (typeof v === 'string' ? v : '')
const asNum = (v: unknown): number => (typeof v === 'number' ? v : 0)

/** Counts as `type n · type n`, largest first. */
export function countsLine(counts: unknown): string {
  return Object.entries(asObj(counts))
    .map(([k, v]) => [k, asNum(v)] as const)
    .filter(([, n]) => n > 0)
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([k, n]) => `${k} ${n}`)
    .join(' · ')
}

/** `strata garden --format json` → a plan summary. */
export function renderPlan(raw: unknown, limit = 15): string {
  const p = asObj(raw)
  const tasks = asArr(p.tasks).map(asObj)
  if (tasks.length === 0) {
    return 'Garden is tidy: no tasks.'
  }
  const lines = [`Garden plan: ${tasks.length} tasks — ${countsLine(p.counts)}`]
  for (const t of tasks.slice(0, limit)) {
    lines.push(`  ${asStr(t.risk).padEnd(8)} ${asStr(t.id)}`)
  }
  if (tasks.length > limit) {
    lines.push(`  … ${tasks.length - limit} more (.strata/garden/plan.json)`)
  }
  return lines.join('\n')
}

const MARKS: Record<string, string> = { ok: '✓', warn: '⚠', fail: '✗', info: 'ℹ' }

/** `strata doctor --format json` → one line per check. */
export function renderDoctor(raw: unknown): string {
  const d = asObj(raw)
  const lines = asArr(d.checks).map(c => {
    const k = asObj(c)
    const mark = k.fixed === true ? '✓' : (MARKS[asStr(k.status)] ?? '·')
    const fix = asStr(k.fix) === '' || k.fixed === true ? '' : ` → ${asStr(k.fix)}`
    return `${mark} ${asStr(k.name).padEnd(18)} ${asStr(k.message)}${fix}`
  })
  return [`strata doctor: ${asStr(d.status) || 'unknown'}`, ...lines].join('\n')
}

/** `strata project list --format json` → slug, status, next. */
export function renderProjects(raw: unknown): string {
  const ps = asArr(asObj(raw).projects).map(asObj)
  if (ps.length === 0) {
    return 'No projects. Start one: /project new <slug> "<title>"'
  }
  return ps
    .map(p => {
      const next = asStr(p.next).split('\n')[0] ?? ''
      return `${asStr(p.slug)} (${asStr(p.status)}) — ${next === '' ? asStr(p.title) : `Next: ${next}`}`
    })
    .join('\n')
}

/** `strata bench … --format json` → metrics vs. baseline. */
export function renderBench(raw: unknown): string {
  const b = asObj(raw)
  const metrics = asObj(b.metrics)
  const base = asObj(b.baseline)
  const rows = Object.keys(metrics)
    .sort()
    .map(k => {
      const now = asNum(metrics[k])
      const was = base[k]
      return typeof was === 'number'
        ? `${k} ${was.toFixed(3)} → ${now.toFixed(3)}`
        : `${k} ${now.toFixed(3)}`
    })
  return rows.length === 0 ? JSON.stringify(raw) : rows.join(' · ')
}

/** `strata search --format json` → one compact line per hit, for the model. */
export function renderSearch(raw: unknown): string {
  const r = asObj(raw)
  const hits = asArr(r.results).map(asObj)
  if (hits.length === 0) {
    const sug = asArr(r.suggestions)
      .map(asStr)
      .filter(s => s !== '')
    return sug.length === 0 ? 'No results.' : `No results. Did you mean: ${sug.join(', ')}?`
  }
  const lines = hits.map(h => {
    const kind = asStr(h.type) === '' ? asStr(h.kind) : `${asStr(h.kind)}/${asStr(h.type)}`
    const stale = h.stale === true ? ' ⚠ stale' : ''
    return `${asStr(h.id)} [${kind}]${stale} — ${asStr(h.summary) || asStr(h.title)}`
  })
  const total = asNum(r.total)
  if (total > hits.length) {
    lines.push(`(${hits.length} of ${total}; strata_show <ids> to read)`)
  }
  return lines.join('\n')
}

/** `strata garden start --format json`. */
export type GardenStart = {
  tidy: boolean
  worktree: string
  branch: string
  base: string
  planFile: string
  tasks: number
  /** The run's lease fence; passed back to `garden finish` (0: none). */
  fence: number
  counts: string
  skipped: string[]
}

export function parseGardenStart(raw: unknown): GardenStart {
  const s = asObj(raw)
  return {
    tidy: s.tidy === true,
    worktree: asStr(s.worktree),
    branch: asStr(s.branch),
    base: asStr(s.base),
    planFile: asStr(s.plan_file),
    tasks: asNum(s.tasks),
    fence: asNum(s.fence),
    counts: countsLine(s.counts),
    skipped: asArr(s.skipped).map(asStr),
  }
}

/** `strata garden finish --format json` → one line. */
export function renderFinish(raw: unknown): string {
  const f = asObj(raw)
  const pr = asObj(f.pr)
  if (typeof pr.url === 'string' && pr.url !== '') {
    return `Garden PR #${asNum(pr.number)} ${f.ready === false ? '(draft)' : 'ready for review'}: ${pr.url}`
  }
  if (asStr(f.compare_url) !== '') {
    return `Garden branch pushed; open the PR: ${asStr(f.compare_url)}`
  }
  return 'Garden: nothing to propose.'
}
