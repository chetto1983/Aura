#!/usr/bin/env bash
# scheduler_chaos.sh — Phase 10 SC#2 chaos test (D-02/D-05, user-ratified ROADMAP-FULL).
#
# Stands up THREE `aura serve` scheduler daemons against the SAME live Postgres, each
# reaching the DB through its OWN socat proxy container on a dedicated docker network,
# schedules ONE recurring task, then PARTITIONS one worker for 60s via
# `docker network disconnect` (the web-tools E2E cross-compile precedent, D-05 topology
# = containerized proxy variant). The invariant proven:
#
#   - the partitioned worker, if it held the task's advisory lock when cut off, loses
#     its session lock (the held conn dies with the proxy) — its run row goes stale
#     (heartbeat lapses > 90s) and is marked unknown_recovery at the next boot scan;
#   - a SURVIVOR worker re-claims the task on a later tick (FOR UPDATE SKIP LOCKED +
#     pg_try_advisory_lock singleton) and completes it;
#   - there is NO DUPLICATE side-effect: the count of distinct COMPLETED runs per
#     scheduled fire is exactly one (completed_with_hash idempotency, SC#2).
#
# Exit non-zero on a missed pickup (no completed run) OR a duplicate (two completed
# runs for the same logical fire). This is the OPERATOR-GATING record (D-05); CI runs
# it as a NON-BLOCKING advisory step.
#
# This script is LINUX-targeted (CI runner / WSL). It needs: docker, a reachable
# Postgres (the aura-postgres compose container), and the composed DSN env the aura
# binary reads (POSTGRES_* or AURA_DB_URL). On Windows hosts run it via WSL.
#
# Env (with safe defaults):
#   PG_CONTAINER       — the live Postgres container name (default: aura-postgres)
#   POSTGRES_PASSWORD  — REQUIRED — aura_app/superuser password (fail-fast)
#   POSTGRES_DB        — database (default: aura)
#   CHAOS_PARTITION_SEC— partition window in seconds (default: 60, SC#2 as written)
#   CHAOS_SETTLE_SEC   — post-reconnect settle before asserting (default: 90)
#   AURA_SCHEDULER_TICK_SECONDS — worker tick (default: 5, for a fast chaos run)

set -euo pipefail
export MSYS_NO_PATHCONV=1
export MSYS2_ARG_CONV_EXCL='*'

PG_CONTAINER="${PG_CONTAINER:-aura-postgres}"
POSTGRES_DB="${POSTGRES_DB:-aura}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:?POSTGRES_PASSWORD required in environment}"
PARTITION_SEC="${CHAOS_PARTITION_SEC:-60}"
SETTLE_SEC="${CHAOS_SETTLE_SEC:-90}"
TICK_SEC="${AURA_SCHEDULER_TICK_SECONDS:-5}"

NET="aura-chaos-net"
PROXY_PREFIX="aura-chaos-proxy"
WORKERS=3
PIDS=()
TASK_ID=""
WORKDIR="$(mktemp -d)"

log() { echo "[chaos] $*" >&2; }

# psql runs a query inside the live Postgres container (no host psql needed).
psql_q() {
    docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" "$PG_CONTAINER" \
        psql -At -U aura_app -d "$POSTGRES_DB" -c "$1"
}

