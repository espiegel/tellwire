import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ConnectionManager } from './ConnectionManager'
import { FakeWebSocket } from '../test/FakeWebSocket'

vi.stubGlobal('WebSocket', FakeWebSocket)

function setup() {
  const cm = new ConnectionManager({ url: 'wss://x/ws', socketFactory: (u, p) => new FakeWebSocket(u, p) as unknown as WebSocket })
  const states: string[] = []
  const errors: string[] = []
  const output: string[] = []
  const echoes: boolean[] = []
  const gmcp: string[] = []
  cm.on('state', (s) => states.push(s))
  cm.on('error', (e) => errors.push(e.code))
  cm.on('output', (d) => output.push(new TextDecoder().decode(d)))
  cm.on('echo', (e) => echoes.push(e))
  cm.on('gmcp', (m) => gmcp.push(m.package))
  return { cm, states, errors, output, echoes, gmcp }
}

beforeEach(() => FakeWebSocket.reset())

describe('ConnectionManager', () => {
  it('negotiates the subprotocol and sends session.connect on open', () => {
    const { cm, states } = setup()
    cm.connect(100, 30)
    expect(states).toEqual(['connecting'])
    const ws = FakeWebSocket.last
    expect(ws.protocols).toEqual(['tellwire.v1'])
    expect(ws.binaryType).toBe('arraybuffer')
    ws.open()
    expect(ws.messages[0]).toMatchObject({ v: 1, type: 'session.connect', cols: 100, rows: 30 })
  })

  it('becomes connected only when the gateway says so', () => {
    const { cm, states } = setup()
    cm.connect()
    FakeWebSocket.last.open()
    expect(cm.state).toBe('connecting')
    FakeWebSocket.last.json({ v: 1, type: 'connection.status', state: 'connected' })
    expect(cm.state).toBe('connected')
    expect(states).toEqual(['connecting', 'connected'])
  })

  it('delivers binary frames as output bytes, preserving ANSI and UTF-8', () => {
    const { cm, output } = setup()
    cm.connect()
    FakeWebSocket.last.open()
    FakeWebSocket.last.binary('\x1b[31mred\x1b[0m 🪱')
    expect(output).toEqual(['\x1b[31mred\x1b[0m 🪱'])
  })

  it('routes echo, gmcp and ignores unknown/invalid control frames', () => {
    const { cm, echoes, gmcp } = setup()
    cm.connect()
    const ws = FakeWebSocket.last
    ws.open()
    ws.json({ v: 1, type: 'session.echo', enabled: false })
    ws.json({ v: 1, type: 'gmcp.message', package: 'Room.Info', data: { name: 'x' } })
    ws.json({ v: 1, type: 'future.thing', x: 1 })
    ws.json({ v: 2, type: 'session.echo', enabled: true })
    ws.onmessage?.({ data: 'not json' })
    expect(echoes).toEqual([true, false]) // initial reset + the real one
    expect(gmcp).toEqual(['Room.Info'])
  })

  it('only sends input while connected', () => {
    const { cm } = setup()
    expect(cm.sendInput('look\n')).toBe(false)
    cm.connect()
    FakeWebSocket.last.open()
    expect(cm.sendInput('look\n')).toBe(false) // still connecting
    FakeWebSocket.last.json({ v: 1, type: 'connection.status', state: 'connected' })
    expect(cm.sendInput('look\n')).toBe(true)
    expect(FakeWebSocket.last.messages.at(-1)).toEqual({ v: 1, type: 'terminal.input', data: 'look\n' })
  })

  it('sends resize only when connected and remembers the size for reconnects', () => {
    const { cm } = setup()
    cm.resize(90, 20)
    cm.connect()
    FakeWebSocket.last.open()
    expect(FakeWebSocket.last.messages[0]).toMatchObject({ cols: 90, rows: 20 })
    FakeWebSocket.last.json({ v: 1, type: 'connection.status', state: 'connected' })
    cm.resize(120, 40)
    expect(FakeWebSocket.last.messages.at(-1)).toEqual({ v: 1, type: 'terminal.resize', cols: 120, rows: 40 })
    cm.resize(0, 10)
    expect(FakeWebSocket.last.messages).toHaveLength(2)
  })

  it('surfaces gateway errors and ends disconnected with the message', () => {
    const { cm, errors, states } = setup()
    cm.connect()
    const ws = FakeWebSocket.last
    ws.open()
    ws.json({ v: 1, type: 'connection.error', code: 'upstream_tls', message: 'certificate bad' })
    ws.json({ v: 1, type: 'connection.status', state: 'disconnected', detail: 'certificate bad' })
    ws.serverClose(1011)
    expect(errors).toEqual(['upstream_tls'])
    expect(states.at(-1)).toBe('disconnected')
    expect(cm.state).toBe('disconnected')
  })

  it('reports an unreachable gateway (close 1006 without any message)', () => {
    const { cm, errors, states } = setup()
    cm.connect()
    FakeWebSocket.last.serverClose(1006)
    expect(errors).toEqual(['socket_closed'])
    expect(states).toEqual(['connecting', 'disconnected'])
  })

  it('a clean server close after disconnected status raises no error', () => {
    const { cm, errors } = setup()
    cm.connect()
    const ws = FakeWebSocket.last
    ws.open()
    ws.json({ v: 1, type: 'connection.status', state: 'connected' })
    ws.json({ v: 1, type: 'connection.status', state: 'disconnected', detail: 'bye' })
    ws.serverClose(1000)
    expect(errors).toEqual([])
  })

  it('disconnect() asks the gateway to end the session and closes the socket', () => {
    const { cm, states } = setup()
    cm.connect()
    const ws = FakeWebSocket.last
    ws.open()
    ws.json({ v: 1, type: 'connection.status', state: 'connected' })
    cm.disconnect()
    expect(ws.messages.at(-1)).toEqual({ v: 1, type: 'session.disconnect' })
    expect(ws.closedWith).toBe(1000)
    expect(states.at(-1)).toBe('disconnected')
  })

  it('reconnect opens a brand-new socket and ignores stale events from the old one', () => {
    const { cm, output, states } = setup()
    cm.connect()
    const first = FakeWebSocket.last
    first.open()
    first.json({ v: 1, type: 'connection.status', state: 'connected' })
    first.serverClose(1006)
    cm.connect()
    const second = FakeWebSocket.last
    expect(second).not.toBe(first)
    first.binary('stale') // must be ignored
    first.json({ v: 1, type: 'connection.status', state: 'connected' })
    second.open()
    second.json({ v: 1, type: 'connection.status', state: 'connected' })
    second.binary('fresh')
    expect(output).toEqual(['fresh'])
    expect(states).toEqual(['connecting', 'connected', 'disconnected', 'connecting', 'connected'])
  })

  it('connect() while a socket is open replaces it (no leaked session)', () => {
    const { cm } = setup()
    cm.connect()
    const a = FakeWebSocket.last
    a.open()
    cm.connect()
    expect(a.closedWith).toBe(1000)
    expect(FakeWebSocket.instances).toHaveLength(2)
  })

  it('reports a failure to construct the socket', () => {
    const cm = new ConnectionManager({ url: 'x', socketFactory: () => { throw new Error('blocked') } })
    const errors: string[] = []
    cm.on('error', (e) => errors.push(e.code))
    cm.connect()
    expect(errors).toEqual(['socket_error'])
    expect(cm.state).toBe('disconnected')
  })

  it('does not leave echo masked after a reconnect', () => {
    const { cm, echoes } = setup()
    cm.connect()
    FakeWebSocket.last.open()
    FakeWebSocket.last.json({ v: 1, type: 'session.echo', enabled: false })
    cm.connect()
    expect(echoes.at(-1)).toBe(true)
  })
})
