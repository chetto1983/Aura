#!/usr/bin/env bash
# The mcp_live_integration tier (internal/webauth/mcp_resources_live_integration_test.go):
# the three first-party MCP resource servers behind a real `aura serve` as their OAuth issuer,
# each proving that one subject's token never reaches another subject's data.
#
# It brings up, on the host network, what the tier talks to and nothing else:
#   - its own Postgres container on 127.0.0.1:5434 (scripts/lib/disposable_stack.sh,
#     disposable_stack_bring_up_local) holding the throwaway database, migrated by the binary
#     under test;
#   - its own ArcadeDB container on 127.0.0.1:2490, at the image compose.yaml pins;
#   - `aura serve` on 127.0.0.1:9080, built from this tree: the issuer whose JWKS every sidecar
#     verifies against, sharing the throwaway database's authula schema with the tier's minter;
#   - the memory MCP (cmd/arcadedb-mcp) on 8096, built from this tree, on that ArcadeDB;
#   - the PIM on 8093 at the pinned image, with the operator configuration in
#     internal/mcp/testdata/pim-appsettings.json (the PIM refuses a tenant-written account that
#     reads its own disk, so the two subjects' fixture accounts are the operator's);
#   - the published WhatsApp MCP on 8092 (bridge on 8094), its store a fresh directory of this
#     run, bind-mounted so the tier can seed each tenant's SQLite.
# Then it runs the tier and removes all of it, both containers' data included.
#
# What it reaches outside that: the Docker daemon (it pulls and runs the images above), the
# loopback ports above, and the caller's embedding sidecar, which is stateless: it embeds text
# and keeps nothing. It reads the GGUF in AURA_EMBED_MODEL_DIR, and writes only below its own
# temporary directory.
#
# What it cannot reach:
#   - another Postgres or ArcadeDB: it never takes a database address from its caller, so no
#     live database can be targeted, and a caller DSN naming `aura` is refused before anything
#     starts;
#   - .env, or anything else in the caller's environment: aura serve and the memory MCP run
#     under `env -i` with an explicit allowlist, from a directory with no .env, on credentials
#     the run generates;
#   - Telegram: aura serve runs with --only=cli, which keeps its one channel off even if a
#     settings row held a token.
#
# The caller provides only the embedding sidecar: AURA_EMBED_BASE_URL, AURA_EMBED_MODEL_DIR
# (holding the GGUF a strict boot hashes), AURA_EMBED_REVISION and AURA_EMBED_FINGERPRINT.
# ci.yml's mcp-live-integration-test job is the reference caller.
# Needs loopback 2490, 5434, 8092, 8093, 8094, 8096 and 9080 free.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
# shellcheck source=scripts/lib/disposable_stack.sh
source scripts/lib/disposable_stack.sh

# A caller that exports a DSN naming the live database expects the tier to use it: refuse that
# loudly instead of ignoring it.
for dsn in "${AURA_DB_URL:-}" "${AURA_AUTHULA_DSN:-}"; do
  [ -n "$dsn" ] || continue
  name="${dsn##*/}"
  disposable_stack_guard_name "${name%%\?*}"
done
DB="${AURA_MCP_LIVE_DB:-aura_mcp_live}"
disposable_stack_guard_name "$DB"

: "${AURA_EMBED_BASE_URL:?}" "${AURA_EMBED_MODEL_DIR:?}" "${AURA_EMBED_REVISION:?}" "${AURA_EMBED_FINGERPRINT:?}"

PG_PORT=5434
ARCADE_PORT=2490
for port in "$ARCADE_PORT" "$PG_PORT" 8092 8093 8094 8096 9080; do
  if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
    echo "FATAL: 127.0.0.1:$port is already in use" >&2
    exit 1
  fi
done

ARCADE_IMAGE="$(sed -nE 's/^    image: (arcadedata\/arcadedb:[^[:space:]]+)$/\1/p' compose.yaml | head -1)"
[ -n "$ARCADE_IMAGE" ] || { echo "FATAL: no arcadedb image in compose.yaml" >&2; exit 1; }
PIM_IMAGE="${AURA_PIM_MCP_DEV_IMAGE:-ghcr.io/chetto1983/aura-pim-mcp:bcab36e2a4f0c18ead2aed4205485c22b3d530f5}"
WHATSAPP_IMAGE="${AURA_WHATSAPP_MCP_IMAGE:-ghcr.io/chetto1983/whatsapp-mcp:latest}"
ARCADE=aura-arcadedb-mcp-live
PIM=aura-pim-mcp-live
WHATSAPP=aura-whatsapp-mcp-live
random_hex() { od -An -tx1 -N"$1" /dev/urandom | tr -d ' \n'; }
PG_PASSWORD="$(random_hex 16)"
ARCADE_PASSWORD="$(random_hex 16)"
ARCADEDB_URL="http://127.0.0.1:$ARCADE_PORT"
# A fresh authula schema is self-consistent with any 64-hex secret, and the tenant secret only
# derives the memory databases this run's ArcadeDB holds.
AUTHULA_SECRET="$(random_hex 32)"
TENANT_SECRET="$(random_hex 32)"
BRIDGE_TOKEN="$(random_hex 16)"
WORK="$(mktemp -d)"
STORE="$WORK/whatsapp-store"
SERVE_PID=""
MEMORY_PID=""

