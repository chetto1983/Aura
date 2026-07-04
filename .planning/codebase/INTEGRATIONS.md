# External Integrations

**Analysis Date:** 2026-07-04

## APIs & External Services

**LLM inference:**
- OpenRouter (cloud, default) — `internal/llm/config.go`: `defaultProvider = "openrouter"`, `defaultBaseURL = "https://openrouter.ai/api/v1"`, `defaultModel = "deepseek/deepseek-v4-flash:nitro"`
  - Auth: `OPENROUTER_API_KEY` env var (canonical third-party name, not `AURA_`-prefixed)
  - Client: hand-rolled OpenAI-compatible streaming client, `internal/llm/openai_compat/client.go` (SSE parsing `sse.go`, idle-stream watchdog `stream_idle.go`, HTTP error mapping `httperror.go`)
  - Attribution headers sent on every request: `HTTP-Referer`, `X-Title` (D-20, OpenRouter dashboard visibility)
  - Also used as the shared cloud fallback endpoint for rerank (`cohere/rerank-4-fast`), vision (`minimax/minimax-m3`), STT (e.g. `openai/whisper-large-v3`), TTS (e.g. `hexgrad/kokoro-82m`) — one `OPENROUTER_API_KEY` authenticates all cloud backends
- Local llama.cpp server (self-hosted alternative, `compose.llm.yaml` `aura-llm` service, image `ghcr.io/ggml-org/llama.cpp:server-cuda`) — swapped in via `AURA_LLM_BASE_URL`
- Local embedding sidecar `aura-llama-embed` (`compose.yaml`, same llama.cpp server-cuda image) — `AURA_EMBED_BASE_URL` (default `http://127.0.0.1:8081`), `AURA_EMBED_MODEL`, `AURA_EMBED_DIMENSIONS` (`internal/knowledge/config.go`)
- Local rerank sidecar `aura-rerank` (`compose.yaml`, llama.cpp server-cuda) — `internal/rerank/client.go`; `AURA_RERANK_BASE_URL` (default `http://127.0.0.1:8085`); `AURA_RERANK_MODEL` set to a cloud model id swaps the same client to OpenRouter (fails soft to RRF/vector order if unreachable)
- Local vision/OCR sidecar `aura-ocr-vl` (`compose.yaml`, llama.cpp server-cuda) — `MULTIMODAL_BASE_URL`/`MULTIMODAL_MODEL`; `AURA_VISION_CLOUD=true` routes instead to OpenRouter `minimax/minimax-m3` (`internal/multimodal/vision.go`)
- Local STT sidecar `aura-stt` (`compose.yaml`, image `hwdsl2/whisper-server`) — `STT_BASE_URL`/`STT_MODEL`/`STT_LANGUAGE` (default `it`); `AURA_STT_CLOUD_MODEL` swaps to OpenRouter (`internal/multimodal/stt.go`)
- Local TTS sidecar `aura-tts` (`compose.yaml`, image `ghcr.io/remsky/kokoro-fastapi-cpu`) — `TTS_BASE_URL`/`TTS_VOICE` (default `if_sara`)/`TTS_FORMAT` (default `opus`); `AURA_TTS_MODEL` swaps to OpenRouter (`internal/multimodal/tts.go`)

**Search:**
- SearXNG (self-hosted meta-search, `compose.yaml` `searxng` service, image `searxng/searxng:2026.5.31-7159b8aed`) — `web_search`/`web_fetch` tool backend, `SEARXNG_URL` (upstream-canonical name, no `AURA_` prefix); empty is not boot-fatal, fails closed at call time with `web_search_unavailable{searxng_not_configured}` (`internal/web/`, `internal/config/config.go`)

**Document conversion:**
- Markitdown sidecar (`compose.yaml` `markitdown` service, image `aura-markitdown:local`) — `/convert` HTTP endpoint, `DOCUMENTS_BASE_URL` (`internal/documents/`)

**Messaging channels:**
- Telegram Bot API — `gopkg.in/telebot.v4`, `internal/channels/telegram/` (bot dispatch, media, HITL approvals, TTS voice notes, status pane, onboarding). Auth: `TELEGRAM_BOT_TOKEN` (canonical upstream name). Optional local Bot API server override: `TELEGRAM_API_BASE_URL` / `TELEGRAM_FILE_BASE_URL`, gated by `AURA_TELEGRAM_LOCAL_BOT_API`
- WhatsApp bridge sidecar (`compose.yaml` `whatsapp` service, image `ghcr.io/chetto1983/whatsapp-mcp:sidecar`) — reached via MCP (`internal/mcp/whatsapp_integration_test.go`) and via a management REST API proxied from the cockpit (`internal/agui/connect_api.go`, `AURA_WHATSAPP_BRIDGE_URL`, default `http://whatsapp:8081`); connect routes answer 503 if unset/unreachable (non-fatal)

