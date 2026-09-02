#!/usr/bin/env bash
#
# The artifact ships a copy of 25 repo files. Nothing else notices when one of them changes
# and the artifact is not rebuilt, so an appliance would install last month's compose.yaml
# against this month's images. This gate makes that impossible to do quietly.
#
# It watches the INPUTS, not the artifact: makeself makes no reproducible-output promise,
# so diffing the built archive would flap and be switched off within a fortnight.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="$repo_root/scripts/payload_manifest.txt"

# shellcheck source=scripts/payload_files.sh
source "$repo_root/scripts/payload_files.sh"

# Resolved once, at the top level of the script (not inside a process substitution), so the
# $-literal guard in payload_files can actually stop the run -- see payload_files.sh for why
# `< <(payload_files ...)` would let that guard fail silently instead.
payload_rels="$(payload_files "$repo_root")" || exit 1

compute() {
  while read -r rel; do
    if [ -z "$rel" ]; then continue; fi
    printf '%s  %s\n' "$(sha256sum "$repo_root/$rel" | cut -d' ' -f1)" "$rel"
  done < <(sort <<< "$payload_rels")
}

if [ "${1:-}" = "--write" ]; then
  # compose.yaml (payload file #1) has sat modified in this worktree by a concurrent
  # session; freezing those in-progress bytes into the manifest would make CI red for a
  # reason nobody could explain once that session commits something different. Refuse
  # instead of guessing which bytes are "real".
  #
  # Compare against HEAD, not the index: bare `git diff` only sees unstaged-vs-index
  # changes, so a file the other session has `git add`-ed reads as clean even though it is
  # still uncommitted -- measured live in this worktree when compose.yaml got staged
  # mid-session and a bare `git diff --quiet` let --write sail through and freeze it.
  dirty=""
  while read -r rel; do
    if [ -z "$rel" ]; then continue; fi
    if ! git -C "$repo_root" diff --quiet HEAD -- "$rel"; then dirty="$dirty $rel"; fi
  done <<< "$payload_rels"
  if [ -n "$dirty" ]; then
    echo "FAIL: refusing to freeze a manifest while payload files are modified:$dirty" >&2
    echo "      commit or stash them first -- a manifest of uncommitted bytes fails in CI." >&2
    exit 1
  fi

  compute > "$manifest"
  echo "ok: wrote $(wc -l < "$manifest") payload hashes"
  exit 0
fi

if [ ! -f "$manifest" ]; then
  echo "FAIL: $manifest is missing; run make payload-manifest" >&2
  exit 1
fi

if ! diff -u "$manifest" <(compute); then
  echo "FAIL: the payload changed without its manifest. Run 'make payload-manifest' and commit." >&2
  exit 1
fi
echo "ok: payload matches its manifest ($(wc -l < "$manifest") files)"
