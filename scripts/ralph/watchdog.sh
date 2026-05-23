#!/bin/bash
# Aura Ralph watchdog — overnight autonomous Phase-MODERNIZE Wave A → Wave C chain.
#
# Polls every CHECK_INTERVAL (default 300s = 5min):
#   1. Read prd.json → count passes:false stories
#   2a. If 0 remaining AND prd-phase-modernize-wave-c-staged.json exists →
#       archive current prd.json (Wave A) as prd-completed-phase-modernize-wave-a.json,
#       swap Wave C in, restart Ralph.
#   2b. If 0 remaining AND no Wave C staged → exit cleanly (queue done).
#   3. If queue non-empty AND no ralph.sh process alive → restart with the
#      remaining count as MAX_ITER.
#   4. Anti-loop safeguard: if 3 consecutive restarts produce NO new commit,
#      exit watchdog (stuck on the same story or every Ralph dies on launch).
#   5. Otherwise → "alive, ok" tick.
#
# Use: bash D:/Aura/scripts/ralph/watchdog.sh &
# Stop: kill <pid> OR pkill -f watchdog.sh

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -W 2>/dev/null || cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
PRD_FILE="$SCRIPT_DIR/prd.json"
WAVE_C_STAGED="$SCRIPT_DIR/prd-phase-modernize-wave-c-staged.json"
WAVE_A_ARCHIVE="$SCRIPT_DIR/prd-completed-phase-modernize-wave-a.json"
LOG_DIR="$SCRIPT_DIR/logs"
WATCHDOG_LOG="$LOG_DIR/watchdog-$(date +%Y%m%d-%H%M%S).log"
CHECK_INTERVAL="${CHECK_INTERVAL:-300}"
STUCK_LIMIT="${STUCK_LIMIT:-3}"   # consecutive no-commit restarts before giving up

mkdir -p "$LOG_DIR"
echo "[watchdog] started at $(date) pid=$$ interval=${CHECK_INTERVAL}s log=$WATCHDOG_LOG stuck_limit=$STUCK_LIMIT" | tee -a "$WATCHDOG_LOG"

remaining_count() {
  python -c "
import json, sys
try:
    with open(r'$PRD_FILE', 'r', encoding='utf-8') as f:
        prd = json.load(f)
    print(sum(1 for s in prd['userStories'] if not s.get('passes', False)))
except Exception:
    sys.exit(1)
" 2>/dev/null
}

ralph_alive() {
  ps -ef 2>/dev/null \
    | grep -v grep \
    | grep -v 'watchdog.sh' \
    | grep -q 'ralph.sh'
}

restart_ralph() {
  local n="$1"
  local restart_log="$LOG_DIR/watchdog-restart-$(date +%Y%m%d-%H%M%S).log"
  nohup bash "$SCRIPT_DIR/ralph.sh" "$n" > "$restart_log" 2>&1 &
  echo "[$ts] restarted ralph.sh $n pid=$! log=$restart_log" | tee -a "$WATCHDOG_LOG"
}

swap_to_wave_c() {
  echo "[$ts] Wave A queue empty — archiving current prd.json + swapping Wave C in" | tee -a "$WATCHDOG_LOG"
  cp "$PRD_FILE" "$WAVE_A_ARCHIVE" 2>>"$WATCHDOG_LOG"
  cp "$WAVE_C_STAGED" "$PRD_FILE" 2>>"$WATCHDOG_LOG"
  echo "[$ts] Wave C now active — archived Wave A → $WAVE_A_ARCHIVE" | tee -a "$WATCHDOG_LOG"
  rm -f "$WAVE_C_STAGED" 2>>"$WATCHDOG_LOG"  # so we don't re-swap if Wave C empties later
}

LAST_HEAD="$(git -C "$REPO_DIR" rev-parse --short HEAD 2>/dev/null)"
STUCK_COUNT=0

while true; do
  sleep "$CHECK_INTERVAL"
  ts="$(date '+%Y-%m-%d %H:%M:%S')"

  rem="$(remaining_count)"
  if [ -z "$rem" ]; then
    echo "[$ts] could not read prd.json, skipping tick" | tee -a "$WATCHDOG_LOG"
    continue
  fi

  if [ "$rem" -eq 0 ]; then
    if [ -f "$WAVE_C_STAGED" ]; then
      swap_to_wave_c
      sleep 5
      rem="$(remaining_count)"
      if [ -z "$rem" ] || [ "$rem" -eq 0 ]; then
        echo "[$ts] swap to Wave C did not populate queue — exiting" | tee -a "$WATCHDOG_LOG"
        exit 1
      fi
      restart_ralph "$rem"
      continue
    fi
    echo "[$ts] queue empty (0 remaining) AND no Wave C staged — exiting watchdog cleanly" | tee -a "$WATCHDOG_LOG"
    exit 0
  fi

  if ralph_alive; then
    echo "[$ts] Ralph alive, $rem stories remaining — ok" | tee -a "$WATCHDOG_LOG"
    continue
  fi

  HEAD_NOW="$(git -C "$REPO_DIR" rev-parse --short HEAD 2>/dev/null)"
  if [ "$HEAD_NOW" = "$LAST_HEAD" ]; then
    STUCK_COUNT=$((STUCK_COUNT + 1))
    echo "[$ts] no new commit since last restart (HEAD=$HEAD_NOW); stuck_count=$STUCK_COUNT/$STUCK_LIMIT" | tee -a "$WATCHDOG_LOG"
    if [ "$STUCK_COUNT" -ge "$STUCK_LIMIT" ]; then
      echo "[$ts] STUCK — $STUCK_LIMIT consecutive restarts without commit. Exiting watchdog so you can investigate." | tee -a "$WATCHDOG_LOG"
      exit 2
    fi
  else
    STUCK_COUNT=0
    LAST_HEAD="$HEAD_NOW"
  fi

  echo "[$ts] Ralph DEAD with $rem stories remaining (HEAD=$HEAD_NOW) — restarting" | tee -a "$WATCHDOG_LOG"
  restart_ralph "$rem"
done
