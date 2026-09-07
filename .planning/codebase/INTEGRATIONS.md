# External Integrations

**Analysis Date:** 2026-09-07

## APIs & External Services

**LLM inference (OpenAI-compatible, one client for all three providers):**
- OpenRouter — default provider (`internal/llm/config.go`: `defaultProvider = "openrouter"`, `defaultBaseURL = "https://openrouter.ai/api/v1"`).
  - Client: `internal/llm/openai_compat/client.go` over `github.com/openai/openai-go/v3`
  - Auth: `OPENROUTER_API_KEY` (plus `OPENROUTER_MANAGEMENT_KEY` for spend/limits)
  - Knobs: `AURA_LLM_PROVIDER`, `AURA_LLM_BASE_URL`, `AURA_LLM_MODEL`, `AURA_LLM_OPENROUTER_MIDDLE_OUT`, sampling knobs default UNSET so backend-published values win
- llama.cpp server — local chat model, opt-in Compose profile `localllm` (`compose.yaml` service `aura-llm`, image `ghcr.io/ggml-org/llama.cpp:server-cuda`, port 8084).
  - Capability probe: `internal/llm/llamacpp_caps.go` (reads `GET /props`)
  - Env: `AURA_LLM_MODEL_PATH`, `AURA_LLM_DRAFT_MODEL_PATH` (speculative decoding), `AURA_LLM_NGL`, `AURA_LLM_CTX`, `AURA_LLM_KV_QUANT`, `LLAMA_ARG_HOST`
  - Contract gate: `make llm-model-contract`
- Ollama — alternate local backend; capability probe `internal/llm/ollama_caps.go` (reads `/api/show`), live tier `internal/llm/openai_compat/ollama_live_e2e_test.go`.
  - Env: `AURA_OLLAMA_BASE_URL`, `AURA_OLLAMA_MODEL`, `AURA_OLLAMA_LIVE`
- Shared model metadata: `internal/llm/model_catalog.go`, `prices.go`, `pricing_source.go`, `capabilities.go`; spend accounting in `internal/llm/spend.go`; circuit breaker in `internal/llm/breaker.go`.

**Embeddings (local, llama.cpp GGUF):**
- Service `aura-llama-embed` (`compose.yaml`, same llama.cpp CUDA image), client `internal/embeddings/client.go`, task prefixes in `internal/embeddings/tasks.go`.
- Env: `AURA_EMBED_BASE_URL`, `AURA_EMBED_MODEL`, `AURA_EMBED_MODEL_PATH`, `AURA_EMBED_DIMENSIONS`, `AURA_EMBED_REVISION`, `AURA_EMBED_FINGERPRINT`, `AURA_EMBED_API_KEY`, `AURA_EMBED_NGL`; memory-side overrides `AURA_MEMORY_EMBED_BASE_URL` / `AURA_MEMORY_EMBED_API_KEY`.
- Model materialization is contract-gated: `make embedding-model-contract`.

**Multimodal sidecars:**
- Vision / OCR — `internal/multimodal/vision.go`; Compose `aura-ocr-vl` (llama.cpp CUDA, profile `ocr`). Env `MULTIMODAL_BASE_URL`, `MULTIMODAL_MODEL`, `MULTIMODAL_TIMEOUT_SEC`, `AURA_VISION_CLOUD`, `AURA_OCR_VL_PORT`.
- Speech-to-text — `internal/multimodal/stt.go`; Compose `aura-stt` (`hwdsl2/whisper-server`). Env `STT_BASE_URL`, `STT_MODEL`, `STT_LANGUAGE`, `STT_COMPUTE_TYPE`, `STT_DEVICE`, `AURA_STT_PORT`, `AURA_STT_CLOUD_MODEL`.
- Text-to-speech — `internal/multimodal/tts.go`; Compose `aura-tts` (`ghcr.io/remsky/kokoro-fastapi-cpu`). Env `TTS_BASE_URL`, `TTS_VOICE`, `TTS_FORMAT`, `AURA_TTS_PORT`, `AURA_TTS_MODEL`, `AURA_TTS_MAX_CHARS`.
- Reranking: `AURA_RERANK_BASE_URL`, `AURA_RERANK_MODEL`.

