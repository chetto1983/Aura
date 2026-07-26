#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "FATAL: deadcode gate requires a command" >&2
  exit 2
fi

output="$(mktemp)"
trap 'rm -f "$output"' EXIT

set +e
"$@" >"$output" 2>&1
status=$?
set -e

cat "$output"
if [ "$status" -ne 0 ]; then
  exit "$status"
fi
if [ -s "$output" ]; then
  echo "FATAL: deadcode reported unreachable symbols" >&2
  exit 1
fi
