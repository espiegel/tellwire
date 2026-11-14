import { describe, expect, it, vi } from 'vitest'
import { DEFAULT_BRAND, applyBrand, loadBrand, parseBrand } from './brand'

describe('parseBrand', () => {
  it('returns defaults for non-objects', () => {
    for (const v of [null, undefined, 'x', 5, [], true]) expect(parseBrand(v)).toEqual(DEFAULT_BRAND)
  })
  it('accepts a full valid brand', () => {
    const b = parseBrand({ name: 'DuneMUD', accentStart: 4, tagline: 'hello', title: 'DuneMUD — Play', description: 'd' })
    expect(b).toEqual({ name: 'DuneMUD', accentStart: 4, tagline: 'hello', title: 'DuneMUD — Play', description: 'd' })
  })
  it('renamed without accentStart renders in a single colour and titles itself by name', () => {
    const b = parseBrand({ name: 'MyMUD' })
    expect(b.accentStart).toBe(5)
    expect(b.title).toBe('MyMUD')
    expect(b.tagline).toBe(DEFAULT_BRAND.tagline)
  })
  it('rejects bad accentStart, wrong types, empty and oversized strings', () => {
    expect(parseBrand({ name: 'ABC', accentStart: 9 }).accentStart).toBe(3)
    expect(parseBrand({ name: 'ABC', accentStart: -1 }).accentStart).toBe(3)
    expect(parseBrand({ name: 'ABC', accentStart: 1.5 }).accentStart).toBe(3)
    expect(parseBrand({ name: 42, tagline: '   ' })).toEqual(DEFAULT_BRAND)
    expect(parseBrand({ name: 'x'.repeat(25) }).name).toBe(DEFAULT_BRAND.name)
  })
  it('keeps markup as inert text (it is only ever rendered via React text nodes)', () => {
    expect(parseBrand({ name: '<img src=x>' }).name).toBe('<img src=x>')
  })
})

describe('loadBrand', () => {
  const res = (body: unknown, ok = true) => ({ ok, json: async () => body }) as unknown as Response
  it('uses the served brand', async () => {
    const f = vi.fn().mockResolvedValue(res({ name: 'DuneMUD', accentStart: 4 }))
    expect((await loadBrand(f as unknown as typeof fetch)).name).toBe('DuneMUD')
    expect(f).toHaveBeenCalledWith('/brand.json', expect.objectContaining({ cache: 'no-cache' }))
  })
  it('falls back on 404, network error, and invalid JSON', async () => {
    expect(await loadBrand(vi.fn().mockResolvedValue(res({}, false)) as unknown as typeof fetch)).toEqual(DEFAULT_BRAND)
    expect(await loadBrand(vi.fn().mockRejectedValue(new Error('net')) as unknown as typeof fetch)).toEqual(DEFAULT_BRAND)
    const bad = { ok: true, json: async () => { throw new SyntaxError('html') } } as unknown as Response
    expect(await loadBrand(vi.fn().mockResolvedValue(bad) as unknown as typeof fetch)).toEqual(DEFAULT_BRAND)
  })
  it('gives up after the timeout', async () => {
    const never = vi.fn((_u: string, init: RequestInit) => new Promise((_r, rej) => init.signal!.addEventListener('abort', () => rej(new Error('abort')))))
    expect(await loadBrand(never as unknown as typeof fetch, 20)).toEqual(DEFAULT_BRAND)
  })
})

describe('applyBrand', () => {
  it('sets the title and description', () => {
    document.head.innerHTML = '<meta name="description" content="old">'
    applyBrand({ ...DEFAULT_BRAND, title: 'T', description: 'D' })
    expect(document.title).toBe('T')
    expect(document.querySelector('meta[name="description"]')!.getAttribute('content')).toBe('D')
  })
})