**Web search & fetch:**
- SearXNG — self-hosted meta-search (`compose.yaml` service `searxng`, image `searxng/searxng:2026.7.26`). Client `internal/web/searxng.go`, env `SEARXNG_URL`, `AURA_WEB_SEARCH_TIMEOUT_SEC`.
- Outbound page fetch — `internal/web/fetcher.go`, `fetcher_text.go`, `fetcher_image.go`, with SSRF guard `internal/web/ssrf.go`, DNS pinning `dnspin.go`, throttling `throttle.go`, cache `cache.go`. Env `AURA_WEB_FETCH_MAX_BODY_BYTES`, `AURA_WEB_FETCH_TIMEOUT_SEC`, `AURA_WEB_USER_AGENT`, `AURA_WEB_DNS_PIN_TTL_SEC`, `AURA_WEB_CACHE_PERSISTENT`.

**MCP servers (Aura is both client and server):**
- Client runtime: `internal/mcp/` (`manager/runtime.go`, `manager/catalog.go`, `manager/config.go`, `probe.go`, `bounded_call.go`, `egress_policy.go`); per-server credentials in `internal/mcp/mcpenv/` under `AURA_MCP_ENV_DIR`. Registry persistence: `internal/mcpregistry/store.go`.
- First-party recipes are recognised byte-for-byte by `internal/mcp/manager/first_party.go`; their URLs are computed in `internal/mcp/manager/catalog.go` and switch on `AURA_IN_CONTAINER`:
  - Memory — `http://aura-arcadedb-mcp:8096/mcp/` in-container, `http://127.0.0.1:${AURA_ARCADEDB_MCP_PORT:-8096}/mcp/` otherwise
  - WhatsApp — `http://whatsapp:8080/mcp/` / `http://127.0.0.1:${AURA_WHATSAPP_MCP_PORT:-8092}/mcp/`
  - Calendar/PIM — `PIMSidecarBaseURL()` + `/`, i.e. `http://aura-pim-mcp:8080` / `http://127.0.0.1:${AURA_PIM_MCP_PORT:-8093}`
- Server side: `cmd/arcadedb-mcp/` — the memory MCP Aura writes for herself (`main.go`, `auth.go`, `tenant.go`, `tool_memory.go`, `tool_memory_graph.go`, `tool_memory_recall.go`, `tool_memory_batch.go`, `tool_memory_maintenance.go`, `tool_forget.go`, `tool_browse.go`, `tool_graph_schema.go`). Env `AURA_ARCADEDB_MCP_HOST`, `AURA_ARCADEDB_MCP_PORT`, `AURA_ARCADEDB_MCP_BODY_MAX_BYTES`, `AURA_AGENT_MEMORY_MCP_PORT`, `AURA_AGENT_MEMORY_MCP_AUTH_SECRET`.
- Timeouts/guards: `AURA_MCP_CALL_TIMEOUT_SEC`, `AURA_MCP_PROBE_TIMEOUT`, `AURA_MCP_MOUNT_TIMEOUT`, `AURA_MCP_MOUNT_RETRY_ATTEMPTS`, `AURA_MCP_SHUTDOWN_TIMEOUT`, `AURA_MCP_SSRF_ENFORCE`, `AURA_MCP_ELICITATION_TIMEOUT_SEC`, `AURA_MCP_SANDBOX_ORIGIN`.

