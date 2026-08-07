#!/usr/bin/env bash
# Fetches and verifies the exact Qwen3.5-9B-UD-Q4_K_XL GGUF that aura-llm serves locally
# (compose.yaml), so the closing #115 gate never has to spend an OpenRouter credit.
#
# RULING 1 (task-7 dispatch, 2026-08-07): unlike fetch_embedding_model.sh -- which only
# checks the GGUF magic plus the two dense-projection tensor names, because a wrong
# EmbeddingGemma build still LOOKS structurally right -- the answering model has no such
# tell for a wrong quantization or a corrupted transfer. So this script pins the
# published byte size AND SHA-256 of one specific upstream commit and refuses anything
# else, loudly, leaving no partial file in place. The URL is pinned to that commit, not
# `main`, so a later upstream re-quantization under the same filename cannot silently
# swap what gets served; the size/sha256 checks are the actual guarantee regardless of
# how the bytes arrived.
#
# unsloth's UD (Unsloth Dynamic) Q4_K_XL quant, not a plain Q4_K_M: compose already
# defaulted to the unsloth UD family before this task (Qwen3-8B-UD-Q4_K_XL.gguf), so this
# stays on the house pattern rather than switching quant families. Size/sha256/commit
# cross-verified from TWO independent HuggingFace sources on 2026-08-07: a HEAD on the
# resolve URL (X-Linked-Size / X-Linked-ETag / X-Repo-Commit) and
# https://huggingface.co/api/models/unsloth/Qwen3.5-9B-GGUF?blobs=true (siblings[].lfs.sha256
# + siblings[].size); both agreed exactly.
set -euo pipefail

official_url="https://huggingface.co/unsloth/Qwen3.5-9B-GGUF/resolve/3885219b6810b007914f3a7950a8d1b469d598a5/Qwen3.5-9B-UD-Q4_K_XL.gguf"
official_bytes=5966095584
official_sha256="6f5d30666c2d8ae16a306e616d95341dcf3cc46810df84d7e6f5a7d1e4c1b293"

if [ "$#" -lt 1 ] || [ "$#" -gt 4 ]; then
  echo "usage: fetch_llm_model.sh TARGET [URL] [EXPECTED_BYTES] [EXPECTED_SHA256]" >&2
  exit 2
fi

target="$1"
model_url="${2:-$official_url}"
expected_bytes="${3:-$official_bytes}"
expected_sha256="${4:-$official_sha256}"
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
  echo "Qwen3.5-9B-UD-Q4_K_XL model already present at $target"
  exit 0
fi

partial="$(mktemp "${target}.partial.XXXXXX")"
trap 'rm -f "$partial"' EXIT

echo "Downloading Qwen3.5-9B-UD-Q4_K_XL (${expected_bytes} bytes) to $target"
curl -fsSL --retry 3 --retry-delay 2 "$model_url" -o "$partial"
if ! valid_model "$partial"; then
  echo "FAIL: downloaded model does not match the pinned artifact (expected ${expected_bytes} bytes, sha256 ${expected_sha256})" >&2
  exit 1
fi

mv -f "$partial" "$target"
trap - EXIT
