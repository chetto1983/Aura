#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="$repo_root/scripts/workflow_pin_gate.sh"
fixture_root="$(mktemp -d)"
trap 'rm -rf -- "$fixture_root"' EXIT

mkdir -p "$fixture_root/valid" "$fixture_root/floating" "$fixture_root/latest"

cat >"$fixture_root/valid/ci.yml" <<'YAML'
steps:
  - uses: actions/checkout@0123456789abcdef0123456789abcdef01234567 # v7.0.0
  - run: go install example.com/tool@v1.2.3
YAML
bash "$gate" "$fixture_root/valid" >/dev/null

cat >"$fixture_root/floating/ci.yml" <<'YAML'
steps:
  - uses: owner/action/subpath@v4
YAML
if bash "$gate" "$fixture_root/floating" >/dev/null 2>&1; then
  echo "workflow-pin-gate-test: floating multi-segment action passed" >&2
  exit 1
fi

cat >"$fixture_root/latest/ci.yml" <<'YAML'
steps:
  - uses: actions/checkout@0123456789abcdef0123456789abcdef01234567 # v7
  - run: |
      go install example.com/tool@latest
YAML
if bash "$gate" "$fixture_root/latest" >/dev/null 2>&1; then
  echo "workflow-pin-gate-test: go install @latest inside run block passed" >&2
  exit 1
fi

echo "workflow-pin-gate-test: pass"
