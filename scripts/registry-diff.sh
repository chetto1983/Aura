#!/usr/bin/env bash
# scripts/registry-diff.sh — detect Tool types in internal/agent/tools/ that are
# not referenced in the production composition root (cmd/aura/).
#
# Limitation: constructor aliasing (e.g. NewFooBarTool for type FooTool) may
# produce false positives. Treat output as a checklist, not a hard gate.
#
# Usage:
#   bash scripts/registry-diff.sh          # check all tool packages
#   bash scripts/registry-diff.sh --probe  # self-test: injects a synthetic orphan
#
# Exit: 0 = all wired, 1 = orphans found.

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOOLS_DIR="$REPO/internal/agent/tools"

# Scan all non-test Go files in cmd/aura for wiring references.
WIRING_FILES=()
while IFS= read -r f; do
  WIRING_FILES+=("$f")
done < <(find "$REPO/cmd/aura" -name "*.go" ! -name "*_test.go" -type f | sort)

if [[ ${#WIRING_FILES[@]} -eq 0 ]]; then
  echo "registry-diff: no wiring files found under cmd/aura" >&2
  exit 1
fi

# Write combined wiring content to a temp file to avoid SIGPIPE when grep -q
# exits early on match (piping a large string through grep -q causes echo to get
# SIGPIPE, which with pipefail looks like "pattern not found").
WIRING_TMP=$(mktemp)
trap 'rm -f "$WIRING_TMP"' EXIT
cat "${WIRING_FILES[@]}" > "$WIRING_TMP"

# ── Step 1: find exported struct types that implement Tool interface ───────────
# Proxy: exported type with a "func (*Foo) Name() string" receiver method.
# awk captures the type name between the last * (or space) and ) before " Name()".
TOOL_TYPES=()
while IFS= read -r TYPE; do
  [[ -z "$TYPE" ]] && continue
  TOOL_TYPES+=("$TYPE")
done < <(
  grep -r --include="*.go" --exclude="*_test.go" -h \
    'func (.* Name() string' "$TOOLS_DIR" 2>/dev/null \
    | awk 'match($0, /\*?([A-Z][A-Za-z0-9]+)\) Name\(\)/, arr) { print arr[1] }' \
    | sort -u
)

if [[ "${1:-}" == "--probe" ]]; then
  # Self-test: inject a synthetic orphan so the script always exits 1.
  TOOL_TYPES+=("SyntheticOrphanProbeTool")
fi

if [[ ${#TOOL_TYPES[@]} -eq 0 ]]; then
  echo "registry-diff: no Tool types detected — check selector pattern" >&2
  exit 1
fi

TOTAL=${#TOOL_TYPES[@]}

# ── Step 2: check each type against wiring ────────────────────────────────────
# For type FooBarTool, strip "Tool" suffix to get "FooBar" and match:
#   NewFooBarTool, NewFooBar<anything>, &FooBarTool{, FooBarTool{
ORPHANS=()
for TYPE in "${TOOL_TYPES[@]}"; do
  BASE="${TYPE%Tool}"
  if ! grep -qE \
    "(New${TYPE}|New${BASE}[A-Za-z0-9]*|&${TYPE}\{|[^A-Za-z0-9]${TYPE}\{|\"${TYPE}\")" \
    "$WIRING_TMP"; then
    ORPHANS+=("$TYPE")
  fi
done

# ── Result ────────────────────────────────────────────────────────────────────
if [[ ${#ORPHANS[@]} -eq 0 ]]; then
  echo "registry-diff: OK — all $TOTAL Tool types are wired in production"
  exit 0
fi

ORPHAN_COUNT=${#ORPHANS[@]}
echo "registry-diff: $ORPHAN_COUNT orphan Tool type(s) not found in cmd/aura wiring:"
for T in "${ORPHANS[@]}"; do
  echo "  - $T"
done
echo ""
echo "If wired via a non-standard constructor, add a comment '// registry-diff:ok \$T' to app.go."
exit 1
