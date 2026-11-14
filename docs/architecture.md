# Architecture

```
 Browser (React + xterm.js)                    Gateway (Go)                         MUD
┌──────────────────────────┐   WSS /ws    ┌───────────────────────────┐   TLS/Telnet   ┌─────────────┐
│ UI  ─ SessionController  │◀────────────▶│ session ─ telnet.Parser   │◀──────────────▶│ dunemud.net │
│     ─ ConnectionManager  │ binary: text │         ─ telnet.Client   │ 1 conn/session │  :6788 (TLS)│
│     ─ GmcpStore          │ JSON: control│         ─ gmcp.Parse      │                └─────────────┘
└──────────────────────────┘              └───────────────────────────┘
        served by Caddy (static)                  behind Caddy (/ws)
```

The three pieces deploy independently: the frontend is static files, the gateway is one static
binary, Caddy is only glue. The frontend only needs a same‑origin `/ws` (or `VITE_GATEWAY_URL`).

## Gateway (`gateway/`)

| Package | Responsibility |
|---|---|
| `internal/telnet` | `Parser` (incremental IAC/SB state machine, bounded), `Client` (RFC 1143‑style negotiation, TTYPE/NAWS/ECHO/SGA/EOR/GMCP), encoders, `Normalizer` (line endings). No I/O, no goroutines. |
| `internal/gmcp` | Generic `Package {json}` parser; flags malformed input instead of dropping it. |
| `internal/protocol` | Wire types shared by tests and the server. |
| `internal/config` | Environment → `Config`; upstream allowlist parsing. Fails closed (no origins ⇒ no start). |
| `internal/server` | HTTP handler, origin check, limits, one `session` per WebSocket, metrics. |
| `internal/mockmud` | Deterministic fake MUD (real Telnet, TTYPE/NAWS/ECHO/GMCP) for tests and the demo. |
| `internal/live` | Opt‑in probe of the real server. Skipped by default. |
| `cmd/gateway`, `cmd/mockmud` | Binaries. `gateway healthcheck` is the container health probe. |

### Session model

Per WebSocket the gateway runs a small, fixed set of goroutines:

* **browser reader** – validates JSON, applies the input rate limit, writes to upstream, handles resize/disconnect;
* **upstream reader** – `Read` → `Parser.Feed` → `Client` → (text → normalise → queue, GMCP → parse → queue);
* **writer** – the *only* place frames are written to the WebSocket;
* **pinger** – WebSocket pings (dead‑peer detection);
* supervisor (the handler goroutine) – waits for the session context, then tears down in order.

Sessions share only immutable config and atomic counters, so they are isolated by construction
(`TestSessionIsolationConcurrent` asserts this with 40 concurrent players).

**Backpressure.** The upstream reader pushes frames into a bounded queue (`GATEWAY_OUT_QUEUE`,
default 64). A slow browser fills it, the reader blocks, the kernel buffer fills and TCP flow
control throttles the MUD — no unbounded memory. If a single WebSocket write stalls for
`GATEWAY_WRITE_TIMEOUT` the session is ended and the upstream released
(`TestSlowBrowserBackpressureEndsSession`, ~20 MB flood to a non‑reading client).

**Teardown order** (matters because coder/websocket closes the connection when a *read* context is
cancelled): session context cancelled → upstream closed → upstream reader joined → final
`connection.error`/`status` frames flushed through the writer → WebSocket close handshake →
I/O context cancelled → browser reader joined.

### Security model

| Threat | Mitigation | Test |
|---|---|---|
| Open TCP relay | Destinations are a server‑side allowlist; clients send a profile *name* only; unknown fields ignored | `TestClientCannotChooseUpstream` |
| Cross‑site WebSocket hijack | Exact scheme+host+port `Origin` match; missing Origin refused | `TestOriginValidation` |
| MITM on the upstream leg | TLS verification always on (system roots + optional extra CA file); there is **no** insecure flag | `TestUpstreamTLS` |
| Resource exhaustion | Global + per‑IP caps; WebSocket read limit; sub‑negotiation cap; bounded queues; write/idle/dial timeouts; input rate limit | `TestConnectionLimits`, `TestOversizedMessage…`, `TestHostileUpstream…`, `TestIdleTimeout`, `TestInputRateLimit` |
| X‑Forwarded‑For spoofing | Header honoured only from `GATEWAY_TRUSTED_PROXIES`, rightmost untrusted hop | `TestClientIPTrustedProxies` |
| Credential/PII leakage | Terminal text and input are never logged; client IP is not logged on the success path; errors shown to users never contain upstream addresses | `TestPasswordMaskingAndNeverLogged`, `TestUpstreamUnreachable` |
| Telnet control injection | Parser strips IAC before text reaches the browser; user input has IAC escaped | `TestNoControlBytesLeakIntoData`, `TestEncode` |

Containers run as non‑root on a read‑only filesystem with all capabilities dropped.

## Frontend (`frontend/src`)

Strict layering — each layer only imports downwards:

```
ui/ (React components)  ──▶  session/SessionController  ──▶  connection/ConnectionManager  ──▶  protocol/
terminal/TerminalView (xterm wrapper, display only)         gmcp/GmcpStore   prefs/
```

* `ConnectionManager` – one WebSocket, the `tellwire.v1` protocol, typed events. Knows nothing about React or xterm. Each `connect()` is a fresh socket; callbacks of an old socket are ignored by a generation counter.
* `SessionController` – composes connection, `GmcpStore`, `CommandHistory`, and the masking policy; exposes `useSyncExternalStore`‑compatible state. **No game commands live here or in the transport.**
* `TerminalView` – output only (`disableStdin`); the dedicated command line is the single input path.
* `GmcpStore` – generic package map; merges only the packages the wiki documents as deltas; `select("Room.Info.exits.north")` returns `undefined` for anything missing so UI can omit/disable features rather than invent state.

### Password masking

Masked ⇔ `server ECHO active` **or** `prompt heuristic`. Primary signal: gateway `session.echo{enabled:false}` (Telnet `WILL ECHO`). Fallback for servers that do not negotiate ECHO: the text after the last newline looks like a password prompt (`password:`, `passphrase`, `pin`…). While masked: the field is `type=password`, the line is not echoed to the terminal, history recall is disabled, and the line never enters history. Nothing is ever persisted (only font size, scrollback and two booleans go to `localStorage`).

### Extension point for the RPG dashboard (Milestone 5)

Not implemented yet by design. It plugs in as follows: components read `session.gmcp` via
`useSyncExternalStore(store.subscribe, store.snapshot)`; a declarative action registry
(`{id,label,command,category,requires:{"Room.Info.exits.north":true}}`) resolves `requires`
with `GmcpStore.select()` and sends `command` through `SessionController.submit()` — the same path as typed input.
The real GMCP schema is in `docs/upstream-notes.md`; e.g. `Room.Info.exits` is a map of exit name → hash.
