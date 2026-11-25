#!/usr/bin/env bash
# The mcp_live_integration tier (internal/webauth/mcp_resources_live_integration_test.go):
# the three first-party MCP resource servers behind a real `aura serve` as their OAuth issuer,
# each proving that one subject's token never reaches another subject's data.
#
# It brings up, on the host network, what the tier talks to and nothing else:
#   - `aura serve` on 127.0.0.1:9080, built from this tree: the issuer whose JWKS every sidecar
#     verifies against, sharing AURA_DB_URL's authula schema with the tier's token minter;
#   - the memory MCP (cmd/arcadedb-mcp) on 8096, built from this tree;
#   - the PIM on 8093 at the pinned image, with the operator configuration in
#     internal/mcp/testdata/pim-appsettings.json (the PIM refuses a tenant-written account that
#     reads its own disk, so the two subjects' fixture accounts are the operator's);
#   - the published WhatsApp MCP on 8092 (bridge on 8094), its store bind-mounted at
#     AURA_WHATSAPP_STORE_ROOT so the tier can seed each tenant's SQLite.
# Then it runs the tier and tears everything down.
#
# The caller provides a migrated Postgres (AURA_DB_URL, which must not be a live deployment's:
# aura serve writes to it), ArcadeDB (ARCADEDB_URL + the root pair), the embedding sidecar
# (AURA_EMBED_BASE_URL, AURA_EMBED_MODEL_DIR) and the strict-profile env `aura serve` boots on.
# ci.yml's mcp-live-integration-test job is the reference caller.
# Needs loopback 8092, 8093, 8094, 8096 and 9080 free.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

: "${AURA_DB_URL:?}" "${AURA_AUTHULA_SECRET:?}" "${ARCADEDB_URL:?}" "${ARCADEDB_ADMIN_PASSWORD:?}"
: "${AURA_EMBED_MODEL_DIR:?}" "${WHATSAPP_BRIDGE_TOKEN:?}" "${AURA_WHATSAPP_STORE_ROOT:?}"

PIM_IMAGE="${AURA_PIM_MCP_DEV_IMAGE:-ghcr.io/chetto1983/aura-pim-mcp:8fbd79960eee2fbb2970a5a725d5759fb44ccfdb}"
WHATSAPP_IMAGE="${AURA_WHATSAPP_MCP_IMAGE:-ghcr.io/chetto1983/whatsapp-mcp:latest}"
PIM=aura-pim-mcp-live
WHATSAPP=aura-whatsapp-mcp-live
WORK="$(mktemp -d)"
SERVE_PID=""
MEMORY_PID=""