**Calendar / Email / Contacts (PIM):**
- Sidecar `aura-pim-mcp` (`ghcr.io/chetto1983/aura-pim-mcp:latest`, a .NET `CalendarMcp.HttpServer`) exposes both an MCP endpoint and a token-gated `/admin` REST API.
- Aura proxies the admin API for the cockpit "Connect account" flow: `internal/agui/connect_pim_api.go` — routes `GET|POST /api/connect/pim/accounts`, `DELETE /api/connect/pim/accounts/{id}`, `.../status`, `.../google/start`, `.../logout`, `.../auth/start`, `.../auth/status`, `.../auth/cancel`. Absent sidecar degrades to 503.
- Providers reached through it: Microsoft 365 / Outlook.com (device-code flow, `pimDeviceStartTimeout = 35s`), Google Workspace (web OAuth redirect `<base>/admin/auth/google/callback`), IMAP+SMTP, iCalendar URL, JSON file.
- Config: `AURA_PIM_MCP_URL` (default `http://aura-pim-mcp:8080`, `internal/config/config.go:550`), `AURA_PIM_MCP_PORT`, `AURA_PIM_EXTERNAL_BASE_URL`, `AURA_PIM_MCP_OAUTH_RESOURCE`, `AURA_PIM_MCP_TRUSTED_ISSUERS`, sidecar-side `CALENDAR_MCP_*`.
- Live tier: `internal/mcp/calendar_integration_test.go`; CI job `calendar-integration-test` in `.github/workflows/ci.yml`.

**Messaging channels:**
- Telegram — `internal/channels/telegram/` (`bot.go`, `bot_dispatch*.go`, `deliver.go`, `voice.go`, `tts.go`, `mdv2.go`, `status_pane.go`, `hitl.go`, `onboarding.go`) on `gopkg.in/telebot.v4`. Env `TELEGRAM_BOT_TOKEN`, `TELEGRAM_API_BASE_URL`, `TELEGRAM_FILE_BASE_URL`, `AURA_TELEGRAM_LOCAL_BOT_API`, `AURA_CHANNEL_TELEGRAM_ENABLED`, throttles `AURA_TELEGRAM_STATUS_THROTTLE_MS` / `AURA_TELEGRAM_CONTENT_THROTTLE_MS` / `AURA_TELEGRAM_CHAT_RATE_LIMIT_MS`.
- WhatsApp — sidecar `whatsapp` (`ghcr.io/chetto1983/whatsapp-mcp:latest`) reached as an MCP server plus a bridge. Env `AURA_WHATSAPP_MCP_URL`, `AURA_WHATSAPP_MCP_PORT`, `AURA_WHATSAPP_BRIDGE_URL`, `AURA_WHATSAPP_BRIDGE_PORT`, `AURA_WHATSAPP_BRIDGE_TOKEN`, `AURA_WHATSAPP_GATEWAY_URL`, `AURA_WHATSAPP_STORE_ROOT`, `AURA_MCP_WHATSAPP_BRIDGE_URL`. Pairing QR via `qrterminal`.
- Channel registry/dispatch: `internal/channels/registry.go`, `internal/channels/deliver.go`, enable flags `AURA_CHANNEL_<NAME>_ENABLED`.
- Cockpit (AG-UI over SSE): `internal/agui/` + the embedded SPA. Bind `AURA_AGUI_BIND`, run controls `AURA_AGUI_RUN_*`, `AURA_AGUI_SSE_HEARTBEAT_SEC`, `AURA_AGUI_BUFFER_CAP`.