# Best effort, step by step: under the inherited `set -e` one failing step would skip the rest,
# the disposable Postgres container among them.
teardown() {
  set +e
  for pid in "$MEMORY_PID" "$SERVE_PID"; do
    [ -n "$pid" ] && kill "$pid" 2>/dev/null
  done
  wait 2>/dev/null
  # The WhatsApp image writes the store as root; empty it, dotfiles included, from inside.
  docker exec "$WHATSAPP" find /app/whatsapp-bridge/store -mindepth 1 -delete >/dev/null 2>&1
  docker rm -f "$PIM" "$WHATSAPP" "$ARCADE" >/dev/null 2>&1
  rm -rf "$WORK"
  disposable_stack_teardown
}
trap teardown EXIT

disposable_stack_bring_up_local "$DB" "$PG_PASSWORD" aura-postgres-mcp-live "$PG_PORT"
disposable_stack_export_env "$PG_PASSWORD"

# compose.yaml's own JAVA_OPTS form: the empty brackets after the database name are
# load-bearing (see the arcadedb-integration-test job in ci.yml).
docker run -d --rm --name "$ARCADE" -p "127.0.0.1:$ARCADE_PORT:2480" \
  -e ARCADEDB_OPTS_MEMORY="-Xms256M -Xmx768M" \
  -e JAVA_OPTS="-Darcadedb.server.rootPassword=$ARCADE_PASSWORD -Darcadedb.server.defaultDatabases=aura_memory[]" \
  "$ARCADE_IMAGE" >/dev/null

go build -o "$WORK/aura" ./cmd/aura
go build -o "$WORK/arcadedb-mcp" ./cmd/arcadedb-mcp
mkdir -p "$WORK/run" "$WORK/skills" "$STORE"

curl --retry 60 --retry-delay 2 --retry-connrefused --retry-all-errors -fsS -o /dev/null \
  "$ARCADEDB_URL/api/v1/ready" || { echo "FATAL: the run's ArcadeDB never became ready" >&2; docker logs "$ARCADE" --tail 40; exit 1; }

base_env=(HOME="$WORK" PATH="$PATH" TMPDIR="$WORK")
if [ -n "${DOCKER_HOST:-}" ]; then base_env+=(DOCKER_HOST="$DOCKER_HOST"); fi
db_env=(AURA_DB_URL="$AURA_DB_URL" AURA_DB_MIGRATE_URL="$AURA_DB_MIGRATE_URL" AURA_AUTHULA_SECRET="$AUTHULA_SECRET")
arcade_env=(ARCADEDB_URL="$ARCADEDB_URL" ARCADEDB_ADMIN_USER=root ARCADEDB_ADMIN_PASSWORD="$ARCADE_PASSWORD"
  AURA_ARCADEDB_TENANT_SECRET="$TENANT_SECRET")
embed_env=(AURA_EMBED_BASE_URL="$AURA_EMBED_BASE_URL" AURA_EMBED_DIMENSIONS="${AURA_EMBED_DIMENSIONS:-768}"
  AURA_EMBED_MODEL_DIR="$AURA_EMBED_MODEL_DIR" AURA_EMBED_REVISION="$AURA_EMBED_REVISION"
  AURA_EMBED_FINGERPRINT="$AURA_EMBED_FINGERPRINT")
# The strict profile an appliance runs, with the model-in-the-loop skill's dummies
# (.claude/skills/aura-model-in-the-loop/env.e2e.sh): no model is called, and the object store
# keys are ones a strict boot accepts.
serve_env=(AURA_PROFILE=single_user_hardened AURA_TIMEZONE=Europe/Rome
  AURA_LLM_PROVIDER=llamacpp AURA_LLM_MODEL=no-model AURA_LLM_BASE_URL=http://127.0.0.1:18484/v1
  AURA_OBJECTSTORE_BACKEND=filesystem-dev AURA_OBJECTSTORE_ACCESS_KEY=GKe2e0000000000000000000ab
  AURA_OBJECTSTORE_SECRET_KEY=e2e-not-a-sample-secret-0000000000000000000000000000000000000000
  GARAGE_RPC_SECRET=e2e00000000000000000000000000000000000000000000000000000000000d4
  AURA_RUN_DIR="$WORK/run" AURA_SKILL_EXPORT_DIR="$WORK/skills")

(cd "$WORK" && exec env -i "${base_env[@]}" "${db_env[@]}" AURA_DB_BOOTSTRAP_URL="$AURA_DB_BOOTSTRAP_URL" \
  ./aura db migrate) >"$WORK/migrate.log" 2>&1 \
  || { tail -n 40 "$WORK/migrate.log" >&2; exit 1; }

