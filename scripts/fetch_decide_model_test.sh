#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="$repo_root/scripts/fetch_decide_model.sh"
fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

model_fixture="$fixture_root/payload.gguf"
printf 'GGUF fixture payload for the decision model fetch path' > "$model_fixture"
model_bytes="$(wc -c < "$model_fixture" | tr -d ' ')"
model_sha256="$(sha256sum "$model_fixture" | awk '{print $1}')"

license_fixture="$fixture_root/LICENSE.txt"
printf 'LFM Open License v1.0 fixture text' > "$license_fixture"
license_bytes="$(wc -c < "$license_fixture" | tr -d ' ')"
license_sha256="$(sha256sum "$license_fixture" | awk '{print $1}')"

zero_sha256="0000000000000000000000000000000000000000000000000000000000000000"

refused() {
  local label="$1"
  shift
  if bash "$script" "$@" >/dev/null 2>&1; then
    echo "expected $label to be refused" >&2
    exit 1
  fi
}

no_partials() {
  if compgen -G "$fixture_root/cache/*.partial.*" >/dev/null; then
    echo "partial downloads must be removed after $1" >&2
    exit 1
  fi
}

# The right pins pass, and a file already in place is not fetched again.
model_target="$fixture_root/cache/d1-3B-Q4_K_M.gguf"
bash "$script" "$model_target" "file://$model_fixture" "$model_bytes" "$model_sha256"
cmp "$model_fixture" "$model_target"
bash "$script" "$model_target" "http://127.0.0.1:1/unreachable" "$model_bytes" "$model_sha256"
cmp "$model_fixture" "$model_target"

printf 'broken cache entry' > "$model_target"
bash "$script" "$model_target" "file://$model_fixture" "$model_bytes" "$model_sha256"
cmp "$model_fixture" "$model_target"

license_target="$fixture_root/cache/LICENSE"
bash "$script" --artifact license "$license_target" "file://$license_fixture" "$license_bytes" "$license_sha256"
cmp "$license_fixture" "$license_target"
bash "$script" --artifact license "$license_target" "http://127.0.0.1:1/unreachable" "$license_bytes" "$license_sha256"
cmp "$license_fixture" "$license_target"

# The message names the artifact in hand, not a constant.
out="$(bash "$script" "$model_target" "file://$model_fixture" "$model_bytes" "$model_sha256")"
case "$out" in
  *"d1-3B-Q4_K_M.gguf"*) ;;
  *) echo "the message must name the target it handled, got: $out" >&2; exit 1 ;;
esac

# A wrong size or SHA-256 of either artifact is refused, and leaves nothing behind.
refused "a model of the wrong size" "$fixture_root/cache/size.gguf" "file://$model_fixture" "$((model_bytes + 1))" "$model_sha256"
no_partials "a model size mismatch"
refused "a model of the wrong sha256" "$fixture_root/cache/sha.gguf" "file://$model_fixture" "$model_bytes" "$zero_sha256"
no_partials "a model sha256 mismatch"
refused "a licence of the wrong size" --artifact license "$fixture_root/cache/size.txt" "file://$license_fixture" "$((license_bytes + 1))" "$license_sha256"
no_partials "a licence size mismatch"
refused "a licence of the wrong sha256" --artifact license "$fixture_root/cache/sha.txt" "file://$license_fixture" "$license_bytes" "$zero_sha256"
no_partials "a licence sha256 mismatch"

# Only the model is a GGUF; the licence has its own path, which checks size and SHA alone.
printf 'not gguf at all' > "$fixture_root/invalid.gguf"
invalid_bytes="$(wc -c < "$fixture_root/invalid.gguf" | tr -d ' ')"
invalid_sha256="$(sha256sum "$fixture_root/invalid.gguf" | awk '{print $1}')"
refused "a model without the GGUF magic" "$fixture_root/cache/invalid.gguf" "file://$fixture_root/invalid.gguf" "$invalid_bytes" "$invalid_sha256"
bash "$script" --artifact license "$fixture_root/cache/plain.txt" "file://$fixture_root/invalid.gguf" "$invalid_bytes" "$invalid_sha256"

refused "an unknown --artifact" --artifact nonsense "$fixture_root/cache/x.gguf"

# --show resolves each artifact's own pin row without downloading 1.6 GB to find out.
model_show="$(bash "$script" --artifact model --show)"
license_show="$(bash "$script" --artifact license --show)"

expect_show() {
  local label="$1" text="$2" needle="$3"
  case "$text" in
    *"$needle"*) ;;
    *) echo "--show for $label must report $needle" >&2; exit 1 ;;
  esac
}

expect_show model "$model_show" "1674456672"
expect_show model "$model_show" "16aff27ea2eefdc32b9897f43854a5d3170c1dc8dccb9c756905af30a4e22402"
expect_show model "$model_show" "d1-3B-Q4_K_M.gguf"
expect_show license "$license_show" "10574"
expect_show license "$license_show" "f684ace3fe08f9030789383dcb38792f23a74c575ef024e8c2ef76c09f536248"
expect_show license "$license_show" "/LICENSE"

# Pinned to a COMMIT, never to `main`.
for text in "$model_show" "$license_show"; do
  expect_show pin "$text" "LiquidAI/d1-3B-GGUF/resolve/bb1e436ea78eb96a3f1acb6da865f70c2fbeb563/"
  case "$text" in
    *"/resolve/main/"*) echo "the URL must be commit-pinned, not main" >&2; exit 1 ;;
  esac
done

echo "ok: the decision model and its licence are fetched at their pins, and nothing else"