**Container runtime (Docker as a product dependency):**
- Per-identity sandboxes: `internal/sandbox/usersandbox/` — `docker_backend.go`, `docker_backend_exec.go`, `docker_backend_lifecycle.go`, `materialize.go`, `egress.go`, `reap.go`, `router.go`, `spec.go` on `github.com/moby/moby/client`.
- The daemon does not touch the raw socket: `compose.yaml` service `docker-socket-proxy` (`tecnativa/docker-socket-proxy:v0.5.0`, profile `sandbox`) mediates.
- Egress control image built from `docker/aura-egress/`; box image from `docker/aura-sandbox/` (`make sandbox-images`, `make sandbox-image-contract`).
- Env: `AURA_SANDBOX_IMAGE`, `AURA_SANDBOX_EGRESS_IMAGE`, `AURA_SANDBOX_EGRESS_ALLOWLIST`, `AURA_SANDBOX_CPU_LIMIT`, `AURA_SANDBOX_MEMORY_LIMIT`, `AURA_SANDBOX_PIDS_LIMIT`, `AURA_SANDBOX_IDLE_TTL_SEC`, `AURA_SANDBOX_AGENT_URL`, `AURA_SANDBOX_AGENT_TOKEN`, `AURA_SANDBOX_AGENT_TIMEOUT_SEC`, `AURA_EGRESS_ENFORCE`, `AURA_EGRESS_FLOOR_RULESET`.
- CI job `sandbox-docker-integration`; local `make coverage-docker`.

## Data Storage

**Databases:**
- **Postgres 18.4** (`compose.yaml` service `postgres`, image `postgres:18.4-alpine3.24`, port 5432) — control plane, documents, catalogue, schema `aura.*`.
  - Connection: `AURA_DB_URL` (app role), `AURA_DB_MIGRATE_URL` (migrate role), `AURA_DB_BOOTSTRAP_URL`; composed by `internal/config/config.go` from `POSTGRES_HOST`/`POSTGRES_PORT`/`POSTGRES_USER`/`POSTGRES_PASSWORD`/`POSTGRES_DB`/`POSTGRES_SSLMODE`. Roles named by `AURA_DB_APP_ROLE`, `AURA_DB_MIGRATE_ROLE`.
  - Client: pgx/v5 pool + sqlc-generated `internal/db/sqlc/`; queries in `internal/db/queries/`.
  - Migrations: `internal/db/migrations/` (latest `0119_drop_orphan_content_parts`), run by golang-migrate via `aura db migrate` (`cmd/aura/db.go`) and the `aura-migrate` Compose one-shot. **The next migration number is `ls internal/db/migrations/ | tail -1` + 1, never a number copied from a doc.**
- **ArcadeDB 26.9.1-SNAPSHOT** (digest-pinned; `VerifySecureVersion` refuses < 26.4.2 for CVE-2026-44221) — long-term memory, **one database per identity**, tenant credential derived by HMAC over `AURA_ARCADEDB_TENANT_SECRET`.
  - Client: `internal/arcadedb/` (`memory.go`, `memory_graph.go`, `memory_graph_temporal.go`, `memory_mentions*.go`, `transaction.go`, …). Bitemporal facts (`valid_from`/`valid_to` + supersede), native vector + full-text index.
  - Env: `AURA_ARCADEDB_URL`, `AURA_ARCADEDB_DATABASE`, `AURA_ARCADEDB_ADMIN_USER`, `AURA_ARCADEDB_ADMIN_PASSWORD`, `AURA_ARCADEDB_TENANT_SECRET`, sidecar-side `ARCADEDB_PASSWORD`, `ARCADEDB_APP_USER`, `ARCADEDB_APP_PASSWORD`, `ARCADEDB_DATABASE`.
  - Tiers: `make arcadedb-integration`, CI `arcadedb-integration-test`, `make agent-memory-eval`.
- SQLite (`github.com/mattn/go-sqlite3`) — local/sidecar state only.

