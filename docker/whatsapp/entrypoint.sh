#!/bin/sh
set -eu

bridge_port="${WHATSAPP_BRIDGE_PORT:-8081}"
export WHATSAPP_BRIDGE_PORT="$bridge_port"
export WHATSAPP_API_BASE_URL="${WHATSAPP_API_BASE_URL:-http://127.0.0.1:${bridge_port}/api}"

mkdir -p /app/whatsapp-bridge/store

stop_children() {
  if [ "${bridge_pid:-}" ]; then
    kill "$bridge_pid" 2>/dev/null || true
  fi
  if [ "${mcp_pid:-}" ]; then
    kill "$mcp_pid" 2>/dev/null || true
  fi
}

trap 'stop_children; exit 143' INT TERM

(
  cd /app/whatsapp-bridge
  exec /usr/local/bin/whatsapp-bridge
) &
bridge_pid="$!"

(
  cd /app/whatsapp-mcp-server
  exec .venv/bin/python main.py
) &
mcp_pid="$!"

status=0
while true; do
  if ! kill -0 "$bridge_pid" 2>/dev/null; then
    wait "$bridge_pid" || status="$?"
    stop_children
    wait "$mcp_pid" 2>/dev/null || true
    exit "$status"
  fi
  if ! kill -0 "$mcp_pid" 2>/dev/null; then
    wait "$mcp_pid" || status="$?"
    stop_children
    wait "$bridge_pid" 2>/dev/null || true
    exit "$status"
  fi
  sleep 2
done
