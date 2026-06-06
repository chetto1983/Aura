#!/usr/bin/env bash
# agui_boundary_check.sh — enforce the D-17 one-way import boundary (SC2).
# internal/agent MUST NOT (transitively) import internal/agui: the AG-UI gateway
# is a fan-out adapter that consumes agent.Event; the agent runtime never depends
# on the transport. Asserts the dependency CLOSURE, not a shallow source grep — a
# transitive violation (agent → X → agui) would slip past a `grep import` gate.
#
# Usage:
#   bash scripts/agui_boundary_check.sh
#
# Exit codes:
#   0 — internal/agent closure is free of internal/agui
#   1 — boundary violation: internal/agent transitively imports internal/agui

set -euo pipefail

MODULE="github.com/chetto1983/aura"
AGUI_PKG="${MODULE}/internal/agui"

if go list -deps ./internal/agent/... | grep -qx "${AGUI_PKG}"; then
  echo "BOUNDARY VIOLATION: internal/agent transitively imports internal/agui" >&2
  echo "The translator boundary is one-way (agui imports agent, NEVER reverse, D-17)." >&2
  exit 1
fi

echo "agui-boundary: internal/agent closure is free of internal/agui."
