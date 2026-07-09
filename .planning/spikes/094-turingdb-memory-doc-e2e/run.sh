#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
SCRATCH_ROOT="${TURINGDB_SPIKE_HOME:-/tmp/turingdb-aura-spike-docker}"
IMAGE="${TURINGDB_DOCKER_IMAGE:-aura-turingdb-spike:1.35}"

mkdir -p "$SCRATCH_ROOT"

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  docker build -t "$IMAGE" -f "$REPO_ROOT/.planning/spikes/turingdb.Dockerfile" "$REPO_ROOT/.planning/spikes"
fi

PRINTING_PRESS_VERSION_JSON="{}"
if command -v cli-printing-press >/dev/null 2>&1; then
  PRINTING_PRESS_VERSION_JSON="$(cli-printing-press version --json || printf '{}')"
fi
PRINTING_PRESS_VERSION_B64="$(printf '%s' "$PRINTING_PRESS_VERSION_JSON" | base64 | tr -d '\n')"

docker run --rm \
  -e "TURINGDB_CONTAINER_IMAGE=$IMAGE" \
  -e "PRINTING_PRESS_VERSION_B64=$PRINTING_PRESS_VERSION_B64" \
  --mount "type=bind,source=$REPO_ROOT,target=/work" \
  --mount "type=bind,source=$SCRATCH_ROOT,target=/scratch" \
  --entrypoint python \
  "$IMAGE" \
  /work/.planning/spikes/094-turingdb-memory-doc-e2e/probe_e2e.py \
  --scratch /scratch/094-memory-doc-e2e \
  --out /work/.planning/spikes/094-turingdb-memory-doc-e2e/results.json 2>&1 | tee "$SCRIPT_DIR/run.log"
exit "${PIPESTATUS[0]}"
