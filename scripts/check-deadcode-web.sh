#!/usr/bin/env bash
# check-deadcode-web.sh — frontend dead-code detector (knip), the TS/TSX parity to
# the Go `deadcode` gate already wired into lefthook pre-push + the CI Go job.
#
# Reports unused files, unreachable value exports (knip's ignoreExportsUsedInFile is
# on, so an export merely used inside its own file is NOT flagged — matching Go
# deadcode, which only reports truly unreachable code), and unused dependencies.
#
# Type-only findings (unused `*Props` interfaces, exported types) are excluded:
# Go's deadcode has no type concept, and exporting a component's Props type is
# idiomatic React even when not yet imported elsewhere. Config: web/knip.json.
#
# web/knip.json also ignores the `assistant-stream` dependency: it is a regular
# (deduped) transitive of @assistant-ui/react, exact-pinned in package.json to lock
# the assistant-ui streaming runtime to the tested version — deliberate, not dead.
#
# Knip6 is locked with the frontend dependencies and parses through Oxc instead
# of the retired TypeScript JavaScript API. It needs installed dependencies to
# resolve imports, so CI remains the hard gate when node_modules is absent locally.

set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "${root}/web"

if [ ! -d node_modules ]; then
  echo "web/node_modules missing — run 'npm ci' in web/ to enable the frontend dead-code gate. Skipping (CI web-lint is the hard gate)." >&2
  exit 0
fi

npm run deadcode
