#!/usr/bin/env bash
# Spike 075 live runner (WSL, live stack up). config.LoadDB composes the Postgres DSN
# from POSTGRES_* primitives (url.UserPassword-encoded) with the aura_app role default,
# so we only need the primitives in env. All sidecars are local (no paid API).
set -uo pipefail
set +H  # the '!'-containing password must not trigger history expansion
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
# shellcheck disable=SC1090
set -a; source "$HOME/aura.env"; set +a
export DOCUMENTS_BASE_URL=http://127.0.0.1:8083
cd /mnt/d/Aura
go run ./.planning/spikes/075-image-ocr-searchable-chunks
echo "RUN_EXIT=$?"
