#!/usr/bin/env bash
# The body shared by the scripts that fetch ONE pinned artifact of a model repository
# (fetch_llm_model.sh, fetch_decide_model.sh): refuse anything that is not the published
# byte size and SHA-256 of one upstream commit, loudly, leaving no partial file in place.
# Sourced, never executed, so it is installed without an executable bit.
#
# The caller sets, before calling pinned_fetch_main "$@":
#   fetch_script_name  its own file name, for the usage line
#   artifact_names     the artifacts it knows, space separated; the first is the default
#   repo_base          the repository URL up to and including the pinned commit
#   pin_artifact       a function that sets official_file, official_bytes, official_sha256
#                      and gguf_magic (1 when the file must start with "GGUF") for $artifact,
#                      and fails for an artifact it does not know
# fetch_embedding_model.sh is a different contract (magic and tensor names, no size pin) and
# does not use this.

# The caller's variables are read here, never assigned.
# shellcheck disable=SC2154

pinned_fetch_valid() {
  local file="$1"
  [ -f "$file" ] || return 1
  if [ "$gguf_magic" = 1 ]; then
    [ "$(head -c 4 "$file")" = "GGUF" ] || return 1
  fi

  local actual_bytes
  actual_bytes="$(wc -c < "$file" | tr -d ' ')"
  [ "$actual_bytes" = "$expected_bytes" ] || return 1

  local actual_sha256
  actual_sha256="$(sha256sum "$file" | awk '{print $1}')"
  [ "$actual_sha256" = "$expected_sha256" ]
}

pinned_fetch_main() {
  local show_only=""
  artifact="${artifact_names%% *}"
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --artifact) artifact="${2:-}"; shift 2 ;;
      --show) show_only=1; shift ;;
      *) break ;;
    esac
  done

  if ! pin_artifact; then
    echo "unknown --artifact '$artifact' (expected: ${artifact_names// / | })" >&2
    exit 2
  fi
  official_url="${repo_base}/${official_file}"

  # --show answers "what exactly would this fetch?" without spending the egress to find out.
  # It is the only way to read the pin table for ONE artifact; grepping the file cannot
  # tell which selector a number belongs to.
  if [ -n "$show_only" ]; then
    printf 'artifact %s\nfile     %s\nurl      %s\nbytes    %s\nsha256   %s\n' \
      "$artifact" "$official_file" "$official_url" "$official_bytes" "$official_sha256"
    exit 0
  fi

  if [ "$#" -lt 1 ] || [ "$#" -gt 4 ]; then
    echo "usage: ${fetch_script_name} [--artifact ${artifact_names// /|}] [--show] TARGET [URL] [EXPECTED_BYTES] [EXPECTED_SHA256]" >&2
    exit 2
  fi

  local target="$1" file_url="${2:-$official_url}"
  expected_bytes="${3:-$official_bytes}"
  expected_sha256="${4:-$official_sha256}"
  # The name comes from the file in hand, never from a constant: an earlier version
  # announced one hardcoded model whatever it was handed, so its output proved nothing about
  # what it had actually verified.
  local name
  name="$(basename "$target")"
  mkdir -p "$(dirname "$target")"

  if pinned_fetch_valid "$target"; then
    echo "$name already present at $target"
    exit 0
  fi

  partial="$(mktemp "${target}.partial.XXXXXX")"
  trap 'rm -f "$partial"' EXIT

  echo "Downloading $name (${expected_bytes} bytes) to $target"
  curl -fsSL --retry 3 --retry-delay 2 "$file_url" -o "$partial"
  if ! pinned_fetch_valid "$partial"; then
    echo "FAIL: $name does not match the pinned artifact (expected ${expected_bytes} bytes, sha256 ${expected_sha256})" >&2
    exit 1
  fi

  mv -f "$partial" "$target"
  trap - EXIT
}
