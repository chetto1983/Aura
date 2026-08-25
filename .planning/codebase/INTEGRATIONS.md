# External Integrations

**Analysis Date:** 2026-08-25

## APIs & External Services

**LLM Providers:**
- OpenRouter - Default hosted chat provider configured in `internal/llm/config.go`, using the OpenAI-compatible `/chat/completions` and `/models` APIs implemented by `internal/llm/openai_compat/client.go`.
  - SDK/Client: Repository-owned streaming HTTP client in `internal/llm/openai_compat/client.go`.
  - Auth: `OPENROUTER_API_KEY`; provider, model, endpoint, and generation controls use `AURA_LLM_*` variables parsed in `internal/llm/config.go`.
- llama.cpp - Optional local chat provider and the standard local embedding/OCR protocol surface, exposed as OpenAI-compatible services by `aura-llm`, `aura-llama-embed`, and `aura-ocr-vl` in `docker-compose.yml`.
  - SDK/Client: Repository-owned clients in `internal/llm/openai_compat/`, `internal/embeddings/client.go`, and `internal/multimodal/`.
  - Auth: Local Compose endpoints do not require a provider API key; hosted-compatible endpoints may be configured with their own API key through the same clients.

**Embeddings:**
- EmbeddingGemma through llama.cpp - Primary embedding endpoint is the `aura-llama-embed` service in `docker-compose.yml`; `internal/embeddings/client.go` sends OpenAI-compatible `/v1/embeddings` requests and enforces batching plus normalization behavior.
  - SDK/Client: Custom Go client in `internal/embeddings/client.go`; CocoIndex ingestion uses the same endpoint from `services/ingest/app.py`.
  - Contract: Dimension 768 plus model revision/fingerprint configuration in `internal/config/config_embed.go` and `docker-compose.yml`.

**Search & Web Retrieval:**
- SearXNG - Local metasearch endpoint used by `internal/web/searxng.go`, with JSON search responses, retry handling, and a configurable base URL.
  - SDK/Client: Custom Go HTTP client in `internal/web/searxng.go`.
  - Auth: No per-request credential in the application client; the deployment requires `SEARXNG_SECRET` for the sidecar configuration in `docker-compose.yml`.
- Public websites - Arbitrary page retrieval is implemented by `internal/web/fetcher.go`, with DNS pinning, SSRF protections, redirects/body limits, readability extraction, and HTML-to-Markdown conversion.
  - SDK/Client: Go `net/http`, `go-readability`, and `html-to-markdown` as wired under `internal/web/`.
  - Auth: None for public pages; do not weaken the network and URL validation in `internal/web/fetcher.go` when extending retrieval.

**Speech, Vision & OCR:**
- faster-whisper - Local speech-to-text endpoint supplied by `aura-stt` in `docker-compose.yml`; requests use the OpenAI-compatible multipart `/audio/transcriptions` contract from `internal/multimodal/`.
  - SDK/Client: Custom multimodal HTTP clients under `internal/multimodal/`.
  - Auth: Local service endpoint; hosted routing can use the configured LLM/provider key.
- Kokoro FastAPI - Local text-to-speech endpoint supplied by `aura-tts` in `docker-compose.yml`; `internal/multimodal/` sends `/audio/speech` requests and consumes Opus audio.
  - SDK/Client: Custom multimodal HTTP clients under `internal/multimodal/`.
  - Auth: Local service endpoint; hosted routing can use the configured LLM/provider key.
- GLM-OCR through llama.cpp - Optional vision/OCR service supplied by `aura-ocr-vl` under the `ocr` profile in `docker-compose.yml`, consumed by clients under `internal/multimodal/`.
  - SDK/Client: Shared OpenAI-compatible multimodal client under `internal/multimodal/`.
  - Auth: Local endpoint by default; cloud vision routing can use `OPENROUTER_API_KEY`.

