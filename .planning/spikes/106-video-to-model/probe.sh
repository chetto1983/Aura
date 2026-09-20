#!/usr/bin/env bash
# One input_video request to the probe llama-server; prints the answer and the timing.
# Usage: bash probe.sh ../105-studio-media-editing/media/clip-h264-silent.mp4
# python3 builds the body instead of jq: Git Bash on this host has no jq, and a 3.7 MB
# base64 payload is better written straight to a file than carried in a shell variable.
set -euo pipefail
CLIP="${1:?clip path}"
PORT="${PROBE_PORT:-18099}"
PROMPT="${PROMPT:-Read the timestamp written in the top-left corner of the first and of the last frame.}"
mkdir -p out
python3 - "$CLIP" "$PROMPT" <<'PY' > out/request.json
import base64, json, sys
data = base64.b64encode(open(sys.argv[1], "rb").read()).decode()
json.dump({
    "model": "probe",
    "max_tokens": 200,
    "messages": [{"role": "user", "content": [
        {"type": "text", "text": sys.argv[2]},
        {"type": "input_video", "input_video": {"data": data}},
    ]}],
}, sys.stdout)
PY
time curl -s "http://localhost:${PORT}/v1/chat/completions" \
  -H 'Content-Type: application/json' --data-binary @out/request.json \
  -o out/response.json
python3 - <<'PY'
import json
r = json.load(open("out/response.json"))
if "error" in r:
    print("ERROR:", json.dumps(r["error"]))
else:
    c = r["choices"][0]
    print("finish_reason:", c.get("finish_reason"))
    print("usage:", json.dumps(r.get("usage")))
    print("--- content ---")
    print(c["message"]["content"])
PY
