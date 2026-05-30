#!/usr/bin/env bash
# Run the internal/askuser db_integration tier in WSL against the live Postgres.
# Derives AURA_DB_URL / AURA_DB_MIGRATE_URL from POSTGRES_PASSWORD in ~/aura.env.
# Invoked as: wsl bash -lc 'set +H; bash /mnt/d/Aura/scripts/run_askuser_integration.sh'
# FIFO determinism wants -count=10 (the script forwards extra args).
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
go test -tags db_integration -race "$@" ./internal/askuser/