**Messaging & Personal Information:**
- Telegram Bot API - Bidirectional channel implementation under `internal/channels/telegram/` handles text, photos, voice, and documents through long polling and outbound Bot API calls.
  - SDK/Client: `gopkg.in/telebot.v4` configured by `internal/channels/telegram/config.go` and used by `internal/channels/telegram/bot.go`.
  - Auth: `TELEGRAM_BOT_TOKEN`; optional API base URL settings support a local Telegram Bot API deployment.
- Aura PIM MCP - Mail, calendar, and contacts integration is provided by the pinned `aura-pim-mcp` sidecar in `docker-compose.yml`; its curated model-facing catalog entry is `calendar` in `internal/mcp/manager/catalog.go`.
  - SDK/Client: Streamable HTTP MCP through `internal/mcp/`, plus server-side management proxy routes in `internal/agui/connect_pim_api.go`.
  - Auth: `AURA_PIM_MCP_ADMIN_TOKEN` is injected only on server-to-server management calls; upstream account grants are handled by the sidecar.
  - Providers: Google accounts use OAuth redirect, Microsoft/Outlook accounts use device code, and the sidecar also supports IMAP, ICS, and JSON account sources; the management flow is implemented in `internal/agui/connect_pim_api.go`.
- WhatsApp MCP - Messaging and account management are supplied by the pinned `whatsapp-mcp` sidecar in `docker-compose.yml`; the curated tool entry is `whatsapp` in `internal/mcp/manager/catalog.go`.
  - SDK/Client: Streamable HTTP MCP through `internal/mcp/`, with QR/status management routes under `internal/agui/` and proxy wiring in `cmd/aura/integrations_proxy.go`.
  - Auth: Sidecar management credentials and session material remain server-side; browser clients consume Aura's authenticated management routes.

**Model Context Protocol:**
- Managed MCP servers - Generic stdio and Streamable HTTP servers are managed through `internal/mcp/manager/`, using the official Go SDK and persistent registry records from `internal/mcpregistry/store.go`.
  - SDK/Client: `github.com/modelcontextprotocol/go-sdk` 1.7.0 under `internal/mcp/`.
  - Auth: Per-server OAuth, bearer-token, environment, and header configuration is stored encrypted in PostgreSQL; policy and SSRF checks are enforced under `internal/mcp/`.
- ArcadeDB MCP - The built-in `memory` tool is served by `cmd/arcadedb-mcp/` on port 8096 and registered by `internal/mcp/manager/catalog.go`.
  - SDK/Client: Official Go MCP SDK on both the server and application sides.
  - Auth: Application-to-sidecar connection inside the appliance network; ArcadeDB credentials are supplied to the server process through deployment configuration.

**Agent Protocol:**
- AG-UI - Browser and client agent runs enter through the SSE-capable `POST /agent/run` path and detached run/resume routes under `internal/agui/`.
  - SDK/Client: `github.com/ag-ui-protocol/ag-ui/sdks/community/go` in `go.mod`.
  - Auth: Aura web sessions and request middleware under `internal/webauth/` and `internal/agui/` protect the browser-facing routes.

**DNS & TLS:**
- deSEC - Public wildcard certificate issuance uses the Caddy deSEC DNS provider compiled by `docker/caddy/Dockerfile` and configured in `Caddyfile`.
  - SDK/Client: Caddy module `github.com/caddy-dns/desec`.
  - Auth: `DESEC_TOKEN` for deployments using DNS-01 issuance.

## Data Storage

