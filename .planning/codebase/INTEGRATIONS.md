---
last_mapped_commit: 26745a062dd1017c8e9de39a39089bc63559b553
---

# External Integrations

**Analysis Date:** 2026-08-13

## APIs & External Services

**LLM providers (chat/completion):**
- **OpenRouter** (default/production) — `internal/llm/openai_compat` implements an OpenAI-compatible streaming client (`internal/llm/client.go` defines the provider-neutral `Client`/`Request`/`Chunk` interface). Config resolves via a 4-tier load order in `internal/llm/config.go:199-272`: built-in default (`deepseek/deepseek-v4-flash:nitro`, `internal/llm/config.go:20`) < `.env` (`OPENROUTER_API_KEY`) < `~/.aura/llm.json` < `AURA_LLM_*` env. Requests send OpenRouter attribution headers (`HTTP-Referer`, `X-Title`, `internal/llm/config.go:49-52`) and a `SessionID` sticky-routing key for prompt-cache locality (`internal/llm/client.go:110-113`). An opt-in `AURA_LLM_OPENROUTER_MIDDLE_OUT` transform is a last-resort overflow belt (`internal/llm/config.go:134-143`).
  - Auth: `OPENROUTER_API_KEY` (upstream naming, not `AURA_*`)
  - Base URL default: `https://openrouter.ai/api/v1`
- **Local llama.cpp chat LLM** (opt-in) — `aura-llm` compose service (`compose.yaml:1104-1215`, `profiles: [localllm]`), gemma-4-12B-it QAT GGUF plus its MTP drafter served OpenAI-compatible on port 8084; enabled by pointing `AURA_LLM_BASE_URL`/`AURA_LLM_MODEL` at it. Same `openai_compat` client code path as OpenRouter — the client is provider-neutral by design (`internal/llm/client.go:1-5`).
  - No Anthropic-direct wire client was found in `internal/llm` at this commit; `Request.ToolsCacheControl` is a dormant field reserved for a future Anthropic-native branch (`internal/llm/client.go:114-119`, "Slice 13 LLMRouter").
