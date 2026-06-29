#!/usr/bin/env bash
# F-015 (SEC-07): reject standalone-root `./...` in Go build/test/vet/run + govulncheck
# invocations inside CI workflows. Go's `./...` follows the Go examples vendored under
# web/node_modules, so a gate run with raw `./...` silently exercises a different package
# set than the owned-surface gates — a falsely-green assurance hole. Every such gate must
# source its package list from $(bash scripts/go_packages.sh) — the Makefile's GO_PACKAGES.
#
# Scope: .github/workflows/ ONLY (override via AURA_CI_WORKFLOWS_DIR so the self-test can
# target a tmp fixture). scripts/go_packages.sh's OWN `go list ./...` enumeration lives in
# scripts/ and is the legitimate package-list source — it is never scanned, and `go list`
# lines are excluded by token, so enumeration is never confused with a gate invocation.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

SCAN_DIR="${AURA_CI_WORKFLOWS_DIR:-.github/workflows}"

if [[ ! -d "$SCAN_DIR" ]]; then
  echo "F-015: scan dir '$SCAN_DIR' not found" >&2
  exit 2
fi

# Anchored standalone-root token: a bare `./...` bounded by whitespace/line-edges. Catches
# `go test ./...` but NOT scoped specs (`./internal/db/...`). Comment lines (`: #`) and
# `go list` enumeration (the legitimate package-list source) are excluded.
matches="$(grep -rnE '(^|[[:space:]])\./\.\.\.([[:space:]]|$)' "$SCAN_DIR" 2>/dev/null \
  | grep -vE ':[[:space:]]*#' \
  | grep -vE 'go list[[:space:]]' || true)"

if [[ -n "$matches" ]]; then
  {
    echo "F-015: raw root './...' found in a CI workflow — use \$(bash scripts/go_packages.sh):"
    printf '%s\n' "$matches"
  } >&2
  exit 1
fi

echo "F-015: no raw root './...' in $SCAN_DIR"
