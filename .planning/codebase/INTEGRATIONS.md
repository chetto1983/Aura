# External Integrations

**Analysis Date:** 2026-05-04

## Telegram Bot API

- **Service:** Telegram Bot API
  - Client: `gopkg.in/telebot.v4` v4.0.0-beta.7
  - Transport: Long-polling
  - Auth: `TELEGRAM_TOKEN` (required bootstrap secret, stored in `.env`)
  - Location: `internal/telegram/` — 17 files covering bot setup, handlers, conversations, streaming, document handling, markdown rendering, runtime settings

The Telegram integration is the primary user interface. All interactions flow through Telegram messages including:
- `/start` — User registration flow with approval queue
- Tool-triggered conversations
- Dashboard token minting via `request_dashboard_token` LLM tool
- Markdown-to-HTML rendering (`internal/telegram/markdown.go`)

## LLM Providers (OpenAI-Compatible API)

**Category:** Language Model APIs (chat completions, tool calling, streaming)

- **Provider:** Any OpenAI-compatible API endpoint
  - Client: `internal/llm/openai.go` — custom HTTP client implementing `llm.Client` interface
  - Auth: `LLM_API_KEY`, configured via `LLM_BASE_URL` and `LLM_MODEL`
  - Features: Non-streaming `Send()`, SSE streaming via `Stream()`, tool calling, usage tracking
  - Retry: `internal/llm/retry.go` — exponential backoff (5 retries, 1s base, 30s max)
  - Failover: `internal/llm/ollama.go` — `FailoverClient` chains multiple providers

The LLM client uses standard `/chat/completions` endpoint and supports `stream_options.include_usage` for token counting in streaming responses. Tool calls are streamed and reassembled from SSE delta chunks.

## Ollama

**Category:** Local LLM Runtime

