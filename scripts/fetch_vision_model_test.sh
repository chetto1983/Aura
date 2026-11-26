#!/usr/bin/env bash
# The vision model's pin table. The download, cache and refusal mechanics are
# scripts/lib/pinned_fetch.sh's, proven by fetch_llm_model_test.sh and fetch_decide_model_test.sh;
# this proves what is this script's own: each artifact's pins, the commit in its URL, and that
# both artifacts must be GGUF.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="$repo_root/scripts/fetch_vision_model.sh"
fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

fail() {
  echo "$1" >&2
  exit 1
}

expect_show() {
  case "$2" in
    *"$3"*) ;;
    *) fail "--show for $1 must report $3" ;;
  esac
}

model_show="$(bash "$script" --artifact model --show)"
mmproj_show="$(bash "$script" --artifact mmproj --show)"
expect_show model "$model_show" "Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf"
expect_show model "$model_show" "1929901056"
expect_show model "$model_show" "d02fe9b69ad8cadbbd228e387667af66612c44bed29ffc8eb1e7caf9ac486c12"
expect_show mmproj "$mmproj_show" "mmproj-Qwen2.5-VL-3B-Instruct-Q8_0.gguf"
expect_show mmproj "$mmproj_show" "844757728"
expect_show mmproj "$mmproj_show" "980c9b2f78c04e6cff93d277ada09e768394f112d75db3b4e9dea8a69f9fb904"
for text in "$model_show" "$mmproj_show"; do
  expect_show pin "$text" "ggml-org/Qwen2.5-VL-3B-Instruct-GGUF/resolve/5037fcf163dd95d1e41d1974465f0898ed108ca2/"
  case "$text" in
    *"/resolve/main/"*) fail "the URL must be commit-pinned, not main" ;;
  esac
done

if bash "$script" --artifact nonsense "$fixture_root/x.gguf" >/dev/null 2>&1; then
  fail "an unknown --artifact must be refused"
fi

# Both artifacts are GGUF: a file without the magic is refused even at its own size and SHA-256.
printf 'not gguf' >"$fixture_root/plain"
plain_bytes="$(wc -c <"$fixture_root/plain" | tr -d ' ')"
plain_sha256="$(sha256sum "$fixture_root/plain" | awk '{print $1}')"
printf 'GGUF vision fixture' >"$fixture_root/fixture.gguf"
gguf_bytes="$(wc -c <"$fixture_root/fixture.gguf" | tr -d ' ')"
gguf_sha256="$(sha256sum "$fixture_root/fixture.gguf" | awk '{print $1}')"
for artifact in model mmproj; do
  if bash "$script" --artifact "$artifact" "$fixture_root/cache/$artifact-plain.gguf" \
    "file://$fixture_root/plain" "$plain_bytes" "$plain_sha256" >/dev/null 2>&1; then
    fail "$artifact without the GGUF magic must be refused"
  fi
  bash "$script" --artifact "$artifact" "$fixture_root/cache/$artifact.gguf" \
    "file://$fixture_root/fixture.gguf" "$gguf_bytes" "$gguf_sha256" >/dev/null
  cmp "$fixture_root/fixture.gguf" "$fixture_root/cache/$artifact.gguf"
done

echo "ok: the vision model and its projector are fetched at their pins, and nothing else"
