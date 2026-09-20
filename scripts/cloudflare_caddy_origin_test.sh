#!/usr/bin/env bash
# Real Caddy, no external network. Echo the origin seen by Aura through the shipped routes.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
name="aura-caddy-origin-test-$$"
trap 'docker rm -f "$name" >/dev/null 2>&1 || true; rm -rf "$fixture"' EXIT
image="${AURA_CADDY_TEST_IMAGE:-aura-caddy:task3-test}"
docker image inspect "$image" >/dev/null
sed 's/aura:9080/127.0.0.1:18080/g' "$repo_root/caddy/Caddyfile" >"$fixture/Caddyfile"
cat >>"$fixture/Caddyfile" <<'CADDY'
http://:18080 {
  respond "{http.request.header.X-Forwarded-Proto}://{http.request.host}"
}
CADDY
chmod 644 "$fixture/Caddyfile"
docker run -d --name "$name" --network none -v "$fixture/Caddyfile:/etc/caddy/Caddyfile:ro" "$image" >/dev/null
for attempt in $(seq 1 30); do
  result="$(docker exec "$name" wget -qO- --header 'Host: remote.example.test' --header 'X-Forwarded-Proto: https' http://127.0.0.1:8080/api/assets/presign 2>/dev/null)" && break
  sleep 1
done
[[ "${result:-}" == https://remote.example.test ]] || {
  echo "FAIL: tunnel frontdoor delivered origin '${result:-unreachable}' to Aura instead of public HTTPS" >&2; exit 1;
}
echo 'ok: private tunnel listener preserves the public HTTPS origin for Aura/Garage signing'