- **Adaptive reasoning / thinking-token control** — `internal/llm/reasoning_target.go`, `internal/llm/model_reasoning_caps.go` project `ReasoningConfig` (effort levels `max`/`xhigh`/`high`/`medium`/`low`/`minimal`/`none`) onto OpenRouter's `reasoning.effort` wire field or llama.cpp's own `--reasoning`/`--reasoning-budget` server flags depending on target.
- **Completion critic gate** (amendment #54/D-43) — a second LLM call (`AURA_COMPLETION_CRITIC_MODEL`, defaults to the loop model) verifies a voluntary-termination turn that mutated host state before accepting it; default ON (`internal/llm/config.go:46-47,257-261`).

**Embedding stack:**
- Local llama.cpp OpenAI-compatible `/v1/embeddings` sidecar, `aura-llama-embed` compose service (`compose.yaml:636-708`) running `ggml-org/llama.cpp:server-cuda` with model `embeddinggemma-300M-Q8_0.gguf` (EmbeddingGemma-300M, Q8_0 quant, 768 dimensions). Pre-fetched into a cache volume at build/appliance-install time — no `--hf-repo` at boot, so there is no first-run network dependency for the sidecar itself.
- Client: `internal/embeddings/client.go` — `Client.Embed` batches requests (`DefaultBatchSize=32`), truncates/renormalizes Matryoshka (MRL) vectors to the configured dimension (`TruncateMRL`, `internal/embeddings/client.go:196-222`), and is consumed as the `Embedder` interface by memory, documents, and the reasoning classifier.
- Pooling is model-declared (mean pooling baked into the GGUF, `gemma-embedding.pooling_type=1`) — no `--pooling` flag is passed (`compose.yaml:651-663`); the compose comment documents a measured recall@1 regression (0.90→0.70) from a stale `last`-pooling override on a prior model.
- Context ceiling is 2048 tokens, hard (`compose.yaml:671-686`, GGUF-declared `context_length=2048`); the document chunker (`AURA_DOCUMENT_CHUNK_MAX_TOKENS`, default 512) and `AURA_DOCUMENT_CHUNK_TOKENIZER=google/embeddinggemma-300m` must stay under this.
- The same embedder is reused by `cmd/arcadedb-mcp` (`arcadedb.NewSidecarEmbedder`, `cmd/arcadedb-mcp/main.go:65-77`) for the memory dense-retrieval leg, and is optional there — a nil embedder (empty `AURA_EMBED_BASE_URL`) degrades to lexical-only retrieval per call, never at boot.
- Cloud embedding: none found — `internal/embeddings` has no provider branch for a hosted embeddings API; the model contract is local-only (CI verifies via `scripts/fetch_embedding_model_test.sh` / `make embedding-model-contract`).

**Web search / fetch:**
- **SearXNG** — self-hosted metasearch (`searxng` compose service, `compose.yaml:812-841`, image `searxng/searxng:2026.7.26-b060c780d`), reached at `SEARXNG_URL=http://searxng:8080/search`. The agent's `web_search` tool (`internal/agent/tools/web_search.go:19,58`, `Deferred:true`) is a thin adapter over a shared `web.Client`.
- Web fetch/readability extraction: `codeberg.org/readeck/go-readability/v2` + `github.com/JohannesKaufmann/html-to-markdown/v2` power the agent's `web_fetch` tool (`internal/agent/tools/web_fetch.go`), bounded by `AURA_WEB_FETCH_MAX_BODY_BYTES`/`AURA_WEB_FETCH_TIMEOUT_SEC`/`AURA_WEB_DNS_PIN_TTL_SEC` (DNS-pinning against SSRF/rebind).

**Multimodal sidecars (STT / TTS / vision-OCR):**
- Single shared HTTP client package `internal/multimodal/client.go` — "ZERO Go ML here" (`internal/multimodal/client.go:15-17`); every modality is an OpenAI-compatible sidecar call, with a per-modality local↔cloud swap.
- **Vision/OCR**: `internal/multimodal/vision.go` — local default is `aura-ocr-vl` (compose `profiles:[ocr]`, GLM-OCR GGUF on llama.cpp-cuda, port 8082, `compose.yaml:902-959`); `AURA_VISION_CLOUD=true` routes to OpenRouter, picking `Model` (if `SupportsVision`) else `FallbackModel` (`MULTIMODAL_FALLBACK_MODEL`, default `minimax/minimax-m3`) — never routes an image to a non-vision model.
- **STT**: `internal/multimodal/stt.go` — local default is `aura-stt` (hwdsl2/whisper-server, faster-whisper `large-v3-turbo`, port 9000, multipart upload, decodes OGG/Opus inline); cloud arm (unset `STT_LocalBaseURL`/set `CloudModel`) sends base64 JSON to OpenRouter (rejects multipart).
- **TTS**: `internal/multimodal/tts.go` — local `aura-tts` (Kokoro-82M via `ghcr.io/remsky/kokoro-fastapi-cpu`, port 8880, voice `if_sara`, opus output).
- All three share the single `OPENROUTER_API_KEY` when the cloud arm is used (`MULTIMODAL_BASE_URL`, `STT_BASE_URL`, `TTS_BASE_URL` point at the local sidecars by default).

## Data Storage

**Databases:**
- **PostgreSQL 18** (`postgres:18.4-alpine3.24`, `compose.yaml:457-476`) — the control plane: schema `aura.*`, `sqlc`-generated typed client (`internal/db/sqlc/`, 31 files), `golang-migrate/migrate/v4` migrations (`internal/db/migrations/`, 94 numbered migrations as of `0094_verification_evidence`). Three role-separated connection strings: `AURA_DB_URL` (`aura_app` role, request path), `AURA_DB_MIGRATE_URL` (`aura_migrate` role, DDL), `AURA_DB_BOOTSTRAP_URL` (superuser bootstrap) — same split pattern reused for ArcadeDB below.
  - Row-level security: `internal/db/rls.go` — multi-tenant isolation enforced at the Postgres layer, not just in application WHERE clauses (`rls_integration_test.go`, `rls_seed_test.go` present across several packages: `agui`, `gateway`, `telegram`).
- **ArcadeDB 26.7.3** (`arcadedata/arcadedb:26.7.3`, `compose.yaml:506-567`) — long-term bitemporal memory (facts with `valid_from`/`valid_to` + supersede), native vector index (`LSM_VECTOR`) and full-text search in the same engine. **One database per identity** (`mem_<uuid>`), created on first use by the tenant resolver (`cmd/arcadedb-mcp/main.go:78-101`), never at boot. Requires ≥26.4.2 (CVE-2026-44221 gate, `VerifySecureVersion` reiects lower versions). HTTP API (`:2480`) is Aura's own client's transport (`internal/arcadedb/client.go` — chosen over ArcadeDB's *own* MCP server because that one's `query` tool has no bind parameters and only 10 generic tools). A Bolt plugin (`:7687`) is also enlisted — load-bearing for the Python ingest sidecar, which writes via CocoIndex's stock Neo4j/Bolt connector rather than a bespoke writer.
  - Credential derivation: `internal/arcadedb/tenant.go` — per-tenant password is `base64(HMAC-SHA256(AURA_ARCADEDB_TENANT_SECRET, database_name))`, never stored (`internal/arcadedb/tenant.go:69-77,109-112`). Two credential tiers: `ARCADEDB_APP_USER`/`ARCADEDB_APP_PASSWORD` (per-database, scoped) and `ARCADEDB_ADMIN_USER=root`/`ARCADEDB_PASSWORD` (server-level, DDL/database-create/drop only).
  - Backup: ArcadeDB's own `AutoBackupSchedulerPlugin` (`docker/arcadedb/backup.json`), hot/non-blocking, tiered retention (7 daily / 4 weekly / 6 monthly) — chosen because nothing outside ArcadeDB knows the full tenant-database list.

**File Storage:**
- **Garage** (`dxflrs/garage:v2.3.0`, `compose.yaml:481-496`) — S3-compatible self-hosted object storage, the asset backend (`AURA_OBJECTSTORE_BACKEND=garage`). Go client: `internal/objectstore/s3.go`, built on `aws-sdk-go-v2`'s `s3.Client`/`s3.PresignClient` with a custom `EndpointResolverV2` pointing at the Garage endpoint and `RequestChecksumCalculationWhenRequired`/`ResponseChecksumValidationWhenRequired` (Garage rejects the SDK's newer default streaming-checksum mode with `400 InvalidDigest`).
  - Presigned PUT uploads (`PresignPut`, `internal/objectstore/s3.go:71-98`), `AURA_ASSET_PRESIGN_TTL_SEC` default 600s.
  - Admin API: `internal/objectstore/garageadmin/client.go` — Garage Admin API v2, bearer-token-authenticated (`GARAGE_ADMIN_TOKEN`), reachable only on the internal compose network (`garage:3903`, never host-published) — used by the daemon's provisioning saga for bucket/key lifecycle.
  - Bootstrap: `garage-bootstrap` one-shot compose service runs `aura-garage-bootstrap.sh && aura objectstore bootstrap` before Aura starts.
  - `internal/objectstore/filesystem.go` and `internal/objectstore/fake.go` exist as alternate/test backends alongside `s3.go`.
