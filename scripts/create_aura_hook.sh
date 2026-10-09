#!/usr/bin/env bash
# create_aura_hook.sh — pre-push gate for the create-aura-appliance installer (lefthook).
#
# Mirrors the checks of .github/workflows/create-aura-appliance.yml that need no network or
# makeself: the preflight-floor sync check, the TypeScript compile and the vitest suite. The
# compile runs with --noEmit, so the gate writes no dist/. lefthook gates it on
# `packages/create-aura/**`, so any other push never runs it. Same shape and Windows/WSL/CI
# rationale as scripts/web_quality_hook.sh.
#
# Self-guards on a missing node_modules: the gate is skipped with a warning rather than
# failing the push, and the create-aura-appliance workflow stays the hard gate.

set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "${root}/packages/create-aura"

if [ ! -d node_modules ]; then
  echo "packages/create-aura/node_modules missing — run 'npm ci' there to enable this pre-push gate. Skipping (the create-aura-appliance workflow is the hard gate)." >&2
  exit 0
fi

node scripts/sync-preflight-floors.mjs --check
npx tsc --noEmit
npm test
