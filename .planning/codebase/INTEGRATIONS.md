# External Integrations

**Analysis Date:** 2026-07-17

> **Provenance rule.** Every count, version and dimension below was measured on 2026-07-17 from
> the repo at the then-current `master` HEAD. Regeneration commands are given for anything that
> drifts. Facts that could not be verified in-session are labelled **Not verified**. Do not copy
> numbers here from other documents.

## APIs & External Services

**LLM (provider-neutral, no vendor SDK):**
- **OpenRouter** — the default upstream. `internal/llm/config.go:19-21`:
  - `defaultProvider = "openrouter"`
  - `defaultModel = "deepseek/deepseek-v4-flash:nitro"` (DeepSeek-V4 Flash, `:nitro` routing variant)
  - `defaultBaseURL = "https://openrouter.ai/api/v1"`
  - Auth: `OPENROUTER_API_KEY` (canonical upstream name, deliberately **not** renamed to `AURA_*`
    — `internal/llm/config.go:56`)
  - Attribution headers sent on every request: `HTTP-Referer: https://github.com/chetto1983/aura`,
    `X-Title: Aura` (`internal/llm/config.go:49-51`)
  - Client: hand-rolled OpenAI-compatible HTTP (`internal/llm/client.go`,
    `internal/llm/openai_compat/`). Swapping providers is config-only (`AURA_LLM_PROVIDER`,
    `AURA_LLM_BASE_URL`, `AURA_LLM_MODEL`).
  - Config load order (`internal/llm/config.go`): built-in const defaults → `.env` →
    `~/.aura/llm.json` → `AURA_LLM_*` env.
  - Budget inputs: `defaultContextWindow = 1_000_000`, `defaultMaxOutputTokens = 32_768`.
  - Resilience: `github.com/sony/gobreaker` circuit breaker (`internal/llm/breaker.go`);
    `defaultStreamIdleTimeoutSec = 60` stall watchdog (resets on any bytes, incl. OpenRouter's
    `: OPENROUTER PROCESSING` keep-alives) firing before the 120s `defaultTotalTimeoutSec`.

**Local inference sidecars (llama.cpp, GPU-first):**
- **`aura-llama-embed`** — `ghcr.io/ggml-org/llama.cpp:server-cuda`, port `8081` internally.
  - Model: `SandLogicTechnologies/granite-embedding-311m-multilingual-r2-GGUF` →
    `granite-embedding-311M-multilingual-r2_Q6_k.gguf` (`compose.yaml:436-438`)
  - **768 dimensions** — `AURA_EMBED_DIMENSIONS:-768` (`compose.yaml:79`), matching the Neo4j
    HNSW index (below). One dimension knob drives both local and cloud embedding.
  - `AURA_EMBED_NGL:-99` (`compose.yaml:445`) — offload **all** layers to GPU.
  - Consumers: `AURA_EMBED_BASE_URL: http://aura-llama-embed:8081` (`compose.yaml:78`).
  - Cloud swap (D-28): reuses the single OpenRouter key + the single `AURA_EMBED_DIMENSIONS`;
    set `AURA_EMBED_MODEL=qwen/qwen3-embedding-8b` via the `openai/` adapter (`compose.yaml:491-501`).
- **`aura-rerank`** — `llama.cpp:server-cuda`, `AURA_RERANK_NGL:-99`. Go client `internal/rerank/`.
  Env: `AURA_RERANK_BASE_URL`, `AURA_RERANK_MODEL`. `compose.yaml:722` records CPU rerank at ~23s,
  i.e. GPU is mandatory for this path.
- **`aura-ocr-vl`** — `llama.cpp:server-cuda`, `profiles: [ocr]` (`compose.yaml:670`), so it is
  **not** started by a default `docker compose up`. `AURA_OCR_VL_NGL:-99`.

**Speech / vision:**
- **`aura-stt`** — `hwdsl2/whisper-server:latest` (whisper.cpp). Env `AURA_STT_CLOUD_MODEL` for
  the cloud fallback. Client `internal/multimodal/stt.go`.
