#!/usr/bin/env bash
# deadcode_pre_push.sh — the pre-push entry point for the Go dead-code gate.
#
# It exists because the hook cannot express this inline. lefthook runs a step's `run:`
# through `env`, and a command carrying DOUBLE quotes nested inside the SINGLE-quoted
# `bash -c '…'` string is mangled before any shell sees it:
#
#   run: bash -c 'bash scripts/deadcode_gate.sh "$(go env GOPATH)/bin/deadcode" …'
#   → env: -c: line 1: unexpected EOF while looking for matching `''   (exit 2, ~0.2s)
#
# That failed CLOSED on every push while reporting nothing but a red step name, so it read
# as "dead code found" when the gate had not run at all. The neighbouring `build` step
# survives only because it has no inner quotes. Moving the binary resolution into a script
# removes the hazard rather than re-balancing it — the same shape `web-deadcode` already
# uses (scripts/check-deadcode-web.sh).
#
# CI (.github/workflows/ci.yml) and the Makefile keep calling deadcode_gate.sh directly with
# an explicit binary; that contract is unchanged.
set -euo pipefail

cd "$(dirname "$0")/.."

bin="${DEADCODE_BIN:-}"
if [ -z "$bin" ]; then
  bin="$(command -v deadcode || true)"
fi
if [ -z "$bin" ]; then
  bin="$(go env GOPATH)/bin/deadcode"
fi

exec bash scripts/deadcode_gate.sh "$bin" -test $(bash scripts/go_packages.sh)