- **Service:** Ollama (local or self-hosted)
  - Client: `internal/llm/ollama.go` — `OllamaClient` wraps `OpenAIClient` (uses Ollama's OpenAI-compatible API)
  - Auth: None required locally (sends placeholder key `"ollama"`)
  - Config: `OLLAMA_BASE_URL` (e.g. `http://localhost:11434/v1`), `OLLAMA_MODEL`
  - Location: `internal/llm/ollama.go`

**Web Tools powered by Ollama:**
- `web_search` — `internal/tools/websearch.go` — Calls Ollama's web search API (20s timeout)
- `web_fetch` — `internal/tools/webfetch.go` — Fetches page content via URL (30s timeout)
- Both use `internal/tools/ollama_web.go` — shared HTTP client for Ollama's tool endpoints

The `LLM_API_KEY` is used as the chat model; Ollama web tools use their own dedicated configuration when configured.

## Mistral AI APIs

**Category:** Document AI / Embeddings

### Mistral OCR
- **Service:** Mistral Document AI OCR
  - Client: `internal/ocr/client.go` — custom HTTP client
  - Endpoint: `POST {baseURL}/ocr` (typically `https://api.mistral.ai/v1/ocr`)
  - Auth: `MISTRAL_API_KEY` (Bearer token)
  - Model: `mistral-ocr-latest` (default, configurable via env)
  - Features: PDF-to-text extraction, table formatting, header/footer extraction, image inclusion
  - Location: `internal/ocr/` — client, types, render pipeline, tests
  - Used by: `internal/ingest/pipeline.go` — PDF ingestion pipeline with SHA-256 dedup

PDFs are sent as base64-encoded data URLs in JSON payload. Responses are archived as `ocr.json` for replay/debug.

### Mistral Embeddings
- **Service:** Mistral Embeddings API
  - Endpoint: `/v1/embeddings` (OpenAI-compatible)
  - Auth: `EMBEDDING_API_KEY` (dedicated key — never falls back to `LLM_API_KEY`)
  - Model: `mistral-embed`
  - Config: `EMBEDDING_BASE_URL=https://api.mistral.ai/v1`, `EMBEDDING_MODEL=mistral-embed`
  - Used by: `internal/search/search.go` — chromem-go vector search, embedding cache in SQLite
  - Cache: `internal/search/embed_cache.go` — sha256 cache in SQLite to avoid redundant API calls

Per `AGENTS.md`: "do not fall back from embeddings to `LLM_API_KEY`" — embedding auth is strictly separated.

## MCP (Model Context Protocol)

- **Service:** MCP-compatible servers
  - Client: `internal/mcp/client.go` — 377-line implementation
  - Protocol: JSON-RPC 2.0, MCP version `2025-03-26`
  - Transports: stdio (child process) and Streamable-HTTP
  - Config: `mcp.json` file (schema in `internal/mcp/config.go`)
  - Registration: Tools auto-register as `mcp_<server>_<tool>`
  - Location: `internal/mcp/` — client, config, tests
  - Example config: `mcp.example.json`

**Transport details:**
- **stdio:** Spawns a child process, communicates via stdin/stdout pipes
- **HTTP:** Streamable-HTTP with configurable headers (e.g., `Authorization` for remote servers)

The dashboard (`internal/api/mcp.go`, `mcp_write.go`) exposes MCP server management via REST API.

## E2E Testing

- **Service:** Playwright
  - Version: `@playwright/test` 1.59.1
  - Config: `web/playwright.config.ts` — single worker (dashboard shares SQLite), chromium only
  - Auth: Bearer token via `AURA_E2E_TOKEN` env var
  - URL: `AURA_DASHBOARD_URL=http://localhost:8081`
  - Commands: `npm run e2e`, `npm run e2e:headed`, `npm run e2e:debug`, `npm run e2e:report`

## Sandbox (Code Execution)

- **Service:** Pyodide (WebAssembly Python runtime)
  - Location: `internal/sandbox/` — sandbox manager, `pyodide_runner.go`
  - Runtime: Bundled in `runtime/pyodide/` (shipped with release builds)
  - Config: `SANDBOX_ENABLED`, `SANDBOX_RUNTIME_DIR`, `SANDBOX_TIMEOUT_SEC`
  - No host-Python fallback — missing or unhealthy bundles disable `execute_code`

## Observability

**Tracing:**
- Framework: OpenTelemetry (`go.opentelemetry.io/otel` v1.43.0)
- Exporter: stdout (`stdouttrace.WithPrettyPrint()`)
- Config: `OTEL_ENABLED=false` (default disabled)
- Location: `internal/tracing/tracing.go`
- Used by: LLM client (`openai.send`, `openai.stream`), failover client (`failover.send`, `failover.stream`)

**Logging:**
- Framework: `go.uber.org/zap` v1.28.0 (structured, leveled)
- Config: `LOG_LEVEL=info` (debug | info | warn | error)

## Data Storage

**Database:**
- Type: SQLite (via `modernc.org/sqlite` v1.50.0, pure Go, no CGO)
- Connection: `DB_PATH=./aura.db` — single-file database
- Tables: `settings`, `api_tokens`, `pending_users`, `allowed_users`, `wiki_documents` (FTS5), embedding cache, scheduler jobs, conversations
- ORM/Client: Raw `database/sql` with prepared statements throughout

**File Storage:**
- Wiki: Local filesystem at `WIKI_PATH=./wiki` (markdown files with frontmatter)
- Skills: Local filesystem at `SKILLS_PATH=./skills` (Anthropic skill format)
- Sources (PDF): Ingested through pipeline, raw JSON archived locally

**Caching:**
- Embedding cache in SQLite (`internal/search/embed_cache.go`) — sha256 hash-based lookup

## Authentication & Identity

**Auth Provider:**
- Custom: SQLite-backed token auth (`internal/api/auth.go`, `internal/auth/` directory)
- Mechanism: Bearer tokens minted via `request_dashboard_token` LLM tool, validated server-side
- User approval: `/start` flow writes to `pending_users`, operator approves to `allowed_users`
- Telegram user ID is the identity key

## CI/CD & Deployment

**Hosting:**
- Self-hosted: Single binary runs on user's machine
- No cloud dependency required (Ollama for offline LLM)

**CI Pipeline:**
- goreleaser v2 (`.goreleaser.yml`) — GitHub Actions compatible
- Build hooks: `go mod tidy`, icon generation (`go run ./cmd/build_icon`), version info embedding, frontend build (`npm ci && npm run build`)
- Cross-compilation: linux/darwin/windows × amd64/arm64
- Archives: `.tar.gz` (Linux/macOS), `.zip` (Windows)

## Environment Configuration

**Required env vars (bootstrap):**
- `TELEGRAM_TOKEN` — Telegram Bot API token (required)

**Optional env vars (runtime-configurable via dashboard):**
- `LLM_API_KEY`, `LLM_BASE_URL`, `LLM_MODEL` — Primary LLM provider
- `OLLAMA_BASE_URL`, `OLLAMA_MODEL` — Local Ollama configuration
- `MISTRAL_API_KEY` — Mistral OCR access
- `EMBEDDING_API_KEY`, `EMBEDDING_BASE_URL`, `EMBEDDING_MODEL` — Embeddings (Mistral default)
- `HTTP_PORT` — Dashboard listen address (default `127.0.0.1:8080`)
- `DB_PATH`, `WIKI_PATH`, `SKILLS_PATH`, `MCP_SERVERS_PATH` — Filesystem paths
- `SOFT_BUDGET`, `HARD_BUDGET` — Token budget limits

**Secrets location:**
- `.env` file (excluded from git)
- Bootstrap secrets (TELEGRAM_TOKEN) in `.env`
- Operator-rotated API keys persistable to SQLite settings store via dashboard
- Security boundary: OS-level file permissions on both `.env` and `aura.db`

## Webhooks & Callbacks

**Incoming:**
- None (Telegram uses long-polling, not webhooks)

**Outgoing:**
- None explicit; all external API calls are request-response (OCR, LLM, embeddings, web tools)

---

*Integration audit: 2026-05-04*
