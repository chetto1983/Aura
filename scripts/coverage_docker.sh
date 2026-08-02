#!/usr/bin/env bash
# Owned-surface coverage gate (>=85%, CLAUDE.md floor) against DISPOSABLE containers —
# never the live deployment. Host `go test` keeps its warm build cache; only the
# databases the tagged tiers destroy are throwaway. Tier: db_integration.
#
# The graph half of this script is gone with Aura's graph store (internal/knowledge):
# no tagged test reads a graph any more, so there is no containerized MCP shim and no
# disposable graph database to migrate.
#
# Prerequisite: the embed sidecar must be reachable (`make memory-up` starts it
# alongside ArcadeDB) with creds in .env (or exported).
# Mirrors `make coverage`, which also needs the stack.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

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
if [ -z "$PGPW" ]; then
  echo "FATAL: POSTGRES_PASSWORD not found in env or .env" >&2
  exit 3
fi

# AURA_AUTHULA_SECRET — the internal/breakglass db_integration test constructs
# webauth.New (Authula), which REQUIRES a 64-hex secret and t.Fatals under CI=true when
# unset (no-skip-as-green). Export it from .env; fall back to the 64-hex CI dummy — a
# fresh throwaway `authula` schema is self-consistent with any valid 64-hex secret, so
# unlike PGPW this is NOT hard-FATAL (DC-1 local gap; ci.yml:18 already sets it
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
PG_HOST_TARGET="127.0.0.1"
PG_PORT_TARGET="5432"
if [ "$COV_DB" = "aura" ]; then
  echo "FATAL: AURA_COVERAGE_DB must not be 'aura' — the db_integration tier TRUNCATEs it (data loss). Pick a throwaway name." >&2
  exit 4
fi
if [ -z "${GITHUB_ACTIONS:-}" ]; then
  COV_POSTGRES="${AURA_COVERAGE_POSTGRES_CONTAINER:-aura-postgres-cov}"
  PG_PORT_TARGET="${AURA_COVERAGE_POSTGRES_PORT:-5433}"
  COV_POSTGRES_IMAGE="${AURA_COVERAGE_POSTGRES_IMAGE:-${POSTGRES_IMAGE:-postgres:18.4-alpine3.24}}"
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
  echo "FATAL: postgres container '$PG_CONTAINER' not running — bring the stack up (make db-up) or set AURA_PG_CONTAINER." >&2
  exit 3
fi

_cov_cleanup() {
  if [ -n "$COV_POSTGRES" ]; then
    docker rm -f "$COV_POSTGRES" >/dev/null 2>&1 || true
  else
    docker exec -i "$PG_CONTAINER" psql -U aura -d postgres -c "DROP DATABASE IF EXISTS \"$COV_DB\" WITH (FORCE)" >/dev/null 2>&1 || true
  fi
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

# Embed sidecar. The width is the sidecar's native one (config.DefaultEmbedDimensions);
# asking a 768d model for 1024 is an error, not a truncation.
export AURA_EMBED_BASE_URL=http://127.0.0.1:8081 AURA_EMBED_DIMENSIONS=768

# Runtime dirs + no-skip-as-green arm (a tagged tier with unset env t.Fatals under CI).
export AURA_SKILL_EXPORT_DIR="${AURA_SKILL_EXPORT_DIR:-/tmp/aura-skills-export}"
export AURA_RUN_DIR="${AURA_RUN_DIR:-/tmp/aura-run}"
mkdir -p "$AURA_SKILL_EXPORT_DIR" "$AURA_RUN_DIR"
export CI=true

# The disposable Postgres boots empty; lay down the schema before the gate. Uses
# config.LoadDB() (keyless, no OPENROUTER_API_KEY) and reads the DSNs exported above.
# Skipped in CI, where the workflow migrates its own services.
if [ -n "$COV_POSTGRES" ]; then
  echo "==> migrating the schema into the disposable coverage DB"
  go run ./cmd/aura db migrate
fi

echo "==> coverage gate against disposable DB '$COV_DB'"
# NOT `exec` — the EXIT trap must fire to drop the disposable DB (exec replaces the shell).
COV_RC=0
bash scripts/coverage_gate.sh || COV_RC=$?
exit "$COV_RC"
