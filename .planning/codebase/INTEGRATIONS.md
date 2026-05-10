# External Integrations

**Analysis Date:** 2026-05-10

## APIs & External Services

**LLM Providers (OpenAI-compatible + local):**
- OpenAI / OpenAI-compatible API - Primary LLM backend for chat, reasoning, and tool calling
  - SDK/Client: Custom `OpenAIClient` in `internal/llm/openai.go` using standard `net/http`
  - Auth: `LLM_API_KEY` env var, `LLM_BASE_URL` for custom endpoints (e.g., OpenRouter `https://openrouter.ai/api/v1`)
  - Features: Send, Stream, tool calling with JSON schema definitions
  - Temperature control: `LLM_TEMPERATURE` (0 = deterministic for wiki ops)
- Ollama - Local LLM runtime via OpenAI-compatible API
  - SDK/Client: `OllamaClient` in `internal/llm/ollama.go` reuses `OpenAIClient`
  - Auth: `OLLAMA_BASE_URL` env var (default no auth needed for local)
  - Config: `internal/llm/ollama.go`

**Mistral AI:**
- Mistral OCR API - Document OCR for PDF/image processing
  - SDK/Client: Custom `ocr.Client` in `internal/ocr/client.go` using `net/http` + Bearer auth
  - Auth: `MISTRAL_API_KEY` env var; endpoint: `OCR_BASE_URL` (default `https://api.mistral.ai/v1`)
  - Model: `OCR_MODEL` (default `mistral-ocr-latest`)
  - Features: PDF upload with table extraction, header/footer control
  - Config: `internal/ocr/client.go`
- Mistral Embedding API - Text embedding for semantic search
  - SDK/Client: OpenAI-compatible endpoint via `chromem-go` embedding function, cached in `internal/search/embed_cache.go`
  - Auth: `EMBEDDING_API_KEY` env var; `EMBEDDING_BASE_URL` (default `https://api.mistral.ai/v1`)
  - Model: `EMBEDDING_MODEL` (default `mistral-embed`)
  - Cache: Content-addressed SHA-256 SQLite cache to eliminate redundant API calls (`internal/search/embed_cache.go`)

**Telegram Bot API:**
- Telegram - Primary user interaction channel (messaging, commands, file sharing)
  - SDK/Client: `gopkg.in/telebot.v4 v4.0.0-beta.7` in `internal/telegram/setup.go`
  - Auth: `TELEGRAM_TOKEN` env var
  - Features: Message handling, inline keyboards, file upload/download, polling/streaming
  - Allowlist: `TELEGRAM_ALLOWLIST` env var for user access control
  - Streaming: Real-time token streaming to chat (`internal/telegram/streaming.go`)

**Web Search:**
- SearXNG - Self-hosted meta-search engine for web search queries
  - SDK/Client: Custom `SearXNGSearchTool` in `internal/tools/searxng.go` using `net/http`
  - Config: `WEB_SEARCH_PROVIDER=searxng`, `SEARXNG_BASE_URL` env var (default `http://127.0.0.1:8088`)
  - Docker: `searxng` service in `compose.yaml` (image: `docker.io/searxng/searxng:latest`)
  - Config files: `./data/secrets/searxng/` mounted read-only
- Ollama Web Search API - Alternative web search via Ollama's built-in web capabilities
  - SDK/Client: `WebSearchTool` in `internal/tools/websearch.go` using custom `ollamaWebClient`
  - Same interface as SearXNG tool (tool name `web_search`)

**Web Fetching:**
- Ollama Web Fetch API - URL content fetching via Ollama
  - SDK/Client: `WebFetchTool` in `internal/tools/webfetch.go` using custom `ollamaWebClient`
  - 30-second timeout; response capped at 8000 characters

## Data Storage

**Databases:**
- SQLite (via `modernc.org/sqlite`)
  - Connection: `DB_PATH` env var (default `./aura.db`); single-file database
  - Client: Direct `database/sql` pool managed in `internal/db/db.go`
  - Journal mode: Configurable via `AURA_SQLITE_JOURNAL_MODE` (WAL for desktop, DELETE for Docker)
  - Stores: Settings, conversations, tasks, auth tokens, wiki content, sources, embedding cache, migration state

**File Storage:**
- Local filesystem - Primary storage for runtime workspace, wiki pages, skills, uploads
  - Paths: `AURA_RUNTIME_WORKSPACE_PATH` (default `./runtime-workspace`), `WIKI_PATH`, `SKILLS_PATH`
- Garage S3 - Self-hosted S3-compatible object storage for backups and artifacts
  - Client: AWS SDK v2 for Go (`github.com/aws/aws-sdk-go-v2/service/s3`) in `internal/backup/s3.go`
  - Config: `GARAGE_S3_ENDPOINT`, `GARAGE_S3_REGION`, `GARAGE_S3_BUCKET`, `GARAGE_S3_ACCESS_KEY`, `GARAGE_S3_SECRET_KEY` env vars
  - Docker: `garage` service in `compose.yaml` (image: `dxflrs/garage:v2.3.0`)
  - Features: Backup export/import, artifact set management
  - Access: Can use file-based secrets (`GARAGE_S3_ACCESS_KEY_FILE`, `GARAGE_S3_SECRET_KEY_FILE`) for Docker secret mounting

