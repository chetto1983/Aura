#!/bin/sh
# Spike 099 live driver — a real four-goal fan-out whose every goal needs
# shell_exec, so the run measures worker durations AND proves whether a worker can
# dispatch a tool at all.
#
# Runs from a throwaway curlimages/curl container so .env secrets never touch a
# host shell (HANDOFF section 8). No secret is written by this script; the
# credentials arrive as env vars from --env-file and are only ever sent to the
# local cockpit.
#
#   docker run --rm --env-file .env --network aura_default \
#     -v "$PWD/.planning/spikes/099-worker-duration-and-progress:/work" \
#     --entrypoint sh curlimages/curl:latest /work/drive.sh
#
# The verdict is NOT read from this stream: the parent's prose has already lied
# about what its workers did once. Ground truth is the per-child transcripts under
# $AURA_RUN_DIR/<conv>/swarm/w*.jsonl, which the CONV printed below addresses.
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
# curl refuses to STORE a __Host- cookie over plain http, so read Set-Cookie
# off the response headers and hand it back as a plain Cookie header.
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

echo "== 4. start the fan-out =="
# The goals live in goals.txt beside this script so a re-run measures the SAME
# fan-out, and so a change to what the workers are asked shows up in the diff.
# They must be executable INSIDE the sandbox: the second run's goals asked for
# counts under a repo /workspace does not contain, and the workers spent their
# rounds searching for it instead of working.
PROMPT=$(sed 's/"/\\"/g' /work/goals.txt | tr -d '\015\012')
MSG_ID=$(uuid)
curl -sS -N -X POST "$BASE/agent/run" \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -H "X-AUTHULA-CSRF-TOKEN: $CSRF" \
  -H "Cookie: $COOKIE" \
  -H "Idempotency-Key: $(uuid)" \
  --data "{\"threadId\":\"$CONV\",\"messages\":[{\"id\":\"$MSG_ID\",\"role\":\"user\",\"content\":\"$PROMPT\"}]}" \
  > "$OUT/stream.sse" 2>"$OUT/stream.err" &
SSE_PID=$!

echo "== 5. wait for runId =="
RUN=""
i=0
while [ $i -lt 60 ]; do
  RUN=$(sed -n 's/.*"runId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$OUT/stream.sse" 2>/dev/null | head -1)
  [ -n "$RUN" ] && break
  i=$((i+1))
  sleep 1
done
[ -n "$RUN" ] || { echo "FATAL: no runId in stream after 60s" >&2; head -40 "$OUT/stream.sse" >&2; kill $SSE_PID 2>/dev/null || true; exit 1; }
echo "runId=$RUN"

echo "== 6. draining the stream =="
wait $SSE_PID 2>/dev/null || true

echo "== 7. verdict inputs =="
echo "--- workers spawned ---"
grep -o '"childID":"w[0-9]*"' "$OUT/stream.sse" | sort -u | tr '
' ' '; echo
echo "--- transcripts to read for GROUND TRUTH ---"
echo "  docker exec aura sh -c 'grep -c \"operation fingerprint mismatch\" \$AURA_RUN_DIR/$CONV/swarm/w*.jsonl'"
echo "  docker exec aura sh -c 'grep -c shell_exec \$AURA_RUN_DIR/$CONV/swarm/w*.jsonl'"
echo "--- child durations (from the daemon, not the stream) ---"
echo "  docker logs aura --since 10m | grep swarm.child.completed"
echo "--- terminal frame ---"
grep -o 'RUN_FINISHED\|RUN_ERROR' "$OUT/stream.sse" | tail -1 || echo "(no terminal frame)"
echo
echo "stream saved to $OUT/stream.sse ($(wc -c < "$OUT/stream.sse") bytes)"
