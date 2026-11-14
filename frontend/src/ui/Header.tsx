import type { ConnectionState } from '../protocol/messages'
import { DEFAULT_BRAND, type Brand } from '../brand/brand'
import { BracesIcon, GearIcon, PowerIcon, RefreshIcon } from './icons'

const LABEL: Record<ConnectionState, string> = {
  idle: 'Not connected',
  connecting: 'Connecting…',
  connected: 'Connected',
  disconnected: 'Disconnected',
}
const DOT: Record<ConnectionState, string> = {
  idle: 'bg-muted',
  connecting: 'bg-warn animate-pulse',
  connected: 'bg-ok',
  disconnected: 'bg-bad',
}

export function StatusPill({ state }: { state: ConnectionState }) {
  return (
    <span
      data-testid="status"
      data-state={state}
      role="status"
      aria-live="polite"
      className="inline-flex items-center gap-2 rounded-full border border-ink-700 bg-ink-850 px-3 py-1 text-xs font-medium text-sand-300"
    >
      <span className={`h-2 w-2 rounded-full ${DOT[state]}`} aria-hidden />
      {LABEL[state]}
    </span>
  )
}

const btn =
  'inline-flex h-9 items-center gap-2 rounded-md border border-ink-700 bg-ink-850 px-3 text-sm text-sand-300 transition-colors hover:border-sand-500 hover:text-sand-100 disabled:opacity-40 disabled:hover:border-ink-700'

interface Props {
  state: ConnectionState
  brand?: Brand
  inspectorOpen: boolean
  onReconnect: () => void
  onDisconnect: () => void
  onSettings: () => void
  onToggleInspector: () => void
}

export function Header({ state, brand = DEFAULT_BRAND, inspectorOpen, onReconnect, onDisconnect, onSettings, onToggleInspector }: Props) {
  const live = state === 'connected' || state === 'connecting'
  return (
    <header className="flex-none border-b border-ink-700 bg-ink-900 pt-[env(safe-area-inset-top)]">
      <div className="flex items-center gap-3 px-3 py-2 sm:px-4">
        <div className="flex min-w-0 items-baseline gap-2">
          <h1 data-testid="brand-name" className="font-semibold uppercase tracking-[0.28em] text-sand-400 text-sm sm:text-base">
            {brand.name.slice(0, brand.accentStart)}
            <span className="text-spice-500">{brand.name.slice(brand.accentStart)}</span>
          </h1>
          <span className="hidden text-xs text-muted sm:inline">{brand.tagline}</span>
        </div>
        <div className="ml-auto flex items-center gap-2">
          <StatusPill state={state} />
          {live ? (
            <button className={btn} onClick={onDisconnect} aria-label="Disconnect" title="Disconnect">
              <PowerIcon />
              <span className="hidden md:inline">Disconnect</span>
            </button>
          ) : (
            <button className={`${btn} border-sand-500 text-sand-100`} onClick={onReconnect} aria-label="Reconnect" title="Reconnect (Alt+R)">
              <RefreshIcon />
              <span className="hidden sm:inline">{state === 'idle' ? 'Connect' : 'Reconnect'}</span>
            </button>
          )}
          <button className={`${btn} ${inspectorOpen ? 'border-sand-500 text-sand-100' : ''}`} onClick={onToggleInspector} aria-label="GMCP inspector" aria-pressed={inspectorOpen} title="GMCP inspector (Ctrl+Shift+G)">
            <BracesIcon />
          </button>
          <button className={btn} onClick={onSettings} aria-label="Settings" title="Settings (Ctrl+,)">
            <GearIcon />
          </button>
        </div>
      </div>
      <div className="desert-line" />
    </header>
  )
}
