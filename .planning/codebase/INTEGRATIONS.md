# External Integrations

**Analysis Date:** 2026-08-02

## APIs & External Services

**AI Inference & Models:**
- OpenRouter - Default hosted OpenAI-compatible LLM, model catalogue/pricing source, and optional cloud route for embeddings, vision, STT, and TTS.
  - SDK/Client: Aura's typed `net/http` OpenAI-compatible client under `internal/llm/openai_compat/`, routing/config in `internal/llm/config.go`, pricing lookup in `internal/llm/pricing_source.go`, and multimodal clients in `internal/multimodal/`.
  - Auth: `OPENROUTER_API_KEY`, with endpoint/model overrides via `AURA_LLM_BASE_URL`, `AURA_LLM_MODEL`, and `AURA_LLM_PROVIDER` in `internal/llm/config.go`.
- Local OpenAI-compatible backends - llama.cpp is the supported local reasoning/embedding/vision implementation; private llama.cpp, vLLM, Ollama, and similar endpoints may run keyless when addressed on a trusted local/private host.
  - SDK/Client: `internal/llm/openai_compat/`, private-host recognition in `cmd/aura/llm_client.go`, capability probing in `internal/llm/llamacpp_caps.go`, and compose services in `compose.yaml`.
  - Auth: No bearer token on local routes; configure `AURA_LLM_BASE_URL`, `AURA_LLM_PROVIDER`, `AURA_EMBED_BASE_URL`, and optional `AURA_EMBED_MODEL` through `internal/llm/config.go` and `internal/config/config_routes.go`.
- EmbeddingGemma sidecar - Default local embedding endpoint used by memory, semantic routing, and document-related features.
  - SDK/Client: OpenAI-compatible `/v1/embeddings` client in `internal/arcadedb/embedding.go`; the `aura-llama-embed` service is declared in `compose.yaml`.
  - Auth: Local route is unauthenticated; a hosted route reuses `OPENROUTER_API_KEY` through `internal/config/config_routes.go`.
- Multimodal sidecars - Local faster-whisper STT, Kokoro TTS, GLM-OCR/llama.cpp vision, and MarkItDown conversion are reached over bounded HTTP clients.
  - SDK/Client: `internal/multimodal/stt.go`, `internal/multimodal/tts.go`, `internal/multimodal/vision.go`, `internal/channels/telegram/documents.go`, and the `aura-stt`, `aura-tts`, `aura-ocr-vl`, `markitdown` services in `compose.yaml`.
  - Auth: Local routes are unauthenticated; cloud media routes reuse `OPENROUTER_API_KEY` as implemented in `internal/multimodal/client.go`.

**Search & Content Discovery:**
- SearXNG - Local meta-search backend for the `web_search` tool; Aura sends normalized `/search?format=json` queries and post-filters domain constraints.
  - SDK/Client: Standard HTTP client in `internal/web/searxng.go`; service and engine configuration live in `compose.yaml` and `searxng/settings.yml`.
  - Auth: No per-request credential from Aura; service configuration uses `SEARXNG_URL`, while the sidecar secret is represented by `SEARXNG_SECRET` in deployment configuration referenced by `compose.yaml`.
- skills.sh - Public skill catalogue searched by the cockpit before installation; `npx skills find` is the bounded fallback and `npx skills add` performs installation inside the appliance boundary.
  - SDK/Client: JSON API client in `internal/skills/catalog_search.go` and CLI transport in `internal/skills/installer.go`.
  - Auth: None for catalogue search; external discovery is opt-out with `AURA_SKILLS_EXTERNAL_DISCOVERY` in `internal/skills/installer.go`.
- Arbitrary web content - The `web_fetch` path downloads public HTTP(S) resources, applies DNS/SSRF controls, readability extraction, and HTML-to-Markdown conversion.
  - SDK/Client: `internal/web/` with `go-readability` and `html-to-markdown` dependencies pinned in `go.mod`.
  - Auth: No generic credential injection; configured headers/tokens are not attached to arbitrary fetches by the clients under `internal/web/`.

