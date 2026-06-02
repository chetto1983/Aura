#!/usr/bin/env bash
# Phase 7 (Slice 5) SC#3 SSRF-block smoke + non-leak gate.
#
# This script ACTUALLY RUNS `aura web tool web_fetch` against the four canonical
# SSRF targets and asserts each one is rejected with the stable `blocked_url`
# error code BEFORE any dial (D-26..D-30) — and that the sanitized output greps
# CLEAN of any resolved IP / internal host / response header (D-27). It is the
# CI-grep contract for SC#3.
#
#   - 169.254.169.254          — AWS/GCP/Azure IMDS (link-local metadata, D-24)
#   - [::ffff:169.254.169.254] — the IPv4-mapped IPv6 bypass (must Unmap-then-classify, D-24/Pitfall)
#   - [fe80::1]                — IPv6 link-local
#   - metadata.google.internal — the GCP metadata HOSTNAME (hostname blocklist, D-24)
#
# The fetch path is independent of SEARXNG_URL (the SSRF gate fires before any
# search backend is touched), so this runs without a live SearXNG container.
#
# Invoke (repo root):  bash scripts/ssrf_smoke.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# Build once; reuse the binary for every target (faster + deterministic).
BIN="$(mktemp -d)/aura"
go build -o "$BIN" ./cmd/aura

TARGETS=(
  'http://169.254.169.254/latest/meta-data/'
  'http://[::ffff:169.254.169.254]/'
  'http://[fe80::1]/'
  'http://metadata.google.internal/'
)

# Concrete leak tokens the sanitized output must NEVER contain (the sanitized
# reason names a CLASS — "private_or_metadata_target" — which is allowed; a
# resolved IP literal, a header echo, or the internal ip=/host=/redirect= debug
# fields are leaks).
LEAK_RE='(\bip=|\bhost=|\bredirect=|Set-Cookie|X-Forwarded|127\.0\.0\.1|\b::1\b)'

fail=0
for url in "${TARGETS[@]}"; do
  out="$("$BIN" web tool web_fetch "{\"url\":\"${url}\"}" 2>&1 || true)"
  printf '==> %s\n%s\n' "$url" "$out"

  if ! printf '%s\n' "$out" | grep -q "blocked_url"; then
    echo "FAIL: ${url} did not return blocked_url" >&2
    fail=1
  fi
  if printf '%s\n' "$out" | grep -Eq "$LEAK_RE"; then
    echo "FAIL: ${url} output leaked a resolved IP / host / header (D-27)" >&2
    fail=1
  fi
done

if [[ "$fail" -ne 0 ]]; then
  echo "==> ssrf_smoke: FAIL" >&2
  exit 1
fi
echo "==> ssrf_smoke: PASS (4/4 blocked_url, grep-clean of IP/host/header leaks)"
