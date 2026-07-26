#!/usr/bin/env bash
# Owned-surface coverage gate (>=85%, CLAUDE.md floor) with mcp-neo4j-cypher
# CONTAINERIZED — the MCP binary is never installed on the host. Host `go test` keeps
# its warm build cache; the stdio MCP subprocess runs via docker (the wrapper shim
# scripts/mcp_neo4j_cypher_docker.sh, image docker/mcp-neo4j-cypher/). Tiers:
# db_integration + neo4j_integration.
#
# Prerequisite: the stack must be up (postgres + neo4j + embed) — `make neo4j-up` —
# with creds in .env (or exported). Mirrors `make coverage`, which also needs the stack.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

IMAGE="${AURA_MCP_IMAGE:-aura-mcp-neo4j-cypher:local}"
if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  echo "==> building $IMAGE from docker/mcp-neo4j-cypher/"
  docker build -q -t "$IMAGE" docker/mcp-neo4j-cypher/ >/dev/null
fi

# Creds: honour an exported value first, else read .env (the running stack was booted
# from it). cut -d= -f2- keeps '=' inside a value intact.
read_secret() {
  local key="$1" val="${!1:-}"
  if [ -z "$val" ] && [ -f .env ]; then
    val="$(grep -E "^${key}=" .env | head -1 | cut -d= -f2-)"
  fi
	# .env is commonly edited on Windows. A trailing CR is data to Bash and makes
	# composed Postgres URLs fail net/url parsing, so normalize the line ending at
	# this single credential boundary without altering any other value bytes.
	printf '%s' "$val" | tr -d '\r'
}
PGPW="$(read_secret POSTGRES_PASSWORD)"
NEOPW="$(read_secret NEO4J_PASSWORD)"
if [ -z "$PGPW" ] || [ -z "$NEOPW" ]; then
  echo "FATAL: POSTGRES_PASSWORD / NEO4J_PASSWORD not found in env or .env" >&2
  exit 3
fi

# AURA_AUTHULA_SECRET — the internal/breakglass db_integration test constructs
# webauth.New (Authula), which REQUIRES a 64-hex secret and t.Fatals under CI=true when
# unset (no-skip-as-green). Export it from .env; fall back to the 64-hex CI dummy — a
# fresh throwaway `authula` schema is self-consistent with any valid 64-hex secret, so
# unlike PGPW/NEOPW this is NOT hard-FATAL (DC-1 local gap; ci.yml:18 already sets it
# workflow-level, so NO ci.yml edit). AURA_AUTHULA_DATABASE_URL stays unset → the test
# derives its throwaway DSN from AURA_DB_URL.
AURA_AUTHULA_SECRET="$(read_secret AURA_AUTHULA_SECRET)"
if [ -z "$AURA_AUTHULA_SECRET" ]; then
  AURA_AUTHULA_SECRET="00000000000000000000000000000000000000000000000000000000000000a1"
fi
export AURA_AUTHULA_SECRET

# --- Isolated coverage database (anti-footgun) --------------------------------
# The db_integration tier TRUNCATEs/DELETEs shared tables on setup. Pointing it at a
# live personal deployment's `aura` DB DESTROYS auth data (operator identity + the
# authula schema) — this happened on 2026-07-10. CI provisions a fresh throwaway
# `aura`, so the gate is only dangerous locally. We therefore ALWAYS run against a
# disposable DB owned by aura_migrate (so migrations' CREATE SCHEMA succeeds by
# ownership), and drop it on exit. Override the name with AURA_COVERAGE_DB.
COV_DB="${AURA_COVERAGE_DB:-aura_cov}"
PG_CONTAINER="${AURA_PG_CONTAINER:-aura-postgres}"
COV_POSTGRES=""
COV_NEO4J=""
PG_HOST_TARGET="127.0.0.1"
PG_PORT_TARGET="5432"
if [ "$COV_DB" = "aura" ]; then
  echo "FATAL: AURA_COVERAGE_DB must not be 'aura' — the db_integration tier TRUNCATEs it (data loss). Pick a throwaway name." >&2
  exit 4
fi
if [ -z "${GITHUB_ACTIONS:-}" ]; then
  COV_POSTGRES="${AURA_COVERAGE_POSTGRES_CONTAINER:-aura-postgres-cov}"
  PG_PORT_TARGET="${AURA_COVERAGE_POSTGRES_PORT:-5433}"
  COV_POSTGRES_IMAGE="${AURA_COVERAGE_POSTGRES_IMAGE:-${POSTGRES_IMAGE:-postgres:18.4-alpine3.23}}"
  docker rm -f "$COV_POSTGRES" >/dev/null 2>&1 || true
  echo "==> provisioning disposable coverage Postgres '$COV_POSTGRES' on 127.0.0.1:${PG_PORT_TARGET}; removed on exit"
  docker run -d --rm --name "$COV_POSTGRES" \
    -p "127.0.0.1:${PG_PORT_TARGET}:5432" \
    -e POSTGRES_USER=aura \
    -e POSTGRES_PASSWORD="$PGPW" \
    -e POSTGRES_DB=aura \
    "$COV_POSTGRES_IMAGE" >/dev/null
  PG_CONTAINER="$COV_POSTGRES"