**Vector/Semantic Search:**
- Qdrant - External vector database for semantic memory search (optional, disabled by default)
  - Client: Custom REST client in `internal/search/qdrant.go` using `net/http`
  - Config: `QDRANT_URL` env var (default `http://127.0.0.1:6333`), `QDRANT_COLLECTION`, `QDRANT_API_KEY`
  - Docker: `qdrant` service in `compose.yaml` (image: `qdrant/qdrant:latest`)
  - Enabled when: `SEARCH_BACKEND=qdrant`
- chromem-go - Built-in local vector database (primary/default backend)
  - Library: `github.com/philippgille/chromem-go v0.7.0` used throughout `internal/search/`
  - Enabled when: `SEARCH_BACKEND=chromem` (default)
  - Features: In-process storage, embedding caching, batch embedding with Mistral

**Caching:**
- SQLite embed cache - Content-addressed embedding vector cache in `internal/search/embed_cache.go`
  - Stores SHA-256 hash of input text to embedding vector mapping
  - Invalidated when `EMBEDDING_BASE_URL` or `EMBEDDING_MODEL` changes
- NPM cache in Docker: `/data/.npm` for persistent package caching across container rebuilds

## Authentication & Identity

**Auth Provider:**
- Dashboard bearer token auth - Custom implementation in `internal/auth/`
  - Files: `internal/auth/middleware.go`, `internal/auth/store.go`
  - Tokens managed via dashboard login page (`web/src/components/Login.tsx`)
  - Token TTL configurable via `DASHBOARD_TOKEN_TTL_HOURS` (default 720 hours / 30 days)
  - Telegram-based token request via `request_dashboard_token` tool
- Telegram allowlist - User-level access control for Telegram bot interactions
  - Implemented in `internal/auth/store.go` via `AllowlistFunc`
  - Config: `TELEGRAM_ALLOWLIST` env var (comma-separated user IDs or usernames)
- Pending user approval - Dashboard approval flow for new Telegram users
  - Files: `internal/auth/pending_test.go`, `internal/api/pending.go`, `web/src/components/PendingUsersPanel.tsx`

## Monitoring & Observability

**Error Tracking:**
- Not detected (no Sentry, Datadog, New Relic, or other APM integration)

**Logs:**
- Structured logging via `go.uber.org/zap v1.28.0` with slog adapter (`internal/logging/zap_slog.go`)
- Daily rotating log files via `internal/logging/daily_writer.go`
- Log directory: `LOG_DIR` env var (default `/data/logs` in Docker)
- Log level: `LOG_LEVEL` env var (debug | info | warn | error; default: info)
- Dashboard surfaces stderr logs via `web/src/components/StderrLogSheet.tsx`
- Docker healthcheck endpoint at `/health` (`internal/health/server.go`)

**Tracing:**
- Agent trace retention via `AURA_TRACE_RETENTION_DAYS` (default 30 days)
- Trace data stored in SQLite database

## CI/CD & Deployment

**Hosting:**
- Docker / Docker Compose - Primary deployment method. Config: `compose.yaml`
- Docker Hub / GHCR - Container registry at `ghcr.io/chetto1983/aura`
- Desktop binary - Standalone Go binary release via GitHub Releases (goreleaser)

**CI Pipeline:**
- GitHub Actions - Two workflows in `.github/workflows/`
  - `release.yml` - Desktop binary release via goreleaser (Go 1.26.2, Node 20, linux runner)
  - `docker-image.yml` - Docker image build and publish (triggered on `v*` tags and manual dispatch)
    - Multi-platform: linux/amd64, linux/arm64
    - Buildx-based with GitHub Actions cache
    - SBOM + provenance attestation enabled

## Environment Configuration

**Required env vars:**
- `TELEGRAM_TOKEN` - Must be set; first-run wizard if empty
- `LLM_API_KEY` / `LLM_BASE_URL` / `LLM_MODEL` - At least one LLM provider must be configured
- `HTTP_PORT` - Dashboard listen address (default `127.0.0.1:8080`)

**Secrets location:**
- `.env` file in project root (contains tokens, API keys, credentials)
- `./data/secrets/` directory in Docker deployments (garage secrets, aura.env)
- Docker Compose `env_file` mounts from `./data/secrets/aura.env`
- Key-file based secret loading supported for Garage S3 access (`GARAGE_S3_ACCESS_KEY_FILE`, `GARAGE_S3_SECRET_KEY_FILE`)

## Webhooks & Callbacks

**Incoming:**
- None exposed externally. Telegram uses long-polling, not webhooks.

**Outgoing:**
- OpenAI-compatible chat completions API (`POST /v1/chat/completions`)
- Mistral OCR API (`POST /v1/ocr`)
- Mistral embeddings API (`POST /v1/embeddings`)
- Qdrant REST API (collection management, point upsert, vector search)
- Garage S3 API (bucket operations, object upload/download)
- SearXNG search API (`GET /search?format=json&q=...`)
- Ollama web search/fetch endpoints
- MCP server process communication (stdio or Streamable-HTTP)

## MCP (Model Context Protocol) Integrations

**MCP Server Configuration:**
- Config file: `mcp.json` (or `MCP_SERVERS_PATH` env var). Example: `mcp.example.json`
- Parser: `internal/mcp/config.go` (`LoadServers`)
- Client management: `internal/mcp/client.go`
- Dashboard management UI: `web/src/components/MCPPanel.tsx`

**Bundled MCP Servers (Docker):**
- `mail-mcp` v0.4.5 - Email operations (IMAP). Binary: `/usr/local/bin/mail-mcp` (Rust, x86_64)
- `ea-database-server` v1.1.0 - SQLite database querying. Binary: `/usr/local/bin/ea-database-server` (Node.js)

---

*Integration audit: 2026-05-10*
