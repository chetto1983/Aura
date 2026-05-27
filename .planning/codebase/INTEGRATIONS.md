# External Integrations

**Analysis Date:** 2026-05-27

## APIs & External Services

**Chat platforms:**
- Telegram Bot API — primary chat surface. Long-poll mode via `gopkg.in/telebot.v4`.
  - SDK/Client: `gopkg.in/telebot.v4`
  - Transport layer: `internal/telegram/bot.go`, `internal/telegram/handlers.go`, `internal/telegram/commands.go`, `internal/telegram/voice_handler.go`, `internal/telegram/documents.go`
  - Chat-hub adapter: `internal/channels/telegram/inbound.go`, `internal/channels/telegram/outbound.go`, `internal/channels/telegram/streaming_outbound.go`
  - Auth: `TELEGRAM_TOKEN` (loaded from SQLite `secrets.telegram_token`, fallback to env)
  - Allowlist gating: `TELEGRAM_ALLOWLIST` (comma-separated user IDs)
  - Outbound throttling: ~600ms progressive Telegram edits during streaming (per `CLAUDE.md`).

**LLM (chat + tool-calling):**
- OpenAI-compatible HTTP API (provider-agnostic; works with OpenAI, OpenRouter, DeepSeek, self-hosted llama.cpp servers).
  - SDK/Client: Hand-rolled in `internal/llm/client.go`, `internal/llm/openai.go`, `internal/llm/openai_stream.go`. No vendor SDK.
  - Auth: `LLM_API_KEY` (env / SQLite `secrets.llm_api_key`)
  - Endpoint: `LLM_BASE_URL` (e.g. `https://api.openai.com/v1`, `https://openrouter.ai/api/v1`, `http://host:port/v1`)
  - Model: `LLM_MODEL` (e.g. `gpt-5`, `deepseek-v4-flash`, `claude-3.5-sonnet:thinking` via OpenRouter)
  - Features: streaming with tool-call fragment accumulation, OpenAI `reasoning_effort` passthrough + OpenRouter nested `reasoning:{effort:...}`, optional `prompt_cache_key` for DeepSeek KV reuse, optional Anthropic-style `cache_control` markers when provider heuristic confirms support (`internal/llm/openai.go:35-52`).
  - Retry: bounded via `LLM_MAX_RETRIES` (default 5; `internal/llm/retry.go`).

**Embeddings (vector search):**
- Local llama.cpp server sidecar (`aura-llama-embed`) speaking the OpenAI `/v1/embeddings` endpoint.
  - Model: `embeddinggemma-300m-Q4_0.gguf` (Google embeddinggemma 300M, Q4_0 quant, 265 MB).
  - Source: `https://huggingface.co/unsloth/embeddinggemma-300m-GGUF/resolve/main/embeddinggemma-300m-Q4_0.gguf` (pinned SHA-256 in `internal/install/embedding.go:25`).
  - Endpoint: `EMBEDDING_BASE_URL` (default `http://aura-llama-embed:8080/v1`).
  - Auth: `EMBEDDING_API_KEY` (sentinel `"no-key"` for the local sidecar; separate from `LLM_API_KEY`).
  - Output dimension: `EMBEDDING_OUTPUT_DIM=256` in production — Matryoshka truncation from native 768 (`internal/config/config.go:121`).
  - Embedding-cache layer: SHA-keyed SQLite cache to avoid re-embedding unchanged pages (`internal/storage/search/embed_cache.go`, table introduced in migration `m26_embed_cache_output_dim.go`).
  - HTTP client: `internal/storage/search/embed_http.go`.
  - Fallback: NONE — this is the embedding backend; if the sidecar is down, vector search degrades gracefully (no error to the user).