**Databases:**
- PostgreSQL 18.4 - Durable control plane for identities, configuration, runs, managed MCP registry records, OAuth grants, and other relational state under the `aura` schema; Authula uses its own `authula` schema.
  - Connection: Role-separated `AURA_DB_URL`, `AURA_DB_MIGRATE_URL`, and `AURA_DB_BOOTSTRAP_URL` are loaded by code under `internal/config/` and injected in `docker-compose.yml`.
  - Client: pgx/v5 pools and sqlc-generated queries under `internal/db/sqlc/`; migrations under `internal/db/migrations/` are applied with golang-migrate.
  - Registry: Managed MCP definitions are stored in `aura.mcp_server` by `internal/mcpregistry/store.go`, with schema migration `internal/db/migrations/0101_mcp_server_registry.up.sql`.
  - OAuth grants: Identity-scoped MCP OAuth grants are encrypted and stored by `internal/mcpoauth/store.go`, using the schema under `internal/db/migrations/0100_identity_mcp_oauth.up.sql`.
- ArcadeDB 26.7.3 - Long-term memory and document-retrieval graph store, with one `mem_<uuid>` database per identity as provisioned and queried under `internal/arcadedb/`.
  - Connection: HTTP API credentials and tenant authorization are supplied through deployment variables in `docker-compose.yml`; the application uses endpoints assembled in `internal/arcadedb/client.go`.
  - Client: Custom HTTP client in `internal/arcadedb/client.go` for `/api/v1/query`, `/command`, and `/server`; CocoIndex writes through the Neo4j-compatible Bolt port from `services/ingest/app.py`.
  - Version guard: `internal/arcadedb/` enforces the supported server baseline required by the application security contract.

**File Storage:**
- Garage 2.3.0 S3 - Primary object store for uploaded and ingested files, declared by the `garage` service and persistent volumes in `docker-compose.yml`.
  - Connection: `AURA_OBJECTSTORE_ACCESS_KEY`, `AURA_OBJECTSTORE_SECRET_KEY`, endpoint, region, and path-style configuration from `internal/config/` and `docker-compose.yml`.
  - Client: AWS SDK for Go v2 adapter in `internal/objectstore/s3.go`, including presigned PUT, get, list, delete, and copy operations.
  - Provisioning: Garage Admin API v2 client in `internal/objectstore/garageadmin/client.go` creates identity-scoped buckets and keys using `AURA_GARAGE_ADMIN_TOKEN`.
- Filesystem and fake backends - Non-S3 storage implementations under `internal/objectstore/` support local development and tests; production appliance data belongs in Garage rather than the replaceable application container filesystem.

**Caching:**
- No external cache service is detected. Application caches are in-process or local-disk concerns under `internal/`, and Docker package-cache volumes in `docker-compose.yml` accelerate sandbox tooling; Redis is not a deployed runtime dependency.

**Ingestion:**
- CocoIndex sidecar - `services/ingest/app.py` reconciles an identity's Garage bucket, extracts content with `iscc-tika` and LibreOffice-compatible tooling, embeds through llama.cpp, and writes document/graph data to ArcadeDB.
  - Connection: Garage S3, embedding, ArcadeDB HTTP, and ArcadeDB Bolt settings are injected by the `aura-ingest` service in `docker-compose.yml`.
  - Client: CocoIndex S3 and Neo4j targets in `services/ingest/app.py`, with ArcadeDB schema operations in `services/ingest/arcade.py`.

## Authentication & Identity

**Auth Provider:**
- Embedded Authula 1.40.0 - Aura runs an in-process identity provider configured by `internal/webauth/authula.go`; no external identity SaaS is required for core web authentication.
  - Implementation: Email/password authentication with TOTP, session, CSRF, and rate-limit plugins, backed by the PostgreSQL `authula` schema and exposed through `/auth/*` handlers under `internal/webauth/`.
  - Session security: Identity linking and session validation live in `internal/webauth/identity_link.go` and `internal/webauth/session_validate.go`; browser sessions use secure `__Host-` cookies where the deployment permits them.
  - Secret: `AURA_AUTHULA_SECRET` protects Authula and encrypted integration material; `AURA_ACCESS_TOKEN` is required by appliance configuration in `docker-compose.yml`.

