#!/usr/bin/env bash
# scripts/lib/disposable_stack.sh — SOURCED library, not a runnable script (D-12).
#
# Extracted from scripts/coverage_docker.sh: the disposable-Postgres bootstrap and its
# `aura`-name anti-footgun, so both scripts/coverage_docker.sh (the release-blocking
# owned-surface coverage gate) and scripts/musr_e2e.sh (the two-identity acceptance gate)
# source ONE copy instead of two that drift. The guard exists because pointing a
# destructive tagged tier at a live personal deployment's `aura` database destroyed auth
# data on 2026-07-10 (see the original comment preserved in scripts/coverage_docker.sh).
#
# Contract for a sourced file: NO `set -euo pipefail` at file scope (that would change
# the CALLER's shell options — a trap for the second caller), NO `cd` at file scope, and
# NO top-level side effects. Every non-comment line below is a function definition. The
# caller keeps its own `set -euo pipefail` and its own working directory.

# read_secret KEY — honour an exported value first, else read .env (the running stack was
# booted from it). Moved verbatim from coverage_docker.sh, including the trailing-CR
# normalization: .env is commonly edited on Windows, and a stray CR is data to Bash that
# breaks net/url parsing on a composed Postgres URL. cut -d= -f2- keeps '=' inside a
# value intact.
read_secret() {
  local key="$1" val="${!1:-}"
  if [ -z "$val" ] && [ -f .env ]; then
    val="$(grep -E "^${key}=" .env | head -1 | cut -d= -f2-)"
  fi
  printf '%s' "$val" | tr -d '\r'
}

# disposable_stack_guard_name DB_NAME — refuses the name "aura" with exit 4. A
# destructive tagged tier TRUNCATEs/DELETEs shared tables on setup; pointing it at a live
# personal deployment's `aura` database is exactly what destroyed auth data on
# 2026-07-10. This is the ONE place that refusal lives; every caller invokes it before
# anything else touches Postgres.
disposable_stack_guard_name() {
  local db_name="$1"
  if [ "$db_name" = "aura" ]; then
    echo "FATAL: disposable Postgres database name must not be 'aura' — a destructive tagged tier TRUNCATEs/DELETEs it (data loss). Pick a throwaway name." >&2
    exit 4
  fi
}

# _disposable_stack_run_and_wait CONTAINER_NAME PORT IMAGE PGPW — starts a throwaway
# Postgres container published on 127.0.0.1:PORT and polls pg_isready (never a fixed
# sleep) until it accepts connections. Moved verbatim from coverage_docker.sh's local
# branch.
_disposable_stack_run_and_wait() {
  local container_name="$1" port="$2" image="$3" pgpw="$4"
  docker rm -f "$container_name" >/dev/null 2>&1 || true
  echo "==> provisioning disposable Postgres '$container_name' on 127.0.0.1:${port}; removed on exit"
  docker run -d --rm --name "$container_name" \
    -p "127.0.0.1:${port}:5432" \
    -e POSTGRES_USER=aura \
    -e POSTGRES_PASSWORD="$pgpw" \
    -e POSTGRES_DB=aura \
    "$image" >/dev/null

  echo -n "==> waiting for disposable postgres"
  local ready="" _i
  for _i in $(seq 1 60); do
    if docker exec "$container_name" pg_isready -h 127.0.0.1 -U aura -d aura >/dev/null 2>&1; then ready=1; break; fi
    echo -n .; sleep 1
  done
  [ -n "$ready" ] || { echo " FATAL: disposable postgres '$container_name' not ready" >&2; exit 3; }
  echo " ready"
}