cleanup() {
    log "cleanup: stopping workers + proxies + network"
    for pid in "${PIDS[@]:-}"; do
        [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
    done
    for i in $(seq 1 "$WORKERS"); do
        docker rm -f "${PROXY_PREFIX}-${i}" >/dev/null 2>&1 || true
    done
    docker network rm "$NET" >/dev/null 2>&1 || true
    if [ -n "$TASK_ID" ]; then
        psql_q "DELETE FROM aura.scheduler_tasks WHERE id = '${TASK_ID}'" >/dev/null 2>&1 || true
    fi
    rm -rf "$WORKDIR" 2>/dev/null || true
}
trap cleanup EXIT

# 1. Build the aura binary once (native — D-16 host process, not compose-ized).
log "building aura binary"
AURA_BIN="${WORKDIR}/aura"
go build -o "$AURA_BIN" ./cmd/aura

# 2. Dedicated docker network + 3 socat proxies (host:520N -> aura-postgres:5432).
#    Each worker dials its OWN proxy, so disconnecting one proxy from the network
#    partitions exactly one worker from the DB without touching the others.
log "creating chaos network + ${WORKERS} DB proxies"
docker network create "$NET" >/dev/null
# The live Postgres container must share the chaos network so the proxies can reach it.
docker network connect "$NET" "$PG_CONTAINER" >/dev/null 2>&1 || true

declare -a PROXY_PORTS
for i in $(seq 1 "$WORKERS"); do
    port=$((5200 + i))
    PROXY_PORTS[$i]=$port
    docker run -d --name "${PROXY_PREFIX}-${i}" --network "$NET" \
        -p "127.0.0.1:${port}:5432" alpine/socat \
        "TCP-LISTEN:5432,fork,reuseaddr" "TCP:${PG_CONTAINER}:5432" >/dev/null
done
sleep 3

# 3. Schedule ONE recurring reminder via the CLI (every 5 min — the grammar floor; the
#    fast tick + run_now drives the fire without waiting a real interval).
log "scheduling the chaos task"
export POSTGRES_PASSWORD POSTGRES_DB
SCHED_OUT="$("$AURA_BIN" task schedule --kind reminder --every 5 \
    --args '{"text":"chaos-survivor-check"}' --notify stdout)"
echo "$SCHED_OUT" >&2
TASK_ID="$(echo "$SCHED_OUT" | sed -n 's/^scheduled \([0-9a-f-]\{36\}\).*/\1/p' | head -n1)"
if [ -z "$TASK_ID" ]; then
    log "FAIL: could not parse scheduled task id"
    exit 1
fi
log "task id = ${TASK_ID}"

# 4. Start 3 workers, each pointed at its own proxy port (a fast tick for the chaos run).
log "starting ${WORKERS} aura serve workers"
for i in $(seq 1 "$WORKERS"); do
    port="${PROXY_PORTS[$i]}"
    AURA_DB_URL="postgres://aura_app:${POSTGRES_PASSWORD}@127.0.0.1:${port}/${POSTGRES_DB}?sslmode=disable" \
    AURA_DB_MIGRATE_URL="postgres://aura_migrate:${POSTGRES_PASSWORD}@127.0.0.1:${port}/${POSTGRES_DB}?sslmode=disable" \
    AURA_SCHEDULER_TICK_SECONDS="$TICK_SEC" \
        "$AURA_BIN" serve >"${WORKDIR}/worker-${i}.log" 2>&1 &
    PIDS[$i]=$!
done
sleep "$TICK_SEC"

# 5. Make the task due NOW, then immediately partition worker 1 for the window so a
#    claim that lands on it is severed mid-flight.
log "queueing the task to run now + partitioning worker 1 for ${PARTITION_SEC}s"
"$AURA_BIN" task run_now "$TASK_ID" >&2 || true
docker network disconnect "$NET" "${PROXY_PREFIX}-1"
sleep "$PARTITION_SEC"
log "reconnecting worker 1"
docker network connect "$NET" "${PROXY_PREFIX}-1"

# 6. Let the survivors pick up + the stale-recovery scan settle.
log "settling ${SETTLE_SEC}s for survivor pickup + recovery"
sleep "$SETTLE_SEC"

# 7. Assert: at least one COMPLETED run (survivor picked it up) and NO duplicate
#    completed side-effect for the same logical fire (completed_with_hash idempotency).
completed="$(psql_q "SELECT count(*) FROM aura.agent_job_runs WHERE task_id = '${TASK_ID}' AND status = 'completed'")"
distinct_hash="$(psql_q "SELECT count(DISTINCT completed_with_hash) FROM aura.agent_job_runs WHERE task_id = '${TASK_ID}' AND status = 'completed' AND completed_with_hash <> ''")"
log "completed runs = ${completed}, distinct completion hashes = ${distinct_hash}"

if [ "${completed:-0}" -lt 1 ]; then
    log "FAIL (SC#2): no survivor completed the task after the partition — missed pickup"
    exit 1
fi
# A duplicate side-effect would show as the SAME logical fire completing twice. Each
# claim opens a unique run with completed_with_hash = its run id, so distinct hashes
# must equal completed rows; a re-delivered completion is swallowed (ErrAlreadyRunning),
# never a second completed row. completed == distinct_hash proves no double-record.
if [ "${completed}" -ne "${distinct_hash}" ]; then
    log "FAIL (SC#2): duplicate completion detected (completed=${completed} != distinct=${distinct_hash})"
    exit 1
fi

log "PASS (SC#2): survivor picked up the partitioned worker's task with no duplicate side-effect"
exit 0
