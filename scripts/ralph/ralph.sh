#!/bin/bash
# Aura Ralph autonomous loop — drives D:/Aura/prd.json
#
# Each iter spawns a fresh `claude -p` against the same prompt (scripts/ralph/CLAUDE.md).
# The agent picks the next passes:false story from prd.json, ships ONE atomic commit,
# marks the story passes:true, appends to progress.txt, and exits.
# This script loops until either:
#   - all stories in prd.json are passes:true (agent emits <promise>ALL_DONE</promise>)
#   - max iterations reached
#   - manual Ctrl+C

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
PROMPT_FILE="$SCRIPT_DIR/CLAUDE.md"
PRD_FILE="$REPO_DIR/prd.json"
PROGRESS_FILE="$REPO_DIR/.planning/progress.txt"
LOG_DIR="$REPO_DIR/.planning/ralph-logs"
mkdir -p "$LOG_DIR" "$REPO_DIR/.planning"
[ -f "$PROGRESS_FILE" ] || echo "# Aura Ralph Progress Log (started $(date))" > "$PROGRESS_FILE"

MAX_ITER="${1:-15}"          # 15 iter default — enough for the ~14 user stories in prd.json
MODEL="${MODEL:-sonnet}"

cd "$REPO_DIR" || exit 1

START_HEAD="$(git rev-parse HEAD)"
echo "[aura-ralph] start HEAD = $START_HEAD"
echo "[aura-ralph] max_iter   = $MAX_ITER"
echo "[aura-ralph] model      = $MODEL"
echo "[aura-ralph] prd        = $PRD_FILE"
echo "[aura-ralph] log dir    = $LOG_DIR"
echo

all_done_check() {
  # Returns 0 if every story in prd.json has passes:true
  python -c "
import json, sys
with open(r'$PRD_FILE','r',encoding='utf-8') as f:
    prd = json.load(f)
remaining = [s for s in prd['userStories'] if not s.get('passes', False)]
sys.exit(0 if not remaining else 1)
" 2>/dev/null
}

if all_done_check; then
  echo "[aura-ralph] all stories already passes:true — nothing to do"
  exit 0
fi

for ((i = 1; i <= MAX_ITER; i++)); do
  ts="$(date +%Y%m%d-%H%M%S)"
  iter_log="$LOG_DIR/iter-${i}-${ts}.log"
  echo "============================================================"
  echo "[aura-ralph] iter $i / $MAX_ITER  ($(date '+%H:%M:%S'))"
  echo "[aura-ralph] log → $iter_log"
  # Show queue status before iter
  python -c "
import json
with open(r'$PRD_FILE','r',encoding='utf-8') as f:
    prd = json.load(f)
done = sum(1 for s in prd['userStories'] if s.get('passes', False))
total = len(prd['userStories'])
print(f'[aura-ralph] queue: {done}/{total} stories complete')
nxt = next((s for s in sorted(prd['userStories'], key=lambda s: s['priority']) if not s.get('passes', False)), None)
if nxt: print(f'[aura-ralph] next:  {nxt[\"id\"]} (prio={nxt[\"priority\"]}) — {nxt[\"title\"]}')
" 2>/dev/null
  echo "============================================================"

  # Fresh claude process. -p = print-and-exit. --add-dir gives access to repo.
  # --dangerously-skip-permissions for non-interactive flow.
  cat "$PROMPT_FILE" | claude -p \
    --model "$MODEL" \
    --add-dir "$REPO_DIR" \
    --dangerously-skip-permissions \
    --output-format stream-json \
    --verbose \
    > "$iter_log" 2>&1
  rc=$?
  echo "[aura-ralph] iter $i exit code = $rc"

  if all_done_check; then
    echo
    echo "============================================================"
    echo "[aura-ralph] ALL_DONE — every story passes:true"
    echo "============================================================"
    git log --oneline "$START_HEAD..HEAD"
    exit 0
  fi

  echo "[aura-ralph] iter $i ended, queue not empty, continuing"
  echo
done

echo
echo "============================================================"
echo "[aura-ralph] MAX_ITER ($MAX_ITER) reached, queue not empty"
echo "============================================================"
echo "Commits since start:"
git log --oneline "$START_HEAD..HEAD"
echo
echo "Remaining stories:"
python -c "
import json
with open(r'$PRD_FILE','r',encoding='utf-8') as f:
    prd = json.load(f)
for s in sorted(prd['userStories'], key=lambda s: s['priority']):
    if not s.get('passes', False):
        print(f'  - {s[\"id\"]} (prio={s[\"priority\"]}): {s[\"title\"]}')
" 2>/dev/null
exit 1
