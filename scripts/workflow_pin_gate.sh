#!/usr/bin/env bash
set -euo pipefail

workflow_dir="${1:-.github/workflows}"

if [[ ! -d "$workflow_dir" ]]; then
  echo "workflow-pin-gate: directory not found: $workflow_dir" >&2
  exit 2
fi

status=0
while IFS= read -r -d '' workflow; do
  while IFS= read -r line; do
    ref="$(sed -E 's/^[[:space:]]*(-[[:space:]]*)?uses:[[:space:]]*([^#[:space:]]+).*$/\2/' <<<"$line")"
    if [[ "$ref" == ./* ]]; then
      continue
    fi
    if [[ ! "$ref" =~ @[0-9a-f]{40}$ ]]; then
      echo "workflow-pin-gate: $workflow: floating action ref: $ref" >&2
      status=1
    fi
    if [[ ! "$line" =~ \#[[:space:]]*v[0-9] ]]; then
      echo "workflow-pin-gate: $workflow: pinned action lacks an inline version comment: $ref" >&2
      status=1
    fi
  done < <(grep -hE '^[[:space:]]*(-[[:space:]]*)?uses:' "$workflow" || true)

  if grep -nE 'go install[[:space:]]+[^[:space:]]+@latest([[:space:]]|$)' "$workflow"; then
    echo "workflow-pin-gate: $workflow: go install @latest is forbidden" >&2
    status=1
  fi
done < <(find "$workflow_dir" -type f \( -name '*.yml' -o -name '*.yaml' \) -print0)

if (( status != 0 )); then
  exit "$status"
fi
echo "workflow-pin-gate: all action and Go-tool refs are immutable"
