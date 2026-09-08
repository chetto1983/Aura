#!/usr/bin/env bash
# Regression test for scripts/lib/disposable_stack.sh (D-12). Runs WITHOUT a Docker
# daemon: every function exercised here is pure shell (the guard, read_secret, the
# composed-DSN exporter). The bring-up/teardown functions DO need Docker and are instead
# proven end-to-end by scripts/coverage_docker.sh's own real run (01-04 Task 1's
# <verify> block) — the extraction is only accepted once that gate produces the same
# result it did before.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

LIB_ABS="$(pwd)/scripts/lib/disposable_stack.sh"
FAIL=0
ASSERTIONS=0

assert_eq() {
  local desc="$1" expected="$2" actual="$3"
  ASSERTIONS=$((ASSERTIONS + 1))
  if [ "$expected" = "$actual" ]; then
    echo "PASS: $desc"
  else
    echo "FAIL: $desc (expected '$expected', got '$actual')"
    FAIL=1
  fi
}

assert_contains() {
  local desc="$1" haystack="$2" needle="$3"
  ASSERTIONS=$((ASSERTIONS + 1))
  if printf '%s' "$haystack" | grep -qF "$needle"; then
    echo "PASS: $desc"
  else
    echo "FAIL: $desc (expected to find '$needle' in: $haystack)"
    FAIL=1
  fi
}

# --- disposable_stack_guard_name -----------------------------------------------
set +e
bash -c "source '$LIB_ABS'; disposable_stack_guard_name aura" >/dev/null 2>&1
guard_rc=$?
set -e
assert_eq "guard exits 4 for the name 'aura'" "4" "$guard_rc"

set +e
bash -c "source '$LIB_ABS'; disposable_stack_guard_name aura_musr_test" >/dev/null 2>&1
guard_ok_rc=$?
set -e
assert_eq "guard exits 0 for a throwaway name" "0" "$guard_ok_rc"

# --- read_secret: exported value wins, .env is the fallback, CR is stripped ----
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
printf 'DISPOSABLE_STACK_TEST_KEY=from-dotenv\r\n' >"$TMP/.env"

exported_out="$(cd "$TMP" && DISPOSABLE_STACK_TEST_KEY=$'from-export\r' bash -c "source '$LIB_ABS'; read_secret DISPOSABLE_STACK_TEST_KEY")"
assert_eq "read_secret prefers an exported value over .env and strips a trailing CR" "from-export" "$exported_out"

fallback_out="$(cd "$TMP" && bash -c "source '$LIB_ABS'; read_secret DISPOSABLE_STACK_TEST_KEY")"
assert_eq "read_secret falls back to .env and strips a trailing CR" "from-dotenv" "$fallback_out"

# --- disposable_stack_export_env: composed DSNs carry the throwaway name/host/port ---
export_out="$(bash -c "
  source '$LIB_ABS'
  DISPOSABLE_STACK_DB=throwaway_db
  DISPOSABLE_STACK_HOST=127.0.0.1
  DISPOSABLE_STACK_PORT=5555
  disposable_stack_export_env testpw
  printf '%s|%s|%s' \"\$AURA_DB_URL\" \"\$AURA_DB_MIGRATE_URL\" \"\$AURA_DB_BOOTSTRAP_URL\"
")"
assert_contains "AURA_DB_URL/MIGRATE_URL/BOOTSTRAP_URL carry the throwaway database name" "$export_out" "throwaway_db"
assert_contains "AURA_DB_URL/MIGRATE_URL/BOOTSTRAP_URL carry the requested host" "$export_out" "127.0.0.1"
assert_contains "AURA_DB_URL/MIGRATE_URL/BOOTSTRAP_URL carry the requested port" "$export_out" "5555"

echo "==> $ASSERTIONS assertions checked"
if [ "$FAIL" -ne 0 ]; then
  echo "FAIL: scripts/lib/disposable_stack_test.sh had failing assertions" >&2
  exit 1
fi
echo "OK: all scripts/lib/disposable_stack.sh assertions passed"