dump_logs() {
  tail -n 60 "$WORK/serve.log" "$WORK/memory.log" 2>/dev/null || true
  docker logs "$PIM" --tail 40 2>&1 || true
  docker logs "$WHATSAPP" --tail 40 2>&1 || true
}

(cd "$WORK" && exec env -i "${base_env[@]}" "${db_env[@]}" "${arcade_env[@]}" "${embed_env[@]}" "${serve_env[@]}" \
  ./aura serve --only=cli) >"$WORK/serve.log" 2>&1 &
SERVE_PID=$!
for _ in $(seq 1 90); do
  kill -0 "$SERVE_PID" 2>/dev/null || break
  curl -fs -o /dev/null http://127.0.0.1:9080/healthz && break
  sleep 2
done
curl -fs -o /dev/null http://127.0.0.1:9080/healthz || { echo "FATAL: aura serve never answered /healthz" >&2; dump_logs; exit 1; }

(cd "$WORK" && exec env -i "${base_env[@]}" "${db_env[@]}" "${arcade_env[@]}" "${embed_env[@]}" \
  ARCADEDB_DATABASE=aura_memory ARCADEDB_USER=root ARCADEDB_PASSWORD="$ARCADE_PASSWORD" \
  MCP_OAUTH_ISSUER=http://127.0.0.1:9080 MCP_OAUTH_JWKS_URL=http://127.0.0.1:9080/oauth/jwks \
  MCP_OAUTH_RESOURCE=http://127.0.0.1:8096/mcp,http://127.0.0.1:8096/mcp/ MCP_OAUTH_SCOPE=mcp:tools \
  AURA_ARCADEDB_MCP_HOST=127.0.0.1 AURA_ARCADEDB_MCP_PORT=8096 ./arcadedb-mcp) >"$WORK/memory.log" 2>&1 &
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

docker run -d --name "$WHATSAPP" --network host \
  -e WHATSAPP_BRIDGE_TOKEN="$BRIDGE_TOKEN" \
  -e MCP_OAUTH_ISSUER=http://127.0.0.1:9080 \
  -e MCP_OAUTH_JWKS_URL=http://127.0.0.1:9080/oauth/jwks \
  -e MCP_OAUTH_RESOURCE=http://127.0.0.1:8092/mcp/ \
  -e WHATSAPP_MCP_PORT=8092 -e WHATSAPP_GATEWAY_PORT=8094 -e WHATSAPP_API_URL=http://127.0.0.1:8094/api \
  -v "$STORE:/app/whatsapp-bridge/store" "$WHATSAPP_IMAGE" >/dev/null

for endpoint in http://127.0.0.1:8096/health http://127.0.0.1:8093/health http://127.0.0.1:8094/healthz; do
  curl --retry 40 --retry-delay 2 --retry-connrefused --retry-all-errors -fsS -o /dev/null "$endpoint" \
    || { echo "FATAL: $endpoint never became healthy" >&2; dump_logs; exit 1; }
done
docker cp internal/mcp/testdata/fixture-calendar.json "$PIM:/app/data/fixture-calendar.json"

# The image runs as root, so a tenant's first request creates its SQLite as root:root below
# the bind mount, where the tier (the runner user) must seed it. Create both tenants first,
# then open the store to either side -- the whatsapp-integration-test job does the same.
for tenant in d1111111-1111-4111-8111-111111111111 d2222222-2222-4222-8222-222222222222; do
  curl -fsS -o /dev/null -H "Authorization: Bearer $BRIDGE_TOKEN" -H "X-Tenant-ID: $tenant" \
    http://127.0.0.1:8094/api/status
done
docker exec "$WHATSAPP" chmod -R a+rwX /app/whatsapp-bridge/store

echo "==> PIM $(docker image inspect "$PIM_IMAGE" --format '{{index .RepoDigests 0}}')," \
  "WhatsApp $(docker image inspect "$WHATSAPP_IMAGE" --format '{{index .RepoDigests 0}}')," \
  "database $DB on 127.0.0.1:$PG_PORT, ArcadeDB $ARCADE_IMAGE on $ARCADEDB_URL"
AURA_AUTHULA_DSN="$AURA_DB_URL" AURA_AUTHULA_SECRET="$AUTHULA_SECRET" \
  AURA_WHATSAPP_STORE_ROOT="$STORE" WHATSAPP_BRIDGE_TOKEN="$BRIDGE_TOKEN" \
  ARCADEDB_URL="$ARCADEDB_URL" ARCADEDB_ADMIN_USER=root ARCADEDB_ADMIN_PASSWORD="$ARCADE_PASSWORD" \
  bash scripts/go_test_ledger.sh mcp-live -race -count=1 -tags mcp_live_integration -run TestMCPResourcesLiveCrossSubjectIsolation -v ./internal/webauth/ \
  || { dump_logs; exit 1; }
