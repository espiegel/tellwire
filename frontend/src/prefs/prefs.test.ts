import { describe, expect, it } from 'vitest'
import { DEFAULT_PREFS, PREFS_KEY, loadPrefs, sanitizePrefs, savePrefs } from './prefs'

const mem = (initial?: string) => {
  const d = new Map<string, string>(initial === undefined ? [] : [[PREFS_KEY, initial]])
  return { getItem: (k: string) => d.get(k) ?? null, setItem: (k: string, v: string) => void d.set(k, v) }
}

describe('prefs', () => {
  it('round-trips', () => {
    const s = mem()
    savePrefs({ ...DEFAULT_PREFS, fontSize: 20, localEcho: false }, s)
    expect(loadPrefs(s)).toEqual({ ...DEFAULT_PREFS, fontSize: 20, localEcho: false })
  })
  it('falls back to defaults on corrupt or hostile data', () => {
    expect(loadPrefs(mem('{not json'))).toEqual(DEFAULT_PREFS)
    expect(sanitizePrefs({ fontSize: 'huge', scrollback: -5, localEcho: 'yes' })).toMatchObject({ fontSize: 15, scrollback: 500, localEcho: true })
    expect(sanitizePrefs({ fontSize: 9999 }).fontSize).toBe(28)
    expect(sanitizePrefs(null)).toEqual(DEFAULT_PREFS)
  })
  it('survives storage that throws', () => {
    const bad = { getItem: () => { throw new Error('denied') }, setItem: () => { throw new Error('denied') } }
    expect(loadPrefs(bad)).toEqual(DEFAULT_PREFS)
    expect(() => savePrefs(DEFAULT_PREFS, bad)).not.toThrow()
  })
})