- **`aura-tts`** — `ghcr.io/remsky/kokoro-fastapi-cpu:latest`. Env `AURA_TTS_MODEL`,
  `AURA_TTS_MAX_CHARS`. Client `internal/multimodal/tts.go`.
- **Vision/multimodal** — `internal/multimodal/vision.go`, `client.go`. Env keeps upstream naming:
  `MULTIMODAL_BASE_URL`, `MULTIMODAL_MODEL`, `MULTIMODAL_FALLBACK_MODEL`, `MULTIMODAL_TIMEOUT_SEC`;
  `AURA_VISION_CLOUD` toggles cloud vision.

**Search:**
- **SearXNG** — `searxng/searxng:2026.7.26-b060c780d` (`compose.yaml:666`). Env `SEARXNG_URL`,
  **empty default on purpose (D-05)** — `internal/config/config.go:400-402` — so `web_search`
  fails *closed* with `web_search_unavailable{searxng_not_configured}` rather than silently
  hitting an unexpected host (`internal/web/searxng.go:86`). Also `SEARXNG_SECRET`.
- Tools: `internal/agent/tools/web_search.go`, `web_fetch.go`. Knobs:
  `AURA_WEB_SEARCH_TIMEOUT_SEC`, `AURA_WEB_FETCH_TIMEOUT_SEC`, `AURA_WEB_FETCH_MAX_BODY_BYTES`,
  `AURA_WEB_USER_AGENT`, `AURA_WEB_DNS_PIN_TTL_SEC`, `AURA_WEB_CACHE_PERSISTENT`.

**Document conversion:**
- **`markitdown`** — `aura-markitdown:local`, built from `docker/markitdown/`. Document→Markdown
  ingest leg (`internal/documents/`).

## MCP Servers (the LLM's tool surface)

Aura mounts MCP servers in-process. Client: `internal/mcp/` (`client.go`, `http_client.go`,
`transport.go`, `manager/`). Composition: `internal/config/config_mcp.go` merges the managed
config doc + `AURA_MCP_SERVERS_JSON` env override + default-on recipes (env override wins over
managed; explicit `aura mcp disable <name>` is respected — D-08/D-09).

Recipe identifiers found in source (regenerate:
`grep -rhoE '"recipe:[a-z-]+"' internal cmd --include='*.go' | sort -u`) — note several
(`alpha`, `broken`, `other`, `plain`, `srv`) are test fixtures, not real recipes:

| Recipe | Sidecar | Default-on? |
|--------|---------|-------------|
| `recipe:memory` | `aura-agent-memory-mcp` (`aura-agent-memory-mcp:local`, `:8080`) | **Yes, everywhere** — `memoryRecipeName`, `injectDefaultOnMemory` (`internal/config/config_mcp.go:16,60`) |
| `recipe:calculator` | uvx-launched | **Yes, but only inside the appliance image** (uvx warm-cached) — `injectDefaultOnContainerCalculator` (`internal/config/config_mcp.go:61`) |
| `recipe:whatsapp` | `ghcr.io/chetto1983/whatsapp-mcp:sidecar` | Connect-only |
| `recipe:calendar` / `recipe:mail` | `ghcr.io/chetto1983/aura-pim-mcp:sidecar` | Connect-only |

**Memory MCP is a hard boot dependency.** `compose.yaml` gates the `aura` service on
`aura-agent-memory-mcp: condition: service_healthy` — the in-process mount has **no boot retry**
(reconnect-on-use only recovers an already-mounted server), so racing the sidecar's startup leaves
the agent with zero memory tools until a full restart. The comment at `compose.yaml:~30` documents
this explicitly.

**`mcp-neo4j-cypher`** — the LLM's *only* interface to the graph. `internal/knowledge/client.go:2`
records the native-Go-driver ban: every agent-facing Cypher call goes over MCP
(`Client.Cypher(ctx, query, params, write)` at `internal/knowledge/client.go:198`). Env:
`AURA_MCP_NEO4J_CYPHER_BIN`, `AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC`. Image built from
`docker/mcp-neo4j-cypher/`.

