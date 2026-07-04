#!/usr/bin/env bash
# Spike 083 — 2-identity E2E tenancy setup + run.
# Drives docker from bash with MSYS_NO_PATHCONV=1 (the distroless garage binary is
# /garage; MSYS otherwise rewrites it to C:/Program Files/Git/garage — CONVENTIONS).
# Prereq: `docker compose up -d garage garage-bootstrap aura-agent-memory-mcp` (needs
# neo4j + aura-llama-embed healthy). Run from repo root.
set -euo pipefail
export MSYS_NO_PATHCONV=1
G() { docker exec aura-garage /garage "$@"; }

echo "### GARAGE plane: 2 buckets + 2 scoped keys (bucket-per-identity, per 080)"
G bucket create spike083-a || true
G bucket create spike083-b || true
G key create spike083-key-a >/dev/null 2>&1 || true
G key create spike083-key-b >/dev/null 2>&1 || true
G bucket allow --read --write spike083-a --key spike083-key-a   # key-a -> bucket-a ONLY
G bucket allow --read --write spike083-b --key spike083-key-b   # key-b -> bucket-b ONLY
KEY_A_ID=$(G key info spike083-key-a --show-secret | awk '/Key ID/{print $3}')
KEY_A_SECRET=$(G key info spike083-key-a --show-secret | awk '/Secret key/{print $3}')
KEY_B_ID=$(G key info spike083-key-b --show-secret | awk '/Key ID/{print $3}')
KEY_B_SECRET=$(G key info spike083-key-b --show-secret | awk '/Secret key/{print $3}')

echo "### BOX plane: per-identity named volumes + boxes (078 mechanism, same A/B ids)"
docker rm -f spike083-box-a spike083-box-b >/dev/null 2>&1 || true
docker volume rm spike083-vol-a spike083-vol-b >/dev/null 2>&1 || true
docker volume create spike083-vol-a >/dev/null
docker volume create spike083-vol-b >/dev/null
docker run -d --name spike083-box-a --network none -v spike083-vol-a:/idbox alpine:3.20 sleep 3600 >/dev/null
docker run -d --name spike083-box-b --network none -v spike083-vol-b:/idbox alpine:3.20 sleep 3600 >/dev/null
docker exec spike083-box-a sh -c 'echo AURA-SPIKE-083-ALICE-SECRET > /idbox/secret.txt'
echo -n "  A reads own secret: "; docker exec spike083-box-a cat /idbox/secret.txt
echo -n "  B /idbox listing (empty => isolated): '"; docker exec spike083-box-b sh -c 'ls -A /idbox'; echo "'"
echo -n "  B read A's secret (must fail): "; docker exec spike083-box-b cat /idbox/secret.txt 2>&1 | head -1 || true

echo "### GARAGE + MEMORY planes: Go harness over Aura's real seams"
GARAGE_ENDPOINT="http://127.0.0.1:3900" BUCKET_A=spike083-a BUCKET_B=spike083-b \
KEY_A_ID="$KEY_A_ID" KEY_A_SECRET="$KEY_A_SECRET" KEY_B_ID="$KEY_B_ID" KEY_B_SECRET="$KEY_B_SECRET" \
MEMORY_URL="http://127.0.0.1:8091/mcp/" MEM_ID_A=spike083-alice MEM_ID_B=spike083-bob \
go run ./.planning/spikes/083-two-identity-e2e-tenancy/

echo "### CLEANUP (spike scratch only; leaves stack services)"
docker rm -f spike083-box-a spike083-box-b >/dev/null 2>&1 || true
docker volume rm spike083-vol-a spike083-vol-b >/dev/null 2>&1 || true
G bucket deny --read --write spike083-a --key spike083-key-a >/dev/null 2>&1 || true
G key delete --yes spike083-key-a >/dev/null 2>&1 || true
G key delete --yes spike083-key-b >/dev/null 2>&1 || true