elif ! docker exec "$PG_CONTAINER" true >/dev/null 2>&1; then
  echo "FATAL: postgres container '$PG_CONTAINER' not running — bring the stack up (make neo4j-up) or set AURA_PG_CONTAINER." >&2
  exit 3
fi

_cov_cleanup() {
  if [ -n "$COV_POSTGRES" ]; then
    docker rm -f "$COV_POSTGRES" >/dev/null 2>&1 || true
  else
    docker exec -i "$PG_CONTAINER" psql -U aura -d postgres -c "DROP DATABASE IF EXISTS \"$COV_DB\" WITH (FORCE)" >/dev/null 2>&1 || true
  fi
  [ -n "$COV_NEO4J" ] && docker rm -f "$COV_NEO4J" >/dev/null 2>&1 || true
}
trap _cov_cleanup EXIT

if [ -n "$COV_POSTGRES" ]; then
  echo -n "==> waiting for coverage postgres"
  COV_POSTGRES_READY=""
  for _ in $(seq 1 60); do
    if docker exec "$COV_POSTGRES" pg_isready -h 127.0.0.1 -U aura -d aura >/dev/null 2>&1; then COV_POSTGRES_READY=1; break; fi
    echo -n .; sleep 1
  done
  [ -n "$COV_POSTGRES_READY" ] || { echo " FATAL: coverage postgres '$COV_POSTGRES' not ready" >&2; exit 3; }
  echo " ready"
fi

pg_admin() { docker exec -i "$PG_CONTAINER" psql -v ON_ERROR_STOP=1 -U aura -d postgres "$@"; }
ESC_PGPW="$(printf '%s' "$PGPW" | sed "s/'/''/g")"
# Both fixed application roles are prerequisites of migration 0001. Provision
# them before creating the disposable DB so the coverage bootstrap does not
# depend on a later CLI side effect; reset existing passwords as well so local
# and CI runs use the same deterministic credential.
for role in aura_app aura_migrate; do
  if pg_admin -tAc "SELECT 1 FROM pg_roles WHERE rolname='${role}'" | grep -q 1; then
    pg_admin -c "ALTER ROLE ${role} WITH LOGIN PASSWORD '${ESC_PGPW}'"
  else
    pg_admin -c "CREATE ROLE ${role} WITH LOGIN PASSWORD '${ESC_PGPW}'"
  fi
done
echo "==> provisioning disposable coverage DB '$COV_DB' (owner aura_migrate); dropped on exit"
pg_admin -c "DROP DATABASE IF EXISTS \"$COV_DB\" WITH (FORCE)"
pg_admin -c "CREATE DATABASE \"$COV_DB\" OWNER aura_migrate"
# --- Isolated coverage Neo4j (anti-footgun, mirrors aura_cov) -----------------
# neo4j:5.26-COMMUNITY has ONLY the fixed 'neo4j' DB (+ 'system') — there is NO
# named-DB analog of aura_cov. The neo4j_integration tier's Reset()
# (internal/knowledge/reset.go:24) runs an UNSCOPED `MATCH (n) DETACH DELETE n`
# that WIPES the whole graph; pointing it at the live neo4j DESTROYS real
# agent-memory (proven 2026-07-20, ~55 nodes -> 1). So LOCALLY spin a disposable
# throwaway neo4j and target the tests at it. In CI ($GITHUB_ACTIONS) the workflow
# already provisions a fresh disposable neo4j service on 7687, so this is skipped
# (avoids a port clash). APOC only (graphview uses apoc.*); GDS is unused here.
NEO4J_BOLT_TARGET="bolt://127.0.0.1:7687"
if [ -z "${GITHUB_ACTIONS:-}" ]; then
  COV_NEO4J="${AURA_COVERAGE_NEO4J_CONTAINER:-aura-neo4j-cov}"
  COV_NEO4J_PORT="${AURA_COVERAGE_NEO4J_BOLT_PORT:-7688}"
  COV_NEO4J_IMAGE="${AURA_COVERAGE_NEO4J_IMAGE:-neo4j:5.26.26-community}"
  docker rm -f "$COV_NEO4J" >/dev/null 2>&1 || true
  echo "==> provisioning disposable coverage Neo4j '$COV_NEO4J' on 127.0.0.1:${COV_NEO4J_PORT}; removed on exit"
  docker run -d --rm --name "$COV_NEO4J" \
    -p "127.0.0.1:${COV_NEO4J_PORT}:7687" \
    -e NEO4J_AUTH="neo4j/${NEOPW}" \
    -e NEO4J_PLUGINS='["apoc"]' \
    -e NEO4J_dbms_security_procedures_unrestricted='apoc.*' \
    "$COV_NEO4J_IMAGE" >/dev/null
  NEO4J_BOLT_TARGET="bolt://127.0.0.1:${COV_NEO4J_PORT}"
