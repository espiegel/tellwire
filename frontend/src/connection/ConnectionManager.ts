import {
  type ClientMessage,
  type ConnectionState,
  type GmcpMsg,
  PROTOCOL_VERSION,
  SUBPROTOCOL,
  parseServerMessage,
} from '../protocol/messages'

export interface ConnectionErrorInfo {
  code: string
  message: string
}

export interface ConnectionEvents {
  state: (state: ConnectionState, detail?: string) => void
  output: (data: Uint8Array) => void
  gmcp: (msg: GmcpMsg) => void
  /** enabled=false means the server echoes (password prompt). */
  echo: (enabled: boolean) => void
  error: (err: ConnectionErrorInfo) => void
}

type Listeners = { [K in keyof ConnectionEvents]: Set<ConnectionEvents[K]> }

export type WebSocketFactory = (url: string, protocols: string[]) => WebSocket

export interface ConnectionOptions {
  url: string
  profile?: string
  /** Injected for tests. */
  socketFactory?: WebSocketFactory
}

/**
 * Owns exactly one WebSocket at a time and speaks the tellwire.v1 protocol.
 * It knows nothing about terminals, React or game commands. A new connect()
 * always opens a fresh socket (and therefore a fresh upstream MUD session).
 */
export class ConnectionManager {
  private ws: WebSocket | null = null
  private listeners: Listeners = { state: new Set(), output: new Set(), gmcp: new Set(), echo: new Set(), error: new Set() }
  private _state: ConnectionState = 'idle'
  private lastError: ConnectionErrorInfo | null = null
  private size = { cols: 80, rows: 24 }
  private gen = 0 // identifies the current socket; stale callbacks are ignored

  constructor(private opts: ConnectionOptions) {}

  get state(): ConnectionState {
    return this._state
  }

  on<K extends keyof ConnectionEvents>(ev: K, fn: ConnectionEvents[K]): () => void {
    this.listeners[ev].add(fn)
    return () => this.listeners[ev].delete(fn)
  }

  private emit<K extends keyof ConnectionEvents>(ev: K, ...args: Parameters<ConnectionEvents[K]>) {
    for (const fn of this.listeners[ev]) (fn as (...a: unknown[]) => void)(...args)
  }

  private setState(s: ConnectionState, detail?: string) {
    if (this._state === s && detail === undefined) return
    this._state = s
    this.emit('state', s, detail)
  }

  connect(cols = this.size.cols, rows = this.size.rows): void {
    if (this.ws) this.teardown()
    this.size = { cols, rows }
    this.lastError = null
    const gen = ++this.gen
    this.setState('connecting')
    // The gateway always starts with echo on; reset in case a previous session ended mid-password.
    this.emit('echo', true)

    let ws: WebSocket
    try {
      ws = (this.opts.socketFactory ?? ((u, p) => new WebSocket(u, p)))(this.opts.url, [SUBPROTOCOL])
    } catch {
      this.fail('socket_error', 'Could not open a connection to the gateway.')
      return
    }
    ws.binaryType = 'arraybuffer'
    this.ws = ws

    ws.onopen = () => {
      if (gen !== this.gen) return
      this.send({ v: PROTOCOL_VERSION, type: 'session.connect', profile: this.opts.profile, cols, rows })
    }
    ws.onmessage = (ev: MessageEvent) => {
      if (gen !== this.gen) return
      if (typeof ev.data === 'string') this.onControl(ev.data)
      else if (typeof ev.data === 'object' && ev.data !== null && 'byteLength' in ev.data) {
        // binaryType is 'arraybuffer'; a structural check avoids cross-realm instanceof pitfalls.
        this.emit('output', new Uint8Array(ev.data as ArrayBuffer))
      }
    }
    ws.onerror = () => {
      // The browser exposes no detail; the close event follows and carries the outcome.
    }
    ws.onclose = (ev: CloseEvent) => {
      if (gen !== this.gen) return
      this.ws = null
      if (this._state !== 'disconnected') {
        if (!this.lastError && ev.code !== 1000) {
          const msg =
            ev.code === 1006
              ? 'Could not reach the gateway, or the connection was lost.'
              : `The connection closed unexpectedly (code ${ev.code}).`
          this.lastError = { code: 'socket_closed', message: msg }
          this.emit('error', this.lastError)
        }
        this.setState('disconnected', this.lastError?.message)
      }
    }
  }

  private onControl(text: string) {
    const m = parseServerMessage(text)
    if (!m) return
    switch (m.type) {
      case 'connection.status':
        // 'disconnected' with a following socket close is handled by onclose.
        if (m.state === 'disconnected') this.setState('disconnected', m.detail)
        else this.setState(m.state, m.detail)
        break
      case 'connection.error':
        this.lastError = { code: m.code, message: m.message }
        this.emit('error', this.lastError)
        break
      case 'session.echo':
        this.emit('echo', m.enabled)
        break
      case 'gmcp.message':
        this.emit('gmcp', m)
        break
    }
  }

  private fail(code: string, message: string) {
    this.lastError = { code, message }
    this.emit('error', this.lastError)
    this.setState('disconnected', message)
  }

  private send(m: ClientMessage): boolean {
    if (this.ws?.readyState !== WebSocket.OPEN) return false
    this.ws.send(JSON.stringify(m))
    return true
  }

  /** Sends one line (or pasted block) of user input. Returns false if not connected. */
  sendInput(data: string): boolean {
    if (this._state !== 'connected') return false
    return this.send({ v: PROTOCOL_VERSION, type: 'terminal.input', data })
  }

  resize(cols: number, rows: number): void {
    if (cols < 1 || rows < 1) return
    this.size = { cols, rows }
    if (this._state === 'connected') this.send({ v: PROTOCOL_VERSION, type: 'terminal.resize', cols, rows })
  }

  /** Asks the gateway to end the session, then closes the socket. */
  disconnect(): void {
    if (!this.ws) return
    this.send({ v: PROTOCOL_VERSION, type: 'session.disconnect' })
    this.teardown()
    this.setState('disconnected', 'Disconnected.')
  }

  private teardown() {
    this.gen++ // invalidate callbacks of the old socket
    const ws = this.ws
    this.ws = null
    if (ws) {
      ws.onopen = ws.onmessage = ws.onerror = ws.onclose = null
      try {
        ws.close(1000)
      } catch {
        /* already closed */
      }
    }
  }
}