**Document OCR:**
- Mistral Document AI (`/v1/ocr`).
  - SDK/Client: hand-rolled `internal/storage/sources/ocr/client.go` (Bearer auth, JSON POST, ≤256-char snippets on error, 256-MiB response cap).
  - Auth: `MISTRAL_API_KEY` (SQLite `secrets.mistral_api_key`; routed through 3-way wiring per memory `feedback_secret_settings_routing_pattern`).
  - Endpoint: `MISTRAL_OCR_BASE_URL` (default `https://api.mistral.ai/v1`).
  - Model: `MISTRAL_OCR_MODEL` (default `mistral-ocr-latest`).
  - Tuning: `MISTRAL_OCR_TABLE_FORMAT` (default `markdown`), `MISTRAL_OCR_EXTRACT_HEADER`, `MISTRAL_OCR_EXTRACT_FOOTER`.
  - Caps: `OCR_MAX_PAGES=500`, `OCR_MAX_FILE_MB=100`.
  - Output target: `wiki/raw/src_<id>/ocr.md`, rendered via `internal/storage/sources/ocr/render.go`.

**Document conversion (DOCX/XLSX/PPTX/etc.):**
- Microsoft `markitdown-mcp` sidecar (`aura-markitdown`).
  - Protocol: JSON-RPC 2.0 over Streamable HTTP (MCP).
  - Endpoint: `MARKITDOWN_URL` (default `http://aura-markitdown:3001`).
  - Tool: `convert_to_markdown` (single tool advertised; `internal/storage/sources/markitdown/client.go:33`).
  - Wire format: source bytes encoded as `data:<mime>;base64,<...>` URI.
  - Caps: `DefaultMaxBytes = 64 MiB`, `DefaultTimeout = 120 s` (`internal/storage/sources/markitdown/client.go:21-30`).
  - Supported formats: docx, pdf, pptx, xls, xlsx, outlook (msg). Image/audio converters intentionally NOT enabled — Aura uses Mistral OCR + Whisper instead.
  - Aura-local converter plugins co-installed: `docker/markitdown/aura-plugins/`.

**Speech-to-text (audio in):**
- `whisper.cpp` HTTP server sidecar (`aura-whisper`).
  - Binary: `whisper-server` from `ggerganov/whisper.cpp` v1.8.4, CMake-built at `build/bin/whisper-server` (`docker/whisper/Dockerfile`).
  - Model: `ggml-small.bin` (487 MB) pinned at SHA-256 in `internal/install/whisper.go:22`.
  - Source: `https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.bin`.
  - Endpoint: `WHISPER_BASE_URL` (default `http://aura-whisper:8082`).
  - Wire: multipart POST to `/inference`. Language hint via `WHISPER_LANGUAGE` (default `it`).
  - Client: `internal/llm/whisper/client.go`.
  - Transcoding: ffmpeg shells out before posting (whisper-server only accepts 16 kHz mono WAV); `Client.Transcode` is injectable for tests.
  - Threads: 4 (matches mini-PC CPU budget per memory `feedback_minipc_cpu_budget`).

**Text-to-speech (audio out):**
- Kyutai `pocket-tts` HTTP server sidecar (`aura-pocket-tts`).
  - Endpoint: `POCKETTTS_BASE_URL` (default `http://aura-pocket-tts:8000`; host port 8084).
  - Wire: multipart POST to `/tts` (text + voice), `/health` for status. Returns `audio/wav` (chunked).
  - Client: `internal/llm/pockettts/client.go`.
  - Default voice: `giovanni`, language `italian_24l` (selected 2026-05-20 per memory `project_2026-05-20_wave3_tts_decision_pocket_tts`).
  - Models auto-fetched from HuggingFace on first boot (~600 MB) into the `pockettts-hf-cache` volume.
  - 8 MiB response cap (~170 s of 24 kHz mono audio).
  - Default OFF — `AURA_TTS_ENABLED=false` in `compose.yaml`; per-chat `/voice on|tts|off` command toggles it (`internal/audio/policy.go`).

