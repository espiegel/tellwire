import { forwardRef, useImperativeHandle, useRef, useState, type ClipboardEvent, type KeyboardEvent } from 'react'
import type { CommandHistory } from '../session/history'
import { SendIcon } from './icons'

export interface CommandInputHandle {
  focus(): void
  insert(text: string): void
}

interface Props {
  history: CommandHistory
  masked: boolean
  disabled: boolean
  /** Returns false if the line could not be sent (e.g. disconnected). */
  onSubmit: (line: string) => boolean
  onScrollPages: (n: number) => void
}

const MAX_PASTE_LINES = 50

/**
 * The dedicated command line. Arbitrary text can always be typed: nothing here
 * knows about game commands. When `masked`, it renders as a password field,
 * and history navigation is disabled so secrets can never be recalled.
 */
export const CommandInput = forwardRef<CommandInputHandle, Props>(function CommandInput({ history, masked, disabled, onSubmit, onScrollPages }, ref) {
  const [value, setValue] = useState('')
  const input = useRef<HTMLInputElement>(null)

  useImperativeHandle(ref, () => ({
    focus: () => input.current?.focus({ preventScroll: true }),
    insert: (text) => setValue((v) => v + text),
  }))

  const submit = (line: string) => {
    if (onSubmit(line)) {
      setValue('')
      history.reset()
    }
  }

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.nativeEvent.isComposing) return
    switch (e.key) {
      case 'Enter':
        e.preventDefault()
        submit(value)
        break
      case 'ArrowUp':
        if (masked) break
        e.preventDefault()
        setValue((v) => history.prev(v))
        break
      case 'ArrowDown':
        if (masked) break
        e.preventDefault()
        setValue((v) => history.next(v))
        break
      case 'PageUp':
        e.preventDefault()
        onScrollPages(-1)
        break
      case 'PageDown':
        e.preventDefault()
        onScrollPages(1)
        break
      case 'Escape':
        setValue('')
        history.reset()
        break
    }
  }

  const onPaste = (e: ClipboardEvent<HTMLInputElement>) => {
    const text = e.clipboardData.getData('text')
    if (!/[\r\n]/.test(text)) return // single line: let the browser paste normally
    e.preventDefault()
    if (masked) {
      setValue((v) => v + text.split(/\r?\n/)[0])
      return
    }
    const lines = text.split(/\r?\n/)
    if (lines[lines.length - 1] === '') lines.pop()
    // Multi-line paste: send each complete line as its own command (bounded).
    for (const l of lines.slice(0, MAX_PASTE_LINES)) {
      if (!onSubmit(l)) break
    }
  }

  return (
    <form
      className="flex flex-none items-center gap-2 border-t border-ink-700 bg-ink-900 px-3 py-2 pb-[max(0.5rem,env(safe-area-inset-bottom))] sm:px-4"
      onSubmit={(e) => {
        e.preventDefault()
        submit(value)
      }}
    >
      <span className="select-none font-mono text-sand-500" aria-hidden>
        {masked ? '🔒' : '›'}
      </span>
      <input
        ref={input}
        data-testid="command-input"
        data-masked={masked}
        type={masked ? 'password' : 'text'}
        value={value}
        disabled={disabled}
        onChange={(e) => {
          setValue(e.target.value)
          history.reset()
        }}
        onKeyDown={onKeyDown}
        onPaste={onPaste}
        placeholder={disabled ? 'Not connected' : masked ? 'Password (hidden)' : 'Type a command…'}
        aria-label={masked ? 'Password (hidden)' : 'Command'}
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        spellCheck={false}
        enterKeyHint="send"
        autoFocus
        className="min-w-0 flex-1 rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-[16px] text-sand-100 placeholder:text-muted focus:border-sand-500 focus:outline-none disabled:opacity-50"
      />
      <button
        type="submit"
        disabled={disabled}
        aria-label="Send"
        className="inline-flex h-10 w-10 flex-none items-center justify-center rounded-md border border-sand-500 bg-ink-800 text-sand-400 transition-colors hover:bg-ink-700 disabled:opacity-40"
      >
        <SendIcon />
      </button>
    </form>
  )
})
