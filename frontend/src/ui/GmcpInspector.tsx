import { useState, useSyncExternalStore } from 'react'
import type { GmcpStore } from '../gmcp/GmcpStore'
import { CloseIcon } from './icons'

const fmt = (v: unknown) => (v === undefined ? '(no payload)' : JSON.stringify(v, null, 2))

/** Developer view of everything GMCP has delivered: merged state + raw message log. */
export function GmcpInspector({ store, onClose }: { store: GmcpStore; onClose: () => void }) {
  const state = useSyncExternalStore(store.subscribe, store.snapshot)
  const log = useSyncExternalStore(store.subscribe, store.logSnapshot)
  const [tab, setTab] = useState<'state' | 'log'>('state')
  const [filter, setFilter] = useState('')
  const [paused, setPaused] = useState(false)
  const [frozen, setFrozen] = useState(log)
  const shown = (paused ? frozen : log).filter((e) => e.package.toLowerCase().includes(filter.toLowerCase())).slice().reverse()
  const tabBtn = (t: 'state' | 'log', label: string) => (
    <button onClick={() => setTab(t)} aria-pressed={tab === t} className={`rounded-md px-3 py-1 text-sm ${tab === t ? 'bg-ink-700 text-sand-100' : 'text-muted hover:text-sand-100'}`}>
      {label}
    </button>
  )
  return (
    <aside data-testid="gmcp-inspector" aria-label="GMCP inspector" className="flex h-full min-h-0 w-full flex-col border-ink-700 bg-ink-900 md:w-[24rem] md:border-l">
      <div className="flex items-center gap-2 border-b border-ink-700 px-3 py-2">
        <h2 className="mr-auto text-sm font-semibold text-sand-400">GMCP inspector</h2>
        {tabBtn('state', `State (${state.size})`)}
        {tabBtn('log', `Log (${log.length})`)}
        <button onClick={onClose} aria-label="Close inspector" className="rounded-md p-1 text-muted hover:text-sand-100">
          <CloseIcon />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-3 font-mono text-xs">
        {tab === 'state' ? (
          state.size === 0 ? (
            <p className="text-muted">No GMCP data received yet. The server only sends packages your client has opted in to, and only some servers/characters emit data immediately.</p>
          ) : (
            [...state.entries()]
              .sort(([a], [b]) => a.localeCompare(b))
              .map(([pkg, data]) => (
                <details key={pkg} open className="mb-3">
                  <summary className="cursor-pointer text-sand-300">{pkg}</summary>
                  <pre className="mt-1 whitespace-pre-wrap break-words text-sand-100">{fmt(data)}</pre>
                </details>
              ))
          )
        ) : (
          <>
            <div className="mb-2 flex gap-2 font-sans">
              <input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter by package…" aria-label="Filter log" className="min-w-0 flex-1 rounded-md border border-ink-700 bg-ink-950 px-2 py-1 text-sm text-sand-100" />
              <button
                onClick={() => {
                  setFrozen(log)
                  setPaused((p) => !p)
                }}
                className="rounded-md border border-ink-700 px-2 text-sm text-sand-300"
              >
                {paused ? 'Resume' : 'Pause'}
              </button>
            </div>
            {shown.length === 0 && <p className="text-muted">No messages.</p>}
            {shown.map((e) => (
              <div key={e.seq} className={`mb-2 border-l-2 pl-2 ${e.malformed ? 'border-bad' : 'border-ink-600'}`}>
                <div className="text-muted">
                  #{e.seq} {new Date(e.time).toLocaleTimeString()} <span className={e.malformed ? 'text-bad' : 'text-sand-300'}>{e.package || '(no package)'}</span>
                  {e.malformed && <span className="text-bad"> — malformed: {e.error}</span>}
                </div>
                <pre className="whitespace-pre-wrap break-words text-sand-100">{e.malformed ? e.raw : fmt(e.data)}</pre>
              </div>
            ))}
          </>
        )}
      </div>
    </aside>
  )
}
