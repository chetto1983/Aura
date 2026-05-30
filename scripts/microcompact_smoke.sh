#!/usr/bin/env bash
# Phase 4 SC-1 + L1/L2.5 microcompact CI-grep contract (mirrors loop_budget_smoke.sh).
#
# This script ACTUALLY RUNS the conversations db_integration tier against a live
# Postgres (CLAUDE.md NO-SKIP-AS-GREEN): it exercises the deterministic context
# ladder end-to-end and asserts the ground-truth from the test output —
#
#   - L1 microcompact evicts an old role='tool' turn to a read_tool_output(<id>)
#     pointer (the sidecar stays fetchable via the read_tool_output builtin), and
#   - SC-1: when L1 ALONE brings a tool-output-bloated history under budget, ZERO
#     context_rot_events rows are written (tool-clearing is exhausted before the
#     heavier L2.5 pair-drop), and
#   - L2.5: a history past the hard cap drops the oldest user/assistant PAIR and
#     writes exactly one context_rot_events row with action='hard_drop_pairs',
#     leaving the non-system remainder even.
#
# It derives AURA_DB_URL / AURA_DB_MIGRATE_URL from POSTGRES_PASSWORD (the same DSN
# composition the conversations integration runner uses). Under $CI with no stack
# the db_integration envOrSkip helper t.Fatal's (fail-loud), so a skipped/empty run
# fails the line-count + grep gates below rather than passing green.
#
# Invoke (WSL, stack up):
#   wsl bash -lc 'set +H; export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; \
#     set -a; source ~/aura.env; set +a; bash scripts/microcompact_smoke.sh'
set -euo pipefail
set +H  # disable history expansion for the '!'-containing password

cd "$(git rev-parse --show-toplevel)"

# --- DSN composition (mirrors run_conversations_integration.sh) ---------------
if [[ -z "${AURA_DB_URL:-}" || -z "${AURA_DB_MIGRATE_URL:-}" ]]; then
  : "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD must be set (or AURA_DB_URL/AURA_DB_MIGRATE_URL)}"
  HOST="${PGHOST:-127.0.0.1}"
  PORT="${PGPORT:-5432}"
  PWD_ENC="$(python3 -c "import urllib.parse,os;print(urllib.parse.quote(os.environ['POSTGRES_PASSWORD'],safe=''))")"
  export AURA_DB_URL="postgres://aura_app:${PWD_ENC}@${HOST}:${PORT}/aura?sslmode=disable"
  export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${PWD_ENC}@${HOST}:${PORT}/aura?sslmode=disable"
  export PGHOST="$HOST" PGPORT="$PORT"
fi

# The two live tests that assert the L1/SC-1 + L2.5 ground-truth (context_rot_events
# row state + the read_tool_output L1 pointer + hard_drop_pairs action).
RUN_RE='TestLoadManagedHistory_SC1_NoRotEventOnL1Alone|TestLoadManagedHistory_L2_5_WritesRotEvent'

echo "==> microcompact: L1 eviction + SC-1 zero-rot + L2.5 hard_drop_pairs (db_integration, live)"
OUT="$(go test -tags db_integration -race -count=1 -v \
  -run "$RUN_RE" ./internal/conversations/ 2>&1)" || {
  echo "$OUT" >&2
  echo "FAIL: conversations db_integration tier failed (stack down or assertion failed)" >&2
  exit 1
}
printf '%s\n' "$OUT"

# --- NO-SKIP-AS-GREEN: the two tests MUST have actually RUN ---------------------
# `[no tests to run]` (a stale -run name) or a skipped tier is a false green; assert
# both RUN markers + PASS appear, and that nothing was skipped.
for name in TestLoadManagedHistory_SC1_NoRotEventOnL1Alone TestLoadManagedHistory_L2_5_WritesRotEvent; do
  if ! printf '%s\n' "$OUT" | grep -q "=== RUN   ${name}\b"; then
    echo "FAIL: ${name} did not RUN (stale -run name or skipped tier — false green)" >&2
    exit 1
  fi
  if ! printf '%s\n' "$OUT" | grep -q -- "--- PASS: ${name}\b"; then
    echo "FAIL: ${name} did not PASS" >&2
    exit 1
  fi
done
if printf '%s\n' "$OUT" | grep -Eq -- '--- SKIP:|\[no tests to run\]'; then
  echo "FAIL: a microcompact test was SKIPPED (no-skip-as-green: integration must run live)" >&2
  exit 1
fi
if ! printf '%s\n' "$OUT" | grep -Eq '^(ok|PASS)'; then
  echo "FAIL: the test binary did not report a top-level PASS" >&2
  exit 1
fi

# --- Grep contract: the contract literals must be present in the suite source ----
# (the CI-grep half — the file is the durable contract for L1 + L2.5).
if ! grep -q "context_rot_events" internal/conversations/context.go; then
  echo "FAIL: context_rot_events emission missing from the ladder" >&2
  exit 1
fi
if ! grep -q "read_tool_output" internal/conversations/context.go; then
  echo "FAIL: read_tool_output L1 pointer missing from the ladder" >&2
  exit 1
fi
if ! grep -q "hard_drop_pairs" internal/conversations/context.go; then
  echo "FAIL: hard_drop_pairs action missing from the L2.5 path" >&2
  exit 1
fi

echo "==> microcompact_smoke: PASS"
