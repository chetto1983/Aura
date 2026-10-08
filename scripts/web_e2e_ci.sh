#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root/web"
umask 077
runtime_dir="$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/aura-web-e2e.XXXXXX")"
serve_pid=""
proxy_pid=""
target_port="${PROXY_TARGET_PORT:-9080}"
proxy_port="${PROXY_LISTEN_PORT:-9443}"
tls_origin="https://127.0.0.1:$proxy_port"

cleanup() {
  local status=$? pid deadline
  trap - EXIT INT TERM
  for pid in "$proxy_pid" "$serve_pid"; do
    if [ -n "$pid" ]; then kill -TERM "$pid" 2>/dev/null || true; fi
  done
  deadline=$((SECONDS + 10))
  for pid in "$proxy_pid" "$serve_pid"; do
    [ -n "$pid" ] || continue
    while kill -0 "$pid" 2>/dev/null && ((SECONDS < deadline)); do sleep 0.1; done
    if kill -0 "$pid" 2>/dev/null; then kill -KILL "$pid" 2>/dev/null || true; fi
    wait "$pid" 2>/dev/null || true
  done
  rm -f "$runtime_dir/tls.key" "$runtime_dir/tls.crt"
  # Aura can emit an in-memory setup token at boot; retain private logs without printing them.
  echo "Web E2E startup logs: $runtime_dir"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

openssl req -x509 -newkey rsa:2048 -noenc \
  -keyout "$runtime_dir/tls.key" -out "$runtime_dir/tls.crt" -days 1 \
  -subj '/CN=127.0.0.1' -addext 'subjectAltName=IP:127.0.0.1' \
  > "$runtime_dir/tls.log" 2>&1

# Keep Playwright's web cwd and inherited CI exports/HOME for the same MCP configuration.
# The proxy rewrites Origin to the http hop, so without the public URL MCP OAuth would
# authorize and call back on http://127.0.0.1:9080, where the HTTPS session never arrives.
# The E2E stack runs on sample Garage credentials and no pinned embedding revision, which
# the strict default profile rejects at boot; this harness is the dev posture by design.
AURA_PROFILE="${AURA_PROFILE:-dev}" AURA_WEB_PUBLIC_URL="$tls_origin" \
  ../aura serve --only=cli > "$runtime_dir/serve.log" 2>&1 &
serve_pid=$!
PROXY_TLS_KEY="$runtime_dir/tls.key" PROXY_TLS_CERT="$runtime_dir/tls.crt" \
  PROXY_TARGET_PORT="$target_port" PROXY_LISTEN_PORT="$proxy_port" \
  node e2e/https-proxy.mjs > "$runtime_dir/proxy.log" 2>&1 &
proxy_pid=$!

deadline=$((SECONDS + 60))
while true; do
  if ! kill -0 "$serve_pid" 2>/dev/null || ! kill -0 "$proxy_pid" 2>/dev/null; then
    echo 'Web E2E Aura or TLS proxy exited before readiness' >&2
    exit 1
  fi
  if curl --insecure --fail --silent --show-error --connect-timeout 1 --max-time 2 \
    "$tls_origin/healthz" > /dev/null 2>&1; then
    break
  fi
  if ((SECONDS >= deadline)); then
    echo 'Web E2E HTTPS readiness timed out after 60 seconds' >&2
    exit 1
  fi
  sleep 1
done

# Secure cookies are filtered from page.request on HTTP 127.0.0.1 even when Chromium
# stores them. Existing external-serve mode moves the full browser/API suite to TLS.
# https://github.com/microsoft/playwright/blob/v1.63.0/packages/playwright-core/src/server/cookieStore.ts
AURA_E2E_ORIGIN="$tls_origin" npm run test:e2e -- "$@"
