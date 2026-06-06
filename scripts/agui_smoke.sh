#!/usr/bin/env bash
# Phase 12 (Slice 8b) AG-UI gateway live SSE round-trip smoke (SC1/SC3 + amendment
# #57 REASONING_* lifecycle).
#
# This script ACTUALLY DRIVES the live `aura serve` HTTP daemon (CLAUDE.md
# NO-SKIP-AS-GREEN): it builds aura, seeds a conversation row, starts the daemon,
# polls 127.0.0.1:9080, then asserts the WIRE GROUND TRUTH (SSE frame event types,
# never the model's prose) of two endpoints —
#
#   - POST /agent/run  → an SSE stream that opens with `event: RUN_STARTED` and
#     reaches a terminal `event: RUN_FINISHED` (live leg) or RUN_FINISHED/RUN_ERROR
#     (degraded leg). On the LIVE OpenRouter leg it additionally asserts the
#     `event: REASONING_START` … `event: REASONING_END` lifecycle appears and a
#     REASONING_END precedes the first `event: TEXT_MESSAGE_START` (#57); and
#   - GET /threads/<id>/messages → an `MESSAGES_SNAPSHOT` JSON body with ≥1 message
#     (SC3; key-independent — proven on every leg).
#
# Two legs, selected by AGUI_SMOKE_LIVE (NOT by key presence — a CI dummy key is
# indistinguishable from a real one by presence alone):
#   - LIVE leg (AGUI_SMOKE_LIVE=1, requires a REAL OPENROUTER_API_KEY): the daemon's
#     agent turn calls DeepSeek-V4, so RUN_STARTED…RUN_FINISHED + the REASONING_*
#     lifecycle are HARD-asserted. This is the operator Gate-3 reference (Task 2).
#   - DEGRADED leg (default; FakeClient / CI): the daemon boots (config.Load
#     fail-fasts on an EMPTY key, so a dummy non-empty key is set when none is
#     present), the SSE pump + translator + daemon mount are exercised end-to-end, and
#     the terminal frame may be RUN_ERROR (a dummy key 401s at OpenRouter) —
#     RUN_STARTED + a terminal frame still prove the SC1 wire works against a real
#     daemon. The REASONING assertion is SKIPPED on this leg (no real CoT stream). The
#     CI smoke step runs this leg with Postgres up + CI=true armed.
#
# The conversation is seeded directly via `docker exec aura-postgres psql` (key-free,
# deterministic) against the SAME Postgres the daemon reads — the seed needs no LLM.
#
# It derives AURA_DB_URL / AURA_DB_MIGRATE_URL from POSTGRES_PASSWORD (the same DSN
# composition the agui db_integration tier uses). Under $CI with the DB env unset it
# exits non-zero (a CI job that cannot reach Postgres is misconfigured, never a silent
# pass); locally it skips with a clear hint.
#
# Exit codes:
#   0 — the live SSE round-trip + GET snapshot (+ REASONING_* on the live leg) passed
#   1 — an assertion failed (frame ordering, missing snapshot, daemon down, …)
#   2 — required env missing under $CI (no-skip-as-green) or a usage error
#
# Invoke (WSL, stack up):
#   wsl bash -lc 'set +H; export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; \
#     set -a; source <(tr -d "\r" < .env); set +a; bash scripts/agui_smoke.sh'
set -euo pipefail
set +H  # disable history expansion for any '!'-containing password

cd "$(git rev-parse --show-toplevel)"

PG_CONTAINER="${PG_CONTAINER:-aura-postgres}"
BIND="${AURA_AGUI_BIND:-127.0.0.1:9080}"
BASE="http://${BIND}"

# --- DB env (no-skip-as-green) ------------------------------------------------
if [[ -z "${POSTGRES_PASSWORD:-}" && -z "${AURA_DB_URL:-}" ]]; then
  if [[ -n "${CI:-}" ]]; then
    echo "FAIL: POSTGRES_PASSWORD/AURA_DB_URL unset under \$CI — the agui smoke must run live (no-skip-as-green)" >&2
    exit 2
  fi
  echo "SKIP: POSTGRES_PASSWORD unset — bring the stack up (make db-up) and source .env to run this smoke" >&2
  exit 0
fi

HOST="${PGHOST:-127.0.0.1}"
PORT="${PGPORT:-5432}"
if [[ -z "${AURA_DB_URL:-}" || -z "${AURA_DB_MIGRATE_URL:-}" ]]; then
  PWD_ENC="$(python3 -c "import urllib.parse,os;print(urllib.parse.quote(os.environ['POSTGRES_PASSWORD'],safe=''))")"
  export AURA_DB_URL="postgres://aura_app:${PWD_ENC}@${HOST}:${PORT}/aura?sslmode=disable"
  export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${PWD_ENC}@${HOST}:${PORT}/aura?sslmode=disable"
