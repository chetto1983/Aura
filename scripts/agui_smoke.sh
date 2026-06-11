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
#     agent turn calls DeepSeek-V4 with AURA_SHOW_REASONING=1, so
#     RUN_STARTED…RUN_FINISHED + the REASONING_* lifecycle are HARD-asserted. This is
#     the operator Gate-3 reference (Task 2).
#   - DEGRADED leg (default; FakeClient / CI): the daemon boots (config.Load
#     fail-fasts on an EMPTY key, so a dummy non-empty key is set when none is
#     present), the SSE pump + translator + daemon mount are exercised end-to-end, and
#     the terminal frame may be RUN_ERROR (a dummy key 401s at OpenRouter) —
#     RUN_STARTED + a terminal frame still prove the SC1 wire works against a real
#     daemon. The REASONING assertion is SKIPPED on this leg (no real CoT stream). The
#     CI smoke step runs this leg with Postgres up + CI=true armed.
#
# The conversation is seeded directly over the SAME Postgres TCP DSN the daemon reads
# (key-free, deterministic) — the seed needs no LLM and no Docker CLI.
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
#     set -a; source <(awk '{ sub(/\r$/, ""); print }' .env); set +a; bash scripts/agui_smoke.sh'
set -euo pipefail
if (set +H) 2>/dev/null; then
  set +H  # disable history expansion for any '!'-containing password
fi

cd "$(git rev-parse --show-toplevel)"

if [[ -z "${AURA_AGUI_BIND:-}" ]]; then
  BIND="$(python3 - <<'PY'
import socket

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
    s.bind(("127.0.0.1", 0))
    print(f"127.0.0.1:{s.getsockname()[1]}")
PY
)"
  export AURA_AGUI_BIND="${BIND}"
else
  BIND="${AURA_AGUI_BIND}"
fi
BASE="http://${BIND}"
NULL_OUT="/dev/null"
WINDOWS_BASH=0
case "$(uname -s 2>/dev/null || true)" in
Windows_NT|MINGW*_NT*|MSYS*_NT*|CYGWIN*_NT*)
  NULL_OUT="NUL"
  WINDOWS_BASH=1
  ;;
esac

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
  export AURA_SHOW_REASONING=1
  LIVE=1
  echo "==> agui_smoke: LIVE leg (AGUI_SMOKE_LIVE=1 + real key + AURA_SHOW_REASONING=1 — REASONING_* hard-asserted)"
else
  if [[ -z "${OPENROUTER_API_KEY:-}" ]]; then
    export OPENROUTER_API_KEY="sk-agui-smoke-degraded-no-network"
  fi
  echo "==> agui_smoke: DEGRADED leg (SSE wire + GET snapshot only; REASONING skipped — set AGUI_SMOKE_LIVE=1 for the live OpenRouter leg)"
fi
if [[ -n "${AGUI_SMOKE_PROMPT:-}" ]]; then
  USER_PROMPT="${AGUI_SMOKE_PROMPT}"
elif [[ "${LIVE}" -eq 1 ]]; then
  USER_PROMPT="Senza usare strumenti, analizza un bug concorrente: due worker schedulano lo stesso job e a volte producono due side effect. Dammi una diagnosi breve e tre controlli tecnici."
else
  USER_PROMPT="ciao dimmi 2+2"
fi
if [[ "${LIVE}" -eq 1 ]]; then
  SSE_MAX_TIME="${AGUI_SMOKE_SSE_MAX_SEC:-360}"
else
  SSE_MAX_TIME="${AGUI_SMOKE_SSE_MAX_SEC:-90}"
