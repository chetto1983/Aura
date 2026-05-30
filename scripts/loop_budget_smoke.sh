#!/usr/bin/env bash
# Phase 2 SC#2 + B4 phase-close gate.
#
# SC#2: `aura agent dry-run --max-steps 25` drives a mock LoopAgent over the
# constant-result InfiniteToolCallAgent fixture through the real Budget tree. The
# run must terminate on the HARD step cap (the dry-run tool is dedup-exempt, so it
# does NOT terminate via the dedup veto), emitting exactly 25 step Events + 1
# explicit budget-exhausted Event = 26 JSON lines, the last carrying
# "limit_hit":"max_steps". This is the CI-grep contract for SC#2.
#
# B4 / CLAUDE.md COVERAGE FLOOR 85%: the phase does not close below 85% combined
# coverage across the Phase 2 unit surface (internal/agent/... +
# internal/canonicaljson/ + cmd/aura/). The awk gate FAILS the script below 85%.
#
# This script ACTUALLY RUNS the binary and the tests (CLAUDE.md NO-SKIP-AS-GREEN):
# a sub-second / empty / skipped run fails the line-count + grep + coverage gates.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

echo "==> SC#2: aura agent dry-run --max-steps 25"
OUT="$(go run ./cmd/aura agent dry-run --max-steps 25)"

# Guard the count the same way the coverage-rows capture at line ~60 does: under
# `set -euo pipefail`, `grep -c .` exits non-zero on ZERO matches, so an EMPTY $OUT
# (the exact NO-SKIP-AS-GREEN failure this gate exists to catch loudly) would abort
# the pipeline here BEFORE the hand-written diagnostic below could run. `|| true`
# keeps the count at 0 so the diagnostic — not a bare pipefail abort — is what the
# operator sees (WR-05).
LINES="$(printf '%s\n' "$OUT" | grep -c . || true)"
if [[ "${LINES:-0}" -ne 26 ]]; then
  echo "FAIL (SC#2): expected exactly 26 Event lines (25 step + 1 terminal), got ${LINES:-0}" >&2
  printf '%s\n' "$OUT" >&2
  exit 1
fi

if ! printf '%s\n' "$OUT" | grep -q '"limit_hit":"max_steps"'; then
  echo 'FAIL (SC#2): no Event carried "limit_hit":"max_steps"' >&2
  printf '%s\n' "$OUT" >&2
  exit 1
fi
echo "ok (SC#2): 26 lines, terminal Event limit_hit=max_steps"

echo "==> B4: phase-close coverage floor (>= 85%, CLAUDE.md)"
# Measure the PHASE-2 statement surface only (SPEC line 110: internal/agent,
# internal/agent/workflow, internal/canonicaljson, and the cmd/aura DRY-RUN paths).
# The cmd/aura db.go/neo4j.go/main.go subcommands belong to Slices 0.5/0.7 and are
# validated by their own integration tiers (Postgres/Neo4j-backed CI jobs); the
# internal/agent/tools tree is a pre-rewrite skeleton a later slice owns. Including
# either in a UNIT-coverage gate measures the wrong code, so the profile is filtered
# to the Phase-2 surface before the floor is applied.
go test -coverprofile=cover.out ./internal/agent/ ./internal/agent/workflow/ ./internal/canonicaljson/ ./cmd/aura/ >/dev/null
# Exclude the non-Phase-2 cmd/aura subcommands and the pre-rewrite tools skeleton.
# Anchor each excluded file to a PATH-SEGMENT boundary ('/<file>:') so a future
# file whose name merely CONTAINS one of these (e.g. cmd/aura/agentmain.go) is NOT
# silently dropped from the floor (WR-06). cover.out rows are
# '<module>/cmd/aura/main.go:line.col,...', so the leading '/' anchors the segment.
{
  head -1 cover.out
  grep -v '^mode:' cover.out | grep -v -E '/cmd/aura/(db|neo4j|main)\.go:|/internal/agent/tools/'
} > cover_phase2.out

# The filtered profile MUST retain real statement rows beyond the mode: header;
# an over-aggressive filter that strips everything would make go tool cover emit
# no total: line and mis-grade (WR-06).
PROFILE_ROWS="$(grep -c -v '^mode:' cover_phase2.out || true)"
if [[ "${PROFILE_ROWS:-0}" -lt 1 ]]; then
  echo "FAIL (B4): filtered coverage profile has no statement rows (filter too aggressive?)" >&2
  exit 1
fi

TOTAL_LINE="$(go tool cover -func=cover_phase2.out | tail -1)"
echo "$TOTAL_LINE"
# Grep the percentage token explicitly rather than trusting a positional awk field:
# go tool cover's column layout is not a contract (WR-06). Fail loudly if no
# percentage is captured at all.
PCT="$(printf '%s\n' "$TOTAL_LINE" | grep -oE '[0-9]+(\.[0-9]+)?%' | tail -1)"
if [[ -z "$PCT" ]]; then
  echo "FAIL (B4): could not parse a coverage percentage from: $TOTAL_LINE" >&2
  exit 1
fi
PCT_NUM="${PCT%\%}"
awk -v pct="$PCT_NUM" 'BEGIN {
  if (pct + 0 < 85.0) { printf "FAIL (B4): Phase-2 coverage %s < 85%% (CLAUDE.md floor)\n", pct"%" > "/dev/stderr"; exit 1 }
  printf "ok (B4): Phase-2 coverage %s >= 85%%\n", pct"%"
}'

echo "==> loop_budget_smoke: PASS"
