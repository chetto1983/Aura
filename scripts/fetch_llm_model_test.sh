#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="$repo_root/scripts/fetch_llm_model.sh"
fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

fixture="$fixture_root/payload.gguf"
target="$fixture_root/cache/gemma-4-12B-it-qat-UD-Q4_K_XL.gguf"
printf 'GGUF fixture payload for the pinned-size/sha256 fetch path' > "$fixture"
fixture_bytes="$(wc -c < "$fixture" | tr -d ' ')"
fixture_sha256="$(sha256sum "$fixture" | awk '{print $1}')"

bash "$script" "$target" "file://$fixture" "$fixture_bytes" "$fixture_sha256"
cmp "$fixture" "$target"

bash "$script" "$target" "http://127.0.0.1:1/unreachable" "$fixture_bytes" "$fixture_sha256"
cmp "$fixture" "$target"

printf 'broken cache entry' > "$target"
bash "$script" "$target" "file://$fixture" "$fixture_bytes" "$fixture_sha256"
cmp "$fixture" "$target"

# The script used to announce one hardcoded model name whatever file it was handed, so
# its output was evidence of nothing. It has to name the artifact actually in play.
out="$(bash "$script" "$target" "file://$fixture" "$fixture_bytes" "$fixture_sha256")"
case "$out" in
  *"gemma-4-12B-it-qat-UD-Q4_K_XL.gguf"*) ;;
  *) echo "the message must name the target it handled, got: $out" >&2; exit 1 ;;
esac
case "$out" in
  *Qwen*) echo "the message named a model it was never given, got: $out" >&2; exit 1 ;;
esac

draft_target="$fixture_root/cache/mtp-gemma-4-12B-it.gguf"
bash "$script" --artifact draft "$draft_target" "file://$fixture" "$fixture_bytes" "$fixture_sha256"
cmp "$fixture" "$draft_target"

if bash "$script" \
  "$fixture_root/cache/wrong-size.gguf" "file://$fixture" "$((fixture_bytes + 1))" "$fixture_sha256" \
  >/dev/null 2>&1; then
  echo "expected a size mismatch to be rejected" >&2
  exit 1
fi
if compgen -G "$fixture_root/cache/*.partial.*" >/dev/null; then
  echo "partial downloads must be removed after a size-mismatch failure" >&2
  exit 1
fi

if bash "$script" \
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
if bash "$script" \
  "$fixture_root/cache/invalid.gguf" "file://$fixture_root/invalid.gguf" "$fixture_bytes" "$fixture_sha256" \
  >/dev/null 2>&1; then
  echo "expected a missing GGUF magic to be rejected" >&2
  exit 1
fi

if bash "$script" --artifact nonsense "$fixture_root/cache/x.gguf" >/dev/null 2>&1; then
  echo "an unknown --artifact must be refused, not silently treated as the model" >&2
  exit 1
fi

# --show resolves the pin table without spending 6.7GB of egress to find out what it is.
# It is also how this test proves each artifact's pins are wired to its OWN selector
# rather than sitting in the file as dead text a grep would still find.
model_show="$(bash "$script" --artifact model --show)"
draft_show="$(bash "$script" --artifact draft --show)"

expect_show() {
  local label="$1" text="$2" needle="$3"
  case "$text" in
    *"$needle"*) ;;
    *) echo "--show for $label must report $needle" >&2; exit 1 ;;
  esac
}

expect_show model "$model_show" "6716356800"
expect_show model "$model_show" "90fd44e29e0d7cffeb0fd00dc73cfdab9ed0b0e95306ecf7821ea634c940c370"
expect_show model "$model_show" "gemma-4-12B-it-qat-UD-Q4_K_XL.gguf"
expect_show draft "$draft_show" "253708800"
expect_show draft "$draft_show" "fcb35dea42c71333db904cee11baac525c9ef872818ee3753f6cb156f3c6f4f6"
expect_show draft "$draft_show" "mtp-gemma-4-12B-it.gguf"

# Pinned to a COMMIT, never to `main`: a later upstream re-quantization under the same
# filename must not be able to change what a fresh install serves.
for text in "$model_show" "$draft_show"; do
  expect_show pin "$text" "980b060c40a8539ac159e0501a3e0f66a6365af3"
  case "$text" in
    *"/resolve/main/"*) echo "the URL must be commit-pinned, not main" >&2; exit 1 ;;
  esac
done

if ! grep -q 'AURA_LLM_MODEL_PATH' "$repo_root/compose.yaml"; then
  echo "aura-llm must serve a pre-fetched local path, not --hf-repo" >&2
  exit 1
fi
if ! grep -q 'AURA_LLM_DRAFT_MODEL_PATH' "$repo_root/compose.yaml"; then
  echo "the MTP drafter must be served from a pre-fetched local path too" >&2
  exit 1
fi
if ! grep -q -- 'draft-mtp' "$repo_root/compose.yaml"; then
  echo "aura-llm must run the MTP drafter (--spec-type draft-mtp)" >&2
  exit 1
fi
if grep -q -- '--hf-repo' "$repo_root/compose.yaml"; then
  bad_line="$(grep -n -B4 -- '--hf-repo' "$repo_root/compose.yaml" | grep -c 'AURA_LLM_HF_REPO' || true)"
  [ "$bad_line" -eq 0 ] || { echo "aura-llm must not use --hf-repo (no egress-at-boot promise)" >&2; exit 1; }
fi

echo "ok: both gemma-4-12B artifacts verify size+sha256, atomically refresh the cache, and stay commit-pinned"