**File Storage:**
- **Garage S3** (`dxflrs/garage:v2.3.0`, Compose services `garage` + `garage-bootstrap`) — the object store.
  - Client: `internal/objectstore/s3.go`, `s3_seekable.go`, `identity_store.go`, `asset_placement.go`; admin API wrapper `internal/objectstore/garageadmin/`; filesystem fallback `internal/objectstore/filesystem.go`.
  - Env: `AURA_OBJECTSTORE_BACKEND`, `AURA_OBJECTSTORE_ENDPOINT`, `AURA_OBJECTSTORE_PUBLIC_ENDPOINT`, `AURA_OBJECTSTORE_BUCKET`, `AURA_OBJECTSTORE_REGION`, `AURA_OBJECTSTORE_ACCESS_KEY`, `AURA_OBJECTSTORE_SECRET_KEY`, `AURA_OBJECTSTORE_PATH_STYLE`, `AURA_OBJECTSTORE_REPLICATION_FACTOR`, `AURA_GARAGE_ADMIN_ENDPOINT`, `AURA_GARAGE_ADMIN_TOKEN`, `AURA_GARAGE_KEY_NAME`, `AURA_GARAGE_ZONE`, `AURA_GARAGE_CAPACITY`.
- Local filesystem artifacts: `$AURA_RUN_DIR` (tool sidecar results + spillover), `~/.aura/agents/<id>/`, `$AURA_SKILLS_DIR`, `$AURA_SKILLS_IDENTITY_DIR`, `$AURA_MCP_ENV_DIR`, `$AURA_WORKSPACE_DIR`, `$AURA_BACKUP_DIR`.

**Ingestion pipeline:**
- Sidecar `aura-ingest` (`docker/aura-ingest/Dockerfile`) — CocoIndex 1.0.20 reconciles the Garage bucket, `iscc-tika` extracts, LibreOffice normalises the rest, and Go binaries `cmd/aura-filecard` + `cmd/aura-media-index` describe. Supervised host-side by `cmd/aura-ingest-supervisor` / `internal/ingestsupervisor/`.
- Env: `AURA_INGEST_S3_ENDPOINT`, `AURA_INGEST_S3_BUCKET`, `AURA_INGEST_S3_REGION`, `AURA_INGEST_S3_ACCESS_KEY_ID`, `AURA_INGEST_S3_SECRET_ACCESS_KEY`, `AURA_INGEST_S3_PREFIX`, `AURA_INGEST_STATE_ROOT`, `AURA_INGEST_IDENTITY_ID`, `AURA_INGEST_LIVE`, `AURA_INGEST_INTERVAL_SEC`, `AURA_INGEST_SUPERVISOR_INTERVAL`.
- Gates: `make ingest-image`, `make ingest-test`, `make extractor-matrix`, `make ingest-reconcile`; CI job `ingest-sidecar-test`.

**Caching:**
- No Redis/Memcached service. In-process caches only: `internal/agent` prompt/LLM caching (`cmd/aura/cache.go`, `cache_stats.go`, `internal/cachemetrics/`) and the web-fetch cache in `internal/web/cache.go`. `redis`/`nats`/`kafka` in `go.sum` are transitive Watermill deps of Authula, not wired here.

## Authentication & Identity

**Cockpit / operator auth:**
- **Authula** v1.43.0 embedded — `internal/webauth/authula.go`, `internal/webauth/session_validate.go`, `internal/webauth/identity_link.go`. Email + password + TOTP.
- Env: `AURA_WEB_AUTH_PROVIDER`, `AURA_WEB_AUTH_SECRET`, `AURA_AUTHULA_DSN`, `AURA_AUTHULA_DATABASE_URL`, `AURA_AUTHULA_SECRET`, `AURA_AUTHULA_RATE_LIMIT_MAX`, `AURA_AUTHULA_OPERATOR_IDENTITY`, `AURA_AUTHULA_OPERATOR_EMAIL`, `AURA_AUTHULA_OPERATOR_PASSWORD`, `AURA_AUTHULA_OPERATOR_TOTP_SECRET`.
- Cookie/session handling: `internal/agui/auth.go`, `auth_cookie.go`, `auth_validator_test.go`.

