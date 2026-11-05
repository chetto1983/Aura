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
fetch_script_name="fetch_llm_model.sh"
artifact_names="model draft"

pin_artifact() {
  gguf_magic=1
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
    *) return 1 ;;
  esac
}

# shellcheck source=scripts/lib/pinned_fetch.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/pinned_fetch.sh"
pinned_fetch_main "$@"
