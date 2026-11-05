#!/usr/bin/env bash
# Fetches and verifies the two files the optional local decision sidecar (aura-llama-decide,
# compose.yaml) needs: Liquid AI's d1-3B GGUF and its LICENSE. The LFM Open License 1.0 asks
# (§4(a)) that the LICENSE travels with the weights, so it is pinned and fetched like the model.
#
# The model is the one the operator chose on 2026-11-03 after Laya multilingual failed G1.
# Like fetch_llm_model.sh, this pins the published byte size AND SHA-256 of one upstream
# commit and refuses anything else, loudly, leaving no partial file in place; the URL names the
# commit, not `main`, so a later upstream re-quantization under the same filename cannot swap
# what gets served. Size and SHA-256 are from the repo's tree API at that commit
# (https://huggingface.co/api/models/LiquidAI/d1-3B-GGUF/tree/<commit>, lfs.oid + size).
#
# The LICENSE has its own validation: valid_model checks the GGUF magic, which a text file
# does not carry, so it is judged on size and SHA-256 alone.
set -euo pipefail

repo_commit="bb1e436ea78eb96a3f1acb6da865f70c2fbeb563"
repo_base="https://huggingface.co/LiquidAI/d1-3B-GGUF/resolve/${repo_commit}"

artifact="model"
show_only=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --artifact) artifact="${2:-}"; shift 2 ;;
    --show) show_only=1; shift ;;
    *) break ;;
  esac
done

case "$artifact" in
  model)
    official_file="d1-3B-Q4_K_M.gguf"
    official_bytes=1674456672
    official_sha256="16aff27ea2eefdc32b9897f43854a5d3170c1dc8dccb9c756905af30a4e22402"
    ;;
  license)
    official_file="LICENSE"
    official_bytes=10574
    official_sha256="f684ace3fe08f9030789383dcb38792f23a74c575ef024e8c2ef76c09f536248"
    ;;
  *)
    echo "unknown --artifact '$artifact' (expected: model | license)" >&2
    exit 2
    ;;
esac
official_url="${repo_base}/${official_file}"

# --show answers "what exactly would this fetch?" without spending 1.6GB of egress to find out;
# the installer reads the SHA-256 from it to judge a file already in the volume.
if [ -n "$show_only" ]; then
  printf 'artifact %s\nfile     %s\nurl      %s\nbytes    %s\nsha256   %s\n' \
    "$artifact" "$official_file" "$official_url" "$official_bytes" "$official_sha256"
  exit 0
fi

if [ "$#" -lt 1 ] || [ "$#" -gt 4 ]; then
  echo "usage: fetch_decide_model.sh [--artifact model|license] [--show] TARGET [URL] [EXPECTED_BYTES] [EXPECTED_SHA256]" >&2
  exit 2
fi

target="$1"
file_url="${2:-$official_url}"
expected_bytes="${3:-$official_bytes}"
expected_sha256="${4:-$official_sha256}"
name="$(basename "$target")"
mkdir -p "$(dirname "$target")"

valid_file() {
  local file="$1"
  [ -f "$file" ] || return 1
  if [ "$artifact" = "model" ]; then
    [ "$(head -c 4 "$file")" = "GGUF" ] || return 1
  fi

  local actual_bytes
  actual_bytes="$(wc -c < "$file" | tr -d ' ')"
  [ "$actual_bytes" = "$expected_bytes" ] || return 1

  local actual_sha256
  actual_sha256="$(sha256sum "$file" | awk '{print $1}')"
  [ "$actual_sha256" = "$expected_sha256" ]
}

if valid_file "$target"; then
  echo "$name already present at $target"
  exit 0
fi

partial="$(mktemp "${target}.partial.XXXXXX")"
trap 'rm -f "$partial"' EXIT

echo "Downloading $name (${expected_bytes} bytes) to $target"
curl -fsSL --retry 3 --retry-delay 2 "$file_url" -o "$partial"
if ! valid_file "$partial"; then
  echo "FAIL: $name does not match the pinned artifact (expected ${expected_bytes} bytes, sha256 ${expected_sha256})" >&2
  exit 1
fi

mv -f "$partial" "$target"
trap - EXIT
