#!/usr/bin/env bash
# Four runs of throughput.py on one fixed workload: cache off/on x 1/4 requests in flight. The
# server restarts before each run, so no run inherits another's warm cache. MODELS holds the GGUF.
set -euo pipefail
S=${S:?scratch dir holding passages.jsonl, cases.json, gate.jsonl}
MODELS=${MODELS:?directory holding d1-2B-Q4_K_M.gguf}
HERE=$(cd "$(dirname "$0")" && pwd)
serve() {
  docker rm -f spike-d1 >/dev/null 2>&1 || true
  docker run -d --name spike-d1 --network host -v "$MODELS":/m:ro ghcr.io/ggml-org/llama.cpp:server-b11960 \
    -m /m/d1-2B-Q4_K_M.gguf -b 8192 -ub 8192 -c 32768 -np 4 -t 4 --host 127.0.0.1 --port 8080 "$@" >/dev/null
  until curl -sf http://127.0.0.1:8080/health >/dev/null; do sleep 1; done
}
run() {
  python3 "$HERE/throughput.py" --passages "$S/passages.jsonl" --gate "$S/gate.jsonl" --cases "$S/cases.json" \
    --out "$S/throughput.jsonl" "$@"
}
serve;                   run --workers 1 --label "no cache, 1 in flight"
serve;                   run --workers 4 --label "no cache, 4 in flight"
serve --cache-ram 4096;  run --workers 1 --label "cache 4 GiB, 1 in flight"
serve --cache-ram 4096;  run --workers 4 --label "cache 4 GiB, 4 in flight"
