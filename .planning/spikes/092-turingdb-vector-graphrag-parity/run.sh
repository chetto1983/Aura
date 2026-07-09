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
export TURINGDB_DOCKER_IMAGE="$IMAGE"

docker run --rm \
  -e "TURINGDB_CONTAINER_IMAGE=$IMAGE" \
  --mount "type=bind,source=$REPO_ROOT,target=/work" \
  --mount "type=bind,source=$SCRATCH_ROOT,target=/scratch" \
  --entrypoint python \
  "$IMAGE" \
  /work/.planning/spikes/092-turingdb-vector-graphrag-parity/probe_vector.py \
  --scratch /scratch/092-vector-graphrag \
  --out /work/.planning/spikes/092-turingdb-vector-graphrag-parity/results.json 2>&1 | tee "$SCRIPT_DIR/run.log"
exit "${PIPESTATUS[0]}"
