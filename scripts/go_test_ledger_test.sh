#!/usr/bin/env bash
# Self-test of scripts/go_test_ledger.sh on four fixture packages (scripts/testdata/go_test_ledger:
# pass, fail, skip, and one that does not compile). Explicit paths build them in the main module, with
# its toolchain, while ./... never reaches them. It proves the recorded events, that the log reads as
# `go test -v`'s, the exit status, and the refusals.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
helper="scripts/go_test_ledger.sh"
fixtures="./scripts/testdata/go_test_ledger"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fail() {
  echo "go_test_ledger_test: $1" >&2
  exit 1
}

# Timings differ between two runs; nothing else may.
normalize() {
  sed -E 's/\([0-9.]+s\)/(T)/; s/\t[0-9.]+s$/\tT/'
}

# record NAME: runs the helper on fixture NAME into $work/ledger, leaving its log and exit status.
record() {
  set +e
  AURA_TEST_LEDGER_DIR="$work/ledger" bash "$helper" "$1" -count=1 "$fixtures/$1" \
    >"$work/$1.log" 2>"$work/$1.err"
  echo $? >"$work/$1.status"
  set -e
}

# events NAME ACTION: the tests (or "-" for the package) that reached ACTION in NAME's ledger file.
events() {
  jq -r --arg action "$2" 'select(.Action == $action) | .Test // "-"' "$work/ledger/$1.jsonl"
}

expect_status() {
  [[ "$(cat "$work/$1.status")" == "$2" ]] || fail "$1: exit $(cat "$work/$1.status"), want $2"
}

for name in pass fail skip broken; do
  record "$name"
  # -v writes compile errors to stderr and -json carries them as build-output events, which the
  # helper prints; the go command's closing bare FAIL has no event, the exit status carries it.
  set +e
  go test -v -count=1 "$fixtures/$name" 2>"$work/$name.v" >>"$work/$name.v.out"
  set -e
  cat "$work/$name.v.out" >>"$work/$name.v"
  if [[ "$(cat "$work/$name.status")" != 0 ]]; then sed -i '$ { /^FAIL$/d }' "$work/$name.v"; fi
  diff <(normalize <"$work/$name.v") <(normalize <"$work/$name.log") >"$work/$name.diff" ||
    fail "$name: the log is not go test -v's: $(cat "$work/$name.diff")"
done

expect_status pass 0
expect_status skip 0
expect_status fail 1
expect_status broken 1
[[ "$(events pass pass)" == $'TestPass\n-' ]] || fail "pass: want TestPass and its package to pass"
[[ "$(events skip skip)" == "TestSkip" ]] || fail "skip: want TestSkip skipped"
[[ "$(events fail fail)" == $'TestFail\n-' ]] || fail "fail: want TestFail and its package to fail"
[[ "$(events broken build-fail)" == "-" ]] || fail "broken: want a build-fail event"
grep -q 'cannot use "not an int"' "$work/broken.log" || fail "broken: the compile error is not in the log"

# A label used twice appends: the second run must not erase the first one's events.
record pass
[[ "$(events pass pass | grep -c '^TestPass$')" == 2 ]] || fail "a reused label must append"

# go test takes -C only as its first flag (tools/deadexport is tested that way).
for chdir in "-C $fixtures/pass" "-C=$fixtures/pass"; do
  # shellcheck disable=SC2086 # the flag and its value are two words in the first form
  AURA_TEST_LEDGER_DIR="$work/ledger" bash "$helper" chdir $chdir -count=1 . >/dev/null 2>"$work/chdir.err" ||
    fail "$chdir must stay go test's first flag: $(cat "$work/chdir.err")"
done
[[ "$(events chdir pass | grep -c '^TestPass$')" == 2 ]] || fail "-C runs must record their events"

set +e
CI=true AURA_TEST_LEDGER_DIR="" bash "$helper" unrecorded -count=1 "$fixtures/pass" >"$work/ci.log" 2>"$work/ci.err"
ci_status=$?
env -u CI AURA_TEST_LEDGER_DIR="" bash "$helper" local -count=1 "$fixtures/pass" >"$work/local.log" 2>&1
local_status=$?
AURA_TEST_LEDGER_DIR="$work/ledger" bash "$helper" ../escape -count=1 "$fixtures/pass" >/dev/null 2>&1
label_status=$?
set -e
[[ "$ci_status" == 2 ]] || fail "an unset AURA_TEST_LEDGER_DIR under CI must be refused, got exit $ci_status"
grep -q AURA_TEST_LEDGER_DIR "$work/ci.err" || fail "the CI refusal must name AURA_TEST_LEDGER_DIR"
[[ ! -s "$work/ci.log" ]] || fail "a refused run must not run the tests"
[[ "$local_status" == 0 ]] || fail "an unset AURA_TEST_LEDGER_DIR outside CI must run go test, got exit $local_status"
grep -q '^ok' "$work/local.log" || fail "outside CI an unrecorded run must print plain go test output"
[[ "$label_status" == 2 ]] || fail "a label with a path separator must be refused, got exit $label_status"
[[ ! -e "$work/escape.jsonl" ]] || fail "a refused label must write nothing"

echo "ok: go_test_ledger records every event, prints go test -v's log, and exits as go test does"
