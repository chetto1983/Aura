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
    # `-r` alone accepts a directory: sha256sum then fails with "Is a directory" inside a
    # command substitution used as a printf argument, where `set -euo pipefail` cannot reach
    # it, so a hashless line was reaching the manifest. Require a regular file first.
    if [ ! -f "$repo_root/$rel" ] || [ ! -r "$repo_root/$rel" ]; then
      echo "FAIL: payload entry is missing, unreadable, or not a regular file: $rel" >&2
      return 1
    fi
    # Assign before printing so ANY hashing failure -- not just the directory case above --
    # is caught here instead of writing a blank hash for a line that still counts toward
    # "wrote N payload hashes".
    hash="$(sha256sum "$repo_root/$rel" | cut -d' ' -f1)"
    if [ -z "$hash" ]; then
      echo "FAIL: could not hash payload file: $rel" >&2
      return 1
    fi
    printf '%s  %s\n' "$hash" "$rel"
  done < <(LC_ALL=C sort <<< "$payload_rels")
}

if [ "${1:-}" = "--write" ]; then
  # The payload SET comes from install.sh, so a modified install.sh can point at files that
  # do not exist in HEAD yet; it is checked alongside the files it names.
  #
  # Tracked-ness first, cleanliness second. `git status --porcelain` reports neither
  # ignored paths nor a file git has never heard of in some configurations, and chasing
  # those states one at a time is how this guard has now been wrong twice. CI builds from a
  # fresh clone, so the invariant that actually matters is that git HAS the file at all.
  missing=""
  dirty=""
  while read -r rel; do
    if [ -z "$rel" ]; then continue; fi
    if ! git -C "$repo_root" ls-files --error-unmatch -- "$rel" >/dev/null 2>&1; then
      missing="$missing $rel"
      continue
    fi
    # Report git's own status line (` D compose.yaml`, `M compose.yaml`, ...) instead of
    # collapsing every kind of difference from HEAD into "modified" -- a deleted tracked
    # file was being described as edited, which names the wrong state to the operator.
    status="$(git -C "$repo_root" status --porcelain -- "$rel")"
    if [ -n "$status" ]; then dirty="$dirty
  ${status}"; fi
  done <<< "scripts/install.sh
$payload_rels"
  if [ -n "$missing" ]; then
    echo "FAIL: these payload files are not tracked by git, so a fresh clone cannot build the artifact:$missing" >&2
    exit 1
  fi
  if [ -n "$dirty" ]; then
    echo "FAIL: refusing to freeze a manifest while payload files differ from HEAD:$dirty" >&2
    echo "      commit them first -- a manifest of uncommitted bytes fails in CI." >&2
    exit 1
  fi

  # Compute into a variable before touching the manifest: `compute > "$manifest"` truncates
  # the tracked file up front, so a mid-computation failure left it empty on disk with
  # nothing said about it.
  new_manifest="$(compute)" || exit 1
  printf '%s\n' "$new_manifest" > "$manifest"
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
