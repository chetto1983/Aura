#!/usr/bin/env bash
# scripts/musr_e2e.sh — ISO-02a: one command, from a clean checkout, that brings up
# everything the two-identity acceptance gate needs (a disposable Postgres never named
# `aura`, Garage with its Admin API v2 on loopback, ArcadeDB, the embed sidecar), seeds
# Authula, runs the tagged tiers, and tears down what THIS script started. D-13.
#
# Deliberately does NOT name arcadedb-mcp in any `docker compose up` and never calls
# `make memory-up`: both start the WHOLE `aura` daemon via `depends_on: aura`
# (compose.yaml:695-699), racing this tier's own Postgres writes — the measured CI
# #1809 incident the Makefile documents at memory-up-core's definition
# (Makefile:262-270). The MCP-boundary cross-deny test this script runs
# (TestMemoryCrossDenyThroughTheMCPBoundary, cmd/arcadedb-mcp) builds its own
# in-process MCP server over the SAME tenant resolver precisely so it never needs the
# deployed sidecar.
#
# Reuse, not recreate: if garage/arcadedb/aura-llama-embed are ALREADY running (a
# developer's own long-lived stack, or a second concurrent run), this script never
# presents the CI compose override's config diff to an already-live container — it
# reuses what is there and only tears down what it itself started.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
# shellcheck source=scripts/lib/disposable_stack.sh
source scripts/lib/disposable_stack.sh

MUSR_START_EPOCH=$(date +%s)

# --- 1. Preconditions: every secret this gate needs must be resolvable ---------------
# Refuse to proceed on a partial set, naming the first missing one, rather than failing
# halfway through a partially-started stack.
require_secret() {
  local key="$1" val
  val="$(read_secret "$key")"
  if [ -z "$val" ]; then
    echo "FATAL: $key not found in env or .env — musr_e2e needs it before bring-up starts." >&2
    exit 3
  fi
  printf '%s' "$val"
}

PGPW="$(require_secret POSTGRES_PASSWORD)"
AURA_GARAGE_ADMIN_TOKEN="$(require_secret AURA_GARAGE_ADMIN_TOKEN)"
AURA_OBJECTSTORE_ACCESS_KEY="$(require_secret AURA_OBJECTSTORE_ACCESS_KEY)"
AURA_OBJECTSTORE_SECRET_KEY="$(require_secret AURA_OBJECTSTORE_SECRET_KEY)"
AURA_ARCADEDB_TENANT_SECRET="$(require_secret AURA_ARCADEDB_TENANT_SECRET)"
AURA_AUTHULA_SECRET="$(require_secret AURA_AUTHULA_SECRET)"
ARCADEDB_ADMIN_PASSWORD="$(require_secret ARCADEDB_PASSWORD)"
export AURA_GARAGE_ADMIN_TOKEN AURA_OBJECTSTORE_ACCESS_KEY AURA_OBJECTSTORE_SECRET_KEY
export AURA_ARCADEDB_TENANT_SECRET AURA_AUTHULA_SECRET

# --- 2. Disposable Postgres (never the name "aura") + composed DSNs ------------------
# Registered on `trap EXIT` BEFORE anything else is started, so an interrupted run still
# cleans up what it owns.
MUSR_DB="${AURA_MUSR_DB:-aura_musr_e2e}"
MUSR_PG_CONTAINER="${AURA_MUSR_POSTGRES_CONTAINER:-aura-postgres-musr}"
MUSR_PG_PORT="${AURA_MUSR_POSTGRES_PORT:-5434}"

STARTED_COMPOSE_SERVICES=""
_musr_e2e_cleanup() {
  local rc=$?
  echo "==> tearing down what this script started"
  disposable_stack_teardown
  if [ -n "$STARTED_COMPOSE_SERVICES" ]; then
    # shellcheck disable=SC2086
    docker compose stop $STARTED_COMPOSE_SERVICES >/dev/null 2>&1 || true
  fi
  exit "$rc"
}
trap _musr_e2e_cleanup EXIT

disposable_stack_bring_up_local "$MUSR_DB" "$PGPW" "$MUSR_PG_CONTAINER" "$MUSR_PG_PORT"
disposable_stack_export_env "$PGPW"

# --- 3. Garage + ArcadeDB + embed sidecar: reuse already-running, bring up the rest --
# .github/compose.ci-musr.yaml publishes Garage's Admin API v2 on loopback for a
# host-run tier — the loopback publish also already lives in the BASE compose.yaml
# since commit a3536af5d, so layering the override here matches the exact sequence CI
# runs rather than depending on that base-compose fact holding forever.
#
# APPEND, never clobber: the CI job's own env already sets COMPOSE_FILE with a THIRD
# layer, .github/compose.ci-cache.yaml, which swaps aura-llama-embed to the CPU image
# and drops its GPU device reservation — mandatory on a hosted runner with no NVIDIA
# device. Overwriting COMPOSE_FILE here would silently restore the GPU reservation and
# break the job. Locally COMPOSE_FILE is normally unset, so the else branch is what
# actually applies on a developer machine.
if [ -n "${COMPOSE_FILE:-}" ]; then
  case ":${COMPOSE_FILE}:" in
    *:.github/compose.ci-musr.yaml:*) : ;; # already layered by the caller
    *) COMPOSE_FILE="${COMPOSE_FILE}:.github/compose.ci-musr.yaml" ;;
  esac