**Messaging & Channels:**
- Telegram Bot API - Native user channel using long polling, message edits, file/media transfer, onboarding deep links, and bot commands.
  - SDK/Client: `gopkg.in/telebot.v4` through `internal/channels/telegram/bot.go`, with configuration in `internal/channels/telegram/config.go` and optional local Bot API bases in `internal/config/config.go`.
  - Auth: `TELEGRAM_BOT_TOKEN`; optional endpoints use `TELEGRAM_API_BASE_URL` and `TELEGRAM_FILE_BASE_URL` in `internal/config/config.go`.
- WhatsApp - Sibling `whatsapp-mcp`/whatsmeow bridge exposes streamable MCP tools for the agent and a management REST surface for QR pairing/status/logout.
  - SDK/Client: MCP recipe in `internal/mcp/manager/catalog.go`, management proxy in `internal/agui/connect_api.go`, and reverse-proxy registry in `cmd/aura/integrations_proxy.go`; the service is declared in `compose.yaml`.
  - Auth: Device identity is linked by QR; the loopback management REST has no injected token, while any MCP HTTP auth may be configured through server `Env` entries handled by `internal/mcp/transport.go`.

**Mail, Calendar & Contacts:**
- Aura PIM MCP sidecar - Forked calendar MCP service provides unified mail, calendar, and contacts tools over streamable HTTP plus an admin REST API for account lifecycle.
  - SDK/Client: Built-in `calendar` MCP recipe in `internal/mcp/manager/catalog.go`, admin proxy in `internal/agui/connect_pim_api.go`, and integrations reverse proxy in `cmd/aura/integrations_proxy.go`; the `aura-pim-mcp` service is declared in `compose.yaml`.
  - Auth: Aura injects `AURA_PIM_MCP_ADMIN_TOKEN` server-side for admin routes; provider credentials are supplied to the sidecar through the account-management API described in `internal/agui/connect_pim_api.go`.
- Google - PIM account authorization uses browser OAuth and a public redirect callback owned by the PIM sidecar.
  - SDK/Client: Aura only proxies authorization-start/account-management calls in `internal/agui/connect_pim_api.go`; `caddy/Caddyfile` routes the callback directly to `aura-pim-mcp`.
  - Auth: Google client ID/secret are sidecar account configuration; `AURA_PIM_EXTERNAL_BASE_URL` determines the externally registered callback base as documented in `caddy/Caddyfile`.
- Microsoft/Outlook - PIM account authorization uses the device-code flow and polling rather than an Aura callback.
  - SDK/Client: Start/status/cancel proxy endpoints are implemented in `internal/agui/connect_pim_api.go` and forwarded to the PIM sidecar.
  - Auth: Provider device code is handled by the sidecar; Aura uses `AURA_PIM_MCP_ADMIN_TOKEN` only on its sidecar-admin hop in `internal/agui/connect_pim_api.go`.

**Model Context Protocol:**
- Built-in MCP recipes - `memory`, `calendar`, and `whatsapp` are curated streamable-HTTP integrations; memory plus container-shipped recipes are injected by the config layer according to runtime/profile state.
  - SDK/Client: Catalogue in `internal/mcp/manager/catalog.go`, default-on injection in `internal/config/config_mcp.go`, transport opening in `internal/mcp/transport.go`, and tool mounting in `internal/agent/mcptools/`.
  - Auth: HTTP recipes consume `MCP_BEARER_TOKEN` and `MCP_HEADER_*` entries from each server's configured environment through `internal/mcp/transport.go`.
- Operator-defined MCP servers - Aura supports local stdio processes, Docker/Docker-gateway runtimes, and remote streamable-HTTP MCP servers with trust classification and profile selection.
  - SDK/Client: Durable registry schema and path handling in `internal/mcp/managed_config.go`, lifecycle/client implementations under `internal/mcp/`, and runtime isolation under `internal/mcp/manager/`.
  - Auth: Server-specific environment entries in `~/.aura/mcp/servers.json` or `AURA_MCP_SERVERS_JSON`; registry files are persisted with mode 0600 by `internal/mcp/managed_config.go`.
