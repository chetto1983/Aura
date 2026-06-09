#!/usr/bin/env bash
# chat_50_prompt_gate.sh — unattended production-surface chat gate.
#
# Drives the real `aura chat new` REPL with 50 normal user prompts, then validates
# the persisted DB record. The gate checks:
#   - all 50 user prompts persisted in order
#   - every prompt has a non-empty assistant response
#   - every today-flagged prompt answer contains today's ISO date
#   - raw reasoning/CoT protocol fields are absent from chat output and DB rows
#   - tool invocation ledger rows have balanced start/end lifecycle facts
#
# This is a paid live gate when OPENROUTER_API_KEY is real. It is not wired into CI.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROMPTS="${AURA_CHAT_50_PROMPTS:-$ROOT/scripts/fixtures/chat_50_prompts.tsv}"
MAX_SEC="${AURA_CHAT_50_MAX_SEC:-2400}"
MIN_SCORE="${AURA_CHAT_50_MIN_SCORE:-95}"
MIN_TOOL_ENDS="${AURA_CHAT_50_MIN_TOOL_ENDS:-1}"
MAX_TOOLS_PER_REQUEST="${AURA_CHAT_50_MAX_TOOLS_PER_REQUEST:-8}"

cd "$ROOT"

if [[ -f .env ]]; then
  set -a
  # Strip only trailing CR so Windows-authored .env files do not corrupt values.
  # shellcheck disable=SC1090
  . <(awk '{ sub(/\r$/, ""); print }' .env)
  set +a
fi

export AURA_OTEL_EXPORTER="${AURA_OTEL_EXPORTER:-none}"
export AURA_LOOP_MAX_STEPS="${AURA_CHAT_50_MAX_STEPS_PER_TURN:-16}"
export AURA_LOOP_MAX_WALLCLOCK_SEC="${AURA_CHAT_50_WALLCLOCK_SEC_PER_TURN:-120}"

TMP_ROOT="${AURA_CHAT_50_TMP:-}"
if [[ -z "$TMP_ROOT" ]]; then
  if [[ -d "D:/tmp" ]]; then TMP_ROOT="D:/tmp"; else TMP_ROOT="${TMPDIR:-${TEMP:-${TMP:-/tmp}}}"; fi
fi
TMP_ROOT="${TMP_ROOT//\\//}"
TMP_ROOT="${TMP_ROOT%/}"
WS="$TMP_ROOT/aura-chat50-$(date +%s)-$$"
mkdir -p "$WS"

BIN="$WS/aura.exe"
INPUT="$WS/chat50.input"
LOG="$WS/chat50.log"
START_RFC="$(python - <<'PY'
import datetime
print(datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"))
PY
)"

echo "== chat50 gate: workspace $WS"
echo "== prompt fixture: $PROMPTS"

python - "$PROMPTS" "$INPUT" <<'PY'
import csv
import pathlib
import sys

src = pathlib.Path(sys.argv[1])
dst = pathlib.Path(sys.argv[2])
rows = []
with src.open("r", encoding="utf-8", newline="") as f:
    for raw in f:
        raw = raw.strip()
        if not raw or raw.startswith("#"):
            continue
        parts = raw.split("\t", 2)
        if len(parts) != 3:
            raise SystemExit(f"bad prompt fixture line: {raw!r}")
        rows.append(parts)
if len(rows) != 50:
    raise SystemExit(f"prompt fixture has {len(rows)} prompts, want 50")
with dst.open("w", encoding="utf-8", newline="\n") as f:
    for _, _, prompt in rows:
        f.write(prompt + "\n")
    f.write("/exit\n")
print(len(rows))
PY

echo "== building aura"
go build -o "$BIN" ./cmd/aura

if [[ "${AURA_CHAT_50_DB_UP:-0}" == "1" ]]; then
  echo "== starting DB via make db-up"
  make db-up
fi

echo "== db migrate + ping"
"$BIN" db migrate
"$BIN" db ping

echo "== running aura chat new with 50 prompts (timeout ${MAX_SEC}s)"
set +e
python - "$BIN" "$INPUT" "$LOG" "$MAX_SEC" <<'PY'
import pathlib
import subprocess
import sys

bin_path, input_path, log_path, max_sec = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
data = pathlib.Path(input_path).read_bytes()
with open(log_path, "wb") as log:
    try:
        proc = subprocess.run(
            [bin_path, "chat", "new"],
            input=data,
            stdout=log,
            stderr=subprocess.STDOUT,
            timeout=max_sec,
        )
    except subprocess.TimeoutExpired:
        log.write(f"\nTIMEOUT after {max_sec}s\n".encode())
        raise SystemExit(124)
raise SystemExit(proc.returncode)
PY
CHAT_RC=$?
set -e
echo "== aura chat exit: $CHAT_RC"
if [[ "$CHAT_RC" -ne 0 ]]; then
  echo "FAIL: aura chat exited non-zero; last 80 log lines:" >&2
  tail -80 "$LOG" >&2 || true
  exit "$CHAT_RC"
fi

echo "== validating DB + log"
go run ./scripts/chat_50_db_probe.go \
  --prompts "$PROMPTS" \
  --log "$LOG" \
  --since "$START_RFC" \
  --min-score "$MIN_SCORE" \
  --min-tool-ends "$MIN_TOOL_ENDS" \
  --max-tools-per-request "$MAX_TOOLS_PER_REQUEST"

echo "== chat50 gate: PASS"
echo "== log: $LOG"
