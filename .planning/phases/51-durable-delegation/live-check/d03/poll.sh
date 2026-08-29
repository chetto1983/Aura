#!/bin/bash
# Host-side ground-truth poll for a d03 run. Usage: poll.sh <mode>
# Reads ONLY the daemon log, Postgres (as aura_app) and the 51-07 transcript route.
set -u
MODE="${1:-long}"
S="C:/Users/chett/AppData/Local/Temp/claude/D--Repo-Aura/9df173ff-c674-4696-a964-396df5f8ebc7/scratchpad/d03/out/$MODE"
CONV=$(cat "$S/conv.txt")
SINCE=$(cat "$S/started_at.txt")
echo "conv=$CONV since=$SINCE now=$(date -u +%H:%M:%SZ)"
echo "--- daemon: swarm.* since start ---"
docker logs aura --since "$SINCE" 2>&1 | grep -o '"msg":"swarm\.[^"]*"[^}]*' | cut -c1-260
echo "--- daemon: stalled / cancel / budget mentions ---"
docker logs aura --since "$SINCE" 2>&1 | grep -i 'stalled\|budget\|wallclock' | grep -v 'disk pressure' | cut -c1-260 | tail -8
echo "--- ingestion_jobs (swarm_delegation, newest 3) ---"
docker exec aura-postgres psql -U aura -d aura -Atc "select id, status, stage, attempt_count, max_attempts, locked_by, to_char(created_at,'HH24:MI:SS') c, to_char(updated_at,'HH24:MI:SS') u, to_char(completed_at,'HH24:MI:SS') done, left(coalesce(error_code,''),40) ec, left(coalesce(error_message,''),120) em from aura.ingestion_jobs where job_type='swarm_delegation' order by created_at desc limit 3"
echo "--- conversation_turns for conv ---"
docker exec aura-postgres psql -U aura -d aura -Atc "select to_char(created_at,'HH24:MI:SS'), role, left(regexp_replace(content, E'[\\n\\r]+', ' ', 'g'), 160) from aura.conversation_turns where conversation_id='$CONV' order by created_at"
echo "--- steer_queue rows for conv ---"
docker exec aura-postgres psql -U aura -d aura -Atc "select kind, to_char(created_at,'HH24:MI:SS'), drained_at is not null as drained, nudged_at is not null as nudged from aura.steer_queue where conversation_id='$CONV' order by created_at" 2>&1 | head -5
echo "--- transcript files ---"
docker exec aura sh -c "ls -la \$AURA_RUN_DIR/$CONV/swarm/ 2>/dev/null; for f in \$AURA_RUN_DIR/$CONV/swarm/*.jsonl; do [ -f \"\$f\" ] && echo \"\$f lines=\$(wc -l < \$f) last=\$(tail -c 200 \$f | tr -d '\n' | cut -c1-200)\"; done"
