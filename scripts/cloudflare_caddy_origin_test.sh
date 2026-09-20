#!/usr/bin/env bash
# Real Caddy, no external network. Echo the origin seen by Aura through the shipped routes.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
name="aura-caddy-origin-test-$$"
trap 'docker rm -f "$name" >/dev/null 2>&1 || true; rm -rf "$fixture"' EXIT
image="${AURA_CADDY_TEST_IMAGE:-aura-caddy:local}"
docker image inspect "$image" >/dev/null
for variant in Caddyfile Caddyfile.domain; do
  # DNS certificate issuance is external to this test; keep every shipped routing directive.
  sed -e 's/aura:9080/127.0.0.1:18080/g' -e 's/import desec_tls/tls internal/g' \
    "$repo_root/caddy/$variant" >"$fixture/Caddyfile"
  cat >>"$fixture/Caddyfile" <<'CADDY'
http://:18080 {
  respond "{http.request.header.X-Forwarded-Proto}://{http.request.host}|{http.request.header.X-Aura-Remote-Ingress}"
}
CADDY
  chmod 644 "$fixture/Caddyfile"
  docker run -d --name "$name" --network none -e AURA_PUBLIC_HOST=localhost -e AURA_VIEWS_HOST=views.localhost \
    -e DESEC_TOKEN=fixture -v "$fixture/Caddyfile:/etc/caddy/Caddyfile:ro" "$image" >/dev/null
  for attempt in $(seq 1 30); do
    result="$(docker exec "$name" wget -qO- --header 'Host: remote.example.test' --header 'X-Forwarded-Proto: https' http://127.0.0.1:8080/api/assets/presign 2>/dev/null)" && break
    sleep 1
  done
  [[ "${result:-}" == 'https://remote.example.test|tunnel' ]] || {
    echo "FAIL: $variant tunnel frontdoor delivered '${result:-unreachable}' instead of public HTTPS/owned marker" >&2; exit 1;
  }
  spoofed="$(docker exec "$name" wget -qO- --header 'Host: remote.example.test' --header 'X-Forwarded-Proto: http' --header 'X-Aura-Remote-Ingress: forged' http://127.0.0.1:8080/api/assets/presign)"
  [[ "$spoofed" == 'https://remote.example.test|tunnel' ]]
  direct="$(docker exec "$name" wget --no-check-certificate -qO- --header 'X-Forwarded-Proto: http' --header 'X-Aura-Remote-Ingress: tunnel' https://localhost/api/assets/presign)"
  [[ "$direct" == 'https://localhost|' ]] || { echo "FAIL: $variant direct origin or marker boundary: $direct" >&2; exit 1; }
  docker rm -f "$name" >/dev/null
  echo "ok: $variant tunnel and direct HTTPS preserve secure origin; spoofed downgrade and ingress marker refused"
done
