#!/usr/bin/env bash
# Phase 7 (Slice 5) D-43 live web_search smoke against the running SearXNG stack.
#
# This script ACTUALLY RUNS `aura web tool web_search` against the in-network
# SearXNG container (reached via SEARXNG_URL, NO host port — D-02/D-03) and
# asserts a ranked result list comes back. It is the operator/CI surface for SC#1.
#
# It is fail-loud (CLAUDE.md NO-SKIP-AS-GREEN): when SEARXNG_URL is unset it exits
# non-zero under $CI (a CI job that cannot reach the backend is misconfigured,
# never a silent pass) and prints a clear hint for a local developer.
#
# Invoke (stack up, SEARXNG_URL exported):  bash scripts/web_search_smoke.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

if [[ -z "${SEARXNG_URL:-}" ]]; then
  if [[ -n "${CI:-}" ]]; then
    echo "FAIL: SEARXNG_URL unset under \$CI — the web_search smoke must run live (no-skip-as-green)" >&2
    exit 1
  fi
  echo "SKIP: SEARXNG_URL unset — bring the SearXNG stack up and export SEARXNG_URL to run this smoke" >&2
  exit 0
fi

BIN="$(mktemp -d)/aura"
go build -o "$BIN" ./cmd/aura

echo "==> aura web doctor"
"$BIN" web doctor

echo "==> D-43: aura web tool web_search (site:wikipedia.org)"
OUT="$("$BIN" web tool web_search '{"query":"site:wikipedia.org SearXNG","max_results":3}' 2>&1)"
printf '%s\n' "$OUT"

# The CLI prints "N result(s):" then a numbered list. A live search must return
# at least one ranked result (an empty list or a blocked/unavailable line fails).
if printf '%s\n' "$OUT" | grep -Eq 'blocked/unavailable'; then
  echo "FAIL: web_search returned an unavailable/blocked result against the live stack" >&2
  exit 1
fi
COUNT="$(printf '%s\n' "$OUT" | sed -n 's/^\([0-9][0-9]*\) result(s):.*/\1/p')"
if [[ -z "$COUNT" || "$COUNT" -lt 1 ]]; then
  echo "FAIL: web_search returned zero ranked results" >&2
  exit 1
fi

echo "==> web_search_smoke: PASS (${COUNT} ranked result(s))"
