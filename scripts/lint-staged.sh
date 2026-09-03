#!/usr/bin/env bash
# Lint the packages containing the given Go files. Called by lefthook pre-commit with
# {staged_files}; safe to run standalone: scripts/lint-staged.sh file1.go file2.go ...
#
# Whole PACKAGES, not individual files -- golangci-lint's cross-file analyses (dupl,
# deadcode-adjacent linters, staticcheck) need the whole package to be sound -- but only
# the packages this commit actually touches. The full-module form used to run here, and
# in a worktree shared with a second session it failed on THEIR in-progress file,
# blocking commits that had nothing to do with it and tempting whoever was blocked to
# reach for `git stash` or --no-verify. Measured 2026-09-03: a compose-only commit was
# refused for a gofmt finding in cmd/arcadedb-mcp, a package it did not touch.
#
# Narrowing does not isolate a package from a broken DEPENDENCY -- the linters type-check
# those too, and that failure is real -- it removes the unrelated ones. CI keeps the full
# sweep (.github/workflows/ci.yml), so nothing is lost: the same trade vet-staged.sh and
# the file-size hook already document.
set -euo pipefail

if [ "$#" -eq 0 ]; then
  exit 0
fi

# A directory can reach us with no .go file left in it: the commit deleted the last one, or
# renamed the package away. Linting it would fail a gate that has nothing to check.
dirs=()
while IFS= read -r dir; do
  if compgen -G "$dir/*.go" >/dev/null 2>&1; then
    dirs+=("./$dir")
  fi
done < <(for f in "$@"; do dirname "$f"; done | sort -u)

if [ "${#dirs[@]}" -eq 0 ]; then
  exit 0
fi

golangci-lint run "${dirs[@]}"
