#!/usr/bin/env bash
# Drives the fixture login end to end with agent-browser: vault login, TOTP,
# protected page. Usage: login_flow.sh <session> [extra agent-browser flags...]
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
ab="${AB:-agent-browser}"
session="$1"; shift
x() { "$ab" --session "$session" --restore "$@"; }

# Start the TOTP step early in a 30s window so the code cannot expire mid-submit.
while (( $(date +%s) % 30 > 20 )); do sleep 1; done

x auth login fixture >/dev/null
x snapshot -i | grep -q 'textbox "Code"'
x fill @e2 "$(python3 "$here/fixture_site.py" totp)" >/dev/null
x click @e3 >/dev/null
x wait 500 >/dev/null
x snapshot -i | grep -q 'heading "Documents"' && echo "LOGGED_IN $session"
