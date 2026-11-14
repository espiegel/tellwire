# DuneMUD protocol notes — what is known, and what is not

Everything below is labelled **documented** (source: the server's own wiki), **observed** (by the author in
another client) or **unverified**. Nothing here was
learned by connecting to the live server from this project's automated tests.

| Fact | Status | Source |
|---|---|---|
| Secure (TLS) Telnet port **6788**; plain Telnet **6789** | documented | `who g` banner on the server: "Telnet: 6789 \|\| Secure: 6788" |
| Host `dunemud.net` | documented | dunemud.net |
| TLS certificate on 6788 is publicly trusted for `dunemud.net` | **unverified** | Another client the author used connects with TLS; whether it verifies the chain is unknown. Run the live probe (below). |
| Development port **4241** | **unverified** | Given in the project brief; deliberately *not* configured by default. |
| GMCP is offered with `IAC WILL GMCP` at connect | observed | author, in another client |
| GMCP is **output only**; no GMCP input commands are supported | documented | https://dunemud.net/gmcp/start |
| Modules: `Char`, `Room`, `Comm.Channel`, `Guild` (Guild is automatic) | documented | same |
| Clients opt in to modules via `Core.Supports.Set` (some clients do this automatically) | documented ("Some clients… do this automatically; others require you to opt in explicitly") | same |
| `Char.Vitals`, `Char.Stats`, `Char.MaxStats`, `Char.Status(Vars)` are **deltas** (changed fields only) | documented | same |
| `Room.Info`, `Char.Name`, `Comm.Channel.List` are **full** messages | documented | same |
| Room `num` and exit targets are 32‑char hash strings, not integers | documented | same |
| Some highly custom areas lack exit information in `Room.Info` | documented | same |
| The live server sometimes sent **no GMCP data** to tt++ even after the offer | observed (once) | bot README: "`RCVD IAC SB GMCP` never appeared, also during fights" |
| Output may contain emoji / multi‑byte UTF‑8 | observed | `who g` fixture contains emoji |
| MCCP, MSDP, MSSP, CHARSET support | **unverified** | |
| Whether the login password prompt uses Telnet `WILL ECHO` | **unverified** | The gateway handles both: ECHO negotiation, plus a prompt‑text heuristic fallback. |
| Line ending convention (CRLF / LFCR / LF) | **unverified** | The gateway normalises all of them. |
| Whether `Core.Hello` is accepted/ignored | **unverified** | Documented only as "no GMCP input". Sending it is configurable (`GATEWAY_GMCP_HELLO`); turn it off if the live server misbehaves. |

## How the gateway reacts to the unknowns

* **GMCP silent:** the inspector shows "No GMCP data received yet"; every GMCP‑driven UI element must
  stay hidden/disabled when its data is absent (`GmcpStore.select()` returns `undefined`). The
  classic terminal works regardless.
* **Opt‑in:** after `WILL GMCP` the gateway answers `DO GMCP`, then sends `Core.Hello` and
  `Core.Supports.Set ["Char 1","Room 1","Comm.Channel 1"]` (`GATEWAY_GMCP_SUPPORTS`).
* **MCCP2 (option 86):** refused (`DONT`). Output compression is deferred; see *Limitations* in the README.

## Running the live probe (explicit opt‑in)

```bash
cd gateway
TELLWIRE_LIVE=1 go test -v -run TestLive ./internal/live/
# or another endpoint, e.g. after confirming port 4241 with the admins:
TELLWIRE_LIVE=1 LIVE_ADDR=dunemud.net:4241 go test -v -run TestLive ./internal/live/
```
It verifies the certificate against the system roots, answers negotiation like the gateway, prints
the certificate details, every `WILL/DO/…` the server sends, whether GMCP came up and the first
300 bytes of the banner, then disconnects. It never logs in and never sends text. The result tells
you which rows above can be promoted from *unverified*.
