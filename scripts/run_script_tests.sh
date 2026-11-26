#!/usr/bin/env bash
# Runs the repository's shell self-tests and records each one's outcome for the test ledger
# (docs/superpowers/specs/2026-11-25-test-ledger-design.md §4.4):
#
#   run_script_tests.sh [--list] [--except FILE]... [FILE]...
#
# With no FILE it runs every tracked scripts/**/*_test.sh outside testdata, so a new self-test runs
# because it exists. --except leaves a file to the job that has its prerequisite (a built image, a
# toolchain), which runs it by name through this same script. Each file run appends one line,
# "<path> pass|fail|skip <seconds>", to $AURA_TEST_LEDGER_DIR/script-tests.txt: exit 0 is pass, 77
# skip, anything else fail. Every file runs even after one fails, and the exit status is 1 if any did.
# --list prints the files it would run. Under $CI an unset AURA_TEST_LEDGER_DIR is refused, as
# scripts/go_test_ledger.sh refuses it; elsewhere the files run and nothing is recorded.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

refuse() {
  echo "run_script_tests.sh: $1" >&2
  exit 2
}

list=0
except=()
files=()
while (($#)); do
  case "$1" in
    --list) list=1 && shift ;;
    --except) except+=("${2:?--except needs a file}") && shift 2 ;;
    *) files+=("$1") && shift ;;
  esac
done

mapfile -t discovered < <(git ls-files 'scripts/*_test.sh' | grep -v '/testdata/')
for file in "${except[@]}"; do
  printf '%s\n' "${discovered[@]}" | grep -qxF "$file" || refuse "--except $file names no tracked self-test"
done
((${#files[@]})) || files=("${discovered[@]}")
for file in "${files[@]}"; do
  [[ -f "$file" ]] || refuse "$file does not exist"
done

selected=()
for file in "${files[@]}"; do
  printf '%s\n' "${except[@]}" | grep -qxF "$file" || selected+=("$file")
done
if ((list)); then
  printf '%s\n' "${selected[@]}"
  exit 0
fi

record=/dev/null
if [[ -n "${AURA_TEST_LEDGER_DIR:-}" ]]; then
  mkdir -p "$AURA_TEST_LEDGER_DIR"
  record="$AURA_TEST_LEDGER_DIR/script-tests.txt"
elif [[ -n "${CI:-}" ]]; then
  refuse "AURA_TEST_LEDGER_DIR is unset under CI, so these runs would never reach the test ledger"
fi

failed=0
for file in "${selected[@]}"; do
  echo "==> $file"
  started=$SECONDS
  set +e
  bash "$file"
  status=$?
  set -e
  case "$status" in
    0) outcome=pass ;;
    77) outcome=skip ;;
    *) outcome=fail && failed=1 ;;
  esac
  echo "$file $outcome $((SECONDS - started))" >>"$record"
done
exit "$failed"
