#!/usr/bin/env bash
# Source: PRD §Slice 0.5 backup strategy + ROADMAP Phase 1 SC#3.
# Purpose: Smoke-test that a pg_dump file restores into a fresh database in
#          under 90 seconds. Operator-run; not part of unit tests.
#
# Requires on PATH (or available via `docker compose exec postgres`):
#   psql, pg_dump, pg_restore.
#
# Env (with safe defaults):
#   DUMPFILE    — pg_dump artifact path; created if missing (default: /tmp/aura-restore-drill.dump)
#   PGHOST      — Postgres host (default: 127.0.0.1)
#   PGPORT      — Postgres port (default: 5432)
#   PGUSER      — Postgres user (default: aura_migrate)
#   PGPASSWORD  — REQUIRED — Postgres password (fail-fast)

set -euo pipefail

DUMPFILE="${1:-/tmp/aura-restore-drill.dump}"
TARGET_DB="aura_restore_drill"
PG_HOST="${PGHOST:-127.0.0.1}"
PG_PORT="${PGPORT:-5432}"
PG_USER="${PGUSER:-aura_migrate}"
export PGPASSWORD="${PGPASSWORD:?PGPASSWORD required}"

# 1. Make sure dump exists; if not, create one from the live aura DB.
if [[ ! -f "$DUMPFILE" ]]; then
    echo "==> creating sample dump at $DUMPFILE"
    pg_dump -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -Fc aura > "$DUMPFILE"
fi

# 2. Drop + recreate target DB.
psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d postgres -c "DROP DATABASE IF EXISTS $TARGET_DB;"
psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d postgres -c "CREATE DATABASE $TARGET_DB OWNER aura_migrate;"

# 3. Time the restore.
START_NS=$(date +%s%N)
pg_restore -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$TARGET_DB" --no-owner --no-acl "$DUMPFILE"
END_NS=$(date +%s%N)

ELAPSED_MS=$(( (END_NS - START_NS) / 1000000 ))
echo "==> restore took ${ELAPSED_MS} ms"

# 4. Assert < 90 000 ms (90 s) per ROADMAP Phase 1 SC#3.
if (( ELAPSED_MS > 90000 )); then
    echo "FAIL: restore took ${ELAPSED_MS} ms, exceeds 90 s budget (ROADMAP Phase 1 SC#3)"
    exit 1
fi

# 5. Cleanup.
psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d postgres -c "DROP DATABASE $TARGET_DB;"
echo "ok: restore drill PASSED (${ELAPSED_MS} ms < 90 000 ms)"
