#!/usr/bin/env bash
# Pre-push wrapper for the quality-snapshot freshness gate (amendment #20).
#
# The CI job `Build + vet + lint + ...` runs scripts/quality_snapshot_gate.sh on the
# pushed diff; a changed file matching a quality-row glob whose row date predates the
# push base fails the whole matrix (a ~20-min round-trip). This wrapper runs the SAME
# gate locally over the exact commit range being pushed so a stale row is caught here,
# before the push — re-attest the row in docs/aura-quality-snapshot.md and re-push.
#
# Bypass in an emergency: git push --no-verify
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

# Base = the upstream this branch tracks (origin/master on master-direct), else the
# remote master ref, else the previous commit (single-commit push / no remote).
base=""
if up="$(git rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null)"; then
  base="$up"
elif git rev-parse --verify -q origin/master >/dev/null 2>&1; then
  base="origin/master"
elif git rev-parse --verify -q 'HEAD~1' >/dev/null 2>&1; then
  base="HEAD~1"
fi

if [[ -z "$base" ]]; then
  echo "quality-snapshot pre-push: no base ref (initial history) — skipping"
  exit 0
fi

# Diff from the merge-base so the range matches CI's push before...after semantics on
# the linear master history, and read the base DATE from that same ancestor.
mb="$(git merge-base "$base" HEAD 2>/dev/null || echo "$base")"
changed="$(git diff --name-only "$mb" HEAD)"
if [[ -z "$changed" ]]; then
  echo "quality-snapshot pre-push: no committed changes vs $base — nothing to check"
  exit 0
fi
base_date="$(git log -1 --format=%cs "$mb")"

AURA_QUALITY_CHANGED_FILES="$changed" AURA_QUALITY_BASE_DATE="$base_date" \
  bash scripts/quality_snapshot_gate.sh
