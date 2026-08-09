#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

fixture="$fixture_root/embeddinggemma.gguf"
target="$fixture_root/cache/embeddinggemma-300M-Q8_0.gguf"
printf 'GGUF fixture dense_2.weight payload dense_3.weight' > "$fixture"

bash "$repo_root/scripts/fetch_embedding_model.sh" "$target" "file://$fixture"
cmp "$fixture" "$target"

bash "$repo_root/scripts/fetch_embedding_model.sh" "$target" "http://127.0.0.1:1/unreachable"
cmp "$fixture" "$target"

printf 'broken cache entry' > "$target"
bash "$repo_root/scripts/fetch_embedding_model.sh" "$target" "file://$fixture"
cmp "$fixture" "$target"

printf 'not a model' > "$fixture_root/invalid.gguf"
if bash "$repo_root/scripts/fetch_embedding_model.sh" \
  "$fixture_root/cache/invalid.gguf" "file://$fixture_root/invalid.gguf" >/dev/null 2>&1; then
  echo "expected an invalid model to be rejected" >&2
  exit 1
fi
if compgen -G "$fixture_root/cache/*.partial.*" >/dev/null; then
  echo "partial downloads must be removed after failure" >&2
  exit 1
fi

workflow="$repo_root/.github/workflows/ci.yml"
helper_calls="$(grep -c 'bash scripts/fetch_embedding_model[.]sh' "$workflow" || true)"
# Five since 2026-08-09: ingest-sidecar-test joined the four live embedding tiers.
# The count is the point -- a new tier that fetches the model its own way is how the
# validated-artifact contract erodes -- so raising it is a deliberate act, not a fixup.
if [ "$helper_calls" -ne 5 ]; then
  echo "expected all five live embedding CI tiers to use the shared helper" >&2
  exit 1
fi
if grep -q 'qwen3-embedding-0[.]6b' "$workflow"; then
  echo "retired Qwen embedding cache metadata remains in CI" >&2
  exit 1
fi
if ! grep -q 'download_file scripts/fetch_embedding_model[.]sh' "$repo_root/scripts/install.sh"; then
  echo "the streamed installer must fetch the shared model helper" >&2
  exit 1
fi

echo "ok: EmbeddingGemma fetch validates and atomically refreshes the cache"