fi
# --- build --------------------------------------------------------------------
WORK="$(mktemp -d)"
BIN="${WORK}/aura"
cleanup() {
  if [[ "${WINDOWS_BASH}" -eq 1 && -n "${BIN:-}" ]] && command -v powershell.exe >/dev/null 2>&1; then
    powershell.exe -NoProfile -Command "Get-Process -Name aura -ErrorAction SilentlyContinue | Where-Object { \$_.Path -like '*\Temp\tmp.*\aura' } | Stop-Process -Force" >/dev/null 2>&1 || true
  elif [[ "${SERVE_PID:-}" =~ ^[0-9]+$ && "${SERVE_PID:-0}" -gt 1 && "${SERVE_PID:-}" != "$$" ]] && kill -0 "${SERVE_PID}" 2>/dev/null; then
    kill -TERM "${SERVE_PID}" 2>/dev/null || true
    wait "${SERVE_PID}" 2>/dev/null || true
  fi
  if [[ -n "${TID:-}" ]]; then
    go run ./scripts/agui_db_seed.go delete "${TID}" >/dev/null 2>&1 || true
  fi
  if [[ "${WINDOWS_BASH}" -eq 0 ]]; then
    rm -rf "${WORK}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

echo "==> building aura"
go build -o "${BIN}" ./cmd/aura

# --- seed a conversation + one user turn directly (key-free) -------------------
LOCAL_IDENTITY="00000000-0000-0000-0000-000000000001"
TID="$(python3 -c "import uuid;print(uuid.uuid4())")"
echo "==> seeding conversation ${TID} via Postgres TCP"
go run ./scripts/agui_db_seed.go seed "${TID}" "${LOCAL_IDENTITY}" "${USER_PROMPT}"

# --- start the daemon ---------------------------------------------------------
SERVE_LOG="${WORK}/serve.log"
echo "==> starting aura serve (bind ${BIND})"
"${BIN}" serve >"${SERVE_LOG}" 2>&1 &
SERVE_PID=$!
if [[ "${WINDOWS_BASH}" -eq 1 ]]; then
  disown "${SERVE_PID}" 2>/dev/null || true
fi

# Poll the port (no fixed sleep): the daemon logs "agui http server listening" and
# accepts connections within a few seconds.
READY=0
for _ in $(seq 1 60); do
  if ! kill -0 "${SERVE_PID}" 2>/dev/null; then
    echo "FAIL: aura serve exited during boot — log:" >&2
    cat "${SERVE_LOG}" >&2
    exit 1
  fi
  if curl -fsS -o "${NULL_OUT}" "${BASE}/threads/nonexistent/messages" 2>/dev/null \
     || curl -sS -o "${NULL_OUT}" -w '%{http_code}' "${BASE}/threads/nonexistent/messages" 2>/dev/null | grep -qE '^[0-9]{3}$'; then
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
if grep -qiE 'address already in use|listen tcp.*bind|bind:|cannot assign requested address' "${SERVE_LOG}"; then
  echo "FAIL: daemon reported a bind/listen error on ${BIND} — log:" >&2
  cat "${SERVE_LOG}" >&2
  exit 1
fi
echo "==> daemon ready on ${BIND}"

# --- SC1: POST /agent/run SSE round-trip --------------------------------------
RUN_BODY="$(python3 - "${TID}" "${USER_PROMPT}" <<'PY'
import json
import sys

print(json.dumps({
    "threadId": sys.argv[1],
    "messages": [{"id": "m1", "role": "user", "content": sys.argv[2]}],
}, separators=(",", ":")))
PY
)"
echo "==> POST /agent/run (SSE)"
SSE="$(curl -sS -N --max-time "${SSE_MAX_TIME}" -X POST "${BASE}/agent/run" \
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

EVENTS="$(printf '%s\n' "${SSE}" | sed -n 's/^event: //p')"
FIRST_EVENT="$(printf '%s\n' "${EVENTS}" | sed -n '1p')"
LAST_EVENT="$(printf '%s\n' "${EVENTS}" | sed -n '$p')"
if [[ "${FIRST_EVENT}" != "RUN_STARTED" ]]; then
  echo "FAIL: SSE stream did not open with RUN_STARTED (first=${FIRST_EVENT:-none})" >&2
  exit 1