- Aura ArcadeDB MCP - Internal streamable-HTTP MCP server exposes bitemporal facts, search, entities, schema, forget, digest, and merge tools over per-identity ArcadeDB clients.
  - SDK/Client: Server and `/mcp` handler in `cmd/arcadedb-mcp/main.go`; tenant isolation/client code in `internal/arcadedb/`; recipe wiring in `internal/mcp/manager/catalog.go`.
  - Auth: Per-identity ArcadeDB credentials are HMAC-derived from `AURA_ARCADEDB_TENANT_SECRET`; the MCP tool call must carry `user_identifier` as enforced by `internal/mcp/transport.go` and `internal/arcadedb/tenant.go`.

**Container & Host Infrastructure:**
- Docker Engine - Per-identity full-capability sandboxes and egress sidecars are created through the Moby client, optionally through the Compose socket proxy.
  - SDK/Client: Moby dependencies in `go.mod`, runtime under `internal/sandbox/usersandbox/`, and `docker-socket-proxy`/sandbox profiles in `compose.yaml`.
  - Auth: Docker socket access is deployment-controlled; sandbox limits and images use `AURA_SANDBOX_*` configuration in `internal/config/config_sandbox.go`.

## Data Storage

**Databases:**
- PostgreSQL 18.4 - Primary relational store for the Aura control plane, conversations, documents/catalogue, identities/capabilities, scheduler, audit, settings, and object-store credential metadata.
  - Connection: `AURA_DB_URL` for `aura_app`, `AURA_DB_MIGRATE_URL` for `aura_migrate`, and optional `AURA_DB_BOOTSTRAP_URL`; local DSNs can be composed from `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_DB`, and `POSTGRES_SSLMODE` in `internal/config/config.go`.
  - Client: pgx/v5 pool and sqlc-generated queries via `internal/db/db.go`, `internal/db/sqlc/`, and `sqlc.yaml`; migrations run through golang-migrate in `internal/db/migrate.go`.
- Authula schema in PostgreSQL - Embedded Authula gets its own `authula` schema and independent `database/sql` pool, while Aura domain data remains in `aura.*`.
  - Connection: `AURA_AUTHULA_DATABASE_URL`, or a DSN derived from `AURA_DB_URL` with `search_path=authula`, in `cmd/aura/serve_auth.go` and `internal/webauth/authula.go`.
  - Client: Authula's Bun/database layer is encapsulated by `internal/webauth/authula.go`; Aura's runtime pgx pool is not reused for Authula tables.
- ArcadeDB 26.7.3 - Long-term bitemporal graph/vector/full-text memory, with one database and one server credential per Aura identity.
  - Connection: `ARCADEDB_URL`, `ARCADEDB_DATABASE`, `ARCADEDB_USER`, `ARCADEDB_PASSWORD`, `ARCADEDB_ADMIN_USER`, and `ARCADEDB_ADMIN_PASSWORD` in `internal/config/arcadedb.go` and `cmd/arcadedb-mcp/main.go`.
  - Client: Typed HTTP SQL/Cypher client in `internal/arcadedb/client.go`; the server version is checked against the minimum secure 26.4.2 boundary in the same file.

**File Storage:**
- Garage 2.3.0 / S3-compatible object storage - Default asset, document-original, export, and share-snapshot store; browser uploads use presigned PUT URLs.
  - SDK/Client: AWS SDK v2 S3 adapter in `internal/objectstore/s3.go`; service wiring in `compose.yaml` and `cmd/aura/objectstore.go`.
  - Auth: Shared S3 configuration uses `AURA_OBJECTSTORE_ACCESS_KEY` and `AURA_OBJECTSTORE_SECRET_KEY`; per-identity scoped keys are minted by the Garage Admin API client in `internal/objectstore/garageadmin/client.go` and encrypted in PostgreSQL by `internal/objectstore/identity_store.go`.
