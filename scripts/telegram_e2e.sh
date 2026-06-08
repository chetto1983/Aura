#!/usr/bin/env bash
# Phase 13 (Slice 9 / UX-02·03·04) Telegram + multimodal live E2E scorer.
#
# Drives the REAL surfaces (CLAUDE.md NO-SKIP-AS-GREEN) and prints a pass score:
#
#   Integration tiers (response-asserted go tests, the spike-017/019 ground truth =
#   the Bot-API Send reply, never getUpdates):
#     - telegram_integration:  sendPhoto (table→PNG) / sendDocument / sendVoice
#     - multimodal_integration: TTS→STT round-trip / GLM-OCR describe / TTS opus bytes
#
#   Setup-wizard HTTP E2E (the loopback :9081 UX-03 API, driven via `aura serve`):
#     - GET  /setup/status         no token  → 401   (the mandatory gate, T-13-07)
#     - GET  /setup/status         w/ token   → 200
#     - POST /setup/token          bot token  → 200 + bot_username (live getMe)
#     - POST /setup/onboard-link              → 200 + t.me/<bot>?start=<uuid> deep link
#     - onboarding mint round-trip            → the pending row lands in Postgres
#
# Requires (from .env): POSTGRES_PASSWORD, TELEGRAM_BOT_TOKEN, AURA_E2E_CHAT_ID,
# OPENROUTER_API_KEY, NEO4J_PASSWORD; and the 9c sidecars up (aura-stt :9000,
# aura-tts :8880, aura-ocr-vl :8082). Composes AURA_DB_URL/MIGRATE_URL from
# POSTGRES_PASSWORD (the same DSN shape the db_integration tier uses).
#
# Exit 0 if score >= the threshold (default 90), else 1.
set -uo pipefail
set +H

cd "$(git rev-parse --show-toplevel)"

PG_CONTAINER="${PG_CONTAINER:-aura-postgres}"
SETUP_BIND="127.0.0.1:9081"
SETUP_BASE="http://${SETUP_BIND}"
THRESHOLD="${E2E_THRESHOLD:-90}"

PASS=0
TOTAL=0
declare -a RESULTS
score() { # name, ok(0/1), detail
  TOTAL=$((TOTAL+1))
  if [[ "$2" == "1" ]]; then PASS=$((PASS+1)); RESULTS+=("PASS  $1  — $3"); else RESULTS+=("FAIL  $1  — $3"); fi
}

# --- env (individual extraction; .env has JSON values that break `source`) ------
getenv() { grep -E "^$1=" .env | head -1 | cut -d= -f2- | tr -d '\r' | sed -E "s/^'//;s/'$//;s/^\"//;s/\"$//"; }
export POSTGRES_PASSWORD="$(getenv POSTGRES_PASSWORD)"
export TELEGRAM_BOT_TOKEN="$(getenv TELEGRAM_BOT_TOKEN)"
export AURA_E2E_CHAT_ID="$(getenv AURA_E2E_CHAT_ID)"
export OPENROUTER_API_KEY="$(getenv OPENROUTER_API_KEY)"
export NEO4J_PASSWORD="$(getenv NEO4J_PASSWORD)"
export STT_BASE_URL="${STT_BASE_URL:-http://127.0.0.1:9000/v1}"
export TTS_BASE_URL="${TTS_BASE_URL:-http://127.0.0.1:8880/v1}"
export MULTIMODAL_BASE_URL="${MULTIMODAL_BASE_URL:-http://127.0.0.1:8082/v1}"
HOST="${PGHOST:-127.0.0.1}"; PORT="${PGPORT:-5432}"
PWD_ENC="$(python3 -c "import urllib.parse,os;print(urllib.parse.quote(os.environ['POSTGRES_PASSWORD'],safe=''))")"
export AURA_DB_URL="postgres://aura_app:${PWD_ENC}@${HOST}:${PORT}/aura?sslmode=disable"
export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${PWD_ENC}@${HOST}:${PORT}/aura?sslmode=disable"

[[ -z "$POSTGRES_PASSWORD" || -z "$TELEGRAM_BOT_TOKEN" || -z "$AURA_E2E_CHAT_ID" ]] && {
  echo "FATAL: POSTGRES_PASSWORD / TELEGRAM_BOT_TOKEN / AURA_E2E_CHAT_ID must be in .env" >&2; exit 2; }

# --- 1) integration tiers (response-asserted) ---------------------------------
echo "==> [1/2] integration tiers (telegram_integration + multimodal_integration)"
run_tier() { # tag, runregex, label
  local out rc
  out="$(unset CI; go test -tags "$1" -count=1 -timeout 300s -run "$2" -v ./internal/channels/telegram/ 2>&1)"; rc=$?
  while IFS= read -r line; do
    case "$line" in
      "--- PASS: "*) n="${line#--- PASS: }"; n="${n%% *}"; score "$n" 1 "$1" ;;
      "--- FAIL: "*) n="${line#--- FAIL: }"; n="${n%% *}"; score "$n" 0 "$1 ($(printf '%s' "$out" | grep -A2 "$n" | grep -iE 'fatal|error|HTTP' | head -1))" ;;
    esac
  done < <(printf '%s\n' "$out")
}
run_tier telegram_integration 'TestLiveSend' telegram
run_tier multimodal_integration 'TestLiveTTSThenSTTRoundTrip|TestLivePhotoOCRRoundTrip|TestLiveTTSVoiceBytes' multimodal

