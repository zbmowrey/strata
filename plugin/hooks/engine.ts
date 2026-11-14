// The thin client: every operation runs the strata engine by argv and reads
// its JSON (docs/spec/08-claude-integration.md §5). Exit codes follow
// docs/spec/09-cli.md §2.
import type { ProcessRunInit, ProcessRunResult } from 'claude-code'

/** The plugin's version; moves together with the engine's. */
export const PLUGIN_VERSION = '0.1.3'

/** The engine executable, looked up on PATH. */
export const ENGINE = 'strata'

/** The part of `$` the client needs. */
export type ProcessHost = {
  process: { run: (argv: readonly string[], init?: ProcessRunInit) => Promise<ProcessRunResult> }
}

/** Exit codes (docs/spec/09-cli.md §2). */
export const EXIT = {
  ok: 0,
  runtime: 1,
  blocking: 2,
  partial: 3,
  usage: 4,
  notInstance: 5,
  locked: 6,
} as const

export type EngineOk<T> = { ok: true; data: T; exitCode: number }
export type EngineFail = {
  ok: false
  /** The exit code, or null when the engine could not be started. */
  exitCode: number | null
  message: string
  hint: string
}
export type EngineResult<T> = EngineOk<T> | EngineFail

/** Options for one engine call. */
export type CallOptions = {
  cwd?: string
  timeoutMs?: number
  /** Read stdout as text instead of JSON. */
  text?: boolean
  /** Exit codes that still carry a usable result (e.g. 3 = partial). */
  accept?: readonly number[]
}

const ERROR_LINE = /^strata: (.*?)(?: \(hint: (.*)\))?$/m

/** Builds the argv for an engine call: `strata <args…> [--format json]`. */
export function argv(args: readonly string[], text = false): string[] {
  return text ? [ENGINE, ...args] : [ENGINE, ...args, '--format', 'json']
}

/** Runs the engine and decodes its answer. */
export async function call<T>(
  $: ProcessHost,
  args: readonly string[],
  options: CallOptions = {},
): Promise<EngineResult<T>> {
  const init: ProcessRunInit = { timeoutMs: options.timeoutMs ?? 30_000 }
  if (options.cwd !== undefined) {
    init.cwd = options.cwd
  }
  let ran: ProcessRunResult
  try {
    ran = await $.process.run(argv(args, options.text === true), init)
  } catch (err: unknown) {
    return missing(err)
  }
  return decode<T>(ran, options)
}

function missing(err: unknown): EngineFail {
  const why = err instanceof Error ? err.message : String(err)
  return {
    ok: false,
    exitCode: null,
    message: `the strata engine could not be started (${why})`,
    hint: 'install it: go install github.com/zbmowrey/strata/engine/cmd/strata@latest',
  }
}

/** Decodes a finished engine run (exported for tests). */
export function decode<T>(ran: ProcessRunResult, options: CallOptions = {}): EngineResult<T> {
  const usable = ran.exitCode === EXIT.ok || (options.accept ?? []).includes(ran.exitCode)
  if (usable) {
    if (options.text === true) {
      return { ok: true, data: ran.stdout as T, exitCode: ran.exitCode }
    }
    const data = firstJSON(ran.stdout)
    if (data !== undefined) {
      return { ok: true, data: data as T, exitCode: ran.exitCode }
    }
    return fail(ran.exitCode, 'the engine answered without JSON', '')
  }
  return failure(ran)
}

function failure(ran: ProcessRunResult): EngineFail {
  const m = ERROR_LINE.exec(ran.stderr)
  if (m !== null) {
    return fail(ran.exitCode, m[1] ?? '', m[2] ?? '')
  }
  const fromJSON = errorObject(ran.stdout)
  if (fromJSON !== undefined) {
    return fail(ran.exitCode, fromJSON.message, fromJSON.hint)
  }
  const text = (ran.stderr.trim() || ran.stdout.trim()).split('\n')[0] ?? ''
  return fail(ran.exitCode, text === '' ? `exit ${ran.exitCode}` : text, '')
}

function fail(exitCode: number, message: string, hint: string): EngineFail {
  return { ok: false, exitCode, message, hint }
}

/** The first JSON value of a stream (`--format json` writes one; errors may follow). */
export function firstJSON(stdout: string): unknown {
  const text = stdout.trim()
  if (text === '') {
    return undefined
  }
  try {
    return JSON.parse(text) as unknown
  } catch {
    const line = text.split('\n')[0] ?? ''
    try {
      return JSON.parse(line) as unknown
    } catch {
      return undefined
    }
  }
}

function errorObject(stdout: string): { message: string; hint: string } | undefined {
  for (const line of stdout.trim().split('\n').reverse()) {
    try {
      const v = JSON.parse(line) as { error?: { message?: unknown; hint?: unknown } }
      if (typeof v.error?.message === 'string') {
        return {
          message: v.error.message,
          hint: typeof v.error.hint === 'string' ? v.error.hint : '',
        }
      }
    } catch {
      // not a JSON line
    }
  }
  return undefined
}

/** One line for the person: `strata: <message> (hint: …)`. */
export function describe(f: EngineFail): string {
  const where = f.exitCode === EXIT.notInstance ? ' — run `strata init` in this repository' : ''
  return `strata: ${f.message}${f.hint === '' ? '' : ` (hint: ${f.hint})`}${where}`
}

/** Compares engine and plugin versions by MAJOR.MINOR.PATCH (a -dev build counts as its release). */
export function sameRelease(engine: string, plugin: string): boolean {
  const core = (v: string): string => v.replace(/^v/, '').split(/[-+]/)[0] ?? ''
  return core(engine) === core(plugin)
}
