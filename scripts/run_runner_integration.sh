#!/usr/bin/env bash
# Run the internal/runner db_integration tier in WSL against a disposable database.
# Uses an exported POSTGRES_PASSWORD or the repository .env, provisions one database
# in the running Postgres container, and removes it on every exit path.
# The runner tier (LOOP-02/03/04) needs the seeded `local` identity; the harness
# re-seeds it idempotently before each run (a parallel session can wipe it → FK 23503).
# Invoked as: wsl bash -lc 'set +H; bash /mnt/d/Aura/scripts/run_runner_integration.sh -run "Resume|Pause|Multipause"'
set -euo pipefail
set +H  # disable history expansion for the '!'-containing password

export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
cd /mnt/d/Aura

if [ -z "${POSTGRES_PASSWORD:-}" ] && [ -f .env ]; then
  POSTGRES_PASSWORD="$(grep -E '^POSTGRES_PASSWORD=' .env | head -1 | cut -d= -f2- | tr -d '\r')"
fi
if [ -z "${POSTGRES_PASSWORD:-}" ]; then
  echo "FATAL: POSTGRES_PASSWORD not found in env or repository .env" >&2
  exit 3
fi
export POSTGRES_PASSWORD

HOST=127.0.0.1
PORT=5432
PG_CONTAINER="${AURA_PG_CONTAINER:-aura-postgres}"
TEST_DB="${AURA_RUNNER_TEST_DB:-aura_runner_test_$$}"
if [[ ! "$TEST_DB" =~ ^[a-z][a-z0-9_]{0,62}$ ]] || [ "$TEST_DB" = "aura" ]; then
  echo "FATAL: AURA_RUNNER_TEST_DB must be a safe disposable name other than 'aura'" >&2
  exit 4
fi

PWD_ENC=$(python3 -c "import urllib.parse,os;print(urllib.parse.quote(os.environ['POSTGRES_PASSWORD'],safe=''))")
pg_admin() { docker exec -i "$PG_CONTAINER" psql -v ON_ERROR_STOP=1 -U aura -d postgres "$@"; }
cleanup() {
  local ec=$?
  trap - EXIT
  set +e
  pg_admin -c "DROP DATABASE IF EXISTS \"$TEST_DB\" WITH (FORCE)" >/dev/null
  exit "$ec"
}
trap cleanup EXIT

pg_admin -c "DROP DATABASE IF EXISTS \"$TEST_DB\" WITH (FORCE)" >/dev/null
pg_admin -c "CREATE DATABASE \"$TEST_DB\" OWNER aura_migrate" >/dev/null

export POSTGRES_DB="$TEST_DB"
export AURA_DB_URL="postgres://aura_app:${PWD_ENC}@${HOST}:${PORT}/${TEST_DB}?sslmode=disable"
export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${PWD_ENC}@${HOST}:${PORT}/${TEST_DB}?sslmode=disable"
export PGHOST=$HOST PGPORT=$PORT

go test -tags db_integration -race -count=1 "$@" ./internal/runner/
