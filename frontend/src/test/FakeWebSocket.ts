// Minimal controllable WebSocket for unit tests.
export class FakeWebSocket {
  static OPEN = 1
  static instances: FakeWebSocket[] = []
  readyState = 0
  binaryType = 'blob'
  sent: string[] = []
  closedWith: number | undefined
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: unknown }) => void) | null = null
  onerror: (() => void) | null = null
  onclose: ((ev: { code: number }) => void) | null = null
  constructor(
    public url: string,
    public protocols: string[],
  ) {
    FakeWebSocket.instances.push(this)
  }
  send(d: string) {
    this.sent.push(d)
  }
  close(code?: number) {
    this.closedWith = code
    this.readyState = 3
  }
  // ---- test helpers ----
  open() {
    this.readyState = 1
    this.onopen?.()
  }
  json(o: unknown) {
    this.onmessage?.({ data: JSON.stringify(o) })
  }
  binary(bytes: number[] | string) {
    const u = typeof bytes === 'string' ? new TextEncoder().encode(bytes) : Uint8Array.from(bytes)
    this.onmessage?.({ data: u.buffer.slice(u.byteOffset, u.byteOffset + u.byteLength) })
  }
  serverClose(code = 1000) {
    this.readyState = 3
    this.onclose?.({ code })
  }
  get messages() {
    return this.sent.map((s) => JSON.parse(s) as Record<string, unknown>)
  }
  static reset() {
    FakeWebSocket.instances = []
  }
  static get last() {
    return FakeWebSocket.instances[FakeWebSocket.instances.length - 1]!
  }
}
