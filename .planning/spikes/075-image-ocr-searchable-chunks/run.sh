#!/usr/bin/env bash
# Spike 075 live runner (WSL, live stack up). Compose both app and migration DSNs
# from POSTGRES_* primitives when ~/aura.env does not provide explicit overrides.
# Some live helpers read AURA_DB_* directly instead of going through config.LoadDB.
set -uo pipefail
set +H  # the '!'-containing password must not trigger history expansion
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
# shellcheck disable=SC1090
set -a; source "$HOME/aura.env"; set +a
if [[ -z "${AURA_DB_URL:-}" || -z "${AURA_DB_MIGRATE_URL:-}" ]]; then
  : "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD must be set, or set AURA_DB_URL and AURA_DB_MIGRATE_URL}"
  PWD_ENC=$(python3 - <<'PY'
import os
import urllib.parse

print(urllib.parse.quote(os.environ["POSTGRES_PASSWORD"], safe=""))
PY
)
  HOST="${POSTGRES_HOST:-127.0.0.1}"
  PORT="${POSTGRES_PORT:-5432}"
  DB="${POSTGRES_DB:-aura}"
  SSL="${POSTGRES_SSLMODE:-disable}"
  APP_ROLE="${AURA_DB_APP_ROLE:-aura_app}"
  MIGRATE_ROLE="${AURA_DB_MIGRATE_ROLE:-aura_migrate}"
  if [[ -z "${AURA_DB_URL:-}" ]]; then
    export AURA_DB_URL="postgres://${APP_ROLE}:${PWD_ENC}@${HOST}:${PORT}/${DB}?sslmode=${SSL}"
  fi
  if [[ -z "${AURA_DB_MIGRATE_URL:-}" ]]; then
    export AURA_DB_MIGRATE_URL="postgres://${MIGRATE_ROLE}:${PWD_ENC}@${HOST}:${PORT}/${DB}?sslmode=${SSL}"
  fi
fi
export DOCUMENTS_BASE_URL=http://127.0.0.1:8083
cd /mnt/d/Aura
go run ./.planning/spikes/075-image-ocr-searchable-chunks
echo "RUN_EXIT=$?"
