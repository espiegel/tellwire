#!/usr/bin/env bash
# Runs the browser e2e suite through the REAL Caddy configs (frontend/Caddyfile and
# deploy/Caddyfile) in front of the real gateway and mock MUD. No Docker needed.
# Only hosts, ports and the static root are substituted; routing, headers, CSP and
# caching come from the shipped files. Requires: caddy, go, node on PATH.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
command -v caddy >/dev/null || { echo "caddy not found on PATH" >&2; exit 2; }
TMP="$(mktemp -d)"; PIDS=()
cleanup() { for p in "${PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done; rm -rf "$TMP"; }
trap cleanup EXIT

MOCK=12323 GW=18080 FE=14080 EDGE=18088
(cd "$ROOT/gateway" && go build -o bin/ ./cmd/...)
(cd "$ROOT/frontend" && npm run build >/dev/null)

sed -e "s/^:80 {/:$FE {/" -e "s|root \* /srv|root * $ROOT/frontend/dist|" "$ROOT/frontend/Caddyfile" > "$TMP/frontend.Caddyfile"
sed -e "s/gateway:8080/127.0.0.1:$GW/" -e "s/frontend:80/127.0.0.1:$FE/" "$ROOT/deploy/Caddyfile" > "$TMP/edge.Caddyfile"

MOCKMUD_LISTEN=127.0.0.1:$MOCK "$ROOT/gateway/bin/mockmud" >"$TMP/mock.log" 2>&1 & PIDS+=($!)
GATEWAY_LISTEN=127.0.0.1:$GW GATEWAY_UPSTREAMS=default=127.0.0.1:$MOCK:plain \
  GATEWAY_ALLOWED_ORIGINS=http://localhost:$EDGE GATEWAY_TRUSTED_PROXIES=127.0.0.1 GATEWAY_LOG_FORMAT=text \
  "$ROOT/gateway/bin/gateway" >"$TMP/gateway.log" 2>&1 & PIDS+=($!)
caddy run --config "$TMP/frontend.Caddyfile" --adapter caddyfile >"$TMP/fe.log" 2>&1 & PIDS+=($!)
SITE_ADDRESS=:$EDGE caddy run --config "$TMP/edge.Caddyfile" --adapter caddyfile >"$TMP/edge.log" 2>&1 & PIDS+=($!)

for i in $(seq 1 50); do curl -fsS "http://localhost:$EDGE/healthz" >/dev/null 2>&1 && break; sleep 0.2; done
curl -fsS "http://localhost:$EDGE/healthz" >/dev/null || { echo "edge did not come up"; tail "$TMP"/*.log; exit 1; }

# Privacy invariant: the gateway log must never contain player input or terminal text.
cd "$ROOT/frontend"
E2E_BASE="http://localhost:$EDGE" npx playwright test
if grep -qiE "sup3r-secret|Welcome, Paul|golden sand" "$TMP/gateway.log"; then echo "FAIL: gateway log leaked content"; exit 1; fi
echo "edge e2e OK; gateway log contains no player content"
