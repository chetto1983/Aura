#!/bin/sh
# Phase 51 plan 51-09 Task 3 live driver (D-03 inactivity reaping).
#   MODE=long  -> one background worker doing >4 minutes of REAL work in many
#                 short shell_exec rounds; must COMPLETE (old wall clock killed it at ~240s)
#   MODE=stall -> one background worker whose first tool call hangs (sleep with an
#                 explicit 590s timeout_ms); must be reaped at ~AURA_SWARM_CHILD_IDLE_SEC
# Runs from a throwaway curlimages/curl container (secrets via --env-file, never a host shell):
#   MSYS_NO_PATHCONV=1 docker run --rm --env-file .env --network aura_default \
#     -e MODE=long -v "<scratch>/d03:/work" --entrypoint sh curlimages/curl:latest /work/drive.sh
# Verdicts are NOT read from the SSE stream (spike 098): ground truth is the daemon log
# (swarm.child.completed / stalled), aura.ingestion_jobs, aura.conversation_turns and the
# per-child transcript served by GET /api/conversations/<conv>/swarm/<child>/transcript.
set -eu

BASE="${BASE:-http://aura:9080}"
MODE="${MODE:-long}"
OUT=/work/out/$MODE
mkdir -p "$OUT"

uuid() { cat /proc/sys/kernel/random/uuid; }
trim() { printf '%s' "$1" | tr -d ' \r\n'; }

EMAIL=$(trim "${AURA_E2E_AUTHULA_EMAIL:-}")
PASSWORD="${AURA_E2E_AUTHULA_PASSWORD:-}"
[ -n "$EMAIL" ] && [ -n "$PASSWORD" ] || { echo "FATAL: AURA_E2E_AUTHULA_EMAIL / _PASSWORD not in env" >&2; exit 1; }

echo "== 1. csrf =="
CSRF=$(curl -sS "$BASE/api/auth/config" | sed -n 's/.*"csrf_token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
[ -n "$CSRF" ] || { echo "FATAL: no csrf_token" >&2; exit 1; }

echo "== 2. sign-in =="
curl -sS -D "$OUT/signin.h" -o "$OUT/signin.json" \
  -X POST "$BASE/auth/email-password/sign-in" \
  -H "Content-Type: application/json" \
  -H "X-AUTHULA-CSRF-TOKEN: $CSRF" \
  -H "Cookie: __Host-authula_csrf_token=$CSRF" \
  --data "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}"
SESSION=$(sed -n 's/^[Ss]et-[Cc]ookie:[[:space:]]*\([^;]*\).*/\1/p' "$OUT/signin.h" | tr '\n' ';' | sed 's/;$//')
[ -n "$SESSION" ] || { echo "FATAL: sign-in set no cookie:" >&2; cat "$OUT/signin.json" >&2; exit 1; }
COOKIE="$SESSION; __Host-authula_csrf_token=$CSRF"
printf '%s' "$COOKIE" > "$OUT/cookie.txt"
printf '%s' "$CSRF" > "$OUT/csrf.txt"

echo "== 3. conversation =="
CONV=$(curl -sS -X POST "$BASE/api/conversations" \
  -H "Content-Type: application/json" \
  -H "X-AUTHULA-CSRF-TOKEN: $CSRF" \
  -H "Cookie: $COOKIE" \
  -H "Idempotency-Key: $(uuid)" \
  --data '{}' | sed -n 's/.*"ID"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
[ -n "$CONV" ] || { echo "FATAL: no conversation id" >&2; exit 1; }
echo "conversation=$CONV"
printf '%s' "$CONV" > "$OUT/conv.txt"

PROMPT=$(sed 's/"/\\"/g' "/work/goal-$MODE.txt" | tr -d '\015\012')

echo "== 4. delegate ($MODE) =="
MSG_ID=$(uuid)
date -u +%Y-%m-%dT%H:%M:%SZ > "$OUT/started_at.txt"
START=$(date +%s)
curl -sS -N -X POST "$BASE/agent/run" \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -H "X-AUTHULA-CSRF-TOKEN: $CSRF" \
  -H "Cookie: $COOKIE" \
  -H "Idempotency-Key: $(uuid)" \
  --data "{\"threadId\":\"$CONV\",\"messages\":[{\"id\":\"$MSG_ID\",\"role\":\"user\",\"content\":\"$PROMPT\"}]}" \
  > "$OUT/stream1.sse" 2>"$OUT/stream1.err" &
SSE_PID=$!
i=0
while [ $i -lt 180 ]; do
  grep -q 'RUN_FINISHED\|RUN_ERROR' "$OUT/stream1.sse" 2>/dev/null && break
  i=$((i+1)); sleep 1
done
wait $SSE_PID 2>/dev/null || true
echo "operator turn returned after $(( $(date +%s) - START ))s: $(grep -o 'RUN_FINISHED\|RUN_ERROR' "$OUT/stream1.sse" | tail -1)"
echo "--- children announced on the wire ---"
grep -o '"childID":"[^"]*"' "$OUT/stream1.sse" | sort -u | tr '\n' ' '; echo
echo "--- swarm_spawn call args (if any) ---"
grep -o '"toolCallName":"swarm_spawn"' "$OUT/stream1.sse" | head -1 || echo "(no swarm_spawn tool call on the wire)"

echo "== 5. done; poll with poll.sh =="
echo "conv=$CONV mode=$MODE out=$OUT"