- Garage Admin API v2 - Creates per-identity buckets, scoped access keys, permissions, and teardown operations over the internal network.
  - SDK/Client: Standard HTTP bearer client in `internal/objectstore/garageadmin/client.go` and provisioning adapter in `cmd/aura/serve_provisioning_objectstore.go`.
  - Auth: `AURA_GARAGE_ADMIN_TOKEN` with endpoint `AURA_GARAGE_ADMIN_ENDPOINT`; service topology is defined in `compose.yaml` and config defaults in `internal/config/config.go`.
- Local filesystem - Development object-store backend plus run artifacts/spillover, skills, profiles, and fixed workspace roots.
  - SDK/Client: Safe object adapter in `internal/objectstore/filesystem.go`; paths are resolved from `AURA_RUN_DIR`, `AURA_SKILLS_DIR`, `AURA_PROFILE_DIR`, and `AURA_WORKSPACE_DIR` in `internal/config/config.go` and `internal/config/config_paths.go`.

**Caching:**
- No dedicated Redis/Memcached service is deployed; runtime caches are in-process or local-disk/provider caches, as evidenced by the service list in `compose.yaml` and the web-cache configuration in `internal/config/config_web.go`.
- Web search responses use a short in-process cache and web fetch/search may opt into persistent disk caching with `AURA_WEB_CACHE_PERSISTENT`, implemented under `internal/web/` and configured by `internal/config/config_web.go`.
- LLM prompt caching is provider-side/sticky-session behavior rather than a separate Aura cache server, represented by request/session fields in `internal/llm/client.go` and OpenRouter wire handling under `internal/llm/openai_compat/`.

## Authentication & Identity

**Auth Provider:**
- Embedded Authula - Cockpit auth uses email/password + TOTP, hardened `__Host-` cookies, SameSite Strict, double-submit CSRF/Fetch-Metadata checks, and in-memory credential rate limiting.
  - Implementation: `internal/webauth/authula.go`, session validation in `internal/webauth/session_validate.go`, identity linking in `internal/webauth/identity_link.go`, and composition in `cmd/aura/serve_auth.go`.
  - Configuration: `AURA_WEB_AUTH_PROVIDER` defaults to `authula`; `AURA_AUTHULA_SECRET` and a database URL are required for `aura serve` by `internal/config/config.go` and `cmd/aura/serve_auth.go`.
- Aura identity/capability authorization - Authula users resolve through `aura.identity_auth_links`, and HTTP routes apply capability gates rather than relying on Authula roles.
  - Implementation: `internal/identity/`, `internal/webauth/identity_link.go`, `internal/agui/auth.go`, and capability-gated route mounting in `cmd/aura/serve_webui.go`.
- Provider/channel identity - Telegram is authenticated by its bot token and onboarding link/account mapping; WhatsApp uses linked-device QR state; PIM accounts use Google OAuth or Microsoft device code.
  - Implementation: `internal/channels/telegram/`, `internal/agui/connect_api.go`, `internal/agui/connect_pim_api.go`, and `caddy/Caddyfile`.

## Monitoring & Observability

**Error Tracking:**
- No hosted error-tracking SDK/service is detected; failures are exposed through structured logs, OpenTelemetry spans, Prometheus metrics/alerts, readiness endpoints, and CI evidence in `internal/obs/`, `observability/`, and `.github/workflows/ci.yml`.

**Logs:**
- JSON `log/slog` with central redaction is installed by `internal/obs/init.go`; OTel SDK exporter failures are rate-limited into the same log path by `internal/obs/otel_error_handler.go`.
- Traces support `none`, `stdout`, or OTLP/gRPC exporters via `AURA_OTEL_EXPORTER` and `AURA_OTEL_ENDPOINT` in `internal/config/config.go`, `internal/obs/init.go`, and `internal/obs/tracer.go`.
- Metrics expose a private Prometheus handler and optional OTLP metric reader in `internal/obs/meter.go`; the default private bind is `AURA_METRICS_BIND=127.0.0.1:9464` from `internal/config/config.go`.
- Optional local Prometheus, Tempo, and Grafana services, alert rules, dashboards, and runbooks are defined in `compose.yaml` and `observability/`.

