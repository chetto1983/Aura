#!/usr/bin/env bash
# Fetches and verifies the GGUF pair web-e2e serves as a local vision primary for
# web/e2e/native-media-live.spec.ts: Qwen2.5-VL-3B-Instruct at Q4_K_M and its Q8_0 vision
# projector (mmproj), from ggml-org's conversion at one pinned commit. Like fetch_llm_model.sh,
# it pins the published byte size AND SHA-256 of that commit and refuses anything else, loudly,
# leaving no partial file in place. Size and SHA-256 are the repo's blob API at the commit
# (https://huggingface.co/api/models/ggml-org/Qwen2.5-VL-3B-Instruct-GGUF?blobs=true,
# siblings[].size and lfs.sha256), matched against sha256sum of the downloaded bytes.
#
# Chosen by the operator on 2026-11-26 from the measured candidates
# (docs/superpowers/verification/2026-11-26-test-ledger-open-points.md): the only one that read
# the code through ten full Aura turns out of ten on a 4-vCPU CPU. The upstream model card's
# licence is qwen-research, whatever the GGUF repo's tag says; it is a CI fixture, never served
# by the appliance.
set -euo pipefail

repo_commit="5037fcf163dd95d1e41d1974465f0898ed108ca2"
repo_base="https://huggingface.co/ggml-org/Qwen2.5-VL-3B-Instruct-GGUF/resolve/${repo_commit}"
fetch_script_name="fetch_vision_model.sh"
artifact_names="model mmproj"

pin_artifact() {
  gguf_magic=1
  case "$artifact" in
    model)
      official_file="Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf"
      official_bytes=1929901056
      official_sha256="d02fe9b69ad8cadbbd228e387667af66612c44bed29ffc8eb1e7caf9ac486c12"
      ;;
    mmproj)
      official_file="mmproj-Qwen2.5-VL-3B-Instruct-Q8_0.gguf"
      official_bytes=844757728
      official_sha256="980c9b2f78c04e6cff93d277ada09e768394f112d75db3b4e9dea8a69f9fb904"
      ;;
    *) return 1 ;;
  esac
}

# shellcheck source=scripts/lib/pinned_fetch.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/pinned_fetch.sh"
pinned_fetch_main "$@"