**MCP hardening:** SSRF guards (`internal/mcp/ssrf.go`, `transport_ssrf.go`,
`AURA_MCP_SSRF_ENFORCE`), secret redaction (`internal/mcp/redact.go`), call timeout
(`AURA_MCP_CALL_TIMEOUT_SEC`), boot mount retry (`AURA_MCP_MOUNT_RETRY_ATTEMPTS`), liveness probe
(`internal/mcp/probe.go`).

## Data Storage

**Postgres (primary):**
- `postgres:18.4-alpine3.24` (`compose.yaml:421`), port `5432`, schema `aura.*`.
- Connection: `AURA_DB_URL` (app role), `AURA_DB_MIGRATE_URL` (migrate role),
  `AURA_DB_BOOTSTRAP_URL`; composed by `internal/config` from `POSTGRES_HOST`, `POSTGRES_PORT`,
  `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `POSTGRES_SSLMODE`. Roles:
  `AURA_DB_APP_ROLE`, `AURA_DB_MIGRATE_ROLE`.
  > **CI gotcha (no-skip-as-green):** integration tests read the **composed DSNs**, not the
  > `POSTGRES_*` primitives. CI jobs must export `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` or the tier
  > skips (and skip-helpers `t.Fatal` under `$CI`, failing loudly rather than passing).
- Client: `github.com/jackc/pgx/v5`, sqlc-generated (`internal/db/sqlc/`, `emit_interface: true`).
- Migrations: golang-migrate, `internal/db/migrations/`. **40 migrations** as of 2026-07-17,
  latest `0040_shared_links`.
  Floor is whatever `ls internal/db/migrations/*.up.sql | wc -l` returns; latest is
  `ls internal/db/migrations/ | tail -1`.
- Migrate service: `aura-migrate` (same `aura:local` image), a `service_completed_successfully`
  gate for the `aura` service.

**Neo4j (graph + vectors):**
- `neo4j:5.26.28-community` (`compose.yaml:463`) + APOC + GDS. Bolt `7687`, browser `7474`.
- Connection: `AURA_NEO4J_BOLT_URL`, `AURA_NEO4J_DATABASE`, `NEO4J_USER`, `NEO4J_PASSWORD`.
- Migrations: **2 Cypher files** — `internal/knowledge/migrations/0001_init.cypher`,
  `0002_documents.cypher`. Runner: `internal/knowledge/migrate.go`.
- **Vector index: `chunk_embedding` on `(:Chunk).embedding`, `vector.dimensions: 768`,
  `vector.similarity_function: 'cosine'`, HNSW `m: 32`, `ef_construction: 200`**
  (`internal/knowledge/migrations/0001_init.cypher:12`). Matches `AURA_EMBED_DIMENSIONS:-768`.
- Also `chunk_text` fulltext index (standard analyzer, `eventually_consistent: false`) and a
  `chunk_id` uniqueness constraint — the BM25 leg of hybrid retrieval.
- Native driver `neo4j-go-driver/v5` used only by `internal/knowledge/probe.go`,
  `internal/knowledge/schema.go`, `internal/cron/handlers/backup.go`. Shared Cypher helpers:
  `internal/neostore/neostore.go` (stdlib-only leaf, one copy of the `GraphClient` seam +
  `HashText`/`AsString`/`AsFloats` coercers, extracted to stop three stores decoding APOC
  embeddings differently — D-06/QUAL-03).

**Object storage:**
- **Garage** `dxflrs/garage:v2.3.0` (`compose.yaml:445`) — self-hosted S3-compatible store,
  bootstrapped by the `garage-bootstrap` one-shot service.
- Backend selector `AURA_OBJECTSTORE_BACKEND` — `garage|filesystem-dev|fake`
  (`internal/config/config.go:128`), default `garage` (`config.go:435`). Implementations:
  `internal/objectstore/s3.go`, `filesystem.go`, `fake.go`.
- Defaults: endpoint `http://127.0.0.1:3900`, region `garage`, bucket `aura-assets`.
- Env: `AURA_OBJECTSTORE_{ENDPOINT,PUBLIC_ENDPOINT,REGION,BUCKET,ACCESS_KEY,SECRET_KEY,PATH_STYLE,REPLICATION_FACTOR}`,
  `AURA_GARAGE_ADMIN_ENDPOINT`, `AURA_GARAGE_ADMIN_TOKEN`, `GARAGE_RPC_SECRET`.
  Admin client: `internal/objectstore/garageadmin/`. Per-identity keying:
  `internal/objectstore/identity_store.go`.
- Config is **intentionally non-fatal** (`internal/config/config.go:126-127`) so DB/migration paths
  do not depend on Garage being reachable.
- Asset caps: `AURA_ASSET_MAX_{IMAGE,AUDIO,DOCUMENT}_BYTES`, `AURA_ASSET_PRESIGN_TTL_SEC`,
  `AURA_ASSET_PROCESSING_CONCURRENCY`.

**Filesystem artifacts:**
- `$AURA_RUN_DIR/` — sidecar tool results + spillover (swept:
  `AURA_RUN_DIR_SWEEP_INTERVAL_SEC`, `AURA_RUN_DIR_WARN_THRESHOLD_BYTES`)
- `~/.aura/agents/<id>/` — Agent.md profile (`AURA_PROFILE_DIR`)
- `~/.aura/pyscripts/<id>/` — snippets (`AURA_SKILL_SNIPPET_TTL_DAYS`)
- `$AURA_SKILLS_DIR/` — skill instructions
- `~/.aura/llm.json` — LLM config tier
- Named volumes (`compose.yaml:818-833`): `aura-home`, `aura-postgres`, `aura-neo4j`,
  `aura-neo4j-plugins`, `aura-llama-embed`, `aura-whatsapp-session`, `caddy-data`, `aura-ocr-vl`,
  `aura-rerank`, `aura-pim-data`, `garage-data`, `aura-web`, plus host-bound npm/pip/uv caches.

**Caching:**
- No Redis/Memcached service in `compose.yaml`. `github.com/redis/go-redis/v9` is present but
  **indirect** (pulled by Authula's watermill tree), not used by Aura code.
- Provider-side prompt caching (OpenRouter/DeepSeek) is the real cache; invariants guarded by the
  CI `cache-invariant` job. `AURA_WEB_CACHE_PERSISTENT` covers web-tool caching;
  `internal/cachemetrics/` records hit/miss.

## Authentication & Identity

**Auth Provider: Authula** (`github.com/Authula/authula v1.15.0`), embedded in-process.
- Implementation: `internal/webauth/` — `authula.go` (constructs `authula.New` with three
  mandatory hardenings, per `authula.go:4`), `session_validate.go`, `identity_link.go`.
- Authula owns its `/auth/*` HTTP handler and runs its **own schema migrations** into the
  `authula` schema at construction. **`authula.New` panics on any init error**
  (`internal/webauth/authula.go:91`) — misconfiguration is fail-fast, not degraded.
- Env: `AURA_WEB_AUTH_PROVIDER`, `AURA_WEB_AUTH_SECRET`, `AURA_AUTHULA_SECRET`,
  `AURA_AUTHULA_DSN`, `AURA_AUTHULA_DATABASE_URL`, `AURA_AUTHULA_OPERATOR_IDENTITY`,
  `AURA_AUTHULA_RATE_LIMIT_MAX`.
- 2FA/TOTP supported (E2E harness reads `AURA_E2E_AUTHULA_{EMAIL,PASSWORD,TOTP_CODE,TOTP_SECRET}`).
- Cookie/session plumbing on the AG-UI side: `internal/agui/auth.go`, `auth_cookie.go`.
  `AURA_WEB_TRUST_PROXY` governs proxy-header trust.
- **Identity isolation** is a first-class concern: `internal/identity/`, `internal/identityctx/`,
  `internal/objectstore/identity_store.go`, `internal/mcp/managed_config_identity.go`.
  `AURA_MUSR_ISOLATION` toggles the multi-user isolation mode (CI job `musr-e2e`).
- **Break-glass** recovery: `internal/breakglass/` — `AURA_RECOVERY_QUESTION`,
  `AURA_RECOVERY_ANSWER`, `AURA_RECOVERY_PASSWORD`.
- **Capability grants / approval gateway:** `internal/gateway/` (`approvals.go`, `classify.go`,
  `decide.go`, `guard.go`, `reserve.go`, `reconcile.go`) — the HITL escalation surface.
- Setup/bootstrap: `internal/setup/` — `AURA_SETUP_BIND`, `AURA_SETUP_TOKEN`.

> Local dev note: the `local` identity row (`...001`) is wiped by parallel/coverage runs, causing
> FK `23503` in `db_integration` tests — re-seed via docker-exec psql before running the tier.

## Monitoring & Observability

**Tracing:**
- OpenTelemetry SDK v1.44.0, initialized with the metric provider and one shared resource in
  `internal/obs/init.go` (+ `otel_error_handler.go`).
- Exporters: OTLP/gRPC (`otlptracegrpc`) and stdout (`stdouttrace`).
- Env: `AURA_OTEL_EXPORTER`, `AURA_OTEL_ENDPOINT`.

**Metrics:**
- One OTel `sdkmetric.MeterProvider` owns a dedicated-registry Prometheus reader and, when
  `AURA_OTEL_EXPORTER=otlp`, an independent periodic OTLP/gRPC metric reader. Both use the
  catalog and explicit histogram views in `internal/obs/catalog.go`.
- Canonical OTel metrics are served only by the separately joined private listener configured by
  `AURA_METRICS_BIND` (default `127.0.0.1:9464`). The handler is absent from public AG-UI routes,
  and non-loopback bind values fail validation.
- Prometheus `client_golang` v1.23.2 remains for the authenticated legacy-compatibility
  `GET /metrics` route in `internal/agui/server.go`; that route does not expose the OTel catalog.
- Cache telemetry: `internal/cachemetrics/` (Postgres-backed).

**Logs:**
- Structured `log/slog` (stdlib). `github.com/lmittmann/tint` is present but indirect.

**Error tracking:**
- No Sentry/Rollbar/Bugsnag dependency. **Not detected.**

**Audit trail:**
- `internal/agui/audit_api.go` + `audit_store.go`; `internal/toolinvocations/`;
  reasoning traces in `internal/reasoningtrace/` (`AURA_REASONING_TRACE`,
  `AURA_REASONING_TRACE_FILE`, `AURA_REASONING_TRACE_MAX_BYTES`).

## Channels (agent I/O surfaces)

Registry: `internal/channels/registry.go`, `channel.go`, `deliver.go`. Enablement follows
`AURA_CHANNEL_<NAME>_ENABLED` (e.g. `AURA_CHANNEL_TELEGRAM_ENABLED`).

**Telegram** — `internal/channels/telegram/` (~20 non-test files), `gopkg.in/telebot.v4`.
- **Long-polling, NOT webhooks.** `bot.go:279` drives telebot's default `LongPoller`; the
  `stopWaitPoller` (`bot.go:386-390`) is a zero-network-I/O test double. **There is no inbound
  Telegram webhook endpoint.**
- Env: `TELEGRAM_BOT_TOKEN`, `TELEGRAM_API_BASE_URL`, `TELEGRAM_FILE_BASE_URL`,
  `AURA_TELEGRAM_LOCAL_BOT_API`, `AURA_TELEGRAM_{CHAT_RATE_LIMIT_MS,CONTENT_THROTTLE_MS,STATUS_THROTTLE_MS}`.
- Features: HITL approvals (`hitl.go`), MarkdownV2 rendering (`mdv2.go`, `html.go`), asset/file
  dispatch, onboarding, compaction, AG-UI subscription (`agui_subscriber.go`).

**Web cockpit (AG-UI SPA)** — `internal/agui/` + `internal/webui/` (embedded Vite build).
- Env: `AURA_AGUI_BIND`, `AURA_AGUI_BUFFER_CAP`,
  `AURA_SERVE_SHUTDOWN_GRACE_SEC`.
- Ingress: **Caddy 2** (`compose.yaml:330`), HTTPS termination, `AURA_ACCESS_TOKEN`-gated.

**WhatsApp** — connect-only sidecar `ghcr.io/chetto1983/whatsapp-mcp:sidecar` (whatsmeow bridge),
QR pairing rendered via `qrterminal`. Env: `AURA_MCP_WHATSAPP_BRIDGE_URL`,
`AURA_MCP_WHATSAPP_SERVER_JSON`, `AURA_WHATSAPP_BRIDGE_PORT`, `AURA_WHATSAPP_MCP_PORT`.
Session volume `aura-whatsapp-session`.

**PIM (calendar/mail)** — `ghcr.io/chetto1983/aura-pim-mcp:sidecar`. Env: `AURA_PIM_MCP_URL`,
`AURA_PIM_MCP_PORT`, `AURA_PIM_MCP_ADMIN_TOKEN`. Cockpit proxy: `internal/agui/connect_pim_api.go`.

## Sandbox & Egress

- **Per-user Docker sandbox** — `internal/sandbox/usersandbox`, driving the Docker API via
  `github.com/moby/moby/client`. Env: `AURA_SANDBOX_{IMAGE,CPU_LIMIT,MEMORY_LIMIT,PIDS_LIMIT,IDLE_TTL_SEC,AGENT_URL,AGENT_TOKEN,AGENT_TIMEOUT_SEC,EGRESS_ALLOWLIST,EGRESS_IMAGE}`.
  Tool routing: `internal/agent/tools/sandbox_route.go`, `shell_exec_sandbox.go`,
  `send_file_sandbox.go`.
  > **Its `docker_integration` tests never run in CI** — see STACK.md §Test & Tag Matrix.
- **`docker-socket-proxy`** — `tecnativa/docker-socket-proxy:v0.5.0`, `profiles: [sandbox]`
  (`compose.yaml:374`). **Not started by a default `docker compose up`**; enable with
  `docker compose --profile sandbox up -d` and set
  `AURA_SANDBOX_DOCKER_HOST=tcp://docker-socket-proxy:2375`. The comment
  (`compose.yaml:363-373`) frames it as the escalation surface.
- **Egress control** — `docker/aura-egress/`, `AURA_EGRESS_ENFORCE`, `AURA_EGRESS_FLOOR_RULESET`.
- **Shell** — `AURA_SHELL_{MAX_TIMEOUT_MS,OUTPUT_BUF_CAP,BG_MAX,BG_TTL,BG_BUF_CAP,DESTRUCTIVE_PATTERNS}`.

## CI/CD & Deployment

**Hosting:** self-hosted Docker Compose appliance (`compose.yaml`, project `name: aura`), Caddy 2
HTTPS ingress. **18 services** (regenerate: `grep -nE '^  [a-z0-9-]+:' compose.yaml`):
`aura`, `aura-migrate`, `garage-bootstrap`, `docker-socket-proxy` (profile `sandbox`), `caddy`,
`postgres`, `garage`, `neo4j`, `aura-llama-embed`, `aura-agent-memory-mcp`, `whatsapp`, `searxng`,
`aura-stt`, `aura-tts`, `aura-ocr-vl` (profile `ocr`), `aura-rerank`, `markitdown`, `aura-pim-mcp`.

**CI:** GitHub Actions — `.github/workflows/{ci.yml,codeql.yml,release.yml,skills.yml}`.
`ci.yml` has 24 jobs; job list and tag matrix in STACK.md.

**Registries:** GHCR for the forked sidecars (`ghcr.io/chetto1983/whatsapp-mcp:sidecar`,
`ghcr.io/chetto1983/aura-pim-mcp:sidecar`), `ghcr.io/ggml-org/llama.cpp:server-cuda` and
`ghcr.io/remsky/kokoro-fastapi-cpu` upstream. Locally built: `aura:local`,
`aura-agent-memory-mcp:local`, `aura-markitdown:local`.

**Backup:** `internal/cron/handlers/backup.go` — Postgres `pg_dump` + Neo4j
`neo4j-admin database dump`. Env: `AURA_BACKUP_DIR`, `NEO4J_DUMPFILE`. Restore drill:
`make restore-drill`.

## Environment Configuration

**Required for a working deployment** (from `internal/config/config.go`, `internal/llm/config.go`,
`compose.yaml` `:?`-required vars):
- `OPENROUTER_API_KEY` — LLM upstream
- `POSTGRES_PASSWORD` (+ `POSTGRES_{HOST,PORT,USER,DB,SSLMODE}`) → composed into
  `AURA_DB_URL` / `AURA_DB_MIGRATE_URL`
- `NEO4J_USER`, `NEO4J_PASSWORD`, `AURA_NEO4J_BOLT_URL`
- `AURA_ACCESS_TOKEN` — Caddy ingress
- `AURA_AUTHULA_SECRET`, `AURA_WEB_AUTH_SECRET`
- `AURA_OBJECTSTORE_ACCESS_KEY`, `AURA_OBJECTSTORE_SECRET_KEY`, `GARAGE_RPC_SECRET`,
  `AURA_GARAGE_ADMIN_TOKEN`
- `SEARXNG_URL`, `SEARXNG_SECRET` — web search (empty `SEARXNG_URL` fails closed by design)
- `TELEGRAM_BOT_TOKEN` — only if the Telegram channel is enabled
- `AURA_PIM_MCP_ADMIN_TOKEN` — compose interpolation requires it even when unused

**Secrets location:** `.env` at the repo root (git-ignored; `.env.example` is the template).
Compose interpolates from `.env`; container-internal service URLs deliberately use Compose DNS
rather than the host loopback values in `.env` (`compose.yaml` header comment).
> The compiled binary does **not** auto-load `.env` on every path — strip single quotes from
> values or Postgres auth fails with `28P01`.

**Env var inventory:** 255 distinct literals in Go source (~20 are `AURA_TEST_*` fixtures).
Regenerate with the grep in STACK.md §Configuration.

## Webhooks & Callbacks

**Incoming:** none. Telegram uses long-polling (`internal/channels/telegram/bot.go:279`); WhatsApp
and PIM are outbound MCP sidecar connections. The HTTP surface Aura exposes is the AG-UI/SPA API
(`internal/agui/*_api.go`: conversations, documents, assets, approvals, audit, composer, connect,
bootstrap, compaction-memory) plus `/healthz`, `/readyz`, `/metrics`, `/debug/vars` and the
integrations proxy subtree (`internal/webui/doc.go:7`, `cmd/aura/serve_webui.go:8`) — all
auth-gated, none a third-party webhook receiver.

**Outgoing:** LLM calls to OpenRouter; MCP JSON-RPC to the sidecars; S3 API to Garage; HTTP to
SearXNG / STT / TTS / rerank / embed / markitdown; scheduler notifications back through the
originating channel (`AURA_SCHEDULER_{NOTIFY_DEFAULT,NOTIFY_RECIPIENT,NOTIFY_RETRY_ATTEMPTS,PREFER_ORIGIN_CHANNEL,QUIET_HOURS,TZ,TICK_SECONDS,MAX_CONCURRENT_RUNS}`).

**Public share links — IN FLIGHT, NOT SHIPPED.** `AURA_SHARE_PUBLIC_ENABLED` and
`AURA_SHARE_MAX_EXPIRY_DAYS` exist, migration `0040_shared_links` is on disk, and
`internal/share/` holds `token.go`, `expiry.go`, `snapshot.go`, `redact.go`, `markdown.go`,
`jsonfmt.go`. But `.planning/STATE.md:28` reads `Phase: 37F … — EXECUTING`. **Do not describe
unauthenticated public share links as an available integration surface.**

---

*Integration audit: 2026-07-17*
