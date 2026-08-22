#!/usr/bin/env bash
# Fetches and verifies the exact GGUF pair aura-llm serves locally (compose.yaml): the
# gemma-4-12B-it QAT answer model AND the MTP drafter it speculates against, so the closing
# #115 gate never has to spend an OpenRouter credit.
#
# RULING 1 (task-7 dispatch, 2026-08-07): unlike fetch_embedding_model.sh -- which only
# checks the GGUF magic plus the two dense-projection tensor names, because a wrong
# EmbeddingGemma build still LOOKS structurally right -- the answering model has no such
# tell for a wrong quantization or a corrupted transfer. So this script pins the published
# byte size AND SHA-256 of one specific upstream commit and refuses anything else, loudly,
# leaving no partial file in place. The URL is pinned to that commit, not `main`, so a later
# upstream re-quantization under the same filename cannot silently swap what gets served;
# the size/sha256 checks are the actual guarantee regardless of how the bytes arrived.
#
# TWO artifacts, because the drafter is not optional dressing. Without it the model serves
# with no speculative decoding, which measured 3.66x slower wall-clock over six prompts on
# this host (2026-08-22, --spec-draft-n-max 4, /v1/chat/completions at production
# max_tokens). A missing or mismatched drafter is a silent performance cliff, never an
# error, so it earns the same pinning as the model itself.
#
# unsloth's UD (Unsloth Dynamic) Q4_K_XL quant of the QAT weights, not a plain Q4_K_M: this
# default was already on the unsloth UD family and stays on that pattern. Size/sha256/commit
# cross-verified from THREE independent sources on 2026-08-22 --
# https://huggingface.co/api/models/unsloth/gemma-4-12B-it-qat-GGUF?blobs=true
# (siblings[].lfs.sha256 + siblings[].size), a HEAD on each resolve URL
# (X-Linked-Size / X-Linked-ETag / X-Repo-Commit), and sha256sum over the bytes already
# resident in the aura_aura-llm volume. All three agreed exactly.
set -euo pipefail

repo_commit="980b060c40a8539ac159e0501a3e0f66a6365af3"
repo_base="https://huggingface.co/unsloth/gemma-4-12B-it-qat-GGUF/resolve/${repo_commit}"

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
    official_file="gemma-4-12B-it-qat-UD-Q4_K_XL.gguf"
    official_bytes=6716356800
    official_sha256="90fd44e29e0d7cffeb0fd00dc73cfdab9ed0b0e95306ecf7821ea634c940c370"
    ;;
  draft)
    official_file="mtp-gemma-4-12B-it.gguf"
    official_bytes=253708800
    official_sha256="fcb35dea42c71333db904cee11baac525c9ef872818ee3753f6cb156f3c6f4f6"
    ;;
  *)
    echo "unknown --artifact '$artifact' (expected: model | draft)" >&2
    exit 2
    ;;
esac
official_url="${repo_base}/${official_file}"

# --show answers "what exactly would this fetch?" without spending 6.7GB of egress to find
# out. It is the only way to read the pin table for ONE artifact; grepping the file cannot
# tell which selector a number belongs to.
if [ -n "$show_only" ]; then
  printf 'artifact %s\nfile     %s\nurl      %s\nbytes    %s\nsha256   %s\n' \
    "$artifact" "$official_file" "$official_url" "$official_bytes" "$official_sha256"
  exit 0
fi

if [ "$#" -lt 1 ] || [ "$#" -gt 4 ]; then
  echo "usage: fetch_llm_model.sh [--artifact model|draft] [--show] TARGET [URL] [EXPECTED_BYTES] [EXPECTED_SHA256]" >&2
  exit 2
fi

target="$1"
model_url="${2:-$official_url}"
expected_bytes="${3:-$official_bytes}"
expected_sha256="${4:-$official_sha256}"
# The name comes from the file in hand, never from a constant: the previous version
# announced one hardcoded model whatever it was handed, so its output proved nothing about
# what it had actually verified.
name="$(basename "$target")"
mkdir -p "$(dirname "$target")"

valid_model() {
  local model="$1"
  [ -f "$model" ] || return 1
  [ "$(head -c 4 "$model")" = "GGUF" ] || return 1

  local actual_bytes
  actual_bytes="$(wc -c < "$model" | tr -d ' ')"
  [ "$actual_bytes" = "$expected_bytes" ] || return 1

  local actual_sha256
  actual_sha256="$(sha256sum "$model" | awk '{print $1}')"
  [ "$actual_sha256" = "$expected_sha256" ]
}

if valid_model "$target"; then
  echo "$name already present at $target"
  exit 0
fi

partial="$(mktemp "${target}.partial.XXXXXX")"
trap 'rm -f "$partial"' EXIT

echo "Downloading $name (${expected_bytes} bytes) to $target"
curl -fsSL --retry 3 --retry-delay 2 "$model_url" -o "$partial"
if ! valid_model "$partial"; then
  echo "FAIL: $name does not match the pinned artifact (expected ${expected_bytes} bytes, sha256 ${expected_sha256})" >&2
  exit 1
fi

mv -f "$partial" "$target"
trap - EXIT
