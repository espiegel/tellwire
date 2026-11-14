import { describe, expect, it } from 'vitest'
import { CommandHistory } from './history'
import { PromptWatcher, isMasked, looksLikePasswordPrompt, stripAnsi } from './inputPolicy'

describe('CommandHistory', () => {
  it('navigates up and down and restores the draft', () => {
    const h = new CommandHistory()
    h.push('look')
    h.push('north')
    h.push('say hi')
    expect(h.prev('half typed')).toBe('say hi')
    expect(h.prev('')).toBe('north')
    expect(h.prev('')).toBe('look')
    expect(h.prev('')).toBe('look') // stops at the oldest
    expect(h.next('')).toBe('north')
    expect(h.next('')).toBe('say hi')
    expect(h.next('')).toBe('half typed') // draft restored
    expect(h.next('x')).toBe('x') // not browsing: unchanged
  })

  it('skips blanks and consecutive duplicates', () => {
    const h = new CommandHistory()
    h.push('look')
    h.push('look')
    h.push('   ')
    h.push('')
    expect(h.snapshot()).toEqual(['look'])
    h.push('north')
    h.push('look')
    expect(h.snapshot()).toEqual(['look', 'north', 'look'])
  })

  it('is bounded', () => {
    const h = new CommandHistory(3)
    for (const c of ['a', 'b', 'c', 'd', 'e']) h.push(c)
    expect(h.snapshot()).toEqual(['c', 'd', 'e'])
  })

  it('pushing resets browsing state', () => {
    const h = new CommandHistory()
    h.push('a')
    h.push('b')
    h.prev('')
    h.push('c')
    expect(h.prev('')).toBe('c')
  })

  it('empty history leaves the input untouched', () => {
    const h = new CommandHistory()
    expect(h.prev('abc')).toBe('abc')
    expect(h.next('abc')).toBe('abc')
  })
})

describe('password prompt heuristics', () => {
  it.each([
    ['Password: ', true],
    ['\x1b[1;33mPassword:\x1b[0m ', true],
    ['Enter your password: ', true],
    ['Old password> ', true],
    ['Passphrase:', true],
    ['By what name are you known? ', false],
    ['You say: my password is a secret\r\nHP: [1/1] > ', false],
    ['password reset instructions are on the wiki.', false],
  ])('%j -> masked=%s', (text, want) => {
    expect(looksLikePasswordPrompt(text)).toBe(want)
  })

  it('PromptWatcher masks while a password prompt is pending and releases on answer', () => {
    const w = new PromptWatcher()
    const enc = (s: string) => new TextEncoder().encode(s)
    w.push(enc('Welcome back.\r\nPass'))
    expect(w.heuristicMasked).toBe(false)
    w.push(enc('word: ')) // split across chunks
    expect(w.heuristicMasked).toBe(true)
    w.answered()
    expect(w.heuristicMasked).toBe(false)
  })

  it('does not stay masked once the line has ended', () => {
    const w = new PromptWatcher()
    w.push(new TextEncoder().encode('Password:\r\n'))
    expect(w.heuristicMasked).toBe(false)
  })

  it('handles UTF-8 split across chunks without throwing', () => {
    const w = new PromptWatcher()
    const bytes = new TextEncoder().encode('🪱 Password: ')
    w.push(bytes.slice(0, 2))
    w.push(bytes.slice(2))
    expect(w.heuristicMasked).toBe(true)
  })

  it('isMasked combines the server echo signal and the heuristic', () => {
    const w = new PromptWatcher()
    expect(isMasked(true, w)).toBe(false)
    expect(isMasked(false, w)).toBe(true) // server says it echoes
    w.push(new TextEncoder().encode('Password: '))
    expect(isMasked(true, w)).toBe(true) // fallback
  })

  it('stripAnsi removes CSI and OSC sequences', () => {
    expect(stripAnsi('\x1b[1;31mred\x1b[0m\x1b]0;title\x07!')).toBe('red!')
  })
})