**MCP OAuth (Aura is an authorization server for her own sidecars):**
- `internal/webauth/mcp_oauth_server.go`, `mcp_oauth_first_party.go`, `mcp_oauth_handlers.go`, `mcp_live_token_issuer.go`, `mcp_token_plugin.go`; client side `internal/mcpoauth/` and `internal/mcp/oauth_*.go` (`oauth_flows.go`, `oauth_tokensource.go`, `oauth_contract.go`).
- Discovery at `/.well-known/oauth-authorization-server` on port 9080; RFC 8707 resource-bound tokens (audiences configured per sidecar, see `CALENDAR_MCP_OAuth__Resource` in `compose.yaml`). JWT/JWKS via `lestrrat-go/jwx/v3`.

**Other credential surfaces:**
- Service/bootstrap tokens: `AURA_ACCESS_TOKEN`, `AURA_SERVICE_TOKEN`, `AURA_SETUP_TOKEN` (first-boot wizard, `internal/setup/`), `AURA_RECOVERY_QUESTION` / `AURA_RECOVERY_ANSWER` / `AURA_RECOVERY_PASSWORD`, break-glass path `internal/breakglass/`.
- Secret storage/redaction: `internal/secret/`, `internal/redact/`; trace encryption `AURA_TRACE_ENCRYPT_KEY`.
- Identity model: `internal/identity/`, `internal/identityctx/`, `internal/idroot/`; per-identity Postgres RLS (`internal/gateway/rls_seed_test.go`) and per-identity ArcadeDB databases.
- Human-in-the-loop authorization: `internal/gateway/` (classify → reserve → approve → decide), `internal/approvalgrants/`, `internal/skillacl/`.

## Monitoring & Observability

**Tracing:**
- OpenTelemetry SDK, OTLP/gRPC exporter to **Grafana Tempo** (`compose.yaml` service `tempo`, profile `observability`). Wiring: `internal/obs/init.go`, `tracer.go`, `boundary.go`. Env `AURA_OTEL_EXPORTER`, `AURA_OTEL_ENDPOINT`.
- Reasoning traces: `internal/reasoningtrace/`, `internal/tracesink/`; `AURA_REASONING_TRACE`, `AURA_REASONING_TRACE_FILE`, `AURA_REASONING_TRACE_MAX_BYTES`.

**Metrics:**
- Prometheus client + OTel Prometheus exporter (`internal/obs/meter.go`, `internal/obs/catalog.go`), scraped by **Prometheus v3.13.1** (`compose.yaml`, profile `observability`, config under `observability/`). Bind `AURA_METRICS_BIND`.
- **Grafana 12.3.9** for dashboards. Sidecar health probe `internal/obs/sidecar_check.go`, `AURA_OBSERVABILITY_CHECK_ENABLED`; gates `make observability-check`, `make observability-evidence`, CI job `observability-contract`.

**Error tracking:**
- No third-party error service. Structured `log/slog` (`lmittmann/tint` for terminal output) plus the OTel error handler `internal/obs/otel_error_handler.go`.

## CI/CD & Deployment

**Hosting:**
- Self-hosted single-host appliance via Docker Compose (`compose.yaml`), fronted by **Caddy** (`compose.yaml` service `caddy`, image built from `docker/caddy/`) for TLS and public routing. `AURA_PUBLIC_HOST`, `AURA_VIEWS_HOST`, `AURA_HTTPS_PORT`, `AURA_CADDYFILE`, `AURA_WEB_PUBLIC_URL`, `AURA_WEB_TRUST_PROXY`.
- DNS-01 certificate issuance uses deSEC: `DESEC_TOKEN`.
- systemd units in `deploy/`: `aura.service`, `aura-scheduler.service`, `aura-image-update.{service,timer,sh}` (pulls `ghcr.io/chetto1983/aura:edge` when `AURA_PULL_POLICY=always`).
- Installer: `scripts/install.sh` packed by `make installer-artifact`, npx veneer `scripts/create-aura.mjs` / `packages/create-aura`.

