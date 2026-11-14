/**
 * Command history with shell-style Up/Down navigation.
 * Sensitive input (passwords) must never be passed to push(); the caller
 * decides via the input-masking policy. Nothing here is persisted.
 */
export class CommandHistory {
  private items: string[] = []
  private cursor: number | null = null // index into items while browsing, null = editing a fresh line
  private draft = ''

  constructor(private max = 500) {}

  /** Records a submitted command. Blank lines and immediate repeats are skipped. */
  push(cmd: string): void {
    this.cursor = null
    this.draft = ''
    if (cmd.trim() === '' || this.items[this.items.length - 1] === cmd) return
    this.items.push(cmd)
    if (this.items.length > this.max) this.items.shift()
  }

  /** Moves to the previous (older) entry, remembering the unsent draft. */
  prev(current: string): string {
    if (this.items.length === 0) return current
    if (this.cursor === null) {
      this.draft = current
      this.cursor = this.items.length - 1
    } else if (this.cursor > 0) {
      this.cursor--
    }
    return this.items[this.cursor] ?? current
  }

  /** Moves to the next (newer) entry, finally restoring the draft. */
  next(current: string): string {
    if (this.cursor === null) return current
    if (this.cursor < this.items.length - 1) {
      this.cursor++
      return this.items[this.cursor] ?? current
    }
    this.cursor = null
    return this.draft
  }

  /** Abandons navigation without recording anything (e.g. the user edited the line). */
  reset(): void {
    this.cursor = null
    this.draft = ''
  }

  get size(): number {
    return this.items.length
  }

  snapshot(): readonly string[] {
    return this.items
  }
}
