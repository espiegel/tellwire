import { describe, expect, it, vi } from 'vitest'
import { GmcpStore } from './GmcpStore'
import type { GmcpMsg } from '../protocol/messages'

const msg = (pkg: string, data?: unknown, extra: Partial<GmcpMsg> = {}): GmcpMsg => ({ v: 1, type: 'gmcp.message', package: pkg, data, ...extra })

describe('GmcpStore', () => {
  it('merges documented delta packages key by key', () => {
    const s = new GmcpStore()
    s.apply(msg('Char.Vitals', { hp: 389, maxhp: 412, sp: 199, maxsp: 188 }))
    s.apply(msg('Char.Vitals', { hp: 380 }))
    expect(s.get('Char.Vitals')).toEqual({ hp: 380, maxhp: 412, sp: 199, maxsp: 188 })
  })

  it('replaces full packages such as Room.Info instead of merging', () => {
    const s = new GmcpStore()
    s.apply(msg('Room.Info', { name: 'A', exits: { north: '1', south: '2' } }))
    s.apply(msg('Room.Info', { name: 'B', exits: { east: '3' } }))
    expect(s.get('Room.Info')).toEqual({ name: 'B', exits: { east: '3' } })
  })

  it('stores unknown packages untouched', () => {
    const s = new GmcpStore()
    s.apply(msg('Some.Future.Package', { a: [1, 2, { b: null }] }))
    expect(s.get('Some.Future.Package')).toEqual({ a: [1, 2, { b: null }] })
    expect(s.packages()).toEqual(['Some.Future.Package'])
  })

  it('malformed messages are logged but never change state', () => {
    const s = new GmcpStore()
    s.apply(msg('Char.Vitals', { hp: 1 }))
    s.apply(msg('Char.Vitals', undefined, { malformed: true, error: 'invalid JSON payload', raw: '{"hp":' }))
    expect(s.get('Char.Vitals')).toEqual({ hp: 1 })
    const last = s.logSnapshot().at(-1)!
    expect(last).toMatchObject({ package: 'Char.Vitals', malformed: true, raw: '{"hp":' })
  })

  it('select() reads dotted paths and returns undefined for anything missing', () => {
    const s = new GmcpStore()
    s.apply(msg('Room.Info', { name: 'Market', exits: { north: 'abc' } }))
    s.apply(msg('Char.Vitals', { hp: 5 }))
    expect(s.select('Room.Info.exits.north')).toBe('abc')
    expect(s.select('Room.Info.exits.south')).toBeUndefined()
    expect(s.select('Room.Info.nope.deeper')).toBeUndefined()
    expect(s.select('Char.Vitals.hp')).toBe(5)
    expect(s.select('Char.Stats.str')).toBeUndefined() // never fabricated
    expect(s.select('Room')).toBeUndefined()
  })

  it('select() is safe against prototype keys', () => {
    const s = new GmcpStore()
    s.apply(msg('Room.Info', { name: 'x' }))
    expect(s.select('Room.Info.constructor')).toBeUndefined()
    expect(s.select('Room.Info.__proto__')).toBeUndefined()
  })

  it('a delta arriving before any base value just becomes the value', () => {
    const s = new GmcpStore()
    s.apply(msg('Char.Vitals', { hp: 7 }))
    expect(s.get('Char.Vitals')).toEqual({ hp: 7 })
  })

  it('caps the inspector log', () => {
    const s = new GmcpStore(undefined, 3)
    for (let i = 0; i < 10; i++) s.apply(msg('A.B', { i }))
    expect(s.logSnapshot().map((e) => (e.data as { i: number }).i)).toEqual([7, 8, 9])
  })

  it('notifies subscribers and gives referentially stable snapshots between changes', () => {
    const s = new GmcpStore()
    const fn = vi.fn()
    const off = s.subscribe(fn)
    s.apply(msg('A.B', 1))
    expect(fn).toHaveBeenCalledTimes(1)
    const snap = s.snapshot()
    expect(s.snapshot()).toBe(snap)
    s.apply(msg('A.B', 2))
    expect(s.snapshot()).not.toBe(snap)
    off()
    s.apply(msg('A.B', 3))
    expect(fn).toHaveBeenCalledTimes(2)
  })

  it('reset() clears state and log', () => {
    const s = new GmcpStore()
    s.apply(msg('A.B', 1))
    s.reset()
    expect(s.packages()).toEqual([])
    expect(s.logSnapshot()).toEqual([])
  })
})
