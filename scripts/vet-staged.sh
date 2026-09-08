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
#
# A directory can ALSO reach us with .go files present but every one of them excluded by
# its own build constraint (scripts/ is entirely `//go:build ignore` main-package scripts,
# per CLAUDE.md's tool-design convention) -- `go vet ./dir` refuses THAT with "build
# constraints exclude all Go files", which is not a vet finding, it is "there is nothing to
# vet here". Measured 2026-09-08 (01-06 Task 2 RED commit): a commit touching only
# scripts/musr_live_run_assert.go + scripts/musr_live_run_assert_test.go (both `//go:build
# ignore`) hit exactly this and blocked on a directory with zero buildable files, the same
# false-block shape this script's own header already documents for the "no .go file at all"
# case. `go list` is the authority `compgen`'s glob cannot be: it resolves build tags.
dirs=()
while IFS= read -r dir; do
  if compgen -G "$dir/*.go" >/dev/null 2>&1 && go list "./$dir" >/dev/null 2>&1; then
    dirs+=("./$dir")
  fi
done < <(for f in "$@"; do dirname "$f"; done | sort -u)

if [ "${#dirs[@]}" -eq 0 ]; then
  exit 0
fi

go vet "${dirs[@]}"
