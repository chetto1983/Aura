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
  printf '%s' "$val"
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
if [ "$COV_DB" = "aura" ]; then
  echo "FATAL: AURA_COVERAGE_DB must not be 'aura' — the db_integration tier TRUNCATEs it (data loss). Pick a throwaway name." >&2
  exit 4
fi
if ! docker exec "$PG_CONTAINER" true >/dev/null 2>&1; then
  echo "FATAL: postgres container '$PG_CONTAINER' not running — bring the stack up (make neo4j-up) or set AURA_PG_CONTAINER." >&2
  exit 3
fi
pg_admin() { docker exec -i "$PG_CONTAINER" psql -v ON_ERROR_STOP=1 -U aura -d postgres "$@"; }
ESC_PGPW="$(printf '%s' "$PGPW" | sed "s/'/''/g")"
# aura_migrate must exist to own the disposable DB (production already has it).
pg_admin -tAc "SELECT 1 FROM pg_roles WHERE rolname='aura_migrate'" | grep -q 1 \
  || pg_admin -c "CREATE ROLE aura_migrate WITH LOGIN PASSWORD '${ESC_PGPW}'"
echo "==> provisioning disposable coverage DB '$COV_DB' (owner aura_migrate); dropped on exit"
pg_admin -c "DROP DATABASE IF EXISTS \"$COV_DB\" WITH (FORCE)"
pg_admin -c "CREATE DATABASE \"$COV_DB\" OWNER aura_migrate"
_cov_cleanup() { docker exec -i "$PG_CONTAINER" psql -U aura -d postgres -c "DROP DATABASE IF EXISTS \"$COV_DB\" WITH (FORCE)" >/dev/null 2>&1 || true; }
trap _cov_cleanup EXIT

# Postgres env — ALL DB-pointing vars target the disposable DB, never live `aura`.
# (EnsureRoles' hardcoded /aura bootstrap in the test helpers only does idempotent
# role/schema-existence management there; every destructive op follows these URLs.)
export POSTGRES_USER=aura POSTGRES_PASSWORD="$PGPW" POSTGRES_DB="$COV_DB"
export POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=5432 POSTGRES_SSLMODE=disable
export AURA_DB_URL="postgres://aura_app:${PGPW}@127.0.0.1:5432/${COV_DB}?sslmode=disable"
export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${PGPW}@127.0.0.1:5432/${COV_DB}?sslmode=disable"

# Neo4j + the containerized MCP shim.
export NEO4J_USER=neo4j NEO4J_PASSWORD="$NEOPW"
export AURA_NEO4J_BOLT_URL=bolt://127.0.0.1:7687 AURA_NEO4J_DATABASE=neo4j
export AURA_MCP_IMAGE="$IMAGE"
export AURA_MCP_NEO4J_CYPHER_BIN="$(pwd)/scripts/mcp_neo4j_cypher_docker.sh"
export AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC="${AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC:-20}"

# Embed sidecar.
export AURA_EMBED_BASE_URL=http://127.0.0.1:8081 AURA_EMBED_DIMENSIONS=768

# Runtime dirs + no-skip-as-green arm (a tagged tier with unset env t.Fatals under CI).
export AURA_SKILL_EXPORT_DIR="${AURA_SKILL_EXPORT_DIR:-/tmp/aura-skills-export}"
export AURA_RUN_DIR="${AURA_RUN_DIR:-/tmp/aura-run}"
mkdir -p "$AURA_SKILL_EXPORT_DIR" "$AURA_RUN_DIR"
export CI=true

echo "==> coverage gate (mcp via docker image $IMAGE) against disposable DB '$COV_DB'"
# NOT `exec` — the EXIT trap must fire to drop the disposable DB (exec replaces the shell).
COV_RC=0
bash scripts/coverage_gate.sh || COV_RC=$?
exit "$COV_RC"