**External Account Authorization:**
- Google PIM authorization uses a browser OAuth callback terminating at the PIM sidecar's `/admin/auth/google/callback`; Aura initiates the flow through routes in `internal/agui/connect_pim_api.go`.
- Microsoft/Outlook PIM authorization uses device-code start, status, and cancellation routes in `internal/agui/connect_pim_api.go`, so it does not require an inbound OAuth callback.
- Generic MCP OAuth uses identity-scoped encrypted grants from `internal/mcpoauth/store.go`; CLI authorization returns through the ephemeral `/aura/mcp/oauth/callback`, while cockpit authorization returns through `/api/governance/mcp/authorization/callback` under the web server.

## Monitoring & Observability

**Error Tracking:**
- No hosted error-tracking service is detected. Failures are represented through structured logs, HTTP/protocol errors, metrics, and traces initialized by `internal/obs/init.go`.

**Logs:**
- Structured JSON `slog` is initialized with redaction in `internal/obs/init.go`; keep secrets and high-cardinality payloads out of log attributes.
- Container logs flow to Docker and the host's normal logging facilities, while systemd deployments use units under `deploy/`; no external log-shipping backend is configured in the repository.

**Metrics:**
- Application metrics use a private Prometheus registry and metrics server under `internal/obs/`, exported to optional Prometheus 3.13.1 configured by `docker-compose.yml` and `observability/prometheus/`.

**Tracing:**
- OpenTelemetry 1.45.0 supports disabled, stdout, and OTLP-gRPC trace export modes in `internal/obs/init.go`; the optional observability profile sends traces to Tempo 2.9.4 and visualizes them in Grafana 12.3.9 using configuration under `observability/`.

## CI/CD & Deployment

**Hosting:**
- Self-hosted Docker Compose appliance - The full service graph, profiles, persistent volumes, internal networks, health checks, and exposed ports are defined in `docker-compose.yml`.
- systemd - `deploy/aura.service` manages the appliance lifecycle; `deploy/aura-scheduler.service` supports the optional native scheduler deployment.
- Caddy - HTTPS reverse proxy and certificate automation are defined in `Caddyfile` and built by `docker/caddy/Dockerfile`.
- GitHub Container Registry - Appliance images and pinned integration sidecars use `ghcr.io/chetto1983/*` references in `.goreleaser.yaml` and `docker-compose.yml`.

**CI Pipeline:**
- GitHub Actions - Build, lint, unit, integration, race, SQLC, service-integration, web, mutation, E2E, sandbox, and image-freshness jobs are defined in `.github/workflows/ci.yml`.
- CodeQL and production checks - Security analysis and release readiness run through `.github/workflows/codeql.yml` and `.github/workflows/production-readiness.yml`.
- Release automation - `.github/workflows/release.yml` invokes the GoReleaser v2 configuration in `.goreleaser.yaml` to publish GitHub Releases, GHCR images, checksums, and Syft SBOMs.
- Image lifecycle and skill checks - `.github/workflows/retire-aura-images.yml` and `.github/workflows/skills.yml` maintain container and project-skill quality boundaries.

## Environment Configuration

