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
#
# The disposable-Postgres bootstrap (read_secret, the `aura`-name guard, the bring-up/
# teardown, and the composed-DSN export) lives in the extracted library under
# scripts/lib/ (D-12) — scripts/musr_e2e.sh sources the SAME copy, so the anti-footgun
# this script's own 2026-07-10 incident produced exists once, not twice.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
source scripts/lib/disposable_stack.sh

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
# ownership), and drop it on exit. Override the name with AURA_COVERAGE_DB. The
# `aura`-name refusal itself lives in the sourced library's disposable_stack_guard_name,
# called by disposable_stack_bring_up_auto below.
COV_DB="${AURA_COVERAGE_DB:-aura_cov}"
disposable_stack_bring_up_auto "$COV_DB" "$PGPW" \
  "${AURA_COVERAGE_POSTGRES_PORT:-5433}" \
  "${AURA_COVERAGE_POSTGRES_IMAGE:-${POSTGRES_IMAGE:-postgres:18.4-alpine3.24}}" \
  "${AURA_COVERAGE_POSTGRES_CONTAINER:-aura-postgres-cov}" \
  "${AURA_PG_CONTAINER:-aura-postgres}"
trap disposable_stack_teardown EXIT

# Postgres env — ALL DB-pointing vars target the disposable DB, never live `aura`.
# (EnsureRoles' hardcoded /aura bootstrap in the test helpers only does idempotent
# role/schema-existence management there; every destructive op follows these URLs.)
disposable_stack_export_env "$PGPW"

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
# Skipped in CI, where the workflow migrates its own services. DISPOSABLE_STACK_CONTAINER
# is non-empty only when this run owns its own container (the local branch) — matches the
# original COV_POSTGRES semantics exactly.
if [ -n "$DISPOSABLE_STACK_CONTAINER" ]; then
  echo "==> migrating the schema into the disposable coverage DB"
  go run ./cmd/aura db migrate
fi

echo "==> coverage gate against disposable DB '$COV_DB'"
# NOT `exec` — the EXIT trap must fire to drop the disposable DB (exec replaces the shell).
COV_RC=0
bash scripts/coverage_gate.sh || COV_RC=$?
exit "$COV_RC"
