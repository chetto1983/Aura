#!/usr/bin/env bash
# In-box driver for the agent-browser spike (run through boxrun exec, cwd /workspace).
#   box_flow.sh fixture            start the fixture if it is not listening
#   box_flow.sh reset              kill every agent-browser/Chromium, wipe ~/.agent-browser
#   box_flow.sh login <session>    vault save (once) + vault login + TOTP -> Documents
#   box_flow.sh check <session>    restore <session>, open /docs, print where it landed
#   box_flow.sh size <session>     restore-state file size in bytes
# The encryption key is read from $KEY_FILE because Aura's Exec scrubs secret-named env.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
KEY_FILE="${KEY_FILE:-/workspace/.scratch/abkey}"
[[ -r "$KEY_FILE" ]] && export AGENT_BROWSER_ENCRYPTION_KEY="$(cat "$KEY_FILE")"
state_dir=/root/.agent-browser

case "$1" in
fixture)
  curl -s -o /dev/null http://127.0.0.1:8765/login && exit 0
  FIXTURE_STATE=/workspace/spike/sessions.json nohup python3 "$here/fixture_site.py" 8765 >>"$here/site.log" 2>&1 &
  sleep 1
  ;;
reset)
  for pid in $(cat "$state_dir"/*.pid 2>/dev/null); do kill -9 "$pid" 2>/dev/null || true; done
  for p in /proc/[0-9]*; do
    grep -qa chrome "$p/cmdline" 2>/dev/null && kill -9 "${p#/proc/}" 2>/dev/null || true
  done
  rm -rf "$state_dir"
  ;;
login)
  agent-browser auth list 2>/dev/null | grep -q fixture ||
    printf '%s' 'Sp1ke-Passw0rd!x7' | agent-browser auth save fixture \
      --url http://127.0.0.1:8765/login --username alice@example.test --password-stdin >/dev/null
  AB=agent-browser bash "$here/login_flow.sh" "$2" 2>/dev/null | grep -v '^\[agent'
  ;;
check)
  agent-browser --session "$2" --restore open http://127.0.0.1:8765/docs 2>&1 | grep -E '✓|✗|restore:'
  ;;
size)
  stat -c %s "$state_dir/sessions/$2-$2.json.enc"
  ;;
esac
