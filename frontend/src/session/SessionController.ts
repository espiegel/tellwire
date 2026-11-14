import { ConnectionManager, type ConnectionErrorInfo, type ConnectionOptions } from '../connection/ConnectionManager'
import { GmcpStore } from '../gmcp/GmcpStore'
import type { ConnectionState } from '../protocol/messages'
import { CommandHistory } from './history'
import { PromptWatcher, isMasked } from './inputPolicy'

/** Where terminal bytes go. Implemented by the xterm wrapper; faked in tests. */
export interface TerminalSink {
  write(data: Uint8Array | string): void
}

export interface SessionSnapshot {
  state: ConnectionState
  detail?: string
  error: ConnectionErrorInfo | null
  /** True while the command line must be masked (password entry). */
  masked: boolean
}

const SYS = (s: string) => `\x1b[2;33m${s}\x1b[0m\r\n`
const ECHO_STYLE = (s: string) => `\x1b[38;5;180m${s}\x1b[0m\r\n`

/**
 * Framework-free session glue: owns the connection, GMCP state, command
 * history and the password-masking policy. React reads it through
 * subscribe()/getSnapshot() (useSyncExternalStore compatible).
 * It contains no game-specific commands.
 */
export class SessionController {
  readonly conn: ConnectionManager
  readonly gmcp = new GmcpStore()
  readonly history = new CommandHistory()
  private watcher = new PromptWatcher()
  private serverEcho = true
  private sink: TerminalSink | null = null
  private listeners = new Set<() => void>()
  private snap: SessionSnapshot = { state: 'idle', error: null, masked: false }
  localEcho = true

  constructor(opts: ConnectionOptions) {
    this.conn = new ConnectionManager(opts)
    this.conn.on('state', (state, detail) => {
      if (state === 'connecting') {
        this.gmcp.reset()
        this.watcher.answered()
        this.update({ state, detail: undefined, error: null })
        this.sink?.write(SYS('── connecting ──'))
      } else if (state === 'connected') {
        this.update({ state, detail })
        this.sink?.write(SYS('── connected ──'))
      } else {
        this.update({ state, detail })
        this.sink?.write(SYS(`── disconnected${detail ? ': ' + detail : ''} ──`))
      }
    })
    this.conn.on('output', (data) => {
      this.watcher.push(data)
      this.sink?.write(data)
      this.updateMasked()
    })
    this.conn.on('echo', (enabled) => {
      this.serverEcho = enabled
      this.updateMasked()
    })
    this.conn.on('gmcp', (m) => this.gmcp.apply(m))
    this.conn.on('error', (error) => this.update({ error }))
  }

  attachTerminal(sink: TerminalSink | null): void {
    this.sink = sink
  }

  connect(cols?: number, rows?: number): void {
    this.conn.connect(cols, rows)
  }

  disconnect(): void {
    this.conn.disconnect()
  }

  resize(cols: number, rows: number): void {
    this.conn.resize(cols, rows)
  }

  /** Whether the next submitted line would be treated as secret. */
  get masked(): boolean {
    return isMasked(this.serverEcho, this.watcher)
  }

  /**
   * Sends a line to the MUD. Masked lines are never echoed to the terminal
   * and never enter history. Returns false (and sends nothing) if not connected.
   */
  submit(line: string): boolean {
    if (this.conn.state !== 'connected') return false
    const secret = this.masked
    if (!this.conn.sendInput(line + '\n')) return false
    this.watcher.answered()
    if (secret) {
      this.sink?.write('\r\n') // move past the prompt; show nothing
    } else {
      this.history.push(line)
      if (this.localEcho) this.sink?.write(ECHO_STYLE(line))
    }
    this.updateMasked()
    return true
  }

  // ---- external store ----
  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }
  getSnapshot = (): SessionSnapshot => this.snap

  private updateMasked() {
    const masked = this.masked
    if (masked !== this.snap.masked) this.update({ masked })
  }

  private update(p: Partial<SessionSnapshot>) {
    this.snap = { ...this.snap, ...p, masked: p.masked ?? this.masked }
    for (const fn of this.listeners) fn()
  }
}
