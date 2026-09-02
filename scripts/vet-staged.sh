#!/usr/bin/env bash
# Vet the packages containing the given Go files. Called by lefthook pre-commit with
# {staged_files}; safe to run standalone: scripts/vet-staged.sh file1.go file2.go ...
#
# Whole PACKAGES, not individual files -- `go vet` is a package-level analysis and refuses
# a partial file set -- but only the packages this commit actually touches. `go vet ./...`
# used to run here, and in a worktree shared with a second session it failed on THEIR
# in-progress file, blocking commits that had nothing to do with it and tempting whoever
# was blocked to reach for `git stash` or --no-verify. CI keeps the full sweep
# (.github/workflows/ci.yml runs `go vet $(bash scripts/go_packages.sh)` plus the tagged
# tiers), so nothing is lost -- the same trade the file-size hook already documents.
set -euo pipefail

if [ "$#" -eq 0 ]; then
  exit 0
fi

# A directory can reach us with no .go file left in it: the commit deleted the last one, or
# renamed the package away. `go vet` treats that as an error, so a deletion-only commit
# would fail a gate that has nothing to check.
dirs=()
while IFS= read -r dir; do
  if compgen -G "$dir/*.go" >/dev/null 2>&1; then
    dirs+=("./$dir")
  fi
done < <(for f in "$@"; do dirname "$f"; done | sort -u)

if [ "${#dirs[@]}" -eq 0 ]; then
  exit 0
fi

go vet "${dirs[@]}"
