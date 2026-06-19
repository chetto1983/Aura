#!/usr/bin/env bash
# check-dup.sh — frontend copy/paste detector (jscpd), the TS/TSX parity to the Go
# `dupl` linter already wired into .golangci.yml.
#
# Config: web/.jscpd.json — fails on any non-test clone >=100 tokens (mirrors the
# .golangci.yml `dupl` threshold of 100, which likewise excludes _test.go). Test
# files are intentionally exempt: table/arrange-block repetition in tests is
# idiomatic, same policy as the Go side.
#
# The jscpd binary is fetched via pinned `npx` rather than a devDependency so this
# gate adds nothing to package-lock.json (see CLAUDE.md §Env vars / the documented
# npm Windows->Linux lock-drift gotcha). Run as a script (not inline in lefthook.yml)
# so npm/npx resolution parses identically under Windows Git Bash, WSL, and CI Linux
# — same rationale as scripts/web_quality_hook.sh.
#
# Self-guards on a missing web/node_modules: a Go-only contributor who has never run
# `npm ci` in web/ is warned and the gate is SKIPPED rather than failing the commit
# (the CI web-lint job is the hard gate for those changes).

set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "${root}/web"

if [ ! -d node_modules ]; then
  echo "web/node_modules missing — run 'npm ci' in web/ to enable the frontend dup gate. Skipping (CI web-lint is the hard gate)." >&2
  exit 0
fi

npm run dup
