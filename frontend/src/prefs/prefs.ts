export interface Prefs {
  fontSize: number
  scrollback: number
  localEcho: boolean
  inspectorOpen: boolean
}

export const DEFAULT_PREFS: Prefs = { fontSize: 15, scrollback: 10000, localEcho: true, inspectorOpen: false }
export const PREFS_KEY = 'tellwire.prefs.v1'
export const FONT_MIN = 10
export const FONT_MAX = 28

const clamp = (n: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, n))

/** Validates untrusted stored data field by field; anything invalid falls back to defaults. */
export function sanitizePrefs(raw: unknown): Prefs {
  const o = (typeof raw === 'object' && raw !== null ? raw : {}) as Record<string, unknown>
  const num = (v: unknown, d: number, lo: number, hi: number) => (typeof v === 'number' && Number.isFinite(v) ? clamp(Math.round(v), lo, hi) : d)
  const bool = (v: unknown, d: boolean) => (typeof v === 'boolean' ? v : d)
  return {
    fontSize: num(o.fontSize, DEFAULT_PREFS.fontSize, FONT_MIN, FONT_MAX),
    scrollback: num(o.scrollback, DEFAULT_PREFS.scrollback, 500, 100000),
    localEcho: bool(o.localEcho, DEFAULT_PREFS.localEcho),
    inspectorOpen: bool(o.inspectorOpen, DEFAULT_PREFS.inspectorOpen),
  }
}

/** Loads preferences. Never throws: storage may be blocked or corrupt. */
export function loadPrefs(storage: Pick<Storage, 'getItem'> | undefined = safeStorage()): Prefs {
  try {
    const s = storage?.getItem(PREFS_KEY)
    return sanitizePrefs(s ? JSON.parse(s) : null)
  } catch {
    return { ...DEFAULT_PREFS }
  }
}

export function savePrefs(p: Prefs, storage: Pick<Storage, 'setItem'> | undefined = safeStorage()): void {
  try {
    storage?.setItem(PREFS_KEY, JSON.stringify(p))
  } catch {
    /* quota or blocked storage: preferences just won't persist */
  }
}

function safeStorage(): Storage | undefined {
  try {
    return globalThis.localStorage
  } catch {
    return undefined
  }
}