fi
case "${LAST_EVENT}" in
RUN_FINISHED|RUN_ERROR) ;;
*)
  echo "FAIL: SSE stream did not end with a terminal frame (last=${LAST_EVENT:-none})" >&2
  exit 1
  ;;
esac

if [[ "${LIVE}" -eq 1 ]]; then
  # The live leg must end clean (RUN_FINISHED) and stream the REASONING_* lifecycle
  # BEFORE the first TEXT_MESSAGE_START (#57 interleave-before-text).
  if [[ "${LAST_EVENT}" != "RUN_FINISHED" ]]; then
    echo "FAIL: LIVE leg did not reach RUN_FINISHED (turn errored — check OPENROUTER_API_KEY/model)" >&2
    exit 1
  fi
  if ! grep -q '^event: REASONING_START' <<< "${SSE}"; then
    echo "FAIL: LIVE leg missing REASONING_START (amendment #57 — model not reasoning-capable?)" >&2
    exit 1
  fi
  if ! grep -q '^event: REASONING_END' <<< "${SSE}"; then
    echo "FAIL: LIVE leg missing REASONING_END (amendment #57)" >&2
    exit 1
  fi
  RSN_END_LINE="$(printf '%s\n' "${SSE}" | grep -nE '^event: REASONING_END' | head -1 | cut -d: -f1 || true)"
  TXT_START_LINE="$(printf '%s\n' "${SSE}" | grep -nE '^event: TEXT_MESSAGE_START' | head -1 | cut -d: -f1 || true)"
  if [[ -z "${RSN_END_LINE}" || -z "${TXT_START_LINE}" ]]; then
    echo "FAIL: LIVE leg missing REASONING_END or TEXT_MESSAGE_START for ordering check" >&2
    exit 1
  fi
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
SNAP_CHECK="$(SNAP_JSON="${SNAP}" USER_PROMPT="${USER_PROMPT}" python3 - <<'PY'
import json
import os
import sys

try:
    body = json.loads(os.environ["SNAP_JSON"])
except Exception as exc:
    print(f"invalid_json:{exc}")
    sys.exit(1)
if body.get("type") != "MESSAGES_SNAPSHOT":
    print(f"wrong_type:{body.get('type')}")
    sys.exit(2)
messages = body.get("messages")
if not isinstance(messages, list):
    print("messages_not_list")
    sys.exit(3)
prompt = os.environ["USER_PROMPT"]
if not any(isinstance(m, dict) and m.get("role") == "user" and m.get("content") == prompt for m in messages):
    print("missing_prompt")
    sys.exit(4)
print(f"ok:{len(messages)}")
PY
)" || {
  echo "FAIL: GET snapshot validation failed (${SNAP_CHECK})" >&2
  exit 1
}
if false && ! grep -q 'MESSAGES_SNAPSHOT' <<< "${SNAP}"; then
  echo "FAIL: GET snapshot did not return a MESSAGES_SNAPSHOT body" >&2
  exit 1
fi
if false && ! grep -qF "${USER_PROMPT}" <<< "${SNAP}"; then
  echo "FAIL: GET snapshot missing the seeded user turn (≥1 message expected)" >&2
  exit 1
fi

# --- 404 chokepoint (T-12-11) -------------------------------------------------
echo "==> GET /threads/does-not-exist/messages (expect 404)"
CODE="$(curl -sS -o "${NULL_OUT}" -w '%{http_code}' "${BASE}/threads/does-not-exist/messages" 2>/dev/null || true)"
if [[ "${CODE}" != "404" ]]; then
  echo "FAIL: unknown thread returned ${CODE}, want 404" >&2
  exit 1
fi

echo "==> agui_smoke: PASS (leg=$([[ "${LIVE}" -eq 1 ]] && echo live || echo degraded), thread ${TID})"
cleanup
trap - EXIT
exit 0