# _disposable_stack_provision_role_and_db PG_CONTAINER DB_NAME PGPW — the role
# provisioning loop (aura_app/aura_migrate, prerequisites of migration 0001) and the
# `CREATE DATABASE ... OWNER aura_migrate`, moved verbatim from coverage_docker.sh.
# Resets existing passwords too, so local and CI runs use the same deterministic
# credential.
_disposable_stack_provision_role_and_db() {
  local pg_container="$1" db_name="$2" pgpw="$3"
  local esc_pgpw role
  esc_pgpw="$(printf '%s' "$pgpw" | sed "s/'/''/g")"
  for role in aura_app aura_migrate; do
    if docker exec -i "$pg_container" psql -v ON_ERROR_STOP=1 -U aura -d postgres -tAc "SELECT 1 FROM pg_roles WHERE rolname='${role}'" | grep -q 1; then
      docker exec -i "$pg_container" psql -v ON_ERROR_STOP=1 -U aura -d postgres -c "ALTER ROLE ${role} WITH LOGIN PASSWORD '${esc_pgpw}'"
    else
      docker exec -i "$pg_container" psql -v ON_ERROR_STOP=1 -U aura -d postgres -c "CREATE ROLE ${role} WITH LOGIN PASSWORD '${esc_pgpw}'"
    fi
  done
  echo "==> provisioning disposable DB '$db_name' (owner aura_migrate); dropped on exit"
  docker exec -i "$pg_container" psql -v ON_ERROR_STOP=1 -U aura -d postgres -c "DROP DATABASE IF EXISTS \"$db_name\" WITH (FORCE)"
  docker exec -i "$pg_container" psql -v ON_ERROR_STOP=1 -U aura -d postgres -c "CREATE DATABASE \"$db_name\" OWNER aura_migrate"
}

# disposable_stack_bring_up_auto DB_NAME PGPW [PORT] [IMAGE] [LOCAL_CONTAINER] [CI_CONTAINER]
# — coverage_docker.sh's ORIGINAL dual-mode bring-up, moved verbatim: locally (no
# GITHUB_ACTIONS) spins up its own throwaway Postgres container; under GITHUB_ACTIONS
# reuses the already-running compose "postgres" service (default container name
# aura-postgres) and FATALs if it is not reachable — this never starts a second Postgres
# server inside a CI job that already has one.
#
# Sets, for the caller: DISPOSABLE_STACK_DB, DISPOSABLE_STACK_CONTAINER (non-empty ONLY
# when this call owns a container it must remove on teardown — empty in the CI-reuse
# branch, matching coverage_docker.sh's original COV_POSTGRES semantics),
# DISPOSABLE_STACK_PG_CONTAINER (the container to run psql admin commands against),
# DISPOSABLE_STACK_HOST, DISPOSABLE_STACK_PORT.
disposable_stack_bring_up_auto() {
  local db_name="$1" pgpw="$2"
  local port="${3:-5433}" image="${4:-${POSTGRES_IMAGE:-postgres:18.4-alpine3.24}}"
  local local_container="${5:-aura-postgres-cov}" ci_container="${6:-aura-postgres}"
  disposable_stack_guard_name "$db_name"

  DISPOSABLE_STACK_DB="$db_name"
  DISPOSABLE_STACK_CONTAINER=""
  DISPOSABLE_STACK_HOST="127.0.0.1"
  DISPOSABLE_STACK_PORT="5432"

  if [ -z "${GITHUB_ACTIONS:-}" ]; then
    DISPOSABLE_STACK_CONTAINER="$local_container"
    DISPOSABLE_STACK_PORT="$port"
    _disposable_stack_run_and_wait "$DISPOSABLE_STACK_CONTAINER" "$DISPOSABLE_STACK_PORT" "$image" "$pgpw"
    DISPOSABLE_STACK_PG_CONTAINER="$DISPOSABLE_STACK_CONTAINER"
  else
    DISPOSABLE_STACK_PG_CONTAINER="$ci_container"
    if ! docker exec "$DISPOSABLE_STACK_PG_CONTAINER" true >/dev/null 2>&1; then
      echo "FATAL: postgres container '$DISPOSABLE_STACK_PG_CONTAINER' not running — bring the stack up (make db-up) or set AURA_PG_CONTAINER." >&2
      exit 3
    fi
  fi

  _disposable_stack_provision_role_and_db "$DISPOSABLE_STACK_PG_CONTAINER" "$db_name" "$pgpw"
}

