import { forwardRef, useEffect, useImperativeHandle, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'
import { terminalFont, terminalTheme } from './theme'

export interface TerminalHandle {
  write(data: Uint8Array | string): void
  clear(): void
  scrollPages(n: number): void
  getSelection(): string
  size(): { cols: number; rows: number }
}

interface Props {
  fontSize: number
  scrollback: number
  /** Called (debounced) whenever the fitted size changes. */
  onResize: (cols: number, rows: number) => void
  /** Called on a click that did not select text, so the host can focus its input. */
  onActivate?: () => void
}

/**
 * Display-only xterm.js wrapper. It renders ANSI/256-colour output and owns
 * scrollback, selection and fitting. All input goes through the dedicated
 * command line, never through the terminal.
 */
export const TerminalView = forwardRef<TerminalHandle, Props>(function TerminalView({ fontSize, scrollback, onResize, onActivate }, ref) {
  const host = useRef<HTMLDivElement>(null)
  const term = useRef<Terminal | null>(null)
  const fit = useRef<FitAddon | null>(null)
  const resizeCb = useRef(onResize)
  resizeCb.current = onResize

  useImperativeHandle(ref, () => ({
    write: (d) => term.current?.write(d),
    clear: () => term.current?.clear(),
    scrollPages: (n) => term.current?.scrollPages(n),
    getSelection: () => term.current?.getSelection() ?? '',
    size: () => ({ cols: term.current?.cols ?? 80, rows: term.current?.rows ?? 24 }),
  }))

  useEffect(() => {
    const el = host.current
    if (!el) return
    const t = new Terminal({
      fontFamily: terminalFont,
      fontSize,
      lineHeight: 1.18,
      scrollback,
      theme: terminalTheme,
      cursorBlink: false,
      cursorInactiveStyle: 'none',
      disableStdin: true,
      convertEol: false, // the gateway already normalises to CR LF
      allowProposedApi: false,
      macOptionIsMeta: true,
    })
    const f = new FitAddon()
    t.loadAddon(f)
    t.loadAddon(new WebLinksAddon())
    t.open(el)
    // Keep the (input-disabled) hidden textarea out of the tab order: the command line is the focus target.
    if (t.textarea) t.textarea.tabIndex = -1
    t.write('\x1b[?25l') // hide cursor: this is an output-only surface
    term.current = t
    fit.current = f

    let raf = 0
    let timer: ReturnType<typeof setTimeout> | undefined
    let last = ''
    const doFit = () => {
      try {
        f.fit()
      } catch {
        return // container not laid out yet
      }
      const key = `${t.cols}x${t.rows}`
      if (key !== last) {
        last = key
        clearTimeout(timer)
        timer = setTimeout(() => resizeCb.current(t.cols, t.rows), 120)
      }
    }
    const ro = new ResizeObserver(() => {
      cancelAnimationFrame(raf)
      raf = requestAnimationFrame(doFit)
    })
    ro.observe(el)
    doFit()

    return () => {
      cancelAnimationFrame(raf)
      clearTimeout(timer)
      ro.disconnect()
      t.dispose()
      term.current = null
      fit.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- terminal is created once; options are updated below
  }, [])

  useEffect(() => {
    const t = term.current
    if (!t) return
    t.options.fontSize = fontSize
    t.options.scrollback = scrollback
    try {
      fit.current?.fit()
    } catch {
      /* not laid out */
    }
  }, [fontSize, scrollback])

  return (
    <div
      ref={host}
      data-testid="terminal"
      role="log"
      aria-label="Game output"
      className="h-full w-full overflow-hidden bg-ink-950"
      onMouseUp={() => {
        if (!term.current?.hasSelection()) onActivate?.()
      }}
    />
  )
})
