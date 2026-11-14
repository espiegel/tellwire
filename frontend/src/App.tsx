import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import { SessionController } from './session/SessionController'
import { TerminalView, type TerminalHandle } from './terminal/TerminalView'
import { Header } from './ui/Header'
import { DEFAULT_BRAND, type Brand } from './brand/brand'
import { CommandInput, type CommandInputHandle } from './ui/CommandInput'
import { SettingsDialog } from './ui/SettingsDialog'
import { GmcpInspector } from './ui/GmcpInspector'
import { FONT_MAX, FONT_MIN, loadPrefs, savePrefs, type Prefs } from './prefs/prefs'

/** Same-origin WebSocket by default (works behind Caddy and the Vite dev proxy). */
export function defaultGatewayUrl(loc: Pick<Location, 'protocol' | 'host'> = window.location): string {
  const override = import.meta.env.VITE_GATEWAY_URL as string | undefined
  if (override) return override
  return `${loc.protocol === 'https:' ? 'wss:' : 'ws:'}//${loc.host}/ws`
}

export interface AppProps {
  /** Injected in tests. */
  controller?: SessionController
  autoConnect?: boolean
  /** Per-deployment branding (loaded from /brand.json in main.tsx). */
  brand?: Brand
}

export default function App({ controller, autoConnect = true, brand = DEFAULT_BRAND }: AppProps) {
  const session = useMemo(() => controller ?? new SessionController({ url: defaultGatewayUrl() }), [controller])
  const snap = useSyncExternalStore(session.subscribe, session.getSnapshot)
  const [prefs, setPrefs] = useState<Prefs>(() => loadPrefs())
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [errorDismissed, setErrorDismissed] = useState<unknown>(null)
  const term = useRef<TerminalHandle>(null)
  const input = useRef<CommandInputHandle>(null)
  const started = useRef(false)

  useEffect(() => {
    savePrefs(prefs)
    session.localEcho = prefs.localEcho
  }, [prefs, session])

  const reconnect = useCallback(() => {
    const { cols, rows } = term.current?.size() ?? { cols: 80, rows: 24 }
    session.connect(cols, rows)
    input.current?.focus()
  }, [session])

  // Attach the terminal sink and connect once the terminal exists (so the first
  // session.connect carries the real fitted size).
  useEffect(() => {
    session.attachTerminal({ write: (d) => term.current?.write(d) })
    if (autoConnect && !started.current) {
      started.current = true
      reconnect()
    }
    return () => session.attachTerminal(null)
  }, [session, autoConnect, reconnect])

  // Keyboard shortcuts and terminal copy.
  useEffect(() => {
    const setFont = (d: number) => setPrefs((p) => ({ ...p, fontSize: Math.min(FONT_MAX, Math.max(FONT_MIN, p.fontSize + d)) }))
    const onKey = (e: KeyboardEvent) => {
      const k = e.key
      const mod = e.ctrlKey || e.metaKey
      if (mod && k === ',') return e.preventDefault(), setSettingsOpen((o) => !o)
      if (mod && e.shiftKey && k.toLowerCase() === 'g') return e.preventDefault(), setPrefs((p) => ({ ...p, inspectorOpen: !p.inspectorOpen }))
      if (mod && !e.shiftKey && k.toLowerCase() === 'l') return e.preventDefault(), term.current?.clear()
      if (e.altKey && k.toLowerCase() === 'r') return e.preventDefault(), reconnect()
      if (e.altKey && (k === '=' || k === '+')) return e.preventDefault(), setFont(1)
      if (e.altKey && (k === '-' || k === '–')) return e.preventDefault(), setFont(-1)
      // Copy terminal selection (Ctrl+C only when the terminal has a selection, so it never steals input copy).
      if (mod && k.toLowerCase() === 'c') {
        const sel = term.current?.getSelection()
        const inField = (document.activeElement as HTMLInputElement | null)?.selectionStart !== (document.activeElement as HTMLInputElement | null)?.selectionEnd
        if (sel && (e.shiftKey || !inField)) {
          e.preventDefault()
          void navigator.clipboard?.writeText(sel).catch(() => undefined)
        }
      }
    }
    const onPaste = (e: ClipboardEvent) => {
      // A paste with focus outside any field goes to the command line.
      const t = e.target as HTMLElement | null
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA')) return
      const text = e.clipboardData?.getData('text')
      if (text) {
        e.preventDefault()
        input.current?.insert(text.split(/\r?\n/)[0] ?? '')
        input.current?.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    window.addEventListener('paste', onPaste)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('paste', onPaste)
    }
  }, [reconnect])

  const connected = snap.state === 'connected'
  const showError = snap.error && snap.error !== errorDismissed

  return (
    <div className="flex h-dvh flex-col bg-ink-950 text-sand-100">
      <Header
        state={snap.state}
        brand={brand}
        inspectorOpen={prefs.inspectorOpen}
        onReconnect={reconnect}
        onDisconnect={() => session.disconnect()}
        onSettings={() => setSettingsOpen(true)}
        onToggleInspector={() => setPrefs((p) => ({ ...p, inspectorOpen: !p.inspectorOpen }))}
      />
      {showError && (
        <div role="alert" data-testid="error-banner" className="flex flex-none items-start gap-3 border-b border-bad/40 bg-bad/10 px-4 py-2 text-sm text-sand-100">
          <div className="min-w-0 flex-1">
            <strong className="text-bad">Connection problem.</strong> {snap.error!.message}
            <span className="ml-2 font-mono text-xs text-muted">[{snap.error!.code}]</span>
          </div>
          {!connected && (
            <button onClick={reconnect} className="rounded-md border border-sand-500 px-3 py-1 text-sand-100 hover:bg-ink-800">
              Reconnect
            </button>
          )}
          <button onClick={() => setErrorDismissed(snap.error)} aria-label="Dismiss error" className="text-muted hover:text-sand-100">
            ✕
          </button>
        </div>
      )}
      <main className="flex min-h-0 flex-1 flex-col md:flex-row">
        <div className="min-h-0 min-w-0 flex-1">
          <TerminalView
            ref={term}
            fontSize={prefs.fontSize}
            scrollback={prefs.scrollback}
            onResize={(c, r) => session.resize(c, r)}
            onActivate={() => input.current?.focus()}
          />
        </div>
        {prefs.inspectorOpen && (
          <div className="h-[40%] flex-none md:h-auto">
            <GmcpInspector store={session.gmcp} onClose={() => setPrefs((p) => ({ ...p, inspectorOpen: false }))} />
          </div>
        )}
      </main>
      <CommandInput ref={input} history={session.history} masked={snap.masked} disabled={!connected} onSubmit={(l) => session.submit(l)} onScrollPages={(n) => term.current?.scrollPages(n)} />
      {settingsOpen && <SettingsDialog prefs={prefs} onChange={setPrefs} onClose={() => { setSettingsOpen(false); input.current?.focus() }} />}
    </div>
  )
}
