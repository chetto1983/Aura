#!/usr/bin/env bash
# check-file-size.sh — enforce the CLAUDE.md "no god class" cap (≤600 LOC).
# Runs against Go + TypeScript/TSX source files (tests included, matching the Go
# cap which is not test-exempt); sqlc-generated, vendored, node_modules, build
# `dist/`, and `*.d.ts` declaration trees are exempt.
#
# Usage:
#   bash scripts/check-file-size.sh                    # whole tree (CI / make file-size)
#   bash scripts/check-file-size.sh 800                # override cap (e.g. transitional during refactor)
#   bash scripts/check-file-size.sh file1.go file2.tsx # only the given files (lefthook {staged_files})
#   bash scripts/check-file-size.sh 800 file1.go       # both
#
# Exit codes:
#   0 — all checked files within cap
#   1 — at least one file over the cap (full list printed)
#   2 — usage error

set -euo pipefail

CAP=600
if [ "$#" -gt 0 ] && [[ "$1" =~ ^[0-9]+$ ]]; then
  CAP="$1"
  shift
elif [ "$#" -gt 0 ] && [[ "$1" == -* ]]; then
  echo "usage: $0 [cap] [file...]" >&2
  exit 2
fi

# Source files this cap governs, minus generated, vendored, and build-output
# trees. (`*.d.ts` and `dist/` are generated; node_modules vendored.) Applied to
# both modes so a caller-supplied list (staged files) honours the same exemptions.
select_targets() {
  grep -E '\.(go|ts|tsx)$' \
    | grep -v -E '^internal/db/sqlc/' \
    | grep -v -E '^third_party/' \
    | grep -v -E '^vendor/' \
    | grep -v -E '(^|/)node_modules/' \
    | grep -v -E '(^|/)dist/' \
    | grep -v -E '\.d\.ts$' \
    || true
}

if [ "$#" -gt 0 ]; then
  TARGETS=$(printf '%s\n' "$@" | select_targets)
  MODE="named"
else
  TARGETS=$(git ls-files '*.go' '*.ts' '*.tsx' | select_targets)
  MODE="tree"
fi

# Drop paths absent from the working tree: `git ls-files` lists tracked paths that
# may be deleted-but-unstaged (e.g. a file split into siblings), and a missing path
# would make `wc` fail the whole run under `set -e`. The test is a shell builtin, so
# this filter costs no process spawns.
EXISTING=""
while IFS= read -r f; do
  [ -n "$f" ] || continue
  [ -f "$f" ] || continue
  EXISTING="${EXISTING}${f}"$'\n'
done < <(printf '%s\n' "$TARGETS")

if [ -z "$EXISTING" ]; then
  echo "check-file-size: no source files matched; nothing to check."
  exit 0
fi

COUNT=$(printf '%s' "$EXISTING" | grep -c '')
if [ "$MODE" = "named" ]; then
  SCOPE="$COUNT named file(s)"
else
  SCOPE="all $COUNT tracked source file(s)"
fi

# Count with batched `wc` rather than one spawn per file: on the Windows Git Bash
# (MSYS) shell a process spawn costs ~60ms, so per-file `wc` over ~2k tracked
# sources took >2 minutes and made this hook the slowest thing in pre-commit.
#
# Iterate via a pipe-fed process substitution rather than a `<<< "$…"` here-string:
# on that same shell the here-string mangles the final list entry (it re-feeds a
# "<tail>.go" fragment as a phantom iteration, e.g. transport_test.go -> "t.go",
# which `wc` then can't open and `set -e` turns into a false commit-blocking
# failure). Process substitution keeps the loop in the CURRENT shell so the
# violations counter + exit code still propagate.
violations=0
while read -r lines path; do
  # `wc` appends a "N total" summary line per batch of 2+ files; no tracked
  # source is named `total`, so skipping by name is unambiguous here.
  [ "$path" = "total" ] && continue
  if [ "$lines" -gt "$CAP" ]; then
    printf "OVER CAP: %s (%d LOC > %d)\n" "$path" "$lines" "$CAP"
    violations=$((violations + 1))
  fi
done < <(printf '%s' "$EXISTING" | tr '\n' '\0' | xargs -0 -r wc -l)

if [ "$violations" -gt 0 ]; then
  echo ""
  echo "check-file-size: $violations file(s) exceed the ${CAP}-LOC cap." >&2
  echo "Refactor on touch per CLAUDE.md §Behavioral rules: split <name>_<concern>.{go,ts,tsx}." >&2
  exit 1
fi

echo "check-file-size: ${SCOPE} within the ${CAP}-LOC cap."
