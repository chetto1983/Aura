#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
for mode in 0 1 invalid; do
  if env -u CLOUDFLARE_API_TOKEN AURA_E2E_CLOUDFLARE="$mode" CI=true \
    bash "$repo_root/scripts/cloudflare_tunnel_live_e2e.sh" >"$fixture/out" 2>&1; then
    echo "FAIL: $mode passed without live prerequisites" >&2; exit 1
  fi
  case "$mode" in
    0) grep -q 'BLOCKED: named-tunnel' "$fixture/out" ;;
    1) grep -q 'requires CLOUDFLARE_API_TOKEN' "$fixture/out" ;;
    invalid) grep -q 'must be 1' "$fixture/out" ;;
  esac
done
echo 'ok: live suite refuses absent opt-in and fails opted-in missing credentials before network or mutation'
