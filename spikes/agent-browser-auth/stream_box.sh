#!/usr/bin/env bash
# Human login through agent-browser's viewport stream, run INSIDE the box: opens the
# fixture in session "live", reads the element boxes, then drives stream_login.mjs.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
export AGENT_BROWSER_ENCRYPTION_KEY="$(cat "${KEY_FILE:-/run/aura-abkey}")"
ab() { agent-browser --session live "$@"; }
box() { ab get box "$1" --json | python3 -c 'import sys, json; d = json.load(sys.stdin)["data"]; print(json.dumps({k: d[k] for k in ("x", "y", "width", "height")}))'; }

ab open http://127.0.0.1:8765/login >/dev/null 2>&1
ab snapshot -i >/dev/null 2>&1
port="$(cat "$HOME/.agent-browser/live.stream")"
node "$here/stream_login.mjs" "$port" "$(box @e2)" "$(box @e3)" alice@example.test 'Sp1ke-Passw0rd!x7'
tail -2 "$here/site.log"