**Calendar / PIM:**
- `aura-pim-mcp` sidecar (`compose.yaml`, image `ghcr.io/chetto1983/aura-pim-mcp:sidecar`) — Google Calendar integration via MCP; admin REST proxied at `/api/connect/pim/*` (`internal/agui/connect_pim_api.go`), `AURA_PIM_MCP_URL` (default `http://aura-pim-mcp:8080`) + `AURA_PIM_MCP_ADMIN_TOKEN` bearer token (never returned to the client)

**MCP (Model Context Protocol) servers:**
- Generic MCP client/transport: `internal/mcp/client.go`, `http_client.go`, `transport.go` (stdio + streamable HTTP), with SSRF guarding (`ssrf.go`, `transport_ssrf.go`) and secret redaction (`redact.go`)
- Managed server registry: `internal/mcp/managed_config.go` — Claude-Desktop-compatible `mcpServers` JSON shape, extended with Aura metadata (`enabled`, `source`, `trust` class: `trusted_recipe`/`trusted_local`/`sandboxed_local`/`remote_http`/`blocked`, `runtime` kind: `local`/`docker`/`docker_gateway`)
- `mcp-neo4j-cypher` — the LLM-facing interface to the Neo4j graph (get-schema/read-cypher/write-cypher/list-gds-procedures); no native Go driver call path is exposed to the LLM (CLAUDE.md architectural constraint)
- `aura-agent-memory-mcp` sidecar (`compose.yaml`, built from `docker/agent-memory/`, a Python `neo4j_agent_memory` package) — agent long-term memory subgraph over Neo4j, with pluggable embedding backends (OpenAI, Bedrock, sentence-transformers, Vertex AI — `docker/agent-memory/src/neo4j_agent_memory/embeddings/`) and entity extraction (GLiNER, spaCy, LLM-based — `.../extraction/`)
- Calculator, calendar, WhatsApp MCP integrations exercised via integration tests: `internal/mcp/calculator_integration_test.go`, `calendar_integration_test.go`, `whatsapp_integration_test.go`

**Cloud storage (fallback/optional):**
- AWS SDK v2 present (`github.com/aws/aws-sdk-go-v2` + `s3`, `credentials`, `config`) — used as the Go client library against the self-hosted S3-compatible object store (Garage), not necessarily AWS itself; see Data Storage below

## Data Storage

**Databases:**
- PostgreSQL 18.4-alpine (`compose.yaml` `postgres` service, image `postgres:18.4-alpine3.23`) — primary relational store, schema `aura.*`
  - Connection: composed from `POSTGRES_HOST`/`POSTGRES_PORT`/`POSTGRES_USER`/`POSTGRES_PASSWORD`/`POSTGRES_DB`/`POSTGRES_SSLMODE` primitives into role-scoped DSNs (`aura_app` runtime role, `aura_migrate` DDL role), or overridden wholesale via `AURA_DB_URL`/`AURA_DB_MIGRATE_URL`/`AURA_DB_BOOTSTRAP_URL` (`internal/config/config.go` `composeDSN`)
  - Client: `github.com/jackc/pgx/v5` + `sqlc`-generated typed queries (`internal/db/sqlc/`), migrations via `golang-migrate/migrate/v4` (`internal/db/migrations/0001..0020+*.sql`)
  - Also hosts a second, isolated schema `authula` for the embedded Authula auth framework (its own `uptrace/bun`-based migrator fills the contents; Aura migration `0019_authula_schema` only creates the schema/role boundary)
- Neo4j 5.26.26 Community + APOC + GDS (`compose.yaml` `neo4j` service, image `neo4j:5.26.26-community`) — knowledge graph + HNSW vector index (768d cosine)
  - Connection: `AURA_NEO4J_BOLT_URL` (default `bolt://127.0.0.1:7687`), `NEO4J_USER`, `NEO4J_PASSWORD`, `AURA_NEO4J_DATABASE` (`internal/knowledge/config.go`)
  - Client: `github.com/neo4j/neo4j-go-driver/v5` (native driver, `internal/knowledge/client.go`) for schema/migration/status operations (`internal/knowledge/migrate.go`, `migrations/*.cypher`, `ping.go`, `status.go`); the LLM itself talks to the graph only through the `mcp-neo4j-cypher` MCP server, never the native driver directly