**Web search:**
- SearXNG self-hosted (`searxng` container, `docker.io/searxng/searxng:latest`).
  - Endpoint: `SEARXNG_BASE_URL` (default `http://searxng:8080` in compose, `http://127.0.0.1:8088` for host probes).
  - Provider toggle: `WEB_SEARCH_PROVIDER` (`searxng` enabled in compose; `disabled` is the codebase default).
  - Client: `internal/agent/tools/registry/searxng.go`, `internal/agent/tools/registry/web.go`, `internal/agent/tools/registry/web_common.go`.
  - Config: `data/secrets/searxng/` mount (read-only inside the container).

**Web fetch:**
- Direct outbound HTTPS via stdlib `net/http`, hardened by an SSRF gate at the tool boundary (`internal/agent/tools/registry/direct_fetch.go`, `internal/agent/tools/registry/web.go`).
- HTML→Markdown via `github.com/JohannesKaufmann/html-to-markdown/v2`; article extraction via `github.com/go-shiori/go-readability`.

**Headless browser (probes only):**
- `github.com/chromedp/chromedp` — used by `cmd/probe_telegram_ui` against `https://web.telegram.org/a/` via CDP. Not part of the runtime; `scripts/launch_chrome_cdp.ps1` is the launcher.

## Data Storage

**Databases:**
- SQLite (the only durable runtime store).
  - Path: `DB_PATH` (default `/data/aura.db` in container, `./aura.db` locally).
  - Driver: `modernc.org/sqlite` v1.50.0 (CGO-free pure-Go).
  - Journal: `AURA_SQLITE_JOURNAL_MODE=DELETE` in container (WAL avoided on Windows bind-mounts — known corruption per memory `feedback_sqlite_wal_windows_corruption`).
  - Migrations: `internal/db/migrations/m01_*.go` … `m29_*.go` (29 numbered migrations as of 2026-05-27; runner in `internal/db/migrations/migrations.go`).
  - Tables (non-exhaustive): `api_tokens`, `pending_users`, `allowed_users`, `secrets`, `scheduled_tasks`, `conversations`, `conversation_compactions`, `proposed_updates`, `wiki_issues`, `agent_notes`, `embed_cache`, `tool_attempts`, `run_events`, `tool_index_state`, `voice_dispatches`, `chat_settings`, `compact_memory`, `proposed_updates`, `prompt_health_views`, `tokenjuice_runs`, plus FTS5 inverted indexes on wiki/conversations.

**Vector DB:**
- Qdrant (`qdrant/qdrant:latest` sidecar).
  - Endpoint: `QDRANT_URL` (default `http://qdrant:6333`).
  - Collection: `QDRANT_COLLECTION` (default `aura_memory_v1`).
  - Auth: `QDRANT_API_KEY` (optional; empty for the local sidecar).
  - Client: `internal/storage/qdrant/client.go` (REST, no gRPC).
  - Operations used: `Health`, `Search`, `Upsert`, `Delete`, `CreateCollection`, `DeleteCollection`, `CollectionInfo`, `ScrollSlugs` (`internal/storage/qdrant/client.go:21-33`).
  - Vector size: matches `EMBEDDING_OUTPUT_DIM` (256 in production).
  - Boot-time guard: if EMBEDDING_OUTPUT_DIM changes, automatic rebuild fires unless `AURA_NO_REBUILD_ON_DIM_MISMATCH=true`.
  - Downstream callers: `internal/storage/search/compact_qdrant.go`, `internal/storage/memoryindex/`, `internal/storage/reindex/`.
  - Degrades gracefully — sidecar failure does NOT block aura boot (no `depends_on.condition: healthy` in compose).

**File Storage:**
- Wiki: local filesystem under `WIKI_PATH` (Markdown + YAML frontmatter + `.git/` tracking via go-git). Atomic temp-file + rename, per-page mutex. Default `/wiki` in container, `./runtime-workspace/wiki` locally.
- Sources (uploaded PDFs and extracted artifacts): `SOURCES_PATH` (default `/workspace/sources`). Split from `WIKI_PATH/raw/` on 2026-05-23 so operators can browse wiki without bumping into raw artifacts.
- Skills: `SKILLS_PATH` (default `/skills` in container). Each skill is a directory with a `SKILL.md` (Anthropic skill format with YAML frontmatter).
- Logs: `LOG_DIR` (default `/data/logs`).