else
  COMPOSE_FILE="compose.yaml:.github/compose.ci-musr.yaml"
fi
export COMPOSE_FILE

_musr_e2e_service_running() {
  local svc="$1" cid
  cid="$(docker compose ps -q "$svc" 2>/dev/null || true)"
  [ -n "$cid" ] && [ "$(docker inspect -f '{{.State.Running}}' "$cid" 2>/dev/null || echo false)" = "true" ]
}

NEED_UP=""
for svc in garage arcadedb aura-llama-embed; do
  if _musr_e2e_service_running "$svc"; then
    echo "==> $svc already running; reusing without recreating"
  else
    NEED_UP="$NEED_UP $svc"
    STARTED_COMPOSE_SERVICES="$STARTED_COMPOSE_SERVICES $svc"
  fi
done
# Only unstarted services trigger this; when all three are already running (the common
# case on a host with the stack already up) it is skipped entirely, so an already-live
# garage never sees the CI override's config diff and is never recreated out from under
# a concurrent session.
if [ -n "$NEED_UP" ]; then
  docker compose up -d garage arcadedb aura-llama-embed
fi

# Wait for health, polling — never a fixed sleep. arcadedb and aura-llama-embed carry
# Docker healthchecks; garage does not (compose.yaml defines none for it), so its own
# readiness is proven by the `garage status` poll in the bring-up block below, matching
# the pattern ci.yml already uses rather than inventing a second one.
_musr_e2e_wait_healthy() {
  local svc="$1" deadline
  deadline=$(( $(date +%s) + ${AURA_MUSR_HEALTH_TIMEOUT_SEC:-300} ))
  echo "==> waiting for $svc healthy"
  while true; do
    local cid status
    cid="$(docker compose ps -q "$svc" 2>/dev/null || true)"
    if [ -n "$cid" ]; then
      status="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$cid" 2>/dev/null || echo starting)"
    else
      status="starting"
    fi
    [ "$status" = "healthy" ] && { echo "$svc: healthy"; return 0; }
    if [ "$(date +%s)" -ge "$deadline" ]; then
      echo "FATAL: $svc did not become healthy" >&2
      docker compose logs --tail=160 "$svc" || true
      exit 1
    fi
    sleep 2
  done
}
for svc in arcadedb aura-llama-embed; do
  _musr_e2e_wait_healthy "$svc"
done

# --- 4. Migrate the disposable database -----------------------------------------------
echo "==> migrating the schema into the disposable musr-e2e DB '$MUSR_DB'"
go run ./cmd/aura db migrate

# --- 5. Garage layout/bucket/key/allow, then probe the Admin API on loopback ---------
# Ported from ci.yml's "Bring up Garage" + "Verify the Garage Admin API" steps, using
# the deployment's OWN credentials (read from .env in step 1) rather than CI's dummy
# values — every step below is therefore idempotent against an already-configured live
# Garage: the layout is already assigned, the bucket and key already exist, and
# `bucket allow` re-grants permissions the key already has. A broken override fails
# HERE with a clear message rather than as a cryptic tier skip.
garage() { docker compose exec -T garage /garage "$@"; }

AURA_OBJECTSTORE_BUCKET="${AURA_OBJECTSTORE_BUCKET:-aura-assets}"

for _i in $(seq 1 30); do
  if garage status >/tmp/aura-musr-garage-status.txt 2>&1; then
    cat /tmp/aura-musr-garage-status.txt
    break
  fi
  cat /tmp/aura-musr-garage-status.txt || true
  sleep 2
done

node_id="$(garage status | awk '/NO ROLE ASSIGNED|pending\.\.\./ { print $1; exit }')"
if [ -n "$node_id" ]; then
  garage layout assign -z dc1 -c 1G "$node_id" >/dev/null
  version="$(garage layout show | awk '/garage layout apply --version/ { print $NF; exit }')"
  if [ -n "$version" ]; then
    garage layout apply --version "$version" >/dev/null
  fi
fi

if ! garage bucket info "$AURA_OBJECTSTORE_BUCKET" >/dev/null 2>&1; then
  garage bucket create "$AURA_OBJECTSTORE_BUCKET" >/dev/null