# disposable_stack_bring_up_local DB_NAME PGPW CONTAINER_NAME PORT [IMAGE] — always spins
# up its own disposable Postgres container, regardless of GITHUB_ACTIONS. For a caller
# that is fully self-contained (D-13: "brings up everything it needs") and must never
# assume another step already brought up a shared compose "postgres" service.
#
# Sets the same DISPOSABLE_STACK_* variables as disposable_stack_bring_up_auto.
disposable_stack_bring_up_local() {
  local db_name="$1" pgpw="$2" container_name="$3" port="$4"
  local image="${5:-${POSTGRES_IMAGE:-postgres:18.4-alpine3.24}}"
  disposable_stack_guard_name "$db_name"

  DISPOSABLE_STACK_DB="$db_name"
  DISPOSABLE_STACK_CONTAINER="$container_name"
  DISPOSABLE_STACK_HOST="127.0.0.1"
  DISPOSABLE_STACK_PORT="$port"
  DISPOSABLE_STACK_PG_CONTAINER="$container_name"

  _disposable_stack_run_and_wait "$container_name" "$port" "$image" "$pgpw"
  _disposable_stack_provision_role_and_db "$DISPOSABLE_STACK_PG_CONTAINER" "$db_name" "$pgpw"
}

# disposable_stack_teardown — suitable for the caller's own `trap ... EXIT`. Covers both
# branches a bring_up function can leave behind: remove-the-container (this call owns a
# dedicated container) or drop-the-database (disposable_stack_bring_up_auto's CI branch,
# which reused an already-running container it does not own).
disposable_stack_teardown() {
  if [ -n "${DISPOSABLE_STACK_CONTAINER:-}" ]; then
    docker rm -f "$DISPOSABLE_STACK_CONTAINER" >/dev/null 2>&1 || true
  elif [ -n "${DISPOSABLE_STACK_PG_CONTAINER:-}" ] && [ -n "${DISPOSABLE_STACK_DB:-}" ]; then
    docker exec -i "$DISPOSABLE_STACK_PG_CONTAINER" psql -U aura -d postgres -c "DROP DATABASE IF EXISTS \"$DISPOSABLE_STACK_DB\" WITH (FORCE)" >/dev/null 2>&1 || true
  fi
}

# disposable_stack_export_env PGPW — exports the composed environment the tagged tiers
# actually read: POSTGRES_* primitives, PGHOST/PGPORT (legacy libpq consumers), and the
# composed DSNs (AURA_DB_URL/AURA_DB_MIGRATE_URL/AURA_DB_BOOTSTRAP_URL). Composed DSNs,
# not just the POSTGRES_* primitives — CLAUDE.md's NO-SKIP-AS-GREEN rule names this exact
# gap, because the tagged tests read the composed names via envOrSkip/musrEnvOrSkip. Call
# after a bring_up function has set DISPOSABLE_STACK_*.
disposable_stack_export_env() {
  local pgpw="$1"
  export POSTGRES_USER=aura POSTGRES_PASSWORD="$pgpw" POSTGRES_DB="$DISPOSABLE_STACK_DB"
  export POSTGRES_HOST="$DISPOSABLE_STACK_HOST" POSTGRES_PORT="$DISPOSABLE_STACK_PORT" POSTGRES_SSLMODE=disable
  export PGHOST="$DISPOSABLE_STACK_HOST" PGPORT="$DISPOSABLE_STACK_PORT"
  export AURA_DB_URL="postgres://aura_app:${pgpw}@${DISPOSABLE_STACK_HOST}:${DISPOSABLE_STACK_PORT}/${DISPOSABLE_STACK_DB}?sslmode=disable"
  export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${pgpw}@${DISPOSABLE_STACK_HOST}:${DISPOSABLE_STACK_PORT}/${DISPOSABLE_STACK_DB}?sslmode=disable"
  export AURA_DB_BOOTSTRAP_URL="postgres://aura:${pgpw}@${DISPOSABLE_STACK_HOST}:${DISPOSABLE_STACK_PORT}/${DISPOSABLE_STACK_DB}?sslmode=disable"
}
