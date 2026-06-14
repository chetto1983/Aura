#!/usr/bin/env bash
# Source: PRD Slice 0.5 backup strategy + ROADMAP Phase 1 SC#3.
# Purpose: smoke-test that a Postgres dump restores into a fresh database in
#          under 90 seconds, then restore a Neo4j .cypher backup with cypher-shell.
#          Operator-run; not part of unit tests.
#
# Postgres ops run inside the compose postgres service, so no host psql/pg_dump/
# pg_restore is required. Neo4j restore runs cypher-shell inside the compose neo4j
# service and reads a host-visible neo4j-*.cypher file through stdin.
#
# Env:
#   DUMPFILE          path inside the postgres container (default /tmp/aura-restore-drill.dump)
#   PG_CONTAINER      compose service name (default postgres)
#   SOURCE_DB         source DB to dump from (default aura)
#   POSTGRES_USER     superuser (default aura)
#   POSTGRES_PASSWORD required
#   NEO4J_DUMPFILE    host path to a neo4j-*.cypher artifact; defaults to newest
#                     ${AURA_BACKUP_DIR:-./backups}/neo4j-*.cypher
#   NEO4J_SERVICE     compose service name (default neo4j)
#   NEO4J_PASSWORD    required

set -euo pipefail

# Disable MSYS path translation on Git Bash so container-side paths like /tmp/foo
# are not rewritten to host C:\ paths.
export MSYS_NO_PATHCONV=1
export MSYS2_ARG_CONV_EXCL='*'

DUMPFILE="${1:-/tmp/aura-restore-drill.dump}"
TARGET_DB="aura_restore_drill"
PG_SERVICE="${PG_CONTAINER:-postgres}"
SOURCE_DB="${SOURCE_DB:-aura}"
DB_USER="${POSTGRES_USER:-aura}"
DB_PASS="${POSTGRES_PASSWORD:?POSTGRES_PASSWORD required in environment}"

NEO4J_SERVICE="${NEO4J_SERVICE:-neo4j}"
NEO4J_BOLT_URL="${AURA_NEO4J_BOLT_URL:-bolt://neo4j:7687}"
NEO4J_USER="${NEO4J_USER:-neo4j}"
NEO4J_PASS="${NEO4J_PASSWORD:?NEO4J_PASSWORD required in environment}"
NEO4J_DATABASE="${AURA_NEO4J_DATABASE:-neo4j}"
BACKUP_DIR="${AURA_BACKUP_DIR:-./backups}"

if [[ -z "${DOCKER:-}" ]]; then
    if command -v docker.exe >/dev/null 2>&1; then
        DOCKER=docker.exe
    else
        DOCKER=docker
    fi
fi

run_pg() {
    "$DOCKER" compose exec -T -e PGPASSWORD="$DB_PASS" "$PG_SERVICE" "$@"
}

run_neo4j() {
    "$DOCKER" compose exec -T "$NEO4J_SERVICE" "$@"
}

latest_neo4j_dump() {
    if [ -n "${NEO4J_DUMPFILE:-}" ]; then
        echo "$NEO4J_DUMPFILE"
        return
    fi
    find "$BACKUP_DIR" -maxdepth 1 -type f -name 'neo4j-*.cypher' -print 2>/dev/null \
        | sort \
        | tail -n 1
}

# 1. Make sure the Postgres dump exists inside the postgres container.
if ! run_pg test -f "$DUMPFILE"; then
    echo "==> creating sample Postgres dump at container:$DUMPFILE"
    run_pg pg_dump -U "$DB_USER" -Fc -d "$SOURCE_DB" -f "$DUMPFILE"
fi

# 2. Drop + recreate the target Postgres drill DB.
run_pg psql -U "$DB_USER" -d postgres -c "DROP DATABASE IF EXISTS $TARGET_DB;"
run_pg psql -U "$DB_USER" -d postgres -c "CREATE DATABASE $TARGET_DB OWNER $DB_USER;"

# 3. Time the Postgres restore.
START_NS=$(date +%s%N)
run_pg pg_restore -U "$DB_USER" -d "$TARGET_DB" --no-owner --no-acl "$DUMPFILE"
END_NS=$(date +%s%N)

ELAPSED_MS=$(( (END_NS - START_NS) / 1000000 ))
echo "==> Postgres restore took ${ELAPSED_MS} ms"

# 4. Assert < 90 000 ms (90 s) per ROADMAP Phase 1 SC#3.
if [ "$ELAPSED_MS" -gt 90000 ]; then
    echo "FAIL: restore took ${ELAPSED_MS} ms, exceeds 90 s budget (ROADMAP Phase 1 SC#3)" >&2
    exit 1
fi

# 5. Cleanup the Postgres drill DB.
run_pg psql -U "$DB_USER" -d postgres -c "DROP DATABASE $TARGET_DB;"

# 6. Restore Neo4j from the latest host-visible .cypher artifact.
NEO4J_FILE="$(latest_neo4j_dump)"
if [ -z "$NEO4J_FILE" ] || [ ! -f "$NEO4J_FILE" ]; then
    echo "FAIL: no Neo4j .cypher backup found (set NEO4J_DUMPFILE or AURA_BACKUP_DIR)" >&2
    exit 1
fi

echo "==> restoring Neo4j from host:$NEO4J_FILE"
run_neo4j cypher-shell -a "$NEO4J_BOLT_URL" -u "$NEO4J_USER" -p "$NEO4J_PASS" -d "$NEO4J_DATABASE" \
    "MATCH (n) DETACH DELETE n;"
"$DOCKER" compose exec -T "$NEO4J_SERVICE" \
    cypher-shell -a "$NEO4J_BOLT_URL" -u "$NEO4J_USER" -p "$NEO4J_PASS" -d "$NEO4J_DATABASE" \
    < "$NEO4J_FILE"

echo "ok: restore drill PASSED (${ELAPSED_MS} ms < 90 000 ms; Neo4j restored from ${NEO4J_FILE})"
