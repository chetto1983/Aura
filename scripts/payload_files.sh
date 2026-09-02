#!/usr/bin/env bash
#
# Shared by build_installer.sh, build_installer_test.sh and payload_manifest_gate.sh so the
# 25-file payload list is derived in exactly one place. It used to be two independent copies
# of the same grep|awk, and a prior fix (the anchor now tolerates leading whitespace, so an
# indented download_file call inside an if/function is not silently dropped) landed in both
# only because someone remembered to update the second one by hand. A third copy would make
# three expressions that must never drift, which is worse than the duplication it replaces.

# Prints the payload's repo-relative paths, one per line, to stdout. Call it through a
# command substitution the caller checks, e.g.:
#   payload_rels="$(payload_files "$repo_root")" || exit 1
# NOT through process substitution (`< <(payload_files ...)`): that runs the function in a
# subshell whose exit status bash discards, so the $-literal guard below would fail silently
# instead of stopping the build.
payload_files() {
  local repo_root="$1"
  local rel
  while read -r rel; do
    case "$rel" in
      *'$'*)
        echo "FAIL: download_file '$rel' is not a literal path; the payload list cannot be derived from it" >&2
        return 1
        ;;
    esac
    printf '%s\n' "$rel"
  done < <(grep -E '^[[:space:]]*download_file ' "$repo_root/scripts/install.sh" | awk '{print $2}')
}
