#!/usr/bin/env bash
# Source: PRD §Slice 0.5 backup strategy + ROADMAP Phase 1 SC#3.
# Purpose: Smoke-test that a pg_dump file restores into a fresh database in
#          under 90 seconds. Operator-run; not part of unit tests.
#
# All Postgres ops run inside the `aura-postgres` container — no host-side
# psql/pg_dump/pg_restore required. This keeps the drill portable across the
# Linux CI runner and Windows dev hosts.
#
# Env (with safe defaults):
#   DUMPFILE       — path INSIDE the container for the dump artifact
#                    (default: /tmp/aura-restore-drill.dump)
#   PG_CONTAINER   — compose service name (default: postgres)
#   SOURCE_DB      — DB to dump from (default: aura)
#   POSTGRES_USER  — superuser (default: aura)
#   POSTGRES_PASSWORD — REQUIRED — superuser password (fail-fast)

set -euo pipefail

# Disable MSYS path translation on Git Bash so container-side paths like
# `/tmp/foo` are NOT rewritten to host C:\ paths
# (feedback_docker_compose_run_msys_path_mangling).
export MSYS_NO_PATHCONV=1
export MSYS2_ARG_CONV_EXCL='*'

DUMPFILE="${1:-/tmp/aura-restore-drill.dump}"
TARGET_DB="aura_restore_drill"
PG_SERVICE="${PG_CONTAINER:-postgres}"
SOURCE_DB="${SOURCE_DB:-aura}"
DB_USER="${POSTGRES_USER:-aura}"
DB_PASS="${POSTGRES_PASSWORD:?POSTGRES_PASSWORD required in environment}"

run() {
    docker compose exec -T -e PGPASSWORD="$DB_PASS" "$PG_SERVICE" "$@"
}

# 1. Make sure dump exists (inside container at $DUMPFILE).
if ! run test -f "$DUMPFILE"; then
    echo "==> creating sample dump at container:$DUMPFILE"
    run pg_dump -U "$DB_USER" -Fc -d "$SOURCE_DB" -f "$DUMPFILE"
fi

# 2. Drop + recreate target DB.
run psql -U "$DB_USER" -d postgres -c "DROP DATABASE IF EXISTS $TARGET_DB;"
run psql -U "$DB_USER" -d postgres -c "CREATE DATABASE $TARGET_DB OWNER $DB_USER;"

# 3. Time the restore.
START_NS=$(date +%s%N)
run pg_restore -U "$DB_USER" -d "$TARGET_DB" --no-owner --no-acl "$DUMPFILE"
END_NS=$(date +%s%N)

ELAPSED_MS=$(( (END_NS - START_NS) / 1000000 ))
echo "==> restore took ${ELAPSED_MS} ms"

# 4. Assert < 90 000 ms (90 s) per ROADMAP Phase 1 SC#3.
# POSIX `[ -gt ]` not bash `(( ))`: under non-bash shells (busybox/dash) `(( ))`
# is parsed as nested subshells and `> 90000` becomes a redirect that silently
# creates a junk file named 90000. Keep the gate portable per the header claim.
if [ "$ELAPSED_MS" -gt 90000 ]; then
    echo "FAIL: restore took ${ELAPSED_MS} ms, exceeds 90 s budget (ROADMAP Phase 1 SC#3)" >&2
    exit 1
fi

# 5. Cleanup.
run psql -U "$DB_USER" -d postgres -c "DROP DATABASE $TARGET_DB;"
echo "ok: restore drill PASSED (${ELAPSED_MS} ms < 90 000 ms)"