fi
if ! garage key info "$AURA_OBJECTSTORE_ACCESS_KEY" >/dev/null 2>&1; then
  garage key import --yes -n aura-musr-e2e \
    "$AURA_OBJECTSTORE_ACCESS_KEY" \
    "$AURA_OBJECTSTORE_SECRET_KEY" >/dev/null
fi
garage bucket allow --read --write --owner \
  "$AURA_OBJECTSTORE_BUCKET" \
  --key "$AURA_OBJECTSTORE_ACCESS_KEY" >/dev/null

AURA_GARAGE_ADMIN_ENDPOINT="${AURA_GARAGE_ADMIN_ENDPOINT:-http://127.0.0.1:${AURA_GARAGE_ADMIN_PORT:-3903}}"
export AURA_GARAGE_ADMIN_ENDPOINT

code="$(curl -s -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer $AURA_GARAGE_ADMIN_TOKEN" \
  "$AURA_GARAGE_ADMIN_ENDPOINT/v2/GetClusterStatus" || echo 000)"
echo "admin /v2/GetClusterStatus -> $code"
case "$code" in
  200 | 204) : ;;
  *)
    echo "FATAL: Garage Admin API not reachable on $AURA_GARAGE_ADMIN_ENDPOINT" >&2
    exit 1
    ;;
esac

# --- 6. Object store + memory + auth env for the host-run go test --------------------
export AURA_OBJECTSTORE_BACKEND=garage
export AURA_OBJECTSTORE_ENDPOINT="http://127.0.0.1:${AURA_GARAGE_PORT:-3900}"
export AURA_OBJECTSTORE_PUBLIC_ENDPOINT="$AURA_OBJECTSTORE_ENDPOINT"
export AURA_OBJECTSTORE_REGION="${AURA_OBJECTSTORE_REGION:-garage}"
export AURA_OBJECTSTORE_BUCKET
export AURA_OBJECTSTORE_PATH_STYLE="${AURA_OBJECTSTORE_PATH_STYLE:-true}"

export ARCADEDB_URL="${ARCADEDB_URL:-http://127.0.0.1:2480}"
export ARCADEDB_DATABASE="${ARCADEDB_DATABASE:-aura_memory}"
export ARCADEDB_ADMIN_USER="${ARCADEDB_ADMIN_USER:-root}"
export ARCADEDB_PASSWORD="$ARCADEDB_ADMIN_PASSWORD"
export ARCADEDB_ADMIN_PASSWORD

export AURA_WEB_AUTH_PROVIDER=authula
export AURA_AUTHULA_RATE_LIMIT_MAX="${AURA_AUTHULA_RATE_LIMIT_MAX:-1000}"
export AURA_E2E_AUTHULA_EMAIL="${AURA_E2E_AUTHULA_EMAIL:-e2e-operator@example.test}"
export AURA_E2E_AUTHULA_PASSWORD="${AURA_E2E_AUTHULA_PASSWORD:-ci-e2e-password-123}"

export AURA_EMBED_BASE_URL="${AURA_EMBED_BASE_URL:-http://127.0.0.1:8081}"
export AURA_EMBED_DIMENSIONS="${AURA_EMBED_DIMENSIONS:-768}"

export AURA_SKILL_EXPORT_DIR="${AURA_SKILL_EXPORT_DIR:-/tmp/aura-musr-skills-export}"
export AURA_RUN_DIR="${AURA_RUN_DIR:-/tmp/aura-musr-run}"
mkdir -p "$AURA_SKILL_EXPORT_DIR" "$AURA_RUN_DIR"
# No-skip-as-green arm: musrEnvOrSkip/envOrSkip t.Fatal on a missing var under CI=true
# instead of silently skipping the tier (CLAUDE.md).
export CI=true

# --- 7. Seed the Authula operator (embedded provider, 0019 schema) -------------------
go run ./scripts/authula_seed_e2e.go

# --- 8. The tagged tiers: acceptance E2E, then the MCP-boundary memory test ----------
echo "==> two-identity cross-deny E2E (5 tags, ./cmd/aura/)"
go test -race -count=1 -p 1 \
  -tags 'db_integration garage_integration authula_integration musr_e2e arcadedb_integration' \
  -run 'TestTwoIdentityCrossDeny|TestProvisionLoginIsolatedRun|TestIdentityCreateProvisionsEveryPlane' \
  ./cmd/aura/

echo "==> memory cross-deny at the MCP boundary (arcadedb_integration, ./cmd/arcadedb-mcp/)"
go test -race -count=1 -p 1 \
  -tags 'arcadedb_integration' \
  -run 'TestMemoryCrossDenyThroughTheMCPBoundary' \
  ./cmd/arcadedb-mcp/

# --- 9. Teardown happens via the EXIT trap registered in step 2 ----------------------
MUSR_END_EPOCH=$(date +%s)
echo "==> make musr-e2e: $(( MUSR_END_EPOCH - MUSR_START_EPOCH ))s wall-clock"
