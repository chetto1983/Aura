#!/usr/bin/env bash
# Spike 077 live runner (WSL, stack up). Drives Aura's REAL LlmAgent + REAL OpenRouter
# client (PAID, bounded ~7 turns — operator-authorized). config.Load reads
# OPENROUTER_API_KEY + composes the DB/Neo4j env from ~/aura.env primitives.
set -uo pipefail
set +H
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
# shellcheck disable=SC1090
set -a; source "$HOME/aura.env"; set +a
export DOCUMENTS_BASE_URL=http://127.0.0.1:8083
cd /mnt/d/Aura
go run ./.planning/spikes/077-catalog-injection-recall
echo "RUN_EXIT=$?"