**Object Storage (backups / exports):**
- Garage (`dxflrs/garage:v2.3.0` sidecar) — S3-compatible single-node deployment, fronted via the AWS SDK v2.
  - Endpoint: `GARAGE_S3_ENDPOINT` (default `http://garage:3900`).
  - Region: `GARAGE_S3_REGION` (default `garage`).
  - Bucket: `GARAGE_S3_BUCKET` (default `aura-artifacts`).
  - Auth: `GARAGE_S3_ACCESS_KEY` / `GARAGE_S3_SECRET_KEY` (or `*_KEY_FILE` pointing at decrypted secrets), provisioned by `garage-init` sidecar on first boot.
  - Client: `internal/backup/s3.go` (`aws-sdk-go-v2/service/s3`).
  - Use: backup exports + ingested-source archival (`internal/backup/export.go`).
  - Admin UI: `garage-webui` (`khairul169/garage-webui:1.1.0`), profile-gated — `docker compose --profile garage-ui`.

**Caching:**
- In-process: LLM provider cache support detection (`internal/llm/cache.go`), tool description overrides, MCP tool lists.
- SQLite-backed: `embed_cache` (embedding reuse), `tokenjuice_runs` (compactor stats).
- Volume-backed: `searxng-cache` (web search corpus), `pockettts-hf-cache` (HF model cache), `qdrant-storage` (vector index), `go-mod-cache` + `go-build-cache` (test container compile cache).

## Authentication & Identity

**Issuance channel — Telegram:**
- The dashboard token is minted in-Telegram only after the user passes the allowlist check (`internal/telegram/commands.go`, `internal/api/auth/store_tokens.go`). Plaintext never traverses an unauthenticated HTTP path (`internal/api/auth/store.go:1-13`).

**Token storage:**
- SHA-256 hash only — plaintext leaves the process exactly once. Lookup uses `crypto/subtle.ConstantTimeCompare` (`internal/api/auth/store.go:5-13`).
- Table: `api_tokens` (`internal/db/migrations/m01_*.go` + `m03_add_api_token_expiry.go`).
- TTL: `DASHBOARD_TOKEN_TTL_HOURS` (default 720 = 30 days).
- 401 → `/login?expired=1` (frontend handles redirect; auth middleware in `internal/api/auth/middleware.go`).

**Allowlist sources:**
- `auth.SourceTelegramBootstrap` — first-run owner claim.
- `auth.SourceTelegramConfiguredAllowlist` — env-allowlisted users (`internal/api/auth/store.go:47-50`).

**No external IdP** — Aura is a single-user / small-allowlist personal-scale system. There is no OAuth, no SAML, no SSO.

## Monitoring & Observability

**Logging:**
- `go.uber.org/zap` v1.28.0 with a slog-compatible adapter (`internal/logging/zap_slog.go`).
- Daily-rotating file sink (`internal/logging/daily_writer.go`).
- Secret sanitization layer (`internal/logging/sanitize.go`) strips known secret patterns before any log line is written.
- Per CLAUDE.md "Tool argument privacy" rule: only tool names + argument KEYS are logged — never values, URLs with tokens, base64, or source text.

**Error tracking:**
- None (no Sentry, Rollbar, etc.). Errors land in the local log file under `LOG_DIR`.

**Metrics:**
- Internal `/metrics`-style handler at `internal/api/metrics_handler.go` (token/cost counters, conversation budget, agent loop steps).
- Probe harnesses (`cmd/probe_chat`, `cmd/quality_bench`) export latency + tool-count + content assertions (per CLAUDE.md `VALIDATE WITH VERIFIED BENCHMARKS`).
- Container healthchecks: aura via `wget /health`, sidecars via service-specific endpoints (`compose.yaml:329`, `:373`).