## CI/CD & Deployment

**Hosting:**
- Self-hosted Linux appliance - Docker Compose services in `compose.yaml` are launched through `deploy/aura.service`; Caddy fronts the cockpit with internal-CA HTTPS according to `caddy/Caddyfile`.
- Native daemon option - `deploy/aura-scheduler.service` runs `aura serve` as a hardened systemd user service with access to operator state and Docker-backed dependencies.
- Distribution - Native archives go to GitHub Releases and multi-architecture appliance images go to GHCR through `.goreleaser.yaml` and `.github/workflows/release.yml`.

**CI Pipeline:**
- GitHub Actions - Build, vet, lint, deadcode, race tests, tagged integration tiers, coverage, mutation testing, web E2E, sidecar integration, sandbox integration, and observability gates are defined in `.github/workflows/ci.yml` and `.github/workflows/skills.yml`.
- CodeQL - Scheduled/push/PR static analysis for Go and JavaScript/TypeScript is configured in `.github/workflows/codeql.yml` and `.github/codeql/codeql-config.yml`.
- Release readiness - Exact-SHA evidence aggregation and rollback rehearsal run in `.github/workflows/production-readiness.yml`; tag releases are blocked on that check in `.github/workflows/release.yml`.
- Supply-chain output - GoReleaser checksums and Syft SBOMs are configured in `.goreleaser.yaml` and `.github/workflows/release.yml`.

## Environment Configuration

**Required env vars:**
- LLM/cloud routes: `OPENROUTER_API_KEY` for hosted calls; routing knobs include `AURA_LLM_PROVIDER`, `AURA_LLM_MODEL`, `AURA_LLM_BASE_URL`, `AURA_EMBED_BASE_URL`, `AURA_EMBED_MODEL`, and `AURA_EMBED_DIMENSIONS` in `internal/llm/config.go`, `internal/config/config.go`, and `internal/config/config_routes.go`.
- PostgreSQL: `AURA_DB_URL`, `AURA_DB_MIGRATE_URL`, and optional `AURA_DB_BOOTSTRAP_URL`, or the `POSTGRES_*` primitives used to compose them in `internal/config/config.go` and `internal/db/config.go`.
- Web auth: `AURA_AUTHULA_SECRET` plus `AURA_AUTHULA_DATABASE_URL` or `AURA_DB_URL`; optional policy knobs include `AURA_WEB_TRUST_PROXY` and `AURA_AUTHULA_RATE_LIMIT_MAX` in `internal/config/config.go` and `cmd/aura/serve_auth.go`.
- ArcadeDB memory: `ARCADEDB_URL`, tenant/admin credential variables, and the mandatory derivation secret `AURA_ARCADEDB_TENANT_SECRET`; MCP bind knobs live in `cmd/arcadedb-mcp/main.go` and `internal/arcadedb/tenant.go`.
- Object storage: `AURA_OBJECTSTORE_BACKEND`, endpoint/region/bucket/access/secret variables, plus `AURA_GARAGE_ADMIN_ENDPOINT`, `AURA_GARAGE_ADMIN_TOKEN`, and `GARAGE_RPC_SECRET` for provisioned Garage deployments in `internal/config/config.go` and `internal/objectstore/`.
- Search: `SEARXNG_URL` enables search; timeout/cache/user-agent controls are `AURA_WEB_*` variables in `internal/config/config_web.go` and `internal/web/searxng.go`.
- Telegram/WhatsApp: `TELEGRAM_BOT_TOKEN` and optional Telegram API bases in `internal/channels/telegram/config.go` and `internal/config/config.go`; WhatsApp MCP/bridge URL and port controls are defined in `internal/mcp/manager/catalog.go` and `internal/config/config.go`.
- PIM: `AURA_PIM_MCP_URL`, `AURA_PIM_MCP_ADMIN_TOKEN`, and `AURA_PIM_MCP_PORT` connect Aura to the sidecar in `internal/config/config.go`, `internal/agui/connect_pim_api.go`, and `internal/mcp/manager/catalog.go`; the public Google redirect base is documented as `AURA_PIM_EXTERNAL_BASE_URL` in `caddy/Caddyfile`.
- Multimodal/document sidecars: `MULTIMODAL_BASE_URL`, `MULTIMODAL_MODEL`, `MULTIMODAL_FALLBACK_MODEL`, `STT_BASE_URL`, `STT_MODEL`, `STT_LANGUAGE`, `AURA_STT_CLOUD_MODEL`, `TTS_BASE_URL`, `TTS_VOICE`, `TTS_FORMAT`, `AURA_TTS_MODEL`, `DOCUMENTS_BASE_URL`, and `AURA_VISION_CLOUD` are loaded in `internal/config/config.go`.
- MCP extensions: `AURA_MCP_CONFIG` or `AURA_MCP_SERVERS_JSON`; per-server HTTP auth uses `MCP_BEARER_TOKEN` and `MCP_HEADER_*` in `internal/mcp/managed_config.go`, `internal/config/config_mcp.go`, and `internal/mcp/transport.go`.
- Observability: `AURA_OTEL_EXPORTER`, `AURA_OTEL_ENDPOINT`, and `AURA_METRICS_BIND` in `internal/config/config.go` and `internal/obs/`.
- Sandbox: image, limits, TTL, and egress policy use the `AURA_SANDBOX_*` surface in `internal/config/config_sandbox.go` and `internal/sandbox/usersandbox/`.

