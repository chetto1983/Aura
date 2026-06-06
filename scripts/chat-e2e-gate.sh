#!/usr/bin/env bash
# chat-e2e-gate.sh — the Phase-11 E2E gate on the REAL product surface (#53/D-42).
#
# Operator directive 2026-06-06: the synthetic cot_eval harness does not count —
# "l'unico test vero è con aura chat, quello che l'utente usa". This gate builds
# the real binary, drives `aura chat new` one-shot with the NATURAL North-Star
# prompt (no hint words), and scores GROUND TRUTH only:
#
#   C1 clean exit            — the piped chat session exits 0
#   C2 fresh .xlsx           — an .xlsx newer than gate start exists in the workspace cwd
#   C3 re-opens              — openpyxl loads it (a real spreadsheet, not a stub)
#   C4 today's date          — the sheet body or filename carries today's date
#   C5 turns persisted       — PG has the user turn AND a non-empty assistant answer
#   C6 wall-clock budget     — end-to-end under AURA_CHAT_GATE_MAX_SEC (default 420s)
#
# Score = passed/6. Gate threshold: >90% (i.e. 6/6 — with six hard checks, one
# failure is 83%). Timing is reported per phase (build / chat / verify).
#
# Run from the repo root (or anywhere; it cds itself). Requires: the compose stack
# up (postgres + searxng), .env present, host python with openpyxl, docker CLI.
#
#   ./scripts/chat-e2e-gate.sh
#
# PAID + LIVE: one real model round via OpenRouter. Operator-run, never CI.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MAX_SEC="${AURA_CHAT_GATE_MAX_SEC:-420}"
PROMPT="Fammi un file Excel con il mercato di Yahoo Finance di oggi. Voglio un .xlsx vero che si apra in un foglio di calcolo, con i dati di oggi."

cd "$ROOT"
set -a; . ./.env; set +a
export AURA_SKILLS_DIR="${AURA_SKILLS_DIR:-$USERPROFILE/.aura/skills}"
export AURA_RUN_DIR="${AURA_RUN_DIR:-D:/tmp/aura-run}"

# mktemp is unreliable under MSYS (empty output observed live) — plain mkdir.
WS="/d/tmp/aura-chat-gate-$(date +%s)-$$"
mkdir -p "$WS" || { echo "FATAL: cannot create workspace $WS"; exit 2; }
echo "== chat E2E gate: workspace $WS"

T_BUILD0=$(date +%s)
go build -o "$WS/aura.exe" ./cmd/aura || { echo "FATAL: build failed"; exit 2; }
T_BUILD1=$(date +%s)

cd "$WS" || { echo "FATAL: cannot cd into $WS"; exit 2; }
touch .gate-start
T_CHAT0=$(date +%s)
printf '%s\n/exit\n' "$PROMPT" | ./aura.exe chat new > chat.log 2>&1
EC=$?
T_CHAT1=$(date +%s)
CHAT_SEC=$((T_CHAT1 - T_CHAT0))

pass=0; fail=0
check() { # check <id> <desc> <0-ok>
  if [ "$3" -eq 0 ]; then pass=$((pass+1)); echo "  PASS $1 $2"
  else fail=$((fail+1)); echo "  FAIL $1 $2"; fi
}

echo "== checks"
check C1 "clean exit (got $EC)" "$EC"

XLSX="$(find . -maxdepth 2 -name '*.xlsx' -newer .gate-start 2>/dev/null | head -1)"
[ -n "$XLSX" ]; check C2 "fresh .xlsx in workspace (${XLSX:-none})" $?

if [ -n "$XLSX" ]; then
  python - "$XLSX" <<'PYEOF'
import sys, datetime
from openpyxl import load_workbook
p = sys.argv[1]
wb = load_workbook(p)
today = datetime.date.today()
forms = {today.isoformat(), today.strftime("%d/%m/%Y"), today.strftime("%d-%m-%Y")}
hay = p
for ws in wb.worksheets:
    for row in ws.iter_rows(values_only=True):
        hay += " " + " ".join(str(v) for v in row if v is not None)
sys.exit(0 if any(f in hay for f in forms) else 3)
PYEOF
  PYRC=$?
  if [ "$PYRC" -eq 0 ]; then check C3 "re-opens via openpyxl" 0; check C4 "today's date present" 0
  elif [ "$PYRC" -eq 3 ]; then check C3 "re-opens via openpyxl" 0; check C4 "today's date present" 1
  else check C3 "re-opens via openpyxl (rc=$PYRC)" 1; check C4 "today's date present" 1; fi
else
  check C3 "re-opens via openpyxl (no file)" 1
  check C4 "today's date present (no file)" 1
fi

pgcount() { # pgcount <role-predicate>
  docker exec aura-postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "'"
    SELECT count(*) FROM aura.conversation_turns
    WHERE conversation_id = (SELECT id FROM aura.conversations ORDER BY created_at DESC LIMIT 1)
      AND $1"'"' 2>/dev/null
}
UT="$(pgcount "role = 'user'")"
AT="$(pgcount "role = 'assistant' AND length(coalesce(content, '')) > 0")"
[ "${UT:-0}" -ge 1 ] 2>/dev/null && [ "${AT:-0}" -ge 1 ] 2>/dev/null
check C5 "PG turns persisted (user=$UT assistant=$AT)" $?

[ "$CHAT_SEC" -le "$MAX_SEC" ]; check C6 "wall-clock ${CHAT_SEC}s <= ${MAX_SEC}s" $?

TOTAL=$((pass + fail))
SCORE=$((pass * 100 / TOTAL))
echo "== timing: build $((T_BUILD1-T_BUILD0))s · chat ${CHAT_SEC}s"
echo "== artifact: ${XLSX:-none} (workspace $WS)"
echo "== SCORE: $pass/$TOTAL = ${SCORE}%  (gate >90%)"
if [ "$SCORE" -gt 90 ]; then echo "== VERDICT: PASS"; exit 0; else echo "== VERDICT: FAIL"; exit 1; fi