**Trace retention:**
- `AURA_TRACE_RETENTION_DAYS=30` — run-events / tool-attempts older than this are swept.

## CI/CD & Deployment

**Hosting:**
- Self-hosted Docker Compose stack — single host (mini-PC profile validated). No managed cloud deploy target.
- LAN exposure pattern: dev uses `0.0.0.0:18080` (`compose.yaml:115`); production overlay (`compose.prod.yaml:24`) clamps to `127.0.0.1:8080` for SSH-tunnel-only access.

**CI Pipeline:**
- GitHub Actions — `.github/workflows/ci.yml`.
  - Job `test`: depguard, file-size guard, `go vet`, `go build`, Phase-2 regression PowerShell, `go test -race -count=1`.
  - Job `deadcode`: `golang.org/x/tools/cmd/deadcode` against `docs/deadcode-baseline-2026-05-22.json`.
  - Job `frontend`: `npm install` + `npm run build`.
- GitHub Actions — `.github/workflows/docker-image.yml`.
  - Trigger: push of `v*` tag, or `workflow_dispatch`.
  - Builds + pushes 4 images to GHCR: `ghcr.io/chetto1983/aura`, `aura-whisper`, `aura-pocket-tts`, `aura-markitdown`.
  - Multi-arch: amd64 + arm64 (whisper amd64-only due to whisper.cpp arm64 cross-build issue).
  - Cache: GHCR `buildcache` tag via Buildx.
  - Provenance + SBOM attestations enabled.

## Environment Configuration

**Required env vars (minimum to boot):**
- `TELEGRAM_TOKEN` — Telegram Bot API token (else first-run setup wizard takes over).
- `LLM_API_KEY` + `LLM_BASE_URL` + `LLM_MODEL` — for any actual conversational ability (echo mode without).

**Secrets location:**
- Decrypted at boot by the `aura-secrets` sidecar (`docker/secrets/init-secrets.sh`) into `./data/secrets/` (host) → `/data/secrets/` (container).
- Material expected: `garage_s3_access_key`, `garage_s3_secret_key`, `searxng/*`, `garage.toml`.
- Runtime secrets in SQLite `secrets` table (key list in `internal/secrets/store.go:17-25`).
- `.env` file at repo root for `docker compose` variable interpolation (contents not inspected per security policy).

## Webhooks & Callbacks

**Incoming:**
- Telegram: long-polling via `gopkg.in/telebot.v4` — NO webhook endpoint exposed.
- Dashboard REST API: see `internal/api/router.go` (`/api/wiki`, `/api/sources`, `/api/tasks`, `/api/skills`, `/api/mcp`, `/api/conversations`, `/api/maintenance`, `/api/chat`, `/health`). Not classic webhooks — these are user-driven dashboard fetches.
- First-run setup wizard: loopback-only HTTP at boot when unbootstrapped (`internal/api/setup_server.go`).

**Outgoing:**
- All outbound is request/response (LLM API, Mistral OCR, SearxNG search, web fetch, HuggingFace model download). Aura does NOT push webhooks anywhere.

## MCP Servers (Model Context Protocol)

**Transport support:**
- stdio (subprocess + stdin/stdout JSON-RPC 2.0): `internal/mcp/client.go:76-87` via `newStdioTransport`.
- Streamable HTTP (JSON-RPC 2.0 over HTTP): `internal/mcp/client.go:89-98` via `newHTTPTransport`.
- Protocol version: `2025-03-26` (`internal/mcp/client.go:25`).
- Client identifies as `aura/3.0`.

**Configuration:**
- File: `MCP_SERVERS_PATH` (default `./mcp.json` locally, `/data/mcp.json` in container, `/workspace/mcp.json` in dev compose mount).
- Example: `mcp.example.json` (3 servers — calculator, mail, database).
- Dev workspace file: `runtime-workspace/mcp.json` (calculator only).
- Hot-reload: fsnotify watcher rebuilds the tool index when `mcp.json` changes (`internal/mcp/watcher.go`, reconciler in `internal/storage/...`).

