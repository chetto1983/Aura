#!/usr/bin/env bash
# web_dist_freshness.sh — enforce the D-05 committed-dist tamper-evidence gate (T-23-01).
# The single-binary host embeds the committed Vite build (internal/webui/dist via
# //go:embed all:dist). A hand-edited or stale dist would ship bytes that no fresh
# build produces. This rebuilds dist from web/ on the CI Node-24 toolchain and asserts
# `git diff --exit-code` is empty — the committed bytes MUST equal a fresh build.
#
# Path note: web/vite.config.ts sets build.outDir = ../internal/webui/dist (Go embed is
# package-relative, so the source can only live at internal/webui/dist — 23-01/23-02).
# The build writes there, so the diff target is internal/webui/dist from the repo root,
# NOT web/dist (which does not exist).
#
# Usage:
#   bash scripts/web_dist_freshness.sh
#
# Exit codes:
#   0 — internal/webui/dist equals a fresh `npm run build`
#   1 — internal/webui/dist is stale (rebuild + commit needed)

set -euo pipefail

cd "$(dirname "$0")/.."  # repo root

( cd web && npm ci && npm run build )

git diff --exit-code -- internal/webui/dist/ \
  || { echo "internal/webui/dist is stale — run 'npm run build' in web/ and commit the result" >&2; exit 1; }

echo "web-dist-freshness: internal/webui/dist equals a fresh build."