- Local filesystem artifacts: `$AURA_RUN_DIR/` (tool-result sidecars + spillover), `~/.aura/agents/<id>/` (Agent.md profile), `~/.aura/pyscripts/<id>/` (skill snippets), `$AURA_SKILLS_DIR/` (skill instructions), `$AURA_WORKSPACE_DIR` (fixed `/workspace` root for `shell_exec`/`fs_*` tools, bootstrapped by the container entrypoint, `compose.yaml:30-34`).

**Caching:**
- No external cache service (Redis is present only as an **indirect** transitive dependency of Authula's message-bus adapters, `go.mod:175`, and is not wired into any Aura-owned package under `internal/`).
- Warm package caches (npm/pip/uv) are Docker named volumes mounted into the `aura` container for the host-direct `shell_exec` path (`compose.yaml:327-329`), not an application cache.

## Authentication & Identity

**Auth Provider:**
- **Authula** (`github.com/Authula/authula` v1.40.0) — embedded (in-process, not a separate service) auth/session provider, wired through `internal/webauth/authula.go`. Backed by its own Postgres tables (`AURA_AUTHULA_DATABASE_URL`, falls back to the main `AURA_DB_URL` when unset) and secret (`AURA_AUTHULA_SECRET`). Rate-limited (`AURA_AUTHULA_RATE_LIMIT_MAX`, default 30) and supports a designated operator identity (`AURA_AUTHULA_OPERATOR_IDENTITY`).
- `AURA_WEB_AUTH_PROVIDER` defaults to `authula`; `AURA_WEB_TRUST_PROXY` gates whether the cockpit's `0.0.0.0:9080` bind is allowed to boot without Authula configured (reverse-proxy-trusted alternative).
- Session validation: `internal/webauth/session_validate.go`; identity linking (e.g. Telegram account ↔ web identity): `internal/webauth/identity_link.go`.
- MUSR (multi-user single-role) isolation is a separate deployment-time flag (`AURA_MUSR_ISOLATION`), gating whether onboarding provisions more than one identity — independent of which auth provider is configured.

## Monitoring & Observability

**Error Tracking:**
- None found — no Sentry/Rollbar/Bugsnag-style SDK in `go.mod`.

**Metrics & Tracing:**
- **OpenTelemetry** (`go.opentelemetry.io/otel` v1.45.0 family) — `internal/obs`, `internal/tracesink`; exporter selectable via `AURA_OTEL_EXPORTER` (`none` default) and `AURA_OTEL_ENDPOINT` (OTLP gRPC target, default `localhost:4317`), or `stdouttrace` for local debug.
- **Prometheus** (`github.com/prometheus/client_golang`) — `/metrics` exposition mounted in `internal/agui/server.go` (`promhttp` import), scraped by the optional `prometheus` compose service (`profiles:[observability]`, shares `aura`'s network namespace so the loopback metrics listener stays private).
- **Tempo** (`grafana/tempo:2.9.4`) — trace storage, same `observability` profile, receives via OTLP.
- **Grafana** (`grafana/grafana:12.3.9`) — dashboards, published on loopback only (`AURA_GRAFANA_PORT`, default 3000), anonymous-viewer auth enabled for local convenience (`GF_AUTH_ANONYMOUS_ENABLED=true`), depends on both Prometheus and Tempo being healthy.
- Cache-hit/spend metrics: `internal/cachemetrics` tracks LLM prompt-cache reads for cost accounting.

**Logs:**
- Structured logging via stdlib `log/slog` throughout (e.g. `cmd/arcadedb-mcp/main.go:41`, JSON handler to stderr); no external log-shipping integration found.

## CI/CD & Deployment

**Hosting:**
- Self-hosted Docker Compose deployment (`compose.yaml`, service name `aura`), fronted by Caddy 2 for HTTPS ingress (`caddy` service, `AURA_ACCESS_TOKEN`-gated).
- Container registry: **GHCR** (`ghcr.io`) — the base runtime pulls `ghcr.io/ggml-org/llama.cpp:server-cuda` (embed/LLM/OCR sidecars), `ghcr.io/remsky/kokoro-fastapi-cpu` (TTS), and two Aura-authored sibling projects published from their own forks: `ghcr.io/chetto1983/whatsapp-mcp:sha-e0b8345` (pinned by commit off `main`) and `ghcr.io/chetto1983/aura-pim-mcp:10383276961828bc19f34a9372ba2c64a14e2b62` (pinned by commit off `aura/pim-sidecar`).

**CI Pipeline:**
- **GitHub Actions**, workflows in `.github/workflows/`: `ci.yml` (build/lint/vet/deadcode/file-size gates, unit tests with `-race`, capability-eval, and more jobs beyond what was inspected), `codeql.yml` (CodeQL static analysis), `production-readiness.yml`, `release.yml`, `retire-aura-images.yml`, `skills.yml` (skills subsystem gate: unit + `db_integration` + fuzz + mutation + coverage, `.github/workflows/skills.yml:35`).
- CI runs on `ubuntu-latest` runners; Go toolchain pinned via `go-version-file: go.mod`.

## Environment Configuration

**Required env vars (fail-fast, `:?required` in `compose.yaml` or `ErrMissingAPIKey`-style Go checks):**
- `POSTGRES_PASSWORD`, `AURA_ACCESS_TOKEN`, `AURA_AUTHULA_SECRET`, `ARCADEDB_PASSWORD`, `ARCADEDB_APP_PASSWORD`, `AURA_ARCADEDB_TENANT_SECRET`, `AURA_OBJECTSTORE_ACCESS_KEY`/`AURA_OBJECTSTORE_SECRET_KEY`, `GARAGE_RPC_SECRET`, `AURA_GARAGE_ADMIN_TOKEN`, `AURA_EMBED_REVISION`/`AURA_EMBED_FINGERPRINT` (model-contract pinning), `SEARXNG_SECRET`, `AURA_PIM_MCP_ADMIN_TOKEN`
- Optional/default-empty upstream credentials: `OPENROUTER_API_KEY`, `TELEGRAM_BOT_TOKEN`

**Secrets location:**
- `.env` at repo root (not committed; feeds both `compose.yaml` interpolation and `godotenv.Load()` in Go — file existence noted, contents not read for this analysis)
- `~/.aura/llm.json` — optional operator LLM-config override (may carry `api_key`)
- `aura.settings` (Postgres, migration 0024) — cockpit-editable model-backend overlay (`internal/settings/settings.go:1-13`); `OverlayEnv` applies an **allowlisted** subset of model-backend keys onto the process environment before `config.Load` runs at boot, so a DB row can never clobber `POSTGRES_*`/`ARCADEDB_PASSWORD`/`AURA_WEB_AUTH_SECRET`

## Webhooks & Callbacks

**Incoming:**
- **Telegram**: long-polling only at this commit — `internal/channels/telegram/bot.go:289-294` calls `bot.Start()` (telebot's default `LongPoller`), not a registered webhook endpoint. `TELEGRAM_API_BASE_URL`/`TELEGRAM_FILE_BASE_URL`/`AURA_TELEGRAM_LOCAL_BOT_API` exist for pointing at a local Bot API server, but no `SetWebhook`/webhook HTTP handler was found.
- **PIM sidecar OAuth callback**: `<AURA_PIM_EXTERNAL_BASE_URL>/admin/auth/google/callback` — the Go daemon's `/api/connect/pim/admin/*` proxy forwards to the sidecar's token-gated admin API for Google OAuth connect (Calendar/mail).
- **AG-UI**: `POST /agent/run` (`internal/agui/server.go`, body-size-capped at 1 MiB, `maxRunBodyBytes`) is the inbound run-trigger the cockpit (and any AG-UI-speaking client) POSTs to; responses stream back over Server-Sent Events.

**Outgoing:**
- Telegram bot API calls (send/edit message, send file/voice) via `gopkg.in/telebot.v4`.
- MCP servers Aura **consumes** as an HTTP/stdio client (`internal/mcp`, generic stdio JSON-RPC client + streamable-HTTP support): built-in catalog recipes in `internal/mcp/manager/catalog.go` are `memory` (Aura's own `arcadedb-mcp`, default-on, trusted), `calendar` (Aura PIM sidecar, forked `calendar-mcp`, mail+calendar+contacts, install-on-demand), `whatsapp` (forked `whatsmeow` bridge sibling, install-on-demand). A retired `calculator` recipe and retired standalone `mail` recipe are documented as removed in `catalog.go:98-111`. Operators may also declare arbitrary external MCP servers via `AURA_MCP_CONFIG`/`AURA_MCP_SERVERS_JSON`.
- Presigned S3 PUT/GET URLs issued to browser clients for direct Garage upload/download (bypassing the Go daemon for the transfer itself).

## MCP Servers — Hosted vs Consumed

**Hosted by Aura:**
- `cmd/arcadedb-mcp` (`aura-arcadedb-mcp` service, streamable-HTTP `:8096`) — the LLM-facing surface over ArcadeDB long-term memory. Tools (all namespaced `memory_*`/`graph_*`, `cmd/arcadedb-mcp/tool_*.go`): `memory_upsert_fact`, `memory_recall`, `memory_search`, `memory_facts_about`, `memory_entities`, `memory_digest`, `memory_merge_entities`, `memory_forget`, `memory_reembed`, `graph_schema`. Built with the official `github.com/modelcontextprotocol/go-sdk` (`mcp.AddTool`). One admin (root) client for database DDL only; every read/write goes through a per-tenant client so isolation is enforced server-side, never by an application WHERE clause.

**Consumed by Aura (mounted into the agent's tool registry):**
- `memory` → the same `arcadedb-mcp` above, reached over loopback/compose-DNS (self-consumption, not external)
- `calendar` → Aura PIM sidecar (`ghcr.io/chetto1983/aura-pim-mcp:10383276961828bc19f34a9372ba2c64a14e2b62`, forked calendar-mcp 1.4.1 @ `aura/pim-sidecar`), unified mail+calendar+contacts, streamable-HTTP
- `whatsapp` → `ghcr.io/chetto1983/whatsapp-mcp:sha-e0b8345` (forked `whatsmeow` bridge @ `main`), streamable-HTTP
- Generic stdio MCP servers declared via `AURA_MCP_CONFIG`/operator config (`internal/mcp/client.go` spawns any `command`+`args`+`env` server speaking newline-delimited JSON-RPC 2.0, protocol version `2024-11-05`)
- SSRF/egress policy (`internal/mcp/ssrf.go`, `internal/mcp/egress_policy.go`) and per-server trust classification (`internal/mcp/classify.go`) govern what a mounted server's tools are allowed to reach and how they are surfaced to the LLM

## Skills Integration

**Built-in Skill Tool:**
- `internal/agent/tools/skill.go` — defines the read-only `skill` tool (list, info, use actions), non-deferred and non-mutating. The manifest of installed skills is now rendered into the messages[1] always-block (via `skills.RenderAlwaysBlock`) rather than embedded in this tool's Description, protecting the prompt cache from invalidation on every skill add/remove. The tool carries a constant byte-stable description directing the model to read that always-block.
- `internal/agent/tools/skill_manage.go` — defines the `skill_manage` tool (create, update, delete, install, save_snippet, restore, archive actions), deferred and mutating, dispatches against the same underlying `SkillTool` loader/writer/alerter seams so writes are visible to reads in the same turn. The read tool is non-deferred to keep skill discovery cheap; the write tool is deferred to split the authoring grammar into a separate manifest entry (measured 1081 tokens as a floor, 74% of it prose per-action).
- Both tools dispatched through `internal/agent/tools/action.go` ActionRouter pattern (mirroring the `task` tool's architecture), with per-action error handling and no panic surface.
- Skill catalogue composition: `internal/skills/registry.go` → `Loader.ManifestDescription()` renders alphabetical installed-skills list (D-06, byte-stable) and `Loader.List()` ranks by BM25 when the manifest is too large for overflow (`skill_manage` action=list query=<query>).

## AG-UI / SSE Gateway

- `internal/agui` implements the AG-UI protocol server on top of `github.com/ag-ui-protocol/ag-ui/sdks/community/go` (`pkg/core/events`, `pkg/encoding/sse`). `internal/agui/client.go` re-exports `agui.Event`/`agui.EventType` so in-process consumers (the Telegram channel's `agui_subscriber.go`) never import the third-party SDK package directly.
- `internal/agui/server.go` — HTTP routing + `ServerConfig` (buffer cap, SSE heartbeat interval, readiness probes, detached-run knobs `AURA_AGUI_RUN_*`); `Runner` is a narrow consumer-side interface (`Turn`, `SubmitAnswers`, `SubmitAnswer`, `NewConversation`, `TurnBranch`, `DeleteConversationLifecycle`) satisfied by `*runner.Runner`.
- `internal/agui/server_sse.go` — the per-connection SSE pump: producer goroutine ranges the translated event stream into a cap-N buffered channel (`AURA_AGUI_BUFFER_CAP`, drop-on-full so a stalled client never blocks the agent loop), drain goroutine writes frames via the SDK's `sse.NewSSEWriter()`; an idle-keepalive `:hb` SSE comment (protocol-invisible) fires on `AURA_AGUI_SSE_HEARTBEAT_SEC` to defeat proxy/LB idle timeouts.
- `internal/agui/translator.go` — converts internal `agent.Event`s into AG-UI wire events (text/tool-call/reasoning/artifact deltas).
- Detached-run mode (fix-plan 1.3 Tier B, default ON): `AURA_AGUI_RUN_DETACH=true` runs turns via `context.WithoutCancel` with a bounded event-replay buffer (`AURA_AGUI_RUN_BUFFER_EVENTS`, default 2048) and linger window (`AURA_AGUI_RUN_LINGER_SEC`, default 180s), so an SSE client can disconnect/reconnect and resume mid-run rather than losing the turn.
- Governance/approval endpoints (`internal/agui/governance_api.go`, `approvals_api.go`) expose the HITL (`ask_user`) pause/resume surface and the tool-invocation approval center over the same server.

---

*Integration audit: 2026-08-13*
