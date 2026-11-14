import { useEffect, useRef } from 'react'
import { DEFAULT_PREFS, FONT_MAX, FONT_MIN, type Prefs } from '../prefs/prefs'
import { CloseIcon } from './icons'

interface Props {
  prefs: Prefs
  onChange: (p: Prefs) => void
  onClose: () => void
}

const SHORTCUTS: [string, string][] = [
  ['Enter', 'Send command'],
  ['↑ / ↓', 'Command history'],
  ['PageUp / PageDown', 'Scroll terminal'],
  ['Ctrl+L', 'Clear terminal'],
  ['Ctrl+C / Ctrl+Shift+C', 'Copy terminal selection'],
  ['Alt+R', 'Reconnect'],
  ['Alt+= / Alt+−', 'Font size'],
  ['Ctrl+,', 'Settings'],
  ['Ctrl+Shift+G', 'GMCP inspector'],
  ['Esc', 'Clear command line / close dialog'],
]

export function SettingsDialog({ prefs, onChange, onClose }: Props) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    ref.current?.focus()
  }, [])
  const set = <K extends keyof Prefs>(k: K, v: Prefs[K]) => onChange({ ...prefs, [k]: v })
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 p-0 sm:items-center sm:p-4" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div
        ref={ref}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-label="Settings"
        onKeyDown={(e) => e.key === 'Escape' && (e.stopPropagation(), onClose())}
        className="max-h-[90dvh] w-full max-w-lg overflow-y-auto rounded-t-xl border border-ink-600 bg-ink-900 p-5 shadow-2xl sm:rounded-xl"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold text-sand-400">Settings</h2>
          <button onClick={onClose} aria-label="Close settings" className="rounded-md p-2 text-muted hover:text-sand-100">
            <CloseIcon />
          </button>
        </div>

        <label className="mb-1 block text-sm text-sand-300" htmlFor="fs">
          Font size: <span data-testid="font-size-value">{prefs.fontSize}</span>px
        </label>
        <input id="fs" type="range" min={FONT_MIN} max={FONT_MAX} value={prefs.fontSize} onChange={(e) => set('fontSize', Number(e.target.value))} className="mb-5 w-full accent-sand-400" />

        <label className="mb-1 block text-sm text-sand-300" htmlFor="sb">
          Scrollback
        </label>
        <select id="sb" value={prefs.scrollback} onChange={(e) => set('scrollback', Number(e.target.value))} className="mb-5 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sand-100">
          {[1000, 5000, 10000, 25000, 50000].map((n) => (
            <option key={n} value={n}>
              {n.toLocaleString()} lines
            </option>
          ))}
        </select>

        <label className="mb-5 flex items-start gap-3 text-sm text-sand-300">
          <input type="checkbox" checked={prefs.localEcho} onChange={(e) => set('localEcho', e.target.checked)} className="mt-1 h-4 w-4 accent-sand-400" />
          <span>
            Echo my commands in the terminal
            <span className="block text-xs text-muted">Passwords are never echoed or stored, regardless of this setting.</span>
          </span>
        </label>

        <h3 className="mb-2 text-sm font-semibold text-sand-400">Keyboard shortcuts</h3>
        <dl className="mb-5 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
          {SHORTCUTS.map(([k, d]) => (
            <div key={k} className="contents">
              <dt className="font-mono text-sand-300">{k}</dt>
              <dd className="text-muted">{d}</dd>
            </div>
          ))}
        </dl>

        <div className="flex justify-between">
          <button onClick={() => onChange({ ...DEFAULT_PREFS })} className="rounded-md border border-ink-700 px-3 py-2 text-sm text-muted hover:text-sand-100">
            Reset to defaults
          </button>
          <button onClick={onClose} className="rounded-md border border-sand-500 bg-ink-800 px-4 py-2 text-sm text-sand-100 hover:bg-ink-700">
            Done
          </button>
        </div>
      </div>
    </div>
  )
}
