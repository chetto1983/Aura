#!/usr/bin/env bash
# Runs go test and records what it executed for the test ledger
# (docs/superpowers/specs/2026-11-25-test-ledger-design.md §4.2):
#
#   go_test_ledger.sh LABEL [go test arguments...]
#
# appends the `go test -json` events to $AURA_TEST_LEDGER_DIR/LABEL.jsonl (a reused label adds to the
# file, it never erases a previous run) and prints the log `go test -v` prints. Compile errors are in
# it: -json reports them as build-output events, not output ones (measured on go1.27.4, 2026-11-26).
# The one line missing is the go command's closing bare FAIL, which has no event; the exit status,
# go test's, carries it. Under $CI an unset AURA_TEST_LEDGER_DIR is refused, because a run nobody
# records is one the ledger reports as not executed; elsewhere it runs plain go test.
set -euo pipefail

label="${1:-}"
if [[ ! "$label" =~ ^[A-Za-z0-9._-]+$ ]]; then
  echo "usage: go_test_ledger.sh LABEL [go test arguments...]; LABEL is letters, digits, '.', '_' or '-'" >&2
  exit 2
fi
shift

if [[ -z "${AURA_TEST_LEDGER_DIR:-}" ]]; then
  if [[ -n "${CI:-}" ]]; then
    echo "go_test_ledger.sh: AURA_TEST_LEDGER_DIR is unset under CI, so this run would never reach the test ledger" >&2
    exit 2
  fi
  exec go test "$@"
fi

# go test takes -C only as its first flag, so -json goes after it.
chdir=()
case "${1:-}" in
  -C) chdir=("$1" "${2:-}") && shift 2 ;;
  -C=*) chdir=("$1") && shift ;;
esac

mkdir -p "$AURA_TEST_LEDGER_DIR"
go test "${chdir[@]}" -json "$@" | tee -a "$AURA_TEST_LEDGER_DIR/$label.jsonl" |
  jq -rj 'select(.Action == "output" or .Action == "build-output") | .Output'
