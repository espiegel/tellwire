# Deployment (https://play.dunemud.net)

Target: one small Linux VM (1 vCPU / 1 GB is enough to start) running Docker. The stack is
`edge` (Caddy, automatic HTTPS) → `frontend` (static) + `gateway` (Go). It is independent of the
existing PHP/DokuWiki site: it needs only its own hostname and ports 80/443.

> Status: the Compose files validate (`docker compose config`) and the Caddyfiles were validated
> and **exercised with the real Caddy binary** (`deploy/test-edge.sh`), but the container images
> have **not** been built here (no Docker daemon was available while developing). Do a first
> `docker compose … build` on the target host and run the checklist below.

## 1. DNS
Create an `A` (and `AAAA` if the host has IPv6) record: `play.dunemud.net → <server IP>`.
Wait until `dig +short play.dunemud.net` returns it. Caddy's certificate request fails until it does.

## 2. Firewall
Open inbound `80/tcp` (ACME HTTP‑01 + redirect), `443/tcp` and `443/udp` (HTTP/3). Nothing else.
The gateway (`:8080`), frontend (`:80`) and metrics are only on the private Docker network.
Outbound: `dunemud.net:6788/tcp` (and 80/443 for ACME).

## 3. Configure
```bash
git clone https://github.com/espiegel/tellwire.git && cd tellwire
cp deploy/.env.example deploy/.env
$EDITOR deploy/.env            # SITE_ADDRESS, GATEWAY_ALLOWED_ORIGINS, upstream
```
* `SITE_ADDRESS=play.dunemud.net` — Caddy obtains and renews a Let's Encrypt certificate for it.
* `GATEWAY_ALLOWED_ORIGINS=https://play.dunemud.net` — **exact** origin; no wildcard accepted.
* `GATEWAY_UPSTREAMS=default=dunemud.net:6788:tls` — the only place the MUD address lives. Do not add
  the development port 4241 until it has been verified (see `docs/upstream-notes.md`).

## 4. Verify upstream first (recommended, explicit opt‑in)
From the server (or anywhere with outbound access):
```bash
cd gateway && TELLWIRE_LIVE=1 go test -v -run TestLive ./internal/live/
```
Confirm the certificate verifies and note what the server offers. If it fails verification the
gateway will refuse to connect (by design); fix the certificate chain or provide the issuing CA via
`GATEWAY_UPSTREAM_CA_FILE` — never disable verification.

## 5. Start
```bash
docker compose -f deploy/compose.prod.yaml --env-file deploy/.env up -d --build
docker compose -f deploy/compose.prod.yaml ps          # all "healthy"
curl -fsS https://play.dunemud.net/healthz
```
Open `https://play.dunemud.net`, confirm the status pill turns green and the login prompt appears.

## 6. Checklist after first deploy
- [ ] Browser padlock valid; `curl -sI https://play.dunemud.net | grep -i strict-transport`
- [ ] `curl -s -o /dev/null -w '%{http_code}' -H 'Origin: https://evil.example' -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZQ==' https://play.dunemud.net/ws` → `403`
- [ ] `https://play.dunemud.net/metrics` returns the web app, not metrics
- [ ] Log in; password field is masked; reconnect button works after `quit`
- [ ] `docker compose logs gateway` shows session start/end lines and **no** names, passwords or game text

## Operations
| Task | Command |
|---|---|
| Logs | `docker compose -f deploy/compose.prod.yaml logs -f gateway` (JSON; rotated 5×10 MB) |
| Update | `git pull && docker compose -f deploy/compose.prod.yaml --env-file deploy/.env up -d --build` |
| Graceful restart | `docker compose … restart gateway` — live players get a `shutting_down` message and can reconnect (drain ≤ `GATEWAY_SHUTDOWN_TIMEOUT`, compose `stop_grace_period` is 20 s) |
| Roll back | `git checkout <previous tag>` and rerun the update command |
| Metrics | Not exposed publicly. `docker compose exec gateway …` has no shell; scrape from another container on the same network: `http://gateway:8080/metrics` (Prometheus text: `tellwire_sessions_active`, `…_bytes_down_total`, `…_session_ends_total{reason}`, …) |
| Certificates | Stored in the `caddy_data` volume. **Do not delete it** (Let's Encrypt rate limits). Back it up. |

Health: container `HEALTHCHECK`s (gateway: `/gateway healthcheck` → `/healthz`; frontend: `/healthz`),
`/readyz` (503 while draining) for load balancers, `restart: always` for crash recovery.

## Branding

The client shows "Tellwire" by default. To present it as your own MUD's client, serve a `/brand.json` next to
the page. No rebuild is needed: mount a file over `/srv/brand.json` in the frontend container
(see the commented `volumes:` line in `deploy/compose.prod.yaml`).

```json
{
  "name": "DuneMUD",
  "accentStart": 4,
  "tagline": "play in your browser",
  "title": "DuneMUD — Play in your browser",
  "description": "Play DuneMUD in your browser. Unofficial player-built client, powered by Tellwire."
}
```

| Field | Meaning | Limit |
|---|---|---|
| `name` | Header wordmark, shown uppercase | 24 chars |
| `accentStart` | Index in `name` where the accent colour starts (`DuneMUD` + `4` = DUNE + MUD) | `0..len(name)` |
| `tagline` | Small text beside the wordmark | 60 chars |
| `title` | Browser tab title | 80 chars |
| `description` | `<meta name="description">` | 200 chars |

Every field is optional; a missing, invalid or unreachable file falls back to the Tellwire defaults. Values are
rendered as plain text, never HTML. `deploy/brand.dunemud.json` is a ready-made example. If you brand the client
as a specific MUD, keep a visible note that it is an unofficial player project unless the MUD's admins say otherwise.

## Sizing and limits
Defaults: 500 total sessions, 10 per IP. Players behind one NAT (schools, offices) share the per‑IP
cap — raise `GATEWAY_MAX_CONNECTIONS_PER_IP` if that matters. The 500 figure is a *configured limit*,
not a measured capacity; see the README for what has and has not been benchmarked.

## Running the frontend on a different origin (optional)
Build with `VITE_GATEWAY_URL=wss://gateway.example.com/ws`, add the frontend origin to
`GATEWAY_ALLOWED_ORIGINS`, and widen the CSP `connect-src` in `frontend/Caddyfile` to that host.
