// Wire protocol shared with the Go gateway. Normative description: docs/protocol.md.
// Keep in sync with gateway/internal/protocol/protocol.go.

export const PROTOCOL_VERSION = 1
export const SUBPROTOCOL = 'tellwire.v1'

export type ConnectionState = 'idle' | 'connecting' | 'connected' | 'disconnected'

// ---- browser -> gateway ----
export type ClientMessage =
  | { v: 1; type: 'session.connect'; profile?: string; cols: number; rows: number }
  | { v: 1; type: 'session.disconnect' }
  | { v: 1; type: 'terminal.input'; data: string }
  | { v: 1; type: 'terminal.resize'; cols: number; rows: number }

// ---- gateway -> browser (JSON frames; terminal output is a binary frame) ----
export interface ConnectionStatusMsg {
  v: 1
  type: 'connection.status'
  state: 'connecting' | 'connected' | 'disconnected'
  profile?: string
  detail?: string
}
export interface ConnectionErrorMsg {
  v: 1
  type: 'connection.error'
  code: string
  message: string
}
export interface SessionEchoMsg {
  v: 1
  type: 'session.echo'
  /** false = the server is echoing (password prompt): do not echo, mask input. */
  enabled: boolean
}
export interface GmcpMsg {
  v: 1
  type: 'gmcp.message'
  package: string
  data?: unknown
  malformed?: boolean
  error?: string
  raw?: string
}
export type ServerMessage = ConnectionStatusMsg | ConnectionErrorMsg | SessionEchoMsg | GmcpMsg

/** Parses a JSON control frame. Returns null for anything unrecognised. */
export function parseServerMessage(text: string): ServerMessage | null {
  let m: unknown
  try {
    m = JSON.parse(text)
  } catch {
    return null
  }
  if (typeof m !== 'object' || m === null) return null
  const o = m as Record<string, unknown>
  if (o.v !== PROTOCOL_VERSION || typeof o.type !== 'string') return null
  switch (o.type) {
    case 'connection.status':
      return typeof o.state === 'string' ? (o as unknown as ConnectionStatusMsg) : null
    case 'connection.error':
      return typeof o.code === 'string' && typeof o.message === 'string' ? (o as unknown as ConnectionErrorMsg) : null
    case 'session.echo':
      return typeof o.enabled === 'boolean' ? (o as unknown as SessionEchoMsg) : null
    case 'gmcp.message':
      return typeof o.package === 'string' ? (o as unknown as GmcpMsg) : null
    default:
      return null // unknown types are ignored for forward compatibility
  }
}
