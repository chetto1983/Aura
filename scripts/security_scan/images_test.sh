#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/../.." && pwd)"
fixtures="$(mktemp -d)"
trap 'rm -rf -- "$fixtures"' EXIT

fail() { echo "security-scan images test: $*" >&2; exit 1; }

cat >"$fixtures/compose.yaml" <<'YAML'
services:
  aura:
    image: ${AURA_IMAGE:-aura:local}
    environment:
      SECRET: ${SECRET_TOKEN:?SECRET_TOKEN required in .env}
  aura-worker:
    image: ${AURA_IMAGE:-aura:local}
  caddy:
    image: ${AURA_CADDY_IMAGE:-aura-caddy:local}
  postgres:
    image: postgres:18.6-alpine3.24
  pim:
    image: ${AURA_PIM_MCP_IMAGE:-ghcr.io/acme/aura-pim-mcp:latest}
  stt:
    image: ghcr.io/acme/aura-whisper-server:2026.10.21.1401
  grafana:
    profiles: [observability]
    image: grafana/grafana:13.2.4
YAML

out="$(bash "$here/images.sh" "$fixtures/compose.yaml" acme)"
want='[
  {"image":"ghcr.io/acme/aura-caddy:edge","kind":"own","name":"ghcr.io/acme/aura-caddy"},
  {"image":"ghcr.io/acme/aura-egress:edge","kind":"own","name":"ghcr.io/acme/aura-egress"},
  {"image":"ghcr.io/acme/aura-pim-mcp:latest","kind":"own","name":"ghcr.io/acme/aura-pim-mcp"},
  {"image":"ghcr.io/acme/aura-sandbox:edge","kind":"own","name":"ghcr.io/acme/aura-sandbox"},
  {"image":"ghcr.io/acme/aura-whisper-server:2026.10.21.1401","kind":"third-party","name":"ghcr.io/acme/aura-whisper-server"},
  {"image":"ghcr.io/acme/aura:edge","kind":"own","name":"ghcr.io/acme/aura"},
  {"image":"grafana/grafana:13.2.4","kind":"third-party","name":"grafana/grafana"},
  {"image":"postgres:18.6-alpine3.24","kind":"third-party","name":"postgres"}
]'
[[ "$(jq -S . <<<"$out")" == "$(jq -S . <<<"$want")" ]] || fail "fixture list: got $out"

cat >"$fixtures/unmapped.yaml" <<'YAML'
services:
  newcomer:
    image: ${AURA_NEWCOMER_IMAGE:-aura-newcomer:local}
YAML
if err="$(bash "$here/images.sh" "$fixtures/unmapped.yaml" acme 2>&1)"; then
  fail "an image on a local default passed: $err"
fi
grep -q 'aura-newcomer:local' <<<"$err" || fail "the refusal does not name the image: $err"

# The real file: the 20 images the baseline scanned (docs/superpowers/verification/
# 2026-10-24-image-vulnerability-baseline.md), seven of them Aura's own builds at edge.
real="$(bash "$here/images.sh" "$repo_root/compose.yaml" chetto1983)"
[[ "$(jq length <<<"$real")" == 20 ]] || fail "compose.yaml gave $(jq length <<<"$real") images, want 20"
edge="$(jq -r '[.[] | select(.image | endswith(":edge"))] | length' <<<"$real")"
[[ "$edge" == 7 ]] || fail "compose.yaml gave $edge edge images, want 7"
own="$(jq -r '[.[] | select(.kind == "own")] | length' <<<"$real")"
[[ "$own" == 9 ]] || fail "compose.yaml gave $own own images, want 9 (seven at edge, two forks)"

echo "security-scan images test: pass"
