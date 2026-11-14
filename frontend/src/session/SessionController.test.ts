import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SessionController } from './SessionController'
import { FakeWebSocket } from '../test/FakeWebSocket'

vi.stubGlobal('WebSocket', FakeWebSocket)

function setup() {
  const c = new SessionController({ url: 'wss://x/ws', socketFactory: (u, p) => new FakeWebSocket(u, p) as unknown as WebSocket })
  const out: string[] = []
  c.attachTerminal({ write: (d) => out.push(typeof d === 'string' ? d : new TextDecoder().decode(d)) })
  c.connect(80, 24)
  const ws = FakeWebSocket.last
  ws.open()
  ws.json({ v: 1, type: 'connection.status', state: 'connected' })
  return { c, ws, out, text: () => out.join('') }
}

const sentInputs = (ws: FakeWebSocket) => ws.messages.filter((m) => m.type === 'terminal.input').map((m) => m.data)

beforeEach(() => FakeWebSocket.reset())

describe('SessionController', () => {
  it('sends lines with a trailing newline, echoes locally and records history', () => {
    const { c, ws, text } = setup()
    expect(c.submit('look')).toBe(true)
    expect(sentInputs(ws)).toEqual(['look\n'])
    expect(text()).toContain('look')
    expect(c.history.snapshot()).toEqual(['look'])
  })

  it('does not echo locally when disabled, but still keeps history', () => {
    const { c, text } = setup()
    c.localEcho = false
    c.submit('north')
    expect(text()).not.toContain('north')
    expect(c.history.snapshot()).toEqual(['north'])
  })

  it('refuses to send when not connected', () => {
    const c = new SessionController({ url: 'x', socketFactory: (u, p) => new FakeWebSocket(u, p) as unknown as WebSocket })
    expect(c.submit('look')).toBe(false)
    expect(c.history.size).toBe(0)
  })

  it('server ECHO negotiation masks: nothing echoed, nothing in history, flag cleared afterwards', () => {
    const { c, ws, text } = setup()
    ws.binary('Password: ')
    ws.json({ v: 1, type: 'session.echo', enabled: false })
    expect(c.masked).toBe(true)
    expect(c.getSnapshot().masked).toBe(true)
    c.submit('hunter2')
    expect(sentInputs(ws)).toEqual(['hunter2\n']) // still sent to the MUD
    expect(text()).not.toContain('hunter2')
    expect(c.history.snapshot()).toEqual([])
    expect(JSON.stringify(c.history.snapshot())).not.toContain('hunter2')
    ws.json({ v: 1, type: 'session.echo', enabled: true })
    expect(c.masked).toBe(false)
    expect(c.getSnapshot().masked).toBe(false)
    c.submit('look')
    expect(c.history.snapshot()).toEqual(['look'])
  })

  it('falls back to the prompt heuristic when the server never negotiates ECHO', () => {
    const { c, ws, text } = setup()
    ws.binary('Enter password: ')
    expect(c.masked).toBe(true)
    c.submit('topsecret')
    expect(text()).not.toContain('topsecret')
    expect(c.history.size).toBe(0)
    expect(c.masked).toBe(false) // answered
    ws.binary('\r\nWelcome!\r\nHP: [1/1] > ')
    c.submit('look')
    expect(c.history.snapshot()).toEqual(['look'])
  })

  it('forwards terminal bytes untouched and in order', () => {
    const { ws, out } = setup()
    ws.binary('\x1b[1;31mred\x1b[0m')
    ws.binary([0xf0, 0x9f]) // split UTF-8 stays split: xterm reassembles
    ws.binary([0xaa, 0xb1])
    expect(out.slice(-3)[0]).toBe('\x1b[1;31mred\x1b[0m')
  })

  it('applies GMCP deltas to its store and resets it for a new session', () => {
    const { c, ws } = setup()
    ws.json({ v: 1, type: 'gmcp.message', package: 'Char.Vitals', data: { hp: 10, maxhp: 20 } })
    ws.json({ v: 1, type: 'gmcp.message', package: 'Char.Vitals', data: { hp: 9 } })
    expect(c.gmcp.get('Char.Vitals')).toEqual({ hp: 9, maxhp: 20 })
    c.connect()
    expect(c.gmcp.get('Char.Vitals')).toBeUndefined()
  })

  it('exposes state, errors and disconnect detail; writes notices to the terminal', () => {
    const { c, ws, text } = setup()
    ws.json({ v: 1, type: 'connection.error', code: 'upstream_closed', message: 'gone' })
    ws.json({ v: 1, type: 'connection.status', state: 'disconnected', detail: 'The game server closed the connection.' })
    const s = c.getSnapshot()
    expect(s.state).toBe('disconnected')
    expect(s.error?.code).toBe('upstream_closed')
    expect(text()).toContain('disconnected')
  })

  it('a reconnect clears the previous error and re-opens input', () => {
    const { c, ws } = setup()
    ws.json({ v: 1, type: 'connection.error', code: 'x', message: 'y' })
    ws.json({ v: 1, type: 'session.echo', enabled: false })
    c.connect()
    expect(c.getSnapshot().error).toBeNull()
    expect(c.getSnapshot().masked).toBe(false)
  })

  it('notifies subscribers of changes', () => {
    const c = new SessionController({ url: 'x', socketFactory: (u, p) => new FakeWebSocket(u, p) as unknown as WebSocket })
    const fn = vi.fn()
    c.subscribe(fn)
    c.connect()
    expect(fn).toHaveBeenCalled()
  })
})