**CI Pipeline (GitHub Actions, `.github/workflows/`):**
- `ci.yml` — 22+ jobs including tiered integration suites (db, arcadedb, knowledge, ingest, multimodal, telegram, whatsapp, calendar, sandbox-docker, race-db) and the frontend gates.
- `codeql.yml` (SAST), `production-readiness.yml`, `skills.yml`, `release.yml` (GoReleaser on `v*` tags), `publish-aura-edge.yml`, `publish-arcadedb-mcp.yml`, `retire-aura-images.yml`, `create-aura-appliance.yml`, `claude.yml`.
- Registry: GitHub Container Registry (`ghcr.io/chetto1983/*`).

## Environment Configuration

**Required env vars (minimum viable stack):**
- Postgres: `POSTGRES_PASSWORD` (+ `POSTGRES_USER`, `POSTGRES_DB`, `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_SSLMODE`); tests read the composed `AURA_DB_URL` / `AURA_DB_MIGRATE_URL`.
- ArcadeDB: `ARCADEDB_PASSWORD`, `ARCADEDB_APP_USER`, `ARCADEDB_APP_PASSWORD`, `AURA_ARCADEDB_TENANT_SECRET`.
- LLM: `OPENROUTER_API_KEY` (or a local `AURA_LLM_BASE_URL` + `AURA_LLM_PROVIDER=llamacpp`).
- Embeddings: `AURA_EMBED_BASE_URL`, `AURA_EMBED_MODEL_PATH`, `AURA_EMBED_DIMENSIONS`.
- Object store: `AURA_OBJECTSTORE_*` + `AURA_GARAGE_ADMIN_TOKEN`.
- Cockpit auth: `AURA_WEB_AUTH_SECRET`, `AURA_AUTHULA_SECRET`, `AURA_ACCESS_TOKEN`.
- Channels (optional): `TELEGRAM_BOT_TOKEN`, `AURA_WHATSAPP_BRIDGE_TOKEN`.

**Secrets location:**
- `.env` at the repo root (git-ignored; `.env.example` is the 635-line committed catalog). Compose interpolates it; the daemon loads it via `godotenv`.
- Per-MCP-server credentials live as files under `$AURA_MCP_ENV_DIR` (`internal/mcp/mcpenv/`), never in the process environment of unrelated servers.
- CI secrets are GitHub Actions secrets; integration jobs export the composed DSNs so `t.Skip` cannot fire under `$CI`.

## Webhooks & Callbacks

**Incoming:**
- Telegram — long-polling via telebot, not a webhook (`internal/channels/telegram/bot.go`). `TELEGRAM_API_BASE_URL` / `AURA_TELEGRAM_LOCAL_BOT_API` allow a local Bot API server.
- OAuth callbacks — Google redirect `<AURA_PIM_EXTERNAL_BASE_URL>/admin/auth/google/callback` handled by the PIM sidecar; Aura's own MCP OAuth redirect handlers in `internal/webauth/mcp_oauth_handlers.go`.
- AG-UI HTTP + SSE surface on `AURA_AGUI_BIND` (`internal/agui/`); first-boot setup server on `AURA_SETUP_BIND`.
- Sandbox agent callback endpoint: `AURA_SANDBOX_AGENT_URL` + `AURA_SANDBOX_AGENT_TOKEN`.

**Outgoing:**
- Scheduler notifications through the channel registry (`internal/channels/deliver.go`), addressed by `AURA_SCHEDULER_NOTIFY_RECIPIENT`, retried per `AURA_SCHEDULER_NOTIFY_RETRY_ATTEMPTS`, quiet hours `AURA_SCHEDULER_QUIET_HOURS`, timezone `AURA_SCHEDULER_TZ`.
- Share links to the object store: `internal/share/`, `AURA_SHARE_PUBLIC_ENABLED`, `AURA_SHARE_MAX_EXPIRY_DAYS`, presigned URLs with `AURA_ASSET_PRESIGN_TTL_SEC`.
- No generic outbound webhook registry.

---

*Integration audit: 2026-09-07*
