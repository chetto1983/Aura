#!/usr/bin/env bash
# agui_boundary_check.sh — enforce two import-boundary invariants via dependency
# CLOSURE (not a shallow source grep — a transitive violation would slip past a
# `grep import` gate).
#
#   1. D-17 one-way boundary (SC2): internal/agent MUST NOT (transitively) import
#      internal/agui. The AG-UI gateway is a fan-out adapter that consumes
#      agent.Event; the agent runtime never depends on the transport.
#   2. webui leaf invariant (Phase 23, FND-02): internal/webui MUST NOT import ANY
#      other internal/* package (it is the static embed host; "imports only stdlib"
#      is a MUST invariant). A future internal/agui or internal/agent import would
#      compile and pass CI without this gate.
#
# Usage:
#   bash scripts/agui_boundary_check.sh
#
# Exit codes:
#   0 — both closures are clean
#   1 — boundary violation: agent→agui, or webui depends on another internal pkg

set -euo pipefail

MODULE="github.com/chetto1983/aura"
AGUI_PKG="${MODULE}/internal/agui"
WEBUI_PKG="${MODULE}/internal/webui"

if go list -deps ./internal/agent/... | grep -qx "${AGUI_PKG}"; then
  echo "BOUNDARY VIOLATION: internal/agent transitively imports internal/agui" >&2
  echo "The translator boundary is one-way (agui imports agent, NEVER reverse, D-17)." >&2
  exit 1
fi

# webui must be a leaf: its closure may contain itself, never another internal/*.
webui_internal_deps="$(go list -deps ./internal/webui/... \
  | grep "^${MODULE}/internal/" \
  | grep -vx "${WEBUI_PKG}" || true)"
if [ -n "${webui_internal_deps}" ]; then
  echo "BOUNDARY VIOLATION: internal/webui imports other internal/* packages:" >&2
  echo "${webui_internal_deps}" >&2
  echo "internal/webui is the static embed host and MUST import only stdlib (FND-02)." >&2
  exit 1
fi

echo "agui-boundary: internal/agent closure is free of internal/agui; internal/webui is leaf."