teardown() {
  for pid in "$MEMORY_PID" "$SERVE_PID"; do
    [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
  done
  wait 2>/dev/null || true
  docker rm -f "$PIM" "$WHATSAPP" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap teardown EXIT

for port in 8092 8093 8094 8096 9080; do
  if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
    echo "FATAL: 127.0.0.1:$port is already in use" >&2
    exit 1
  fi
done

go build -o "$WORK/aura" ./cmd/aura
go build -o "$WORK/arcadedb-mcp" ./cmd/arcadedb-mcp

dump_logs() {
  tail -n 60 "$WORK/serve.log" "$WORK/memory.log" 2>/dev/null || true
  docker logs "$PIM" --tail 40 2>&1 || true
  docker logs "$WHATSAPP" --tail 40 2>&1 || true
}

# Both binaries run from $WORK: started from a checkout, the daemon's godotenv.Load would read
# a developer's .env, real channel tokens included.
(cd "$WORK" && exec ./aura serve) >"$WORK/serve.log" 2>&1 &
SERVE_PID=$!
for _ in $(seq 1 90); do
  kill -0 "$SERVE_PID" 2>/dev/null || break
  curl -fs -o /dev/null http://127.0.0.1:9080/healthz && break
  sleep 2
done
curl -fs -o /dev/null http://127.0.0.1:9080/healthz || { echo "FATAL: aura serve never answered /healthz" >&2; dump_logs; exit 1; }

MCP_OAUTH_ISSUER=http://127.0.0.1:9080 MCP_OAUTH_JWKS_URL=http://127.0.0.1:9080/oauth/jwks \
  MCP_OAUTH_RESOURCE=http://127.0.0.1:8096/mcp,http://127.0.0.1:8096/mcp/ MCP_OAUTH_SCOPE=mcp:tools \
  AURA_ARCADEDB_MCP_HOST=127.0.0.1 AURA_ARCADEDB_MCP_PORT=8096 \
  sh -c 'cd "$1" && exec ./arcadedb-mcp' _ "$WORK" >"$WORK/memory.log" 2>&1 &
MEMORY_PID=$!

# DOTNET_SYSTEM_NET_DISABLEIPV6: see scripts/pim_tls_mail_up.sh (Docker Desktop's host network).
docker run -d --name "$PIM" --network host \
  -e DOTNET_SYSTEM_NET_DISABLEIPV6=1 -e ASPNETCORE_URLS=http://127.0.0.1:8093 \
  -e CALENDAR_MCP_OAuth__Issuer=http://127.0.0.1:9080 \
  -e CALENDAR_MCP_OAuth__MetadataAddress=http://127.0.0.1:9080/.well-known/oauth-authorization-server \
  -e CALENDAR_MCP_OAuth__Resource=http://127.0.0.1:8093/ \
  -e CALENDAR_MCP_OAuth__ToolsScope=mcp:tools \
  -e AURA_PIM_CONFIG_B64="$(base64 -w0 internal/mcp/testdata/pim-appsettings.json)" \
  --entrypoint /bin/sh "$PIM_IMAGE" \
  -c 'printf "%s" "$AURA_PIM_CONFIG_B64" | base64 -d > /app/data/appsettings.json; exec dotnet CalendarMcp.HttpServer.dll' >/dev/null

mkdir -p "$AURA_WHATSAPP_STORE_ROOT"
docker run -d --name "$WHATSAPP" --network host \
  -e WHATSAPP_BRIDGE_TOKEN="$WHATSAPP_BRIDGE_TOKEN" \
  -e MCP_OAUTH_ISSUER=http://127.0.0.1:9080 \
  -e MCP_OAUTH_JWKS_URL=http://127.0.0.1:9080/oauth/jwks \
  -e MCP_OAUTH_RESOURCE=http://127.0.0.1:8092/mcp/ \
  -e WHATSAPP_MCP_PORT=8092 -e WHATSAPP_GATEWAY_PORT=8094 -e WHATSAPP_API_URL=http://127.0.0.1:8094/api \
  -v "$AURA_WHATSAPP_STORE_ROOT:/app/whatsapp-bridge/store" "$WHATSAPP_IMAGE" >/dev/null

for endpoint in http://127.0.0.1:8096/health http://127.0.0.1:8093/health http://127.0.0.1:8094/healthz; do
  curl --retry 40 --retry-delay 2 --retry-connrefused --retry-all-errors -fsS -o /dev/null "$endpoint" \
    || { echo "FATAL: $endpoint never became healthy" >&2; dump_logs; exit 1; }
done
docker cp internal/mcp/testdata/fixture-calendar.json "$PIM:/app/data/fixture-calendar.json"

# The image runs as root, so a tenant's first request creates its SQLite as root:root below
# the bind mount, where the tier (the runner user) must seed it. Create both tenants first,
# then open the store to either side -- the whatsapp-integration-test job does the same.
for tenant in d1111111-1111-4111-8111-111111111111 d2222222-2222-4222-8222-222222222222; do
  curl -fsS -o /dev/null -H "Authorization: Bearer $WHATSAPP_BRIDGE_TOKEN" -H "X-Tenant-ID: $tenant" \
    http://127.0.0.1:8094/api/status
done
docker exec "$WHATSAPP" chmod -R a+rwX /app/whatsapp-bridge/store

echo "==> PIM $(docker image inspect "$PIM_IMAGE" --format '{{index .RepoDigests 0}}')," \
  "WhatsApp $(docker image inspect "$WHATSAPP_IMAGE" --format '{{index .RepoDigests 0}}')"
AURA_AUTHULA_DSN="${AURA_AUTHULA_DSN:-$AURA_DB_URL}" \
  go test -race -count=1 -tags mcp_live_integration -run TestMCPResourcesLiveCrossSubjectIsolation -v ./internal/webauth/ \
  || { dump_logs; exit 1; }