# --- 2) setup-wizard HTTP E2E via aura serve ----------------------------------
echo "==> [2/2] setup-wizard :9081 E2E (aura serve)"
export AURA_SETUP_TOKEN="e2e-setup-$(python3 -c 'import uuid;print(uuid.uuid4().hex[:16])')"
export AURA_SETUP_BIND="${SETUP_BIND}"
export AURA_VISION_CLOUD=false
WORK="$(mktemp -d)"; BIN="${WORK}/aura"; SERVE_LOG="${WORK}/serve.log"
SERVE_PID=""
cleanup() {
  [[ -n "$SERVE_PID" ]] && kill -0 "$SERVE_PID" 2>/dev/null && { kill -TERM "$SERVE_PID" 2>/dev/null; wait "$SERVE_PID" 2>/dev/null; }
  rm -rf "$WORK" 2>/dev/null || true
}
trap cleanup EXIT

go build -o "$BIN" ./cmd/aura || { echo "FATAL: build failed" >&2; exit 1; }
"$BIN" serve >"$SERVE_LOG" 2>&1 &
SERVE_PID=$!

READY=0
for _ in $(seq 1 60); do
  kill -0 "$SERVE_PID" 2>/dev/null || { echo "FATAL: aura serve exited:" >&2; tail -20 "$SERVE_LOG" >&2; break; }
  code="$(curl -s -o /dev/null -w '%{http_code}' "${SETUP_BASE}/setup/status" 2>/dev/null || true)"
  [[ "$code" == "401" ]] && { READY=1; break; }   # server up + gate active
  sleep 1
done
if [[ "$READY" == "1" ]]; then
  T="$AURA_SETUP_TOKEN"
  # 401 no-token (the readiness probe already saw it)
  score "setup_status_401_no_token" 1 "gate active"
  # 200 with token
  c="$(curl -s -o /dev/null -w '%{http_code}' "${SETUP_BASE}/setup/status?token=${T}" 2>/dev/null)"
  score "setup_status_200_with_token" "$([[ "$c" == 200 ]] && echo 1 || echo 0)" "got $c"
  # token getMe-validate
  tr="$(curl -s "${SETUP_BASE}/setup/token?token=${T}" -X POST -H 'Content-Type: application/json' -d "{\"token\":\"${TELEGRAM_BOT_TOKEN}\"}" 2>/dev/null)"
  echo "$tr" | grep -q '"bot_username"' && score "setup_token_getme_validate" 1 "$(echo "$tr" | head -c 80)" || score "setup_token_getme_validate" 0 "$(echo "$tr" | head -c 120)"
  # onboard-link mint
  ol="$(curl -s "${SETUP_BASE}/setup/onboard-link?token=${T}" -X POST 2>/dev/null)"
  link="$(echo "$ol" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("deep_link",""))' 2>/dev/null || true)"
  if echo "$link" | grep -qE 'https://t\.me/.+\?start=[0-9a-f-]{36}'; then
    score "setup_onboard_link_deeplink" 1 "$link"
    tok="${link##*start=}"
    # mint round-trip: the pending row landed in Postgres
    cnt="$(docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" "$PG_CONTAINER" psql -U aura -d aura -tAc "SELECT count(*) FROM aura.telegram_setup_pending WHERE onboarding_token='${tok}';" 2>/dev/null | tr -d '[:space:]')"
    score "onboarding_mint_persisted" "$([[ "$cnt" == 1 ]] && echo 1 || echo 0)" "pending rows=$cnt"
  else
    score "setup_onboard_link_deeplink" 0 "$(echo "$ol" | head -c 120)"
    score "onboarding_mint_persisted" 0 "no link to mint"
  fi
else
  for n in setup_status_401_no_token setup_status_200_with_token setup_token_getme_validate setup_onboard_link_deeplink onboarding_mint_persisted; do score "$n" 0 "serve not ready"; done
fi

# --- report -------------------------------------------------------------------
echo ""
echo "================ Phase-13 E2E results ================"
for r in "${RESULTS[@]}"; do echo "  $r"; done
PCT=$(( PASS * 100 / TOTAL ))
echo "-----------------------------------------------------"
echo "  SCORE: ${PASS}/${TOTAL} = ${PCT}%  (threshold ${THRESHOLD}%)"
echo "====================================================="
[[ "$PCT" -ge "$THRESHOLD" ]] && exit 0 || exit 1
