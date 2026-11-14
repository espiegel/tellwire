# Browser ⇄ gateway WebSocket protocol (v1)

This document is normative. The Go side is `gateway/internal/protocol`, the TypeScript side
`frontend/src/protocol/messages.ts`.

## Transport

* Endpoint: `GET /ws`, upgraded to WebSocket (`wss://` in production).
* Subprotocol: the client **must** offer `tellwire.v1`; the gateway closes with 1008 otherwise.
* `Origin` is mandatory and must exactly match (scheme + host + port) an entry of
  `GATEWAY_ALLOWED_ORIGINS`; otherwise the handshake is answered `403`.
* **One WebSocket = one upstream Telnet session.** There is no resume: to reconnect, open a new
  WebSocket. The gateway never lets a client choose a host or port (see *Profiles*).

## Framing

| Direction | Frame type | Content |
|---|---|---|
| gateway → browser | **binary** | Raw terminal output. No header. UTF‑8 text with ANSI escapes, Telnet commands removed, every line ending normalised to `CR LF`. A multi‑byte UTF‑8 character may be split across two frames; write the bytes straight to xterm.js, which reassembles them. |
| gateway → browser | **text** | One JSON control message. |
| browser → gateway | **text** | One JSON control message. |
| browser → gateway | binary | Not allowed. The gateway ends the session with `bad_request`. |

Why binary for output: it avoids JSON‑escaping ESC bytes (the dominant content), is cheaper to
produce, and lets the browser hand the buffer to xterm.js with zero decoding.

Every JSON message is an object with `"v": 1` and a `"type"`. Unknown `type`s from the gateway
must be ignored by the browser (forward compatibility). Unknown fields are ignored in both
directions. Maximum browser message size: `GATEWAY_MAX_MESSAGE_BYTES` (default 16 KiB; larger
frames close the socket with 1009).

## Browser → gateway

### `session.connect` (must be the first message, within 10 s)
```json
{"v":1,"type":"session.connect","profile":"default","cols":100,"rows":30}
```
`profile` is optional (empty = first configured upstream) and selects a **named, server-side
allowlisted** upstream. Unknown names → `connection.error` `unknown_profile`. `cols`/`rows`
(clamped to 1‥500 / 1‥200) seed NAWS.

### `terminal.input`
```json
{"v":1,"type":"terminal.input","data":"look\n"}
```
`data` is UTF‑8 text. The gateway converts every `\n`, `\r` or `\r\n` to Telnet `CR LF`, drops
NULs and escapes `0xFF` (IAC). Rate limited (`GATEWAY_INPUT_LINES_PER_SEC`, burst
`GATEWAY_INPUT_BURST`); an over‑limit message is dropped and answered with a non‑fatal
`connection.error` `rate_limited`.

### `terminal.resize`
```json
{"v":1,"type":"terminal.resize","cols":120,"rows":40}
```
Forwarded as a NAWS subnegotiation if the server enabled NAWS.

### `session.disconnect`
```json
{"v":1,"type":"session.disconnect"}
```
Ends the session; the gateway closes the upstream connection and the WebSocket (1000).

## Gateway → browser

### `connection.status`
```json
{"v":1,"type":"connection.status","state":"connected","profile":"default","detail":""}
```
`state`: `connecting` → `connected` → `disconnected`. `detail` is human text, safe to show.
`disconnected` is always the last JSON message before the socket closes.

### `connection.error`
```json
{"v":1,"type":"connection.error","code":"upstream_tls","message":"The game server's TLS certificate could not be verified, so the gateway refused to connect."}
```
`message` is written for end users and never contains addresses or raw error text.

| code | fatal | meaning |
|---|---|---|
| `bad_request` | yes | Malformed/out‑of‑order/binary/unsupported‑version message |
| `unknown_profile` | yes | Profile is not allowlisted |
| `limit_global` / `limit_ip` | yes | Connection cap reached |
| `upstream_unreachable` | yes | Dial failed or timed out |
| `upstream_tls` | yes | Certificate invalid/untrusted/hostname mismatch, or server not speaking TLS |
| _(upstream closed)_ | yes | The MUD closed or dropped the connection (e.g. `quit`, crash). Deliberately **not** a `connection.error`: it is reported only as `connection.status` `disconnected` with a `detail` such as "The game server closed the connection." |
| `idle_timeout` | yes | No `terminal.input` for `GATEWAY_IDLE_TIMEOUT` |
| `shutting_down` | yes | Gateway restarting |
| `rate_limited` | **no** | Input dropped |
| `internal` | yes | Browser unreachable (write timeout) |

### `session.echo`
```json
{"v":1,"type":"session.echo","enabled":false}
```
Sent once at start (`true`) and whenever the MUD toggles Telnet ECHO.
`enabled:false` ⇒ the server sent `WILL ECHO`: it (not the client) echoes, which MUDs use for
password prompts. The browser **must not** echo local input and **must** mask the input field
and exclude the line from history. `enabled:true` restores normal behaviour.
Fallback for servers that never negotiate ECHO: see *Password masking* in `architecture.md`.

### `gmcp.message`
```json
{"v":1,"type":"gmcp.message","package":"Char.Vitals","data":{"hp":380}}
{"v":1,"type":"gmcp.message","package":"Char.Vitals","malformed":true,"error":"invalid JSON payload","raw":"{\"hp\":"}
```
* `package`: dotted name exactly as sent (`[A-Za-z0-9._-]`, ≤128 chars).
* `data`: the JSON payload **as sent** (compacted, otherwise unchanged). Absent if the message had no payload. No package is special‑cased: unknown packages pass through untouched.
* Malformed messages (bad name or bad JSON) are **forwarded** with `malformed:true`, an `error` and the (truncated, UTF‑8‑sanitised) `raw` payload, so the inspector can show them. Clients must not apply them to state.
* Delta semantics (merge vs. replace) are a *client* concern; the gateway does not merge.
* Oversized sub‑negotiations (> `GATEWAY_MAX_SUBNEG_BYTES`) are dropped by the parser and not forwarded.

## Close codes

| code | when |
|---|---|
| 1000 | Normal end (disconnect, MUD quit, idle timeout, shutdown) |
| 1008 | Missing subprotocol / bad first message |
| 1009 | Message larger than the limit |
| 1013 | Capacity limit |
| 1011 | Upstream failure at dial time |

An error is always preceded by a `connection.error` + `connection.status: disconnected` frame
where possible. A close with no JSON (code 1006) means the gateway itself was unreachable.

## Profiles

`GATEWAY_UPSTREAMS="default=dunemud.net:6788:tls,mock=mock:2323:plain"` defines the complete set
of destinations. Names, not addresses, cross the wire. This is the mechanism that prevents the
gateway from being an open TCP relay.

## Versioning

`v` is an integer. A breaking change increments it and the subprotocol (`tellwire.v2`); the gateway
may offer both during a migration. Additive changes (new message types/fields) do not bump it.
