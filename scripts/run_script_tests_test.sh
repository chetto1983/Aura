#!/usr/bin/env bash
# Self-test of scripts/run_script_tests.sh on three fixtures (scripts/testdata/run_script_tests: one
# that passes, named *_test.sh so discovery could reach it, one that fails, one that exits 77) and on
# the real discovery, which must not run them.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
runner="scripts/run_script_tests.sh"
fixtures="scripts/testdata/run_script_tests"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fail() {
  echo "run_script_tests_test: $1" >&2
  exit 1
}

set +e
AURA_TEST_LEDGER_DIR="$work/ledger" bash "$runner" "$fixtures/passes_test.sh" "$fixtures/fails.sh" \
  "$fixtures/skips.sh" >"$work/run.log" 2>&1
status=$?
set -e
[[ "$status" == 1 ]] || fail "a failing file must make the run exit 1, got $status"
expected="$fixtures/passes_test.sh pass
$fixtures/fails.sh fail
$fixtures/skips.sh skip"
[[ "$(cut -d' ' -f1,2 "$work/ledger/script-tests.txt")" == "$expected" ]] ||
  fail "want one line per file, in order: $(cat "$work/ledger/script-tests.txt")"
awk '$3 !~ /^[0-9]+$/ { exit 1 }' "$work/ledger/script-tests.txt" || fail "the third field is whole seconds"
grep -q '^==> scripts/testdata/run_script_tests/fails.sh$' "$work/run.log" || fail "each file is announced in the log"
grep -q 'fixture failure' "$work/run.log" || fail "a file's own output stays in the log"

# Discovery is every tracked scripts/**/*_test.sh outside testdata, this self-test included.
discovered="$(bash "$runner" --list)"
want="$(git ls-files 'scripts/*_test.sh' | grep -v '/testdata/')"
[[ "$discovered" == "$want" ]] || fail "--list is not the tracked self-tests: $discovered"
grep -qx "scripts/run_script_tests_test.sh" <<<"$discovered" || fail "discovery must find this self-test"
grep -q '/testdata/' <<<"$discovered" && fail "discovery must not run fixtures: $fixtures/passes_test.sh is one"
[[ "$(bash "$runner" --list --except scripts/run_script_tests_test.sh | grep -c run_script_tests_test)" == 0 ]] ||
  fail "--except must leave a file out"

set +e
bash "$runner" --list --except scripts/no_such_test.sh >/dev/null 2>&1
stale_status=$?
bash "$runner" --list scripts/no_such_test.sh >/dev/null 2>&1
missing_status=$?
CI=true AURA_TEST_LEDGER_DIR="" bash "$runner" "$fixtures/passes_test.sh" >"$work/ci.log" 2>&1
ci_status=$?
env -u CI AURA_TEST_LEDGER_DIR="" bash "$runner" "$fixtures/passes_test.sh" >"$work/local.log" 2>&1
local_status=$?
set -e
[[ "$stale_status" == 2 ]] || fail "--except naming no tracked self-test must be refused, got $stale_status"
[[ "$missing_status" == 2 ]] || fail "a file that does not exist must be refused, got $missing_status"
[[ "$ci_status" == 2 ]] || fail "an unset AURA_TEST_LEDGER_DIR under CI must be refused, got $ci_status"
grep -q AURA_TEST_LEDGER_DIR "$work/ci.log" || fail "the CI refusal must name AURA_TEST_LEDGER_DIR"
grep -q 'fixture pass' "$work/ci.log" && fail "a refused run must run nothing"
[[ "$local_status" == 0 ]] || fail "outside CI an unrecorded run still runs, got $local_status"
grep -q 'fixture pass' "$work/local.log" || fail "outside CI the file must still run"

echo "ok: run_script_tests runs each self-test once, records its outcome, and refuses what it cannot record"
