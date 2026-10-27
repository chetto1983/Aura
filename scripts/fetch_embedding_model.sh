#!/usr/bin/env bash

set -euo pipefail

official_url="https://huggingface.co/ggml-org/embeddinggemma-300M-GGUF/resolve/main/embeddinggemma-300M-Q8_0.gguf"

if [ "$#" -lt 1 ] || [ "$#" -gt 3 ]; then
  echo "usage: fetch_embedding_model.sh TARGET [URL [SHA256]]" >&2
  exit 2
fi

target="$1"
model_url="${2:-$official_url}"
# SHA256 pins the exact artifact, AURA_EMBED_FINGERPRINT: a file that hashes to anything
# else is not kept, and a cached one is replaced. Without it the checks below stop at "an
# EmbeddingGemma GGUF with its dense projections", which every build of it is.
expected_sha="$(printf '%s' "${3:-}" | tr '[:upper:]' '[:lower:]')"
mkdir -p "$(dirname "$target")"

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  else
    shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

valid_model() {
  local model="$1"
  local expected_bytes="${2:-}"
  [ -f "$model" ] || return 1
  [ "$(head -c 4 "$model")" = "GGUF" ] || return 1

  local actual_bytes
  actual_bytes="$(wc -c < "$model" | tr -d ' ')"
  if [ -n "$expected_bytes" ] && [ "$actual_bytes" != "$expected_bytes" ]; then
    return 1
  fi

  local dense_names dense_count
  dense_names="$(head -c 16000000 "$model" | grep -aoE 'dense_[23][.]weight' || true)"
  dense_count="$(printf '%s\n' "$dense_names" | sed '/^$/d' | sort -u | wc -l | tr -d ' ')"
  [ "$dense_count" -ge 2 ] || return 1
  [ -z "$expected_sha" ] || [ "$(sha256_of "$model")" = "$expected_sha" ]
}

if valid_model "$target"; then
  echo "EmbeddingGemma model already present at $target"
  exit 0
fi

expected_bytes="$(curl -fsIL "$model_url" 2>/dev/null \
  | awk 'BEGIN{IGNORECASE=1} /^x-linked-size:/ { gsub(/[^0-9]/,"",$2); print $2; exit }' || true)"
partial="$(mktemp "${target}.partial.XXXXXX")"
trap 'rm -f "$partial"' EXIT

echo "Downloading EmbeddingGemma Q8_0 to $target"
curl -fsSL --retry 3 --retry-delay 2 "$model_url" -o "$partial"
if ! valid_model "$partial" "$expected_bytes"; then
  echo "FAIL: downloaded model is not the expected EmbeddingGemma GGUF with dense projections${expected_sha:+ hashing to $expected_sha}" >&2
  exit 1
fi

mv -f "$partial" "$target"
trap - EXIT
