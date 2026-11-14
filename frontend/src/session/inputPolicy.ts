// Decides whether the command line is "masked" (password entry).
//
// Primary signal: the gateway's session.echo{enabled:false}, derived from the
// server's Telnet WILL ECHO. Fallback for servers that do not negotiate ECHO:
// the visible prompt ends with something that looks like a password prompt.
// Either signal masks. Masked input is never echoed to the terminal and never
// stored in command history.

// eslint-disable-next-line no-control-regex
const ANSI = /\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]/g
const PASSWORD_PROMPT = /(pass(?:word|phrase|code)|pin|secret)\s*(?:\(.*\))?\s*[:?>]?\s*$/i

/** Strips ANSI escape sequences. */
export function stripAnsi(s: string): string {
  return s.replace(ANSI, '')
}

/** True if the text ending a prompt line looks like a request for a password. */
export function looksLikePasswordPrompt(tail: string): boolean {
  const lastLine = stripAnsi(tail).split(/\r?\n/).pop() ?? ''
  return lastLine.length < 120 && PASSWORD_PROMPT.test(lastLine)
}

/**
 * Tracks the tail of terminal output to run the password-prompt heuristic.
 * Feed it every output chunk; query `heuristicMasked`.
 */
export class PromptWatcher {
  private decoder = new TextDecoder('utf-8')
  private tail = ''
  private _masked = false

  push(chunk: Uint8Array): void {
    this.tail = (this.tail + this.decoder.decode(chunk, { stream: true })).slice(-240)
    const trimmed = this.tail
    // A prompt is the text after the last newline; if output ends with a newline
    // the prompt is over and the heuristic no longer applies.
    this._masked = !/[\r\n]$/.test(trimmed) && looksLikePasswordPrompt(trimmed)
  }

  /** Call when the user submits a line: the prompt has been answered. */
  answered(): void {
    this._masked = false
    this.tail = ''
  }

  get heuristicMasked(): boolean {
    return this._masked
  }
}

/** Combines both signals. */
export function isMasked(serverEcho: boolean, watcher: PromptWatcher): boolean {
  return !serverEcho || watcher.heuristicMasked
}