fi

# --- LIVE vs DEGRADED leg -----------------------------------------------------
# config.Load fail-fasts on an EMPTY OPENROUTER_API_KEY, so the daemon cannot boot
# without one. The LIVE leg is OPT-IN via AGUI_SMOKE_LIVE=1 (the operator Gate-3
# command) and requires a REAL key — REASONING_* is hard-asserted. The default
# DEGRADED leg sets a dummy key when none is present so the daemon boots for the wire
# smoke (the CI step).
LIVE=0
if [[ "${AGUI_SMOKE_LIVE:-0}" == "1" ]]; then
  if [[ -z "${OPENROUTER_API_KEY:-}" ]]; then
    echo "FAIL: AGUI_SMOKE_LIVE=1 requires a real OPENROUTER_API_KEY (the live OpenRouter leg)" >&2
    exit 2
  fi
  LIVE=1
  echo "==> agui_smoke: LIVE leg (AGUI_SMOKE_LIVE=1 + real key — REASONING_* hard-asserted)"
else
  if [[ -z "${OPENROUTER_API_KEY:-}" ]]; then
    export OPENROUTER_API_KEY="sk-agui-smoke-degraded-no-network"
  fi
  echo "==> agui_smoke: DEGRADED leg (SSE wire + GET snapshot only; REASONING skipped — set AGUI_SMOKE_LIVE=1 for the live OpenRouter leg)"
fi

# --- build --------------------------------------------------------------------
WORK="$(mktemp -d)"
BIN="${WORK}/aura"
cleanup() {
  if [[ -n "${SERVE_PID:-}" ]] && kill -0 "${SERVE_PID}" 2>/dev/null; then
    kill -TERM "${SERVE_PID}" 2>/dev/null || true
    wait "${SERVE_PID}" 2>/dev/null || true
  fi
  if [[ -n "${TID:-}" ]]; then
    docker exec -e PGPASSWORD="${POSTGRES_PASSWORD:-}" "${PG_CONTAINER}" \
      psql -U aura -d aura -v ON_ERROR_STOP=1 -c \
      "DELETE FROM aura.conversations WHERE id = '${TID}';" >/dev/null 2>&1 || true
  fi
  rm -rf "${WORK}" 2>/dev/null || true
}
trap cleanup EXIT

echo "==> building aura"
go build -o "${BIN}" ./cmd/aura

# --- seed a conversation + one user turn directly (key-free) -------------------
LOCAL_IDENTITY="00000000-0000-0000-0000-000000000001"
TID="$(python3 -c "import uuid;print(uuid.uuid4())")"
echo "==> seeding conversation ${TID} via docker exec ${PG_CONTAINER} psql"
# NB: pass each statement with -c (NOT a heredoc): `docker exec` without -i does not
# forward stdin, so a heredoc would be silently discarded and the seed would no-op.
docker exec -e PGPASSWORD="${POSTGRES_PASSWORD:-}" "${PG_CONTAINER}" \
  psql -U aura -d aura -v ON_ERROR_STOP=1 \
  -c "INSERT INTO aura.conversations (id, identity_id, model, status) VALUES ('${TID}', '${LOCAL_IDENTITY}', 'agui-smoke', 'active');" \
  -c "INSERT INTO aura.conversation_turns (conversation_id, seq, role, content) VALUES ('${TID}', 1, 'system', 'you are aura'), ('${TID}', 2, 'user', 'ciao dimmi 2+2');"

# --- start the daemon ---------------------------------------------------------
SERVE_LOG="${WORK}/serve.log"
echo "==> starting aura serve (bind ${BIND})"
"${BIN}" serve >"${SERVE_LOG}" 2>&1 &
SERVE_PID=$!

# Poll the port (no fixed sleep): the daemon logs "agui http server listening" and
# accepts connections within a few seconds.
READY=0
for _ in $(seq 1 60); do
  if ! kill -0 "${SERVE_PID}" 2>/dev/null; then
    echo "FAIL: aura serve exited during boot — log:" >&2
    cat "${SERVE_LOG}" >&2
    exit 1
  fi
  if curl -fsS -o /dev/null "${BASE}/threads/nonexistent/messages" 2>/dev/null \
     || curl -sS -o /dev/null -w '%{http_code}' "${BASE}/threads/nonexistent/messages" 2>/dev/null | grep -qE '^[0-9]{3}$'; then
    READY=1
    break
  fi
  sleep 0.5
done
if [[ "${READY}" -ne 1 ]]; then
  echo "FAIL: daemon did not accept connections on ${BIND} — log:" >&2
  cat "${SERVE_LOG}" >&2
  exit 1
fi
echo "==> daemon ready on ${BIND}"