**Required env vars:**
- Core database and application: `POSTGRES_PASSWORD`, `AURA_DB_URL`, `AURA_DB_MIGRATE_URL`, `AURA_DB_BOOTSTRAP_URL`, `AURA_ACCESS_TOKEN`, and `AURA_AUTHULA_SECRET`, consumed by `docker-compose.yml` and code under `internal/config/`.
- ArcadeDB: `ARCADEDB_PASSWORD`, `ARCADEDB_APP_PASSWORD`, and `AURA_ARCADEDB_TENANT_SECRET`, consumed by `docker-compose.yml` and `internal/arcadedb/`.
- Object storage: `GARAGE_RPC_SECRET`, `AURA_GARAGE_ADMIN_TOKEN`, `AURA_OBJECTSTORE_ACCESS_KEY`, and `AURA_OBJECTSTORE_SECRET_KEY`, consumed by `docker-compose.yml` and `internal/objectstore/`.
- Embeddings: `AURA_EMBED_REVISION` and `AURA_EMBED_FINGERPRINT`, with dimension/model settings consumed by `docker-compose.yml` and `internal/config/config_embed.go`.
- Integration sidecars: `AURA_PIM_MCP_ADMIN_TOKEN` and `SEARXNG_SECRET`, consumed by `docker-compose.yml` and the server-side proxies under `internal/agui/`.
- Conditional hosted/provider settings: `OPENROUTER_API_KEY`, `AURA_LLM_PROVIDER`, `AURA_LLM_MODEL`, and `AURA_LLM_BASE_URL`, consumed by `internal/llm/config.go`.
- Conditional channels and public ingress: `TELEGRAM_BOT_TOKEN`, `DESEC_TOKEN`, and `AURA_WEB_PUBLIC_URL`, consumed by `internal/channels/telegram/`, `Caddyfile`, and MCP OAuth callback construction.

**Secrets location:**
- A repository-local `.env` file is present and contains environment configuration; its contents must never be read, quoted, or committed. Runtime loading is implemented in `internal/config/config.go` and deployment interpolation occurs in `docker-compose.yml`.
- Optional user-level LLM configuration can live at `~/.aura/llm.json`, with precedence and parsing in `internal/llm/config.go`; treat any API key in that file as a secret.
- Managed MCP server environment variables, headers, bearer tokens, and OAuth grants are encrypted at rest in PostgreSQL through `internal/mcpregistry/store.go` and `internal/mcpoauth/store.go`.
- CI release and registry credentials are supplied through GitHub Actions secrets referenced by workflows under `.github/workflows/`; no credential values belong in the repository.

## Webhooks & Callbacks

**Incoming:**
- No conventional event webhook endpoint is detected. Telegram receives updates by long polling in `internal/channels/telegram/bot.go` rather than through a public webhook.
- Google PIM OAuth returns directly to the PIM sidecar at `/admin/auth/google/callback`, initiated by the management flow in `internal/agui/connect_pim_api.go`.
- Generic MCP OAuth returns to the CLI loopback path `/aura/mcp/oauth/callback` or the cockpit path `/api/governance/mcp/authorization/callback`, with grant storage under `internal/mcpoauth/`.
- AG-UI clients initiate agent work through `POST /agent/run` and consume SSE/detached-run routes implemented under `internal/agui/`; these are authenticated protocol endpoints, not third-party event webhooks.

**Outgoing:**
- OpenRouter and compatible LLM endpoints receive model, vision, embedding, speech, or capability requests from `internal/llm/`, `internal/embeddings/`, and `internal/multimodal/` when configured.
- Telegram Bot API receives polling and message/file/voice operations from `internal/channels/telegram/`.
- PIM and WhatsApp sidecars receive MCP traffic plus management requests from `internal/mcp/`, `internal/agui/connect_pim_api.go`, and `cmd/aura/integrations_proxy.go`; their upstream provider calls are owned by the sidecars.
- Garage S3 and Admin APIs receive object and provisioning operations from `internal/objectstore/s3.go` and `internal/objectstore/garageadmin/client.go`.
- ArcadeDB HTTP and Bolt endpoints receive memory queries and ingestion writes from `internal/arcadedb/`, `cmd/arcadedb-mcp/`, and `services/ingest/`.
- SearXNG and public websites receive search/fetch requests from `internal/web/`, subject to SSRF and response-size controls in `internal/web/fetcher.go`.
- OTLP-gRPC collectors receive trace exports from `internal/obs/init.go` when the tracing mode is enabled; the appliance profile routes them to Tempo using `docker-compose.yml`.
- deSEC DNS receives certificate DNS-01 operations from the custom Caddy module configured by `docker/caddy/Dockerfile` and `Caddyfile`.

---

*Integration audit: 2026-08-25*
