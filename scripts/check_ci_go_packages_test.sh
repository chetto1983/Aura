#!/usr/bin/env bash
# Negative self-test for scripts/check_ci_go_packages.sh (F-015).
#
# Proves the lint PROVABLY exits non-zero on a planted raw `go test ./...` (no-skip-as-green:
# a gate that can never fail is worthless), and exits 0 on a clean fixture. Mirrors
# scripts/cache_invariant_negative_test.sh: point the gate at a temporary fixture and assert rc.
#
# The raw-root token is assembled from $ROOT_PKGS (a quoted literal) rather than written
# inline, so THIS script's own source never carries a standalone-root `./...` — otherwise
# the lint would flag the self-test when scoped at scripts/ (F-015's own scope-correctness).
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# Quoted so the bounded token only ever lands in the WRITTEN fixture, never this source line.
ROOT_PKGS='./...'

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

run_lint() {
  AURA_CI_WORKFLOWS_DIR="$1" bash scripts/check_ci_go_packages.sh
}

# --- Case 1: a planted standalone-root `go test ./...` MUST make the lint exit non-zero.
VIOLATION_DIR="$TMP/violation"
mkdir -p "$VIOLATION_DIR"
{
  echo "jobs:"
  echo "  unit:"
  echo "    steps:"
  echo "      - name: go test"
  echo "        run: go test -race -count=1 ${ROOT_PKGS}"
} > "$VIOLATION_DIR/bad.yml"

set +e
BAD_OUT="$(run_lint "$VIOLATION_DIR" 2>&1)"
BAD_CODE=$?
set -e

if [[ "$BAD_CODE" -eq 0 ]]; then
  echo "FAIL (F-015): the lint exited 0 on a planted 'go test ./...' — IT IS SILENTLY GREEN." >&2
  printf '%s\n' "$BAD_OUT" >&2
  exit 1
fi
if ! printf '%s\n' "$BAD_OUT" | grep -q 'F-015'; then
  echo "FAIL (F-015): lint exited ${BAD_CODE} but never said 'F-015' (wrong failure)." >&2
  printf '%s\n' "$BAD_OUT" >&2
  exit 1
fi
echo "ok (F-015 case 1): planted '${ROOT_PKGS}' -> lint exit ${BAD_CODE} with 'F-015'"

# --- Case 2: a CLEAN fixture (scoped specs, helper-sourced args, `go list` enumeration)
# MUST exit 0 — no false positives on the legitimate forms.
CLEAN_DIR="$TMP/clean"
mkdir -p "$CLEAN_DIR"
{
  echo "jobs:"
  echo "  unit:"
  echo "    steps:"
  echo "      - name: go test scoped"
  echo "        run: go test -race -count=1 ./internal/db/..."
  echo "      - name: go vet helper-sourced"
  echo '        run: go vet $(bash scripts/go_packages.sh)'
  echo "      - name: enumerate (legitimate)"
  echo "        run: go list ${ROOT_PKGS}"
} > "$CLEAN_DIR/good.yml"

set +e
GOOD_OUT="$(run_lint "$CLEAN_DIR" 2>&1)"
GOOD_CODE=$?
set -e

if [[ "$GOOD_CODE" -ne 0 ]]; then
  echo "FAIL (F-015): the lint exited ${GOOD_CODE} on a CLEAN fixture — false positive." >&2
  printf '%s\n' "$GOOD_OUT" >&2
  exit 1
fi
echo "ok (F-015 case 2): clean scoped/helper/go-list fixture -> lint exit 0"

echo "==> check_ci_go_packages_test: PASS (the F-015 lint is NOT silently green)"