# --- SC1: POST /agent/run SSE round-trip --------------------------------------
RUN_BODY="{\"threadId\":\"${TID}\",\"messages\":[{\"role\":\"user\",\"content\":\"ciao dimmi 2+2\"}]}"
echo "==> POST /agent/run (SSE)"
SSE="$(curl -sS -N -X POST "${BASE}/agent/run" \
  -H 'Content-Type: application/json' \
  -d "${RUN_BODY}" 2>&1)" || {
  echo "FAIL: POST /agent/run curl failed" >&2
  printf '%s\n' "${SSE}" >&2
  exit 1
}

echo "----- SSE frame event types -----"
printf '%s\n' "${SSE}" | grep -E '^event:' || true
echo "----- first SSE frame body (visual inspection, memory: inspect artifact not just PASS) -----"
printf '%s\n' "${SSE}" | sed -n '1,12p'
echo "---------------------------------"

if ! printf '%s\n' "${SSE}" | grep -q '^event: RUN_STARTED'; then
  echo "FAIL: SSE stream did not open with RUN_STARTED" >&2
  exit 1
fi
if ! printf '%s\n' "${SSE}" | grep -qE '^event: (RUN_FINISHED|RUN_ERROR)'; then
  echo "FAIL: SSE stream reached no terminal frame (RUN_FINISHED/RUN_ERROR)" >&2
  exit 1
fi

if [[ "${LIVE}" -eq 1 ]]; then
  # The live leg must end clean (RUN_FINISHED) and stream the REASONING_* lifecycle
  # BEFORE the first TEXT_MESSAGE_START (#57 interleave-before-text).
  if ! printf '%s\n' "${SSE}" | grep -q '^event: RUN_FINISHED'; then
    echo "FAIL: LIVE leg did not reach RUN_FINISHED (turn errored — check OPENROUTER_API_KEY/model)" >&2
    exit 1
  fi
  if ! printf '%s\n' "${SSE}" | grep -q '^event: REASONING_START'; then
    echo "FAIL: LIVE leg missing REASONING_START (amendment #57 — model not reasoning-capable?)" >&2
    exit 1
  fi
  if ! printf '%s\n' "${SSE}" | grep -q '^event: REASONING_END'; then
    echo "FAIL: LIVE leg missing REASONING_END (amendment #57)" >&2
    exit 1
  fi
  RSN_END_LINE="$(printf '%s\n' "${SSE}" | grep -nE '^event: REASONING_END' | head -1 | cut -d: -f1)"
  TXT_START_LINE="$(printf '%s\n' "${SSE}" | grep -nE '^event: TEXT_MESSAGE_START' | head -1 | cut -d: -f1)"
  if [[ -n "${TXT_START_LINE}" && -n "${RSN_END_LINE}" && "${RSN_END_LINE}" -ge "${TXT_START_LINE}" ]]; then
    echo "FAIL: REASONING_END (line ${RSN_END_LINE}) does not precede the first TEXT_MESSAGE_START (line ${TXT_START_LINE}) — #57 interleave broken" >&2
    exit 1
  fi
  echo "==> LIVE: RUN_STARTED…REASONING_START…REASONING_END (before TEXT)…RUN_FINISHED OK"
else
  echo "==> DEGRADED: RUN_STARTED + terminal frame OK (REASONING assertion skipped — no live CoT)"
fi

# --- SC3: GET /threads/<id>/messages snapshot ---------------------------------
echo "==> GET /threads/${TID}/messages"
SNAP="$(curl -sS "${BASE}/threads/${TID}/messages" 2>&1)" || {
  echo "FAIL: GET messages curl failed" >&2
  printf '%s\n' "${SNAP}" >&2
  exit 1
}
echo "----- GET snapshot body -----"
printf '%s\n' "${SNAP}" | head -c 800; echo
echo "-----------------------------"
if ! printf '%s\n' "${SNAP}" | grep -q 'MESSAGES_SNAPSHOT'; then
  echo "FAIL: GET snapshot did not return a MESSAGES_SNAPSHOT body" >&2
  exit 1
fi
if ! printf '%s\n' "${SNAP}" | grep -q 'ciao dimmi 2+2'; then
  echo "FAIL: GET snapshot missing the seeded user turn (≥1 message expected)" >&2
  exit 1
fi

# --- 404 chokepoint (T-12-11) -------------------------------------------------
echo "==> GET /threads/does-not-exist/messages (expect 404)"
CODE="$(curl -sS -o /dev/null -w '%{http_code}' "${BASE}/threads/does-not-exist/messages" 2>/dev/null || true)"
if [[ "${CODE}" != "404" ]]; then
  echo "FAIL: unknown thread returned ${CODE}, want 404" >&2
  exit 1
fi

echo "==> agui_smoke: PASS (leg=$([[ "${LIVE}" -eq 1 ]] && echo live || echo degraded), thread ${TID})"
