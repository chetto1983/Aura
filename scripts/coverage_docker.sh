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

# Postgres (superuser aura; the db_integration migratedPool composes the app/migrate
# role DSNs + runs EnsureRoles from these primitives).
export POSTGRES_USER=aura POSTGRES_PASSWORD="$PGPW" POSTGRES_DB=aura
export POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=5432 POSTGRES_SSLMODE=disable
export AURA_DB_URL="postgres://aura_app:${PGPW}@127.0.0.1:5432/aura?sslmode=disable"
export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${PGPW}@127.0.0.1:5432/aura?sslmode=disable"

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

echo "==> coverage gate (mcp via docker image $IMAGE)"
exec bash scripts/coverage_gate.sh
