import type { GmcpMsg } from '../protocol/messages'

/**
 * Packages DuneMUD documents as "Delta": each message carries only the
 * changed fields, so the client must merge. Source: https://dunemud.net/gmcp/start
 * Everything else (including unknown packages) replaces the previous value.
 */
export const DEFAULT_DELTA_PACKAGES: readonly string[] = [
  'Char.Vitals',
  'Char.Stats',
  'Char.MaxStats',
  'Char.Status',
  'Char.StatusVars',
]

export interface GmcpLogEntry {
  seq: number
  time: number
  package: string
  data?: unknown
  malformed?: boolean
  error?: string
  raw?: string
}

type Listener = () => void

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)

/**
 * Generic GMCP state container. It assumes no fixed package set: every
 * package seen is stored under its name; known delta packages are merged.
 * Malformed messages are logged for the inspector but never touch state.
 * Compatible with React's useSyncExternalStore.
 */
export class GmcpStore {
  private state = new Map<string, unknown>()
  private log: GmcpLogEntry[] = []
  private seq = 0
  private listeners = new Set<Listener>()
  private version = 0
  private snapshotCache: { version: number; value: ReadonlyMap<string, unknown> } | null = null

  constructor(
    private deltaPackages: ReadonlySet<string> = new Set(DEFAULT_DELTA_PACKAGES),
    private maxLog = 500,
  ) {}

  apply(msg: GmcpMsg, now = Date.now()): void {
    this.log.push({ seq: ++this.seq, time: now, package: msg.package, data: msg.data, malformed: msg.malformed, error: msg.error, raw: msg.raw })
    if (this.log.length > this.maxLog) this.log.splice(0, this.log.length - this.maxLog)
    if (!msg.malformed && msg.package) {
      const prev = this.state.get(msg.package)
      if (this.deltaPackages.has(msg.package) && isObject(prev) && isObject(msg.data)) {
        this.state.set(msg.package, { ...prev, ...msg.data })
      } else {
        this.state.set(msg.package, msg.data)
      }
    }
    this.bump()
  }

  /** Forget everything (new session). */
  reset(): void {
    this.state.clear()
    this.log = []
    this.bump()
  }

  /** Latest value of a package, or undefined if never received. */
  get<T = unknown>(pkg: string): T | undefined {
    return this.state.get(pkg) as T | undefined
  }

  /**
   * Reads a dotted path, e.g. "Room.Info.exits.north". The package is the
   * longest known prefix. Returns undefined when any part is unavailable, so
   * UI code can omit/disable features instead of inventing state.
   */
  select(path: string): unknown {
    const parts = path.split('.')
    for (let i = parts.length; i >= 1; i--) {
      const pkg = parts.slice(0, i).join('.')
      if (this.state.has(pkg)) {
        let cur: unknown = this.state.get(pkg)
        for (const k of parts.slice(i)) {
          if (!isObject(cur) || !Object.hasOwn(cur, k)) return undefined
          cur = cur[k]
        }
        return cur
      }
    }
    return undefined
  }

  packages(): string[] {
    return [...this.state.keys()].sort()
  }

  snapshot = (): ReadonlyMap<string, unknown> => {
    if (this.snapshotCache?.version !== this.version) this.snapshotCache = { version: this.version, value: new Map(this.state) }
    return this.snapshotCache.value
  }

  logSnapshot = (): readonly GmcpLogEntry[] => this.logView

  private logView: readonly GmcpLogEntry[] = []

  subscribe = (fn: Listener): (() => void) => {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  private bump() {
    this.version++
    this.logView = [...this.log]
    for (const fn of this.listeners) fn()
  }
}
