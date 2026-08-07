#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

fixture="$fixture_root/qwen3.5-9b.gguf"
target="$fixture_root/cache/Qwen3.5-9B-UD-Q4_K_XL.gguf"
printf 'GGUF fixture payload for the pinned-size/sha256 fetch path' > "$fixture"
fixture_bytes="$(wc -c < "$fixture" | tr -d ' ')"
fixture_sha256="$(sha256sum "$fixture" | awk '{print $1}')"

bash "$repo_root/scripts/fetch_llm_model.sh" "$target" "file://$fixture" "$fixture_bytes" "$fixture_sha256"
cmp "$fixture" "$target"

bash "$repo_root/scripts/fetch_llm_model.sh" "$target" "http://127.0.0.1:1/unreachable" "$fixture_bytes" "$fixture_sha256"
cmp "$fixture" "$target"

printf 'broken cache entry' > "$target"
bash "$repo_root/scripts/fetch_llm_model.sh" "$target" "file://$fixture" "$fixture_bytes" "$fixture_sha256"
cmp "$fixture" "$target"

if bash "$repo_root/scripts/fetch_llm_model.sh" \
  "$fixture_root/cache/wrong-size.gguf" "file://$fixture" "$((fixture_bytes + 1))" "$fixture_sha256" \
  >/dev/null 2>&1; then
  echo "expected a size mismatch to be rejected" >&2
  exit 1
fi
if compgen -G "$fixture_root/cache/*.partial.*" >/dev/null; then
  echo "partial downloads must be removed after a size-mismatch failure" >&2
  exit 1
fi

if bash "$repo_root/scripts/fetch_llm_model.sh" \
  "$fixture_root/cache/wrong-sha.gguf" "file://$fixture" "$fixture_bytes" "0000000000000000000000000000000000000000000000000000000000000000" \
  >/dev/null 2>&1; then
  echo "expected a sha256 mismatch to be rejected" >&2
  exit 1
fi
if compgen -G "$fixture_root/cache/*.partial.*" >/dev/null; then
  echo "partial downloads must be removed after a sha256-mismatch failure" >&2
  exit 1
fi

printf 'not gguf at all' > "$fixture_root/invalid.gguf"
if bash "$repo_root/scripts/fetch_llm_model.sh" \
  "$fixture_root/cache/invalid.gguf" "file://$fixture_root/invalid.gguf" "$fixture_bytes" "$fixture_sha256" \
  >/dev/null 2>&1; then
  echo "expected a missing GGUF magic to be rejected" >&2
  exit 1
fi

if ! grep -q '5966095584' "$repo_root/scripts/fetch_llm_model.sh"; then
  echo "the pinned Qwen3.5-9B-UD-Q4_K_XL byte size must stay in the script's default, not just in this test" >&2
  exit 1
fi
if ! grep -q '6f5d30666c2d8ae16a306e616d95341dcf3cc46810df84d7e6f5a7d1e4c1b293' "$repo_root/scripts/fetch_llm_model.sh"; then
  echo "the pinned Qwen3.5-9B-UD-Q4_K_XL sha256 must stay in the script's default" >&2
  exit 1
fi
if ! grep -q 'AURA_LLM_MODEL_PATH' "$repo_root/compose.yaml"; then
  echo "aura-llm must serve a pre-fetched local path, not --hf-repo" >&2
  exit 1
fi
if grep -q -- '--hf-repo' "$repo_root/compose.yaml"; then
  bad_line="$(grep -n -B4 -- '--hf-repo' "$repo_root/compose.yaml" | grep -c 'AURA_LLM_HF_REPO' || true)"
  [ "$bad_line" -eq 0 ] || { echo "aura-llm must not use --hf-repo (no egress-at-boot promise)" >&2; exit 1; }
fi

echo "ok: Qwen3.5-9B-UD-Q4_K_XL fetch verifies size+sha256 and atomically refreshes the cache"