**Secrets location:**
- A root `.env` file is present and loaded best-effort by `internal/config/config.go`, `internal/llm/config.go`, and `cmd/aura/main.go`; its contents were not read for this audit.
- The native systemd deployment expects runtime secrets in `~/.aura/env`, outside the repository, as documented by `deploy/aura-scheduler.service`.
- Release registry credentials are supplied through GitHub Actions secrets rather than repository config by `.github/workflows/release.yml`.
- MCP server environment entries may contain tokens and are stored in `~/.aura/mcp/servers.json` with user-only permissions by `internal/mcp/managed_config.go`.
- Per-identity Garage secret keys are encrypted with AES-256-GCM using a domain-separated key derived from `AURA_AUTHULA_SECRET`, then persisted in PostgreSQL by `internal/objectstore/identity_store.go`.

## Webhooks & Callbacks

**Incoming:**
- Google OAuth callback: `GET /admin/auth/google/callback` is the one explicit public provider callback; Caddy routes it directly to `aura-pim-mcp`, and OAuth state binds the exchange as documented in `caddy/Caddyfile`.
- No Telegram webhook is registered: the native Telegram channel uses the telebot long poller in `internal/channels/telegram/bot.go`; WhatsApp connectivity is owned by the sibling bridge and driven through `internal/agui/connect_api.go`.
- Internal MCP ingress is service-to-service rather than a public webhook: the ArcadeDB MCP server exposes `/mcp`, `/mcp/`, and `/health` in `cmd/arcadedb-mcp/main.go`, with loopback/compose routing in `internal/mcp/manager/catalog.go` and `compose.yaml`.

**Outgoing:**
- No generic outbound webhook dispatcher is detected; external side effects use synchronous provider clients or MCP calls through `internal/llm/`, `internal/channels/telegram/`, `internal/mcp/`, `internal/objectstore/`, and `internal/web/`.
- OAuth/device-link initiation and polling are forwarded synchronously to the PIM/WhatsApp sidecars by `internal/agui/connect_pim_api.go`, `internal/agui/connect_api.go`, and `cmd/aura/integrations_proxy.go`.

---

*Integration audit: 2026-08-02*