**File Storage:**
- Garage (self-hosted S3-compatible object store, `compose.yaml` `garage` service, image `dxflrs/garage:v2.0.0`) — asset bucket storage (`internal/objectstore/s3.go`, uses AWS SDK v2 S3 client against Garage's S3-compatible endpoint)
  - Config: `AURA_OBJECTSTORE_BACKEND` (`garage`|`filesystem-dev`|`fake`), `AURA_OBJECTSTORE_ENDPOINT`/`AURA_OBJECTSTORE_PUBLIC_ENDPOINT`, `AURA_OBJECTSTORE_REGION`, `AURA_OBJECTSTORE_BUCKET`, `AURA_OBJECTSTORE_ACCESS_KEY`/`AURA_OBJECTSTORE_SECRET_KEY`, `AURA_OBJECTSTORE_PATH_STYLE` (default true), `AURA_OBJECTSTORE_REPLICATION_FACTOR`, `GARAGE_RPC_SECRET` (inter-node RPC secret, upstream canonical name)
  - Alternatives: `internal/objectstore/filesystem.go` (dev-local filesystem backend), `internal/objectstore/fake.go` (test double)
- Local filesystem sidecar artifacts: `$AURA_RUN_DIR/` (tool-result spillover + sidecar content, default `<user-cache-dir>/aura`), `~/.aura/agents/<id>/` (Agent.md profile), `~/.aura/pyscripts/<id>/` (Slice 7e Python snippets), `$AURA_SKILLS_DIR/` (skill instruction trees)

**Caching:**
- In-memory web-fetch/search cache (`internal/web/`), optionally disk-persistent via `AURA_WEB_CACHE_PERSISTENT` (default false)
- Cache metrics tracked in Postgres (`internal/cachemetrics/`, migration `0007_cache_metrics`)
- No Redis/Memcached — no external cache service

## Authentication & Identity

**Auth Provider:**
- Authula (`github.com/Authula/authula` v1.11.0, Apache-2.0) — self-hosted, embedded Go auth framework, not a hosted SaaS provider
  - Implementation: `internal/webauth/authula.go` embeds Authula's `config`, `models`, and plugins (`csrf`, `email-password`, `rate-limit`, `session`, `totp`) and its `services` package; runs against its own isolated `authula` Postgres schema (H1 schema isolation — no table-prefix support upstream)
  - Session cookie: `__Host-`-prefixed hardened cookie (`SessionCookieName`), `Secure=true`/`SameSite` flipped from Authula's insecure defaults
  - Config: `AURA_WEB_AUTH_PROVIDER` (default `authula`), `AURA_AUTHULA_DATABASE_URL` (falls back to `AURA_DB_URL` + `?search_path=authula`), `AURA_AUTHULA_SECRET` (32-byte hex HMAC/token key seed, required for the provider to activate), `AURA_AUTHULA_OPERATOR_IDENTITY`, `AURA_AUTHULA_RATE_LIMIT_MAX` (default 30/min)
  - Legacy fallback: `AURA_WEB_AUTH_SECRET` (deprecated passphrase secret, retained only so old configs load; not the active auth path)
- Boot-time bind guard: `config.GuardWebBind` (`internal/config/config.go`) — a non-loopback `AURA_AGUI_BIND` requires either Authula configured or `AURA_WEB_TRUST_PROXY=true` (operator vouches a reverse proxy terminates auth); loopback always boots unauthenticated

**Capability/Authorization:**
- `internal/identity/` + `internal/identityctx/` — Aura-native identity + capability-grant model (`aura identity <list|get|grant|revoke>` CLI), migration `0004_identity`
- `internal/gateway/` — a policy/approval gateway (classify/decide/approve/reserve/reconcile) gating tool-call side effects, independent of Authula (which only guards the cockpit web session)

## Monitoring & Observability

**Error Tracking:**
- None (no Sentry/Bugsnag/etc.) — errors surface via structured logs (`slog`) and OpenTelemetry spans

**Tracing:**
- OpenTelemetry (`go.opentelemetry.io/otel` v1.44.0) — `internal/obs/init.go`, `otel_error_handler.go`; exporter selectable via `AURA_OTEL_EXPORTER` (`stdout`|`otlp`|`none`, default `otlp`) and `AURA_OTEL_ENDPOINT` (default `localhost:4317`, OTLP/gRPC)
- Agent-level tracing spans: `internal/agent/tracing.go`

**Metrics:**
- Prometheus client (`github.com/prometheus/client_golang`) — `internal/agent/metrics.go`, `internal/agui/metrics.go`, `internal/cachemetrics/`
- Reasoning/tool-selection telemetry stored in Postgres: `internal/reasoningtrace/`, `internal/toolinvocations/`, `internal/toolselectstore/` (migrations `0010_skill_audit`, `0011_tool_invocations`)

**Logs:**
- `log/slog` structured logging throughout (per CLAUDE.md skill guidance); no external log-aggregation sidecar wired into `compose.yaml`

## CI/CD & Deployment

**Hosting:**
- Self-hosted / operator-deployed via Docker Compose (`compose.yaml` + profile overlays) or systemd (`deploy/aura.service`, `deploy/aura-scheduler.service`); container images published to GitHub Container Registry (`ghcr.io/chetto1983/aura`)

**CI Pipeline:**
- GitHub Actions (`.github/workflows/`):
  - `ci.yml` — build+vet+lint+deadcode+file-size gate, unit tests with `-race`, (additional jobs likely cover coverage/integration — see full file for the complete job matrix)
  - `codeql.yml` — CodeQL SAST scanning
  - `release.yml` — goreleaser-driven release + multi-arch Docker image publish on `v*` tags
  - `skills.yml` — CI for the `.claude/skills/` skill set
- CI placeholder secrets are injected as non-secret values purely to satisfy `compose.yaml` variable interpolation in jobs that touch compose (`AURA_ACCESS_TOKEN`, `AURA_AUTHULA_SECRET`, `AURA_PIM_MCP_ADMIN_TOKEN`, `SEARXNG_SECRET`, `AURA_OBJECTSTORE_ACCESS_KEY`/`SECRET_KEY`, `GARAGE_RPC_SECRET`) — none of these are live credentials

## Environment Configuration

**Required env vars (fail-fast if absent/misconfigured for the relevant path):**
- `OPENROUTER_API_KEY` — required for any LLM call path (`aura chat`/`serve`); `LoadDB`/`LoadAllowEmptyKey` paths (DB migration, setup) tolerate it being empty
- `POSTGRES_PASSWORD` — required to compose a working Postgres DSN (an empty password yields an empty DSN, causing downstream connection failure)
- `AURA_AUTHULA_SECRET` — required for the Authula web-auth provider to activate; also gates non-loopback `AURA_AGUI_BIND` (`GuardWebBind`)
- `TELEGRAM_BOT_TOKEN` — required for the Telegram channel to start (channel is independently enable/disable-able; absence does not block other channels/boot)

**Non-fatal / optional (silent fallback to documented defaults):**
- The large majority of the ~60-entry `AURA_<DOMAIN>_<UNIT>` catalog (swarm limits, web-fetch timeouts, skill caps, AG-UI bind/CORS, asset size ceilings, scheduler knobs, multimodal sidecar URLs) — see `internal/config/config.go` `loadBase()` for the authoritative default table
- `SEARXNG_URL`, `AURA_WHATSAPP_BRIDGE_URL`, `AURA_PIM_MCP_URL`, `AURA_RERANK_BASE_URL`, `MULTIMODAL_BASE_URL`/`STT_BASE_URL`/`TTS_BASE_URL` — sidecar endpoints; absence/unreachability degrades the corresponding feature to a fail-closed 503/unavailable response at call time, never a boot failure

**Secrets location:**
- `.env` (git-ignored, present in this repo at root — existence noted only, contents not read per this agent's policy) loaded best-effort via `godotenv.Load()` at every boot path (`config.loadBase`, `llm.load`, `cmd/aura` main)
- `.env.example` — git-tracked template documenting all ~60 catalogued env vars without real values
- `~/.aura/llm.json` — optional per-user LLM secret/config override (tier 3 of the LLM config load order)

## Webhooks & Callbacks

**Incoming:**
- Telegram: long-polling (via `telebot.v4`), not webhook-based, per the channel implementation in `internal/channels/telegram/bot.go`
- AG-UI gateway HTTP server (`internal/agui/server.go`) — the cockpit frontend's SSE/event stream endpoint (`internal/agui/fanout.go`), bound at `AURA_AGUI_BIND` (default `127.0.0.1:9080`)
- Setup wizard HTTP server (`internal/setup/`, `AURA_SETUP_BIND` default `127.0.0.1:9081`) — first-boot device-linking flow, gated by `AURA_SETUP_TOKEN`

**Outgoing:**
- Outbound HTTP calls to every sidecar/cloud endpoint listed above (LLM, embed, rerank, vision, STT, TTS, SearXNG, Markitdown, WhatsApp bridge, PIM MCP) — all guarded by an SSRF check (`internal/mcp/ssrf.go`, `transport_ssrf.go`) where the target is MCP-mediated
- Telegram outbound messages/media/voice notes (`internal/channels/telegram/deliver.go`)
- Scheduled/cron-triggered notifications (`internal/cron/`) routed back to the originating channel (`AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL`, default true)

---

*Integration audit: 2026-07-04*
