#!/usr/bin/env bash
# CLI smoke for `aura identity` against the live Postgres (WSL).
# Invoked as: wsl bash -lc 'set +H; bash ~/smoke_identity_cli.sh'
set -uo pipefail
set +H

export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
set -a; source "$HOME/aura.env"; set +a

cd /mnt/d/Aura
BIN=/tmp/aura-smoke
go build -o "$BIN" ./cmd/aura/ || { echo "BUILD FAILED"; exit 1; }

echo "=== list ==="
"$BIN" identity list || { echo "list FAILED"; exit 1; }

echo "=== get local ==="
"$BIN" identity get local || { echo "get FAILED"; exit 1; }

echo "=== grant local smoke.cap (twice — idempotent) ==="
"$BIN" identity grant local smoke.cap || { echo "grant1 FAILED"; exit 1; }
"$BIN" identity grant local smoke.cap || { echo "grant2 (idempotent) FAILED"; exit 1; }

echo "=== revoke local smoke.cap (twice — idempotent) ==="
"$BIN" identity revoke local smoke.cap || { echo "revoke1 FAILED"; exit 1; }
"$BIN" identity revoke local smoke.cap || { echo "revoke2 (idempotent) FAILED"; exit 1; }

echo "=== grant local '*' (MUST fail non-zero) ==="
if "$BIN" identity grant local '*'; then
  echo "FAIL: grant '*' exited zero (expected non-zero system-managed rejection)"; exit 1
else
  echo "ok: grant '*' rejected with non-zero exit"
fi

echo "=== revoke local '*' (MUST fail non-zero) ==="
if "$BIN" identity revoke local '*'; then
  echo "FAIL: revoke '*' exited zero (expected non-zero)"; exit 1
else
  echo "ok: revoke '*' rejected with non-zero exit"
fi

echo "ALL_SMOKE_PASS"
