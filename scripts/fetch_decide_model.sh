#!/usr/bin/env bash
# Fetches and verifies the two files the optional local decision sidecar (aura-llama-decide,
# compose.yaml) needs: Liquid AI's d1-3B GGUF and its LICENSE. The LFM Open License 1.0 asks
# (§4(a)) that the LICENSE travels with the weights, so it is pinned and fetched like the model.
#
# The model is the one the operator chose on 2026-11-03 after Laya multilingual failed G1.
# Like fetch_llm_model.sh, this pins the published byte size AND SHA-256 of one upstream
# commit and refuses anything else, loudly, leaving no partial file in place; the URL names the
# commit, not `main`, so a later upstream re-quantization under the same filename cannot swap
# what gets served. Size and SHA-256 are from the repo's tree API at that commit
# (https://huggingface.co/api/models/LiquidAI/d1-3B-GGUF/tree/<commit>, lfs.oid + size).
#
# The LICENSE has its own validation: only the model is checked for the GGUF magic, which a text file
# does not carry, so it is judged on size and SHA-256 alone.
set -euo pipefail

repo_commit="bb1e436ea78eb96a3f1acb6da865f70c2fbeb563"
repo_base="https://huggingface.co/LiquidAI/d1-3B-GGUF/resolve/${repo_commit}"
fetch_script_name="fetch_decide_model.sh"
artifact_names="model license"

pin_artifact() {
  gguf_magic=0
  case "$artifact" in
    model)
      official_file="d1-3B-Q4_K_M.gguf"
      official_bytes=1674456672
      official_sha256="16aff27ea2eefdc32b9897f43854a5d3170c1dc8dccb9c756905af30a4e22402"
      gguf_magic=1
      ;;
    license)
      official_file="LICENSE"
      official_bytes=10574
      official_sha256="f684ace3fe08f9030789383dcb38792f23a74c575ef024e8c2ef76c09f536248"
      ;;
    *) return 1 ;;
  esac
}

# shellcheck source=scripts/lib/pinned_fetch.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/pinned_fetch.sh"
pinned_fetch_main "$@"
