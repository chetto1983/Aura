#!/usr/bin/env bash
# Run the internal/runner db_integration tier in WSL against the live Postgres.
# Derives AURA_DB_URL / AURA_DB_MIGRATE_URL from POSTGRES_PASSWORD in ~/aura.env.
# The runner tier (LOOP-02/03/04) needs the seeded `local` identity; the harness
# re-seeds it idempotently before each run (a parallel session can wipe it → FK 23503).
# Invoked as: wsl bash -lc 'set +H; bash /mnt/d/Aura/scripts/run_runner_integration.sh -run "Resume|Pause|Multipause"'
set -euo pipefail
set +H  # disable history expansion for the '!'-containing password

export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"

# shellcheck disable=SC1090
set -a; source "$HOME/aura.env"; set +a

HOST=127.0.0.1
PORT=5432
PWD_ENC=$(python3 -c "import urllib.parse,os;print(urllib.parse.quote(os.environ['POSTGRES_PASSWORD'],safe=''))")
export AURA_DB_URL="postgres://aura_app:${PWD_ENC}@${HOST}:${PORT}/aura?sslmode=disable"
export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${PWD_ENC}@${HOST}:${PORT}/aura?sslmode=disable"
export PGHOST=$HOST PGPORT=$PORT

cd /mnt/d/Aura
go test -tags db_integration -race -count=1 "$@" ./internal/runner/
