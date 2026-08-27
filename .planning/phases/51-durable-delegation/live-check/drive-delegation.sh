#!/bin/sh
# 51-01 Task 3 live driver: proves a top-level swarm_spawn call returns the
# turn BEFORE the workers finish, and that the operator can keep talking on
# the same thread immediately after. Modelled on
# .planning/spikes/098-steer-carries-worker-result/drive.sh.
set -eu

BASE="${BASE:-http://aura:9080}"
OUT=/work/out
mkdir -p "$OUT"

uuid() { cat /proc/sys/kernel/random/uuid; }
trim() { printf '%s' "$1" | tr -d ' \r\n'; }

EMAIL=$(trim "${AURA_E2E_AUTHULA_EMAIL:-}")
PASSWORD="${AURA_E2E_AUTHULA_PASSWORD:-}"
if [ -z "$EMAIL" ] || [ -z "$PASSWORD" ]; then
  echo "FATAL: AURA_E2E_AUTHULA_EMAIL / _PASSWORD not in env" >&2
  exit 1
fi

echo "== 1. csrf =="
CSRF=$(curl -sS "$BASE/api/auth/config" | sed -n 's/.*"csrf_token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
[ -n "$CSRF" ] || { echo "FATAL: no csrf_token" >&2; exit 1; }
echo "csrf ok (${#CSRF} chars)"

echo "== 2. sign-in =="
curl -sS -D "$OUT/signin.h" -o "$OUT/signin.json" \
  -X POST "$BASE/auth/email-password/sign-in" \
  -H "Content-Type: application/json" \
  -H "X-AUTHULA-CSRF-TOKEN: $CSRF" \
  -H "Cookie: __Host-authula_csrf_token=$CSRF" \
  --data "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}"

SESSION=$(sed -n 's/^[Ss]et-[Cc]ookie:[[:space:]]*\([^;]*\).*/\1/p' "$OUT/signin.h" | tr '\n' ';' | sed 's/;$//')
if [ -z "$SESSION" ]; then
  echo "FATAL: sign-in set no cookie. body:" >&2
  cat "$OUT/signin.json" >&2
  exit 1
fi
COOKIE="$SESSION; __Host-authula_csrf_token=$CSRF"
echo "session cookie captured"

echo "== 3. conversation =="
CONV=$(curl -sS -X POST "$BASE/api/conversations" \
  -H "Content-Type: application/json" \
  -H "X-AUTHULA-CSRF-TOKEN: $CSRF" \
  -H "Cookie: $COOKIE" \
  -H "Idempotency-Key: $(uuid)" \
  --data '{}' | sed -n 's/.*"ID"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
[ -n "$CONV" ] || { echo "FATAL: no conversation id" >&2; exit 1; }
echo "conversation=$CONV"

echo "== 4. ask for a 2-goal background delegation =="
PROMPT='Usa swarm_spawn per delegare in background questi due sottotask indipendenti, NON eseguirli tu direttamente: (1) esegui shell_exec con `date` e riporta output. (2) esegui shell_exec con `sleep 5 && echo GOAL-DUE-DONE` e riporta output. Non aspettare i risultati, rispondimi appena i worker sono stati messi in coda.'
MSG_ID=$(uuid)
START_EPOCH=$(date +%s)
curl -sS -N -X POST "$BASE/agent/run" \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -H "X-AUTHULA-CSRF-TOKEN: $CSRF" \
  -H "Cookie: $COOKIE" \
  -H "Idempotency-Key: $(uuid)" \
  --data "{\"threadId\":\"$CONV\",\"messages\":[{\"id\":\"$MSG_ID\",\"role\":\"user\",\"content\":\"$PROMPT\"}]}" \
  > "$OUT/stream1.sse" 2>"$OUT/stream1.err" &
SSE_PID=$!

echo "== 5. wait for RUN_FINISHED (the operator's turn returning) =="
i=0
while [ $i -lt 90 ]; do
  if grep -q 'RUN_FINISHED\|RUN_ERROR' "$OUT/stream1.sse" 2>/dev/null; then
    break
  fi
  i=$((i+1))
  sleep 1
done
wait $SSE_PID 2>/dev/null || true
END_EPOCH=$(date +%s)
ELAPSED=$((END_EPOCH - START_EPOCH))
echo "first turn wall-clock: ${ELAPSED}s"
echo "--- terminal frame ---"
grep -o 'RUN_FINISHED\|RUN_ERROR' "$OUT/stream1.sse" | tail -1 || echo "(no terminal frame)"

echo "== 6. immediately send a SECOND message on the same thread =="
MSG_ID2=$(uuid)
SECOND_START=$(date +%s)
curl -sS -N -X POST "$BASE/agent/run" \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -H "X-AUTHULA-CSRF-TOKEN: $CSRF" \
  -H "Cookie: $COOKIE" \
  -H "Idempotency-Key: $(uuid)" \
  --data "{\"threadId\":\"$CONV\",\"messages\":[{\"id\":\"$MSG_ID2\",\"role\":\"user\",\"content\":\"Nel frattempo, dimmi solo: che ore sono?\"}]}" \
  > "$OUT/stream2.sse" 2>"$OUT/stream2.err" &
SSE_PID2=$!
i=0
while [ $i -lt 60 ]; do
  if grep -q 'RUN_FINISHED\|RUN_ERROR' "$OUT/stream2.sse" 2>/dev/null; then
    break
  fi
  i=$((i+1))
  sleep 1
done
wait $SSE_PID2 2>/dev/null || true
SECOND_END=$(date +%s)
echo "second turn wall-clock: $((SECOND_END - SECOND_START))s"
echo "--- second turn terminal frame ---"
grep -o 'RUN_FINISHED\|RUN_ERROR' "$OUT/stream2.sse" | tail -1 || echo "(no terminal frame)"

echo "== 7. did the first turn's reply mention background/queued dispatch? =="
grep -oi '"delta"[^}]*' "$OUT/stream1.sse" | head -20 || true

echo
echo "conversation id: $CONV"
echo "streams saved to $OUT/stream1.sse and $OUT/stream2.sse"
