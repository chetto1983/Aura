#!/usr/bin/env bash
# Seeds the aura-docling-models volume with the hybrid-chunk tokenizer.
#
# Docling Serve runs with HF_HUB_OFFLINE=1 on an `internal: true` network with
# /opt/app-root/src/.cache mounted read-only, so it can never fetch a tokenizer
# itself; its healthcheck calls AutoTokenizer.from_pretrained(local_files_only=True)
# and stays unhealthy until this script has run. The standard-pipeline models are
# already baked into the image and land in the volume through Docker's
# populate-empty-named-volume behaviour, which is why the volume is attached to
# the Docling image here rather than to a generic helper.
#
# Provenance: AURA_DOCUMENT_CHUNK_TOKENIZER pins google/embeddinggemma-300m
# (Amendment #114), but that repository is licence-gated and answers 401 to an
# unauthenticated fetch. The bytes below come from a public mirror instead. This
# is safe only because the mirror is verified, not assumed:
#   - two independent re-uploads (onnx-community, unsloth) carry byte-identical
#     vocab (262144 tokens) and merges (514906), differing only in JSON layout;
#   - the vocab is index-for-index identical to the embeddinggemma-300M GGUF that
#     aura-llama-embed already serves, checked by decoding that server's own
#     /tokenize output back to the source string.
# The second check is the load-bearing one: it proves Docling's chunk token counts
# agree with the embedder that consumes those chunks.
set -euo pipefail

MIRROR_REPO="${AURA_DOCLING_TOKENIZER_REPO:-unsloth/embeddinggemma-300M}"
MIRROR_REV="${AURA_DOCLING_TOKENIZER_REV:-bfa3c846ac738e62aa61806ef9112d34acb1dc5a}"
# compose.yaml declares the volume as `aura-docling-models` with no explicit `name:`,
# so Compose creates it project-prefixed. Seeding the bare name silently produces a
# second, unmounted volume and leaves the appliance unhealthy with no obvious cause.
COMPOSE_PROJECT="${COMPOSE_PROJECT_NAME:-aura}"
VOLUME="${AURA_DOCLING_MODELS_VOLUME:-${COMPOSE_PROJECT}_aura-docling-models}"
IMAGE="${AURA_DOCLING_IMAGE:-quay.io/docling-project/docling-serve-cpu:v1.29.0@sha256:51c18e0e0fec8a13ab858f03d5e9a140178d8d95962400553cb2644ad5ee84da}"

# The cache directory transformers derives from the pinned tokenizer name. It must
# stay google/embeddinggemma-300m even though the bytes are mirrored, because that
# is the identifier the Docling request and the healthcheck both resolve.
CACHE_DIR="models--google--embeddinggemma-300m"

# config.json is required even though nothing here loads weights: AutoTokenizer
# resolves a model config before the tokenizer, and without it the offline lookup
# fails with LocalEntryNotFoundError.
declare -A EXPECTED=(
  [tokenizer.json]=6852f8d561078cc0cebe70ca03c5bfdd0d60a45f9d2e0e1e4cc05b68e9ec329e
  [tokenizer_config.json]=9076840490613047bc9115963ee96b7702018b0d26ba644240bf856efda93118
  [special_tokens_map.json]=2f7b0adf4fb469770bb1490e3e35df87b1dc578246c5e7e6fc76ecf33213a397
  [config.json]=8f863f76e2d9c710cc833dc92efa898c9adfd41031c786507cc6b0e49c2e3e68
)

staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT

echo "seeding ${CACHE_DIR} from ${MIRROR_REPO}@${MIRROR_REV}"
for file in "${!EXPECTED[@]}"; do
  url="https://huggingface.co/${MIRROR_REPO}/resolve/${MIRROR_REV}/${file}"
  curl -fsSL -o "${staging}/${file}" "$url"
  actual="$(sha256sum "${staging}/${file}" | cut -d' ' -f1)"
  if [ "$actual" != "${EXPECTED[$file]}" ]; then
    echo "refusing to seed: ${file} sha256 ${actual} != pinned ${EXPECTED[$file]}" >&2
    exit 1
  fi
  echo "  verified ${file}"
done

# Attaching the volume to the Docling image (not a bare helper) is what copies the
# baked layout/table/OCR models in when the volume is new; seeding a helper-created
# volume would leave the standard pipeline with no models at all.
#
# The payload streams in over stdin as a tar. A bind mount would be the obvious
# choice but the staging path is a POSIX path the Docker daemon cannot resolve on
# a Windows host, and translating it would make this script host-specific.
tar -C "$staging" -cf - tokenizer.json tokenizer_config.json special_tokens_map.json config.json \
  | docker run --rm -i \
      -v "${VOLUME}:/opt/app-root/src/.cache" \
      --entrypoint sh "$IMAGE" -c "
        set -eu
        hub=/opt/app-root/src/.cache/huggingface/hub/${CACHE_DIR}
        mkdir -p \"\$hub/snapshots/${MIRROR_REV}\" \"\$hub/refs\"
        tar -C \"\$hub/snapshots/${MIRROR_REV}\" -xf -
        printf '%s' '${MIRROR_REV}' > \"\$hub/refs/main\"
      "

echo "seeded ${VOLUME}; verifying the healthcheck's own resolution path"
MSYS_NO_PATHCONV=1 docker run --rm \
  -v "${VOLUME}:/opt/app-root/src/.cache:ro" \
  -e HF_HUB_OFFLINE=1 -e TRANSFORMERS_OFFLINE=1 \
  --entrypoint python "$IMAGE" -c "
from transformers import AutoTokenizer
t = AutoTokenizer.from_pretrained('google/embeddinggemma-300m', local_files_only=True)
ids = t('Il cliente di TORINO ha 699 righe.', add_special_tokens=False)['input_ids']
assert ids == [12834,30920,1001,95599,40257,678,236743,236825,236819,236819,10510,499,236761], ids
print('tokenizer ok, vocab', t.vocab_size)
"
