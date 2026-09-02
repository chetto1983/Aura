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
    if [ ! -f "$repo_root/$rel" ]; then
      echo "FAIL: payload file is missing from disk: $rel" >&2
      return 1
    fi
    printf '%s  %s\n' "$(sha256sum "$repo_root/$rel" | cut -d' ' -f1)" "$rel"
  done < <(LC_ALL=C sort <<< "$payload_rels")
}

if [ "${1:-}" = "--write" ]; then
  # The payload SET comes from install.sh, so a modified install.sh can point at files that
  # do not exist in HEAD yet; it is checked alongside the files it names.
  #
  # `git status --porcelain` rather than `git diff HEAD`: git diff never considers UNTRACKED
  # paths, so a brand-new payload file added by a concurrent session read as clean and its
  # uncommitted bytes were frozen into the manifest -- the exact failure this guard exists
  # to stop, reached from the other direction.
  dirty=""
  while read -r rel; do
    if [ -z "$rel" ]; then continue; fi
    if [ -n "$(git -C "$repo_root" status --porcelain -- "$rel")" ]; then dirty="$dirty $rel"; fi
  done <<< "scripts/install.sh
$payload_rels"
  if [ -n "$dirty" ]; then
    echo "FAIL: refusing to freeze a manifest while payload files are modified or untracked:$dirty" >&2
    echo "      commit them first -- a manifest of uncommitted bytes fails in CI." >&2
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

# Checked command substitution, not `<(compute)`: a process substitution runs compute in a
# subshell whose exit status this script never sees, so its missing-file `return 1` would
# be discarded and surface only as a blank hash in the diff -- the same trap payload_files.sh
# documents for payload_files itself.
current="$(compute)" || exit 1
if ! diff -u "$manifest" <(printf '%s\n' "$current"); then
  echo "FAIL: the payload changed without its manifest. Run 'make payload-manifest' and commit." >&2
  exit 1
fi
echo "ok: payload matches its manifest ($(wc -l < "$manifest") files)"
