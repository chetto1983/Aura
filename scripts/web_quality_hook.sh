#!/usr/bin/env bash
# web_quality_hook.sh — fast frontend pre-push gate (lefthook).
#
# Mirrors the CI `web-lint` job (eslint --max-warnings=0 + tsc --noEmit + prettier
# --check) so a broken frontend is caught at push time, not in CI. Run as a script
# (not inline in lefthook.yml) so the npm resolution parses identically under
# Windows Git Bash, WSL, and CI Linux — same rationale as scripts/gofmt-staged.sh.
#
# Heavy gates (vitest coverage, Stryker mutation, Playwright E2E) intentionally
# stay in CI / `make web-quality`; a pre-push hook must be fast enough not to be
# bypassed habitually. lefthook gates this on `glob: web/**`, so a Go-only push
# never runs it.
#
# Self-guards on a missing web/node_modules: a Go-only contributor who has never
# run `npm ci` in web/ is warned and the gate is SKIPPED rather than failing the
# push (the CI web-lint job is the hard gate for those changes).

set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "${root}/web"

if [ ! -d node_modules ]; then
  echo "web/node_modules missing — run 'npm ci' in web/ to enable the frontend pre-push gate. Skipping (CI web-lint is the hard gate)." >&2
  exit 0
fi

npm run lint
npm run typecheck
npm run format:check