fi
if [ -n "$COV_NEO4J" ]; then
  echo -n "==> waiting for coverage neo4j"
  COV_NEO4J_READY=""
  for _ in $(seq 1 60); do
    if docker exec "$COV_NEO4J" cypher-shell -u neo4j -p "$NEOPW" 'RETURN 1' >/dev/null 2>&1; then COV_NEO4J_READY=1; break; fi
    echo -n .; sleep 2
  done
  [ -n "$COV_NEO4J_READY" ] || { echo " FATAL: coverage neo4j '$COV_NEO4J' not ready" >&2; exit 3; }
  echo " ready"
fi

# Postgres env — ALL DB-pointing vars target the disposable DB, never live `aura`.
# (EnsureRoles' hardcoded /aura bootstrap in the test helpers only does idempotent
# role/schema-existence management there; every destructive op follows these URLs.)
export POSTGRES_USER=aura POSTGRES_PASSWORD="$PGPW" POSTGRES_DB="$COV_DB"
export POSTGRES_HOST="$PG_HOST_TARGET" POSTGRES_PORT="$PG_PORT_TARGET" POSTGRES_SSLMODE=disable
# Legacy integration helpers read libpq's PGHOST/PGPORT directly when composing
# their bootstrap DSN. Keep those variables aligned with the disposable service.
export PGHOST="$PG_HOST_TARGET" PGPORT="$PG_PORT_TARGET"
export AURA_DB_URL="postgres://aura_app:${PGPW}@${PG_HOST_TARGET}:${PG_PORT_TARGET}/${COV_DB}?sslmode=disable"
export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${PGPW}@${PG_HOST_TARGET}:${PG_PORT_TARGET}/${COV_DB}?sslmode=disable"
export AURA_DB_BOOTSTRAP_URL="postgres://aura:${PGPW}@${PG_HOST_TARGET}:${PG_PORT_TARGET}/${COV_DB}?sslmode=disable"

# Neo4j + the containerized MCP shim. Local: the disposable throwaway (the live
# graph is NEVER the DETACH DELETE target). CI: the workflow-provided neo4j on 7687.
export NEO4J_USER=neo4j NEO4J_PASSWORD="$NEOPW"
export AURA_NEO4J_BOLT_URL="$NEO4J_BOLT_TARGET" AURA_NEO4J_DATABASE=neo4j
export AURA_MCP_IMAGE="$IMAGE"
export AURA_MCP_NEO4J_CYPHER_BIN="${AURA_MCP_NEO4J_CYPHER_BIN:-$(pwd)/scripts/mcp_neo4j_cypher_docker.sh}"
export AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC="${AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC:-20}"

# Embed sidecar.
export AURA_EMBED_BASE_URL=http://127.0.0.1:8081 AURA_EMBED_DIMENSIONS=768

# Runtime dirs + no-skip-as-green arm (a tagged tier with unset env t.Fatals under CI).
export AURA_SKILL_EXPORT_DIR="${AURA_SKILL_EXPORT_DIR:-/tmp/aura-skills-export}"
export AURA_RUN_DIR="${AURA_RUN_DIR:-/tmp/aura-run}"
mkdir -p "$AURA_SKILL_EXPORT_DIR" "$AURA_RUN_DIR"
export CI=true

# The disposable Neo4j boots empty; migrate the Cypher schema before the gate (the
# neo4j_integration tests otherwise assume `make neo4j-migrate` already ran against a
# live graph). `aura neo4j migrate` tracks applied migrations in the Postgres ledger
# `aura.knowledge_migrations`, so the disposable Postgres schema must exist FIRST — run
# `aura db migrate` (which creates it) before it. Both use config.LoadDB() (keyless, no
# OPENROUTER_API_KEY) and read the disposable DSNs exported above. Skipped in CI, where
# the workflow migrates its own services.
if [ -n "$COV_NEO4J" ]; then
  echo "==> migrating Postgres + Cypher schema into the disposable coverage DBs"
  go run ./cmd/aura db migrate
  go run ./cmd/aura neo4j migrate
fi

echo "==> coverage gate (mcp via docker image $IMAGE) against disposable DB '$COV_DB'"
# NOT `exec` — the EXIT trap must fire to drop the disposable DB (exec replaces the shell).
COV_RC=0
bash scripts/coverage_gate.sh || COV_RC=$?
exit "$COV_RC"
