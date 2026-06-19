#!/usr/bin/env bash
# check-file-size.sh — enforce the CLAUDE.md "no god class" cap (≤600 LOC).
# Runs against Go + TypeScript/TSX source files (tests included, matching the Go
# cap which is not test-exempt); sqlc-generated, vendored, node_modules, build
# `dist/`, and `*.d.ts` declaration trees are exempt.
#
# Usage:
#   bash scripts/check-file-size.sh              # default 600-LOC cap, fails on violation
#   bash scripts/check-file-size.sh 800          # override cap (e.g. transitional during refactor)
#
# Exit codes:
#   0 — all files within cap
#   1 — at least one file over the cap (full list printed)
#   2 — usage error

set -euo pipefail

CAP="${1:-600}"
if ! [[ "$CAP" =~ ^[0-9]+$ ]]; then
  echo "usage: $0 [cap]" >&2
  exit 2
fi

# Files to check: tracked Go + TS/TSX sources except generated, vendored, and
# build-output trees. (`*.d.ts` and `dist/` are generated; node_modules vendored.)
TARGETS=$(git ls-files '*.go' '*.ts' '*.tsx' \
  | grep -v -E '^internal/db/sqlc/' \
  | grep -v -E '^third_party/' \
  | grep -v -E '^vendor/' \
  | grep -v -E '(^|/)node_modules/' \
  | grep -v -E '(^|/)dist/' \
  | grep -v -E '\.d\.ts$' \
  || true)

if [ -z "$TARGETS" ]; then
  echo "check-file-size: no source files matched; nothing to check."
  exit 0
fi

# Iterate via a pipe-fed process substitution rather than a `<<< "$TARGETS"`
# here-string: on the Windows Git Bash (MSYS/busybox) shell the here-string
# mangles the final list entry (it re-feeds a "<tail>.go" fragment as a phantom
# iteration, e.g. transport_test.go -> "t.go", which `wc` then can't open and
# `set -e` turns into a false commit-blocking failure). Process substitution keeps
# the loop in the CURRENT shell so the violations counter + exit code still propagate.
violations=0
while IFS= read -r f; do
  [ -n "$f" ] || continue
  # `git ls-files` lists tracked paths that may be deleted-but-unstaged in the
  # working tree (e.g. a file split into siblings); skip what's no longer present.
  [ -f "$f" ] || continue
  lines=$(wc -l < "$f" | tr -d '[:space:]')
  if [ "$lines" -gt "$CAP" ]; then
    printf "OVER CAP: %s (%d LOC > %d)\n" "$f" "$lines" "$CAP"
    violations=$((violations + 1))
  fi
done < <(printf '%s\n' "$TARGETS")

if [ "$violations" -gt 0 ]; then
  echo ""
  echo "check-file-size: $violations file(s) exceed the ${CAP}-LOC cap." >&2
  echo "Refactor on touch per CLAUDE.md §Behavioral rules: split <name>_<concern>.{go,ts,tsx}." >&2
  exit 1
fi

echo "check-file-size: all source files within the ${CAP}-LOC cap."