**Servers shipped in the Aura image:**

| Server | Binary | Source | Transport | Tools exposed |
|--------|--------|--------|-----------|---------------|
| `calculator` | `/usr/local/bin/aura-calculator-mcp` | POSIX shell script wrapping `uvx` calculator-mcp (`Dockerfile:96-98`, `runtime/mcp/aura-calculator-mcp`) | stdio | SymPy + NumPy + SciPy expression eval |
| `mail` | `/usr/local/bin/mail-mcp` v0.4.5 | `tecnologicachile/mail-mcp` GitHub release, SHA-256 pinned (`Dockerfile:100-105`) | stdio | IMAP + SMTP, requires `MAIL_IMAP_DEFAULT_*` env |
| `database` | `/usr/local/bin/ea-database-server` v1.1.0 | `@executeautomation/database-server` npm (`Dockerfile:18-22`) | stdio | SQL execution against a configured SQLite path |
| `markitdown` (not in mcp.json by default — wired directly via `internal/storage/sources/markitdown/`) | `aura-markitdown` sidecar | `markitdown-mcp` (Streamable HTTP, `/mcp` path) | HTTP | `convert_to_markdown` |

**Naming convention:** MCP tools surface to the LLM as `mcp_<server>_<toolname>` (`internal/agent/tools/registry/mcp.go`).

**Override layer:** the dashboard can override individual tool descriptions to bias routing (`Client.ApplyToolDescriptionOverrides` in `internal/mcp/client.go:128-146`).

**Bootstrap behaviour:** an MCP server failure is a WARNING, not a fatal boot error — Aura starts without it and the dashboard shows the failure (`internal/api/mcp.go`, `internal/api/mcp_setup.go`).

## Sidecar Services Summary

| Service | Image | Host port | Container port | Role |
|---------|-------|-----------|----------------|------|
| `aura` | `aura:local` (built from `Dockerfile`) | `0.0.0.0:18080` (dev) / `127.0.0.1:8080` (prod) | 8080 | Main Go binary — Telegram bot + dashboard + tool runtime |
| `aura-secrets` | `alpine:3.21` | — | — | One-shot: decrypts secrets into `./data/secrets/` |
| `aura-init-models` | `aura-init-models:local` | — | — | One-shot: fetches + SHA-verifies embedding + Whisper GGUFs into `./data/` |
| `aura-llama-embed` | `ghcr.io/ggml-org/llama.cpp:server` (or `server-cuda`) | `127.0.0.1:8081` | 8080 | Embedding HTTP server (OpenAI-compatible `/v1/embeddings`) |
| `aura-markitdown` | `aura-markitdown:local` | `127.0.0.1:3001` | 3001 | MCP Streamable HTTP — DOCX/XLSX/PPTX/PDF→Markdown |
| `aura-whisper` | `aura-whisper:local` | `127.0.0.1:8082` | 8082 | whisper.cpp HTTP — audio transcription |
| `aura-pocket-tts` | `aura-pocket-tts:local` | `127.0.0.1:8084` | 8000 | Italian TTS — `pocket-tts` HTTP server |
| `searxng` | `docker.io/searxng/searxng:latest` | `127.0.0.1:8088` | 8080 | Self-hosted metasearch |
| `qdrant` | `qdrant/qdrant:latest` | `127.0.0.1:6333` + `6334` (gRPC) | 6333 + 6334 | Vector database |
| `garage` | `dxflrs/garage:v2.3.0` | `127.0.0.1:3900` | 3900 | S3-compatible object store |
| `garage-init` | `aura-garage-init:local` | — | — | One-shot: registers S3 access key + bucket |
| `garage-webui` | `khairul169/garage-webui:1.1.0` (profile `garage-ui`) | `127.0.0.1:3909` | 3909 | Garage admin web UI |
| `test` | built from `Dockerfile.test` (profile `test`) | — | — | One-shot: runs `go test ./...` |

---

*Integration audit: 2026-05-27*
