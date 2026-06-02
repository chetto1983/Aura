# External Integrations

**Analysis Date:** 2026-05-28

> Aura is a tabula-rasa rewrite. This document distinguishes **Current implementation** (the 633-LOC Go skeleton present in HEAD) from **Planned target** (everything `prd.md` schedules across slices 0.5 → 13).

---

## APIs & External Services

### Current implementation

**LLM:**
- None. `cmd/aura/main.go:74` defines a `stubClient` that returns a hard-coded `text_response` tool call:
  ```go
  func (stubClient) Stream(_ context.Context, _ llm.Request) (<-chan llm.Chunk, error) {
      // returns canned "hello from tabula-rasa stub" via text_response
  }
  ```
- No HTTP client, no SSE parser, no SDK imports.

**Other integrations:** none. `internal/sandbox/sandbox.go` and `internal/swarm/swarm.go` only define `Stub` types whose methods return `"not yet implemented — see sandbox slice"` / `"not yet implemented — see swarm slice"`.

### Planned (per prd.md)

**LLM (remote, default):**
- **OpenRouter** — OpenAI-compatible chat completions endpoint at `https://openrouter.ai/api/v1`.
  - SDK/Client: hand-rolled `internal/llm/openai_compat/client.go` (no third-party SDK). SSE parser + tool-call delta accumulator (delta-merge per `index`).
  - Auth: `OPENROUTER_API_KEY` env (loaded via `.env` + `godotenv`), forwarded to `AURA_LLM_API_KEY`.
  - Default model: `deepseek/deepseek-v4-flash:exacto` (`AURA_LLM_MODEL`).
  - Recommended headers (sent by default): `HTTP-Referer: https://github.com/chetto1983/aura`, `X-Title: Aura` (for OpenRouter attribution / discount tier visibility).
  - Timeouts: dial 10 s, total 120 s (`AURA_LLM_TOTAL_TIMEOUT_SEC`, no idle-gap). No retry at wire layer — errors bubble up wrapped in `HTTPError{StatusCode, RetryAfterSec, Body}`.
  - Reasoning support: emits `REASONING_*` AG-UI events when provider returns `reasoning_content` (DeepSeek-V4 reasoning style), byte-for-byte passthrough.

**LLM (local, fallback):**
- **vLLM** — local OpenAI-compat server at `http://aura-vllm-chat:8083/v1` (Slice 13).
  - Container: `vllm/vllm-openai:latest` with `--kv-transfer-config '{"kv_connector":"LMCacheConnectorV1","kv_role":"kv_both"}'`.
  - Default model: `gemma-3-12b-it` (Q5_K_M, ~7 GB RAM; alternatives Llama 3.1 8B, Qwen 2.5 7B per pre-merge benchmark).
  - Endpoint env: `AURA_LLM_LOCAL_BASE_URL`, `AURA_LLM_LOCAL_MODEL`.
  - Activation: `LLMRouter` (`internal/llm/router.go`) chooses remote vs local per call. Four triggers in priority order: `conversation.metadata.prefer_local=true`, offline detection (TCP probe to remote every `AURA_LLM_OFFLINE_DETECTION_INTERVAL_SEC=30`, switch on 3 consecutive fails), daily cost cap `AURA_LLM_LOCAL_FALLBACK_COST_USD_DAY=1.0`, identity capability `use_local_llm`.
  - CPU-only fallback path "Slice 13-bis": skip vLLM entirely, reuse `aura-llama-multimodal` (llama.cpp + Gemma 4 E4B) for chat. Decision gated on pre-merge GPU/benchmark question.

- **llama.cpp server** (Gemma 4 multimodal) — local OpenAI-compat at `http://aura-llama-multimodal:8082/v1` (Slice 9c).
  - Container: `ghcr.io/ggml-org/llama.cpp:server` running `gemma-4-e4b-it-Q4_0.gguf` + `gemma-4-e4b-mmproj-Q4_0.gguf`.
  - Endpoints used:
    - `POST /v1/audio/transcriptions` — voice transcription (replaces removed Whisper sidecar).
    - `POST /v1/chat/completions` with base64 `image_url` — photo description.
  - Env propagated to Aura container: `MULTIMODAL_BASE_URL`, `MULTIMODAL_MODEL=gemma-4-e4b`, `MULTIMODAL_API_KEY=no-key`.
  - Retry: 2 attempts exponential (1 s / 2 s) on transcription, then hard-fail with UX message + 😵 reaction.

**Telegram Bot API:**
- **Telegram** — main user-facing channel (Slice 9b).
  - SDK: `gopkg.in/telebot.v4`. Long-poll mode (`tele.Bot.Start`).
  - Auth: `TELEGRAM_BOT_TOKEN` (supplied via Setup Wizard `POST /setup/token`, validated against Telegram `getMe` before persistence).
  - Rate limits: adaptive 429 `retry_after` parse + exponential backoff up to 30 s. Per-chat queue serialized at `AURA_TELEGRAM_CHAT_RATE_LIMIT_MS=1000`. Throttle differentiated: status pane 1500 ms, content streaming 500 ms.
  - HITL surface: `ask_user` pending → `InlineKeyboardMarkup` (callback_data `resume:<token>:<idx>`) for `kind=approval|choice`, `ForceReply` for `kind=clarification`.
  - File handling: voice via `getFile`+downloadURL → Gemma 4; documents via markitdown sidecar (sync ≤5 MB, async 5–50 MB, refuse >50 MB).
  - Bot commands MVP (8): `/start /help /whoami /cancel /new /conversations /resume /reset`. Plus Slice 13: `/local /remote /llm-status`.

**Web search:**
- **SearXNG** — self-hosted meta-search (Slice 5).
  - Endpoint: `SEARXNG_URL` env (default container `searxng` on shared compose network).
  - Client: `internal/web/searxng.go` (~130 LOC HTTP client). Ported almost 1:1 from `pre-rewrite-2026-05-27` `internal/agent/tools/registry/searxng.go`.
  - Tool: `web_search` (`Deferred=true`), args `{query, max_results?, category?, language?, time_range?}`.
  - Timeout: 20 s.

**Web fetch:**
- **Arbitrary HTTPS** — direct HTTP `GET` via `internal/web/fetcher.go` with SSRF defense (Slice 5).
  - SSRF blocklist enumerated for IPv4 (loopback, private, link-local, CGNAT, multicast, broadcast, `0.0.0.0/8`, cloud metadata `169.254.169.254`) and IPv6 (loopback, link-local, ULA, multicast, IPv4-mapped, discard, documentation, `fd00:ec2::254`).
  - DNS-rebinding protection: `safeDialContext` resolves host → validates IP → dials explicit IP (no re-lookup between resolve and dial).
  - HTTP redirect interception: `http.Client.CheckRedirect` re-validates every `Location` against blocklist.
  - Override: `AURA_WEB_FETCH_ALLOW_LOOPBACK=1`, `AURA_WEB_FETCH_ALLOW_HOSTS=host1,host2`.
  - Raw-body download ceiling: `AURA_WEB_FETCH_MAX_BODY_BYTES=5000000` (5 MB, DoS guard applied pre-extraction; NOT the LLM-facing markdown preview cap). Timeout 30 s. Readability filter: pages <250 chars main content return `{warning: "low-content page"}`.

**Document conversion:**
- **markitdown sidecar** — HTTP service for document → markdown (Slice 9c).
  - Endpoint: `POST /convert` (tiered: sync ≤5 MB, async 5–50 MB with background goroutine + edit-on-done in Telegram).
  - Used by Telegram channel for document attachments and by Slice 11b memory ingestion pipeline as the universal document parser.

---

## Data Storage

### Current implementation

None. No database client imports, no filesystem writes beyond Go test conventions.

### Planned (per prd.md)

**Databases:**

- **PostgreSQL 17** — application state (`aura` schema).
  - Connection: `AURA_DB_URL` (default `postgres://aura:aura@127.0.0.1:5432/aura?sslmode=disable`).
  - Client: `jackc/pgx/v5` + `pgxpool.Pool` (`MaxConns=10`, `MinConns=1`). Migrations via `golang-migrate/migrate/v4` with `embed.FS`. Type-safe queries via `sqlc` codegen.
  - Tables (per migration sequence 0001–0014):
    - `aura.knowledge_migrations` (0002) — audit of applied Cypher migrations.
    - `aura.paused_states` (0003) — `ask_user` pending pause/resume FIFO.
    - `aura.identities` + `aura.capability_grants` (Slice 1.7).
    - `aura.conversations` + `aura.conversation_turns` (0005, Slice 1.8) — multi-thread persistence.
    - `aura.scheduler_tasks` + `aura.task_runs` (0006, Slice 6).
    - `aura.skill_audit` + `aura.skills` (0007, Slice 7).
    - `aura.telegram_accounts` + `aura.telegram_setup_pending` (0008, Slice 9a).
    - `aura.sandbox_sessions` (0010, Slice 2b).
    - `aura.ingest_audit` (0011, Slice 11b).
    - `aura.local_llm_sessions` + `aura.local_llm_cost` (0013, Slice 13).
  - Concurrency: `DueTasks` uses `SELECT ... FOR UPDATE SKIP LOCKED` for multi-instance safety.

- **Neo4j 5.x Community** — knowledge graph + vector embeddings (sole source).
  - Connection: bolt `bolt://127.0.0.1:7687`, browser at `http://127.0.0.1:7474`.
  - Plugins: APOC + GDS (Graph Data Science, for Leiden community detection in Slice 11c) + vector index (built-in HNSW Apache Lucene).
  - **Access exclusively via `mcp-neo4j-cypher` MCP server (stdio subprocess)** — no native Go Neo4j driver. Configured via `Config.Neo4j.MCPBinary` (default `mcp-neo4j-cypher` on PATH).
  - Schema (per Slice 0.7 0001_init + Slice 11a):
    - Constraints: `:Chunk(id) UNIQUE`, `:Document(id) UNIQUE`, `:Entity(id) UNIQUE`, `:Community(id) UNIQUE`.
    - Vector indices (HNSW, 768-dim cosine): `chunk_embedding`, `entity_embedding`, `community_embedding`, `agent_insight_embedding`.
    - Fulltext indices: `chunk_text`, `entity_name`.
    - Labels: `:Document`, `:Chunk`, `:Entity` (Person/Org/Location/Concept/Event/Topic), `:Community`, `:UserConversation`, `:UserSnippet` (Slice 7e mirror), `:AgentEpisode`, `:AgentInsight`.
    - Relations: `HAS_CHUNK`, `MENTIONS`, `RELATED_TO`, `IN_COMMUNITY`, `CONTAINS`, `DISCUSSED`, `CITES`, `USED_SNIPPET`, `LEARNED`, `HANDLED`.
  - Auth: `NEO4J_USER=neo4j` + `NEO4J_PASSWORD` (`NEO4J_AUTH` propagated to container).
  - Cypher migrations: `internal/knowledge/migrations/*.cypher`, audit recorded in Postgres `aura.knowledge_migrations` (centralized audit alongside golang-migrate).

**Vector storage:**
- Inlined in Neo4j (HNSW 768-dim cosine on `:Chunk.embedding`, `:Entity.embedding`, `:Community.embedding`, `:AgentInsight.embedding`). **No Qdrant** (deprecated 2026-05-27 after spike `D:/tmp/aura-neo4j-spike-2026-05-27/` Phase 6b measured 22–30 ms p95 + IT recall@5 5/5 on real Aura corpus).

**Embeddings provider (sidecar):**
- **`aura-llama-embed`** — OpenAI-compat embedding endpoint at `http://aura-llama-embed:8081/v1`.
  - Model: `embeddinggemma` (CPU, 4 threads, ~600 MB idle).
  - Dimension: **768 native** written directly to Neo4j (no MRL truncation, no client-side resize).
  - Batch size: `AURA_MEMORY_EMBED_BATCH_SIZE=32` per roundtrip.
  - Used by: Slice 11b memory ingestion pipeline, Slice 7e snippet semantic search, Slice 11c community summarization.

**KV cache (LLM):**
- **LMCache** disk-tier (Apache 2.0, 8.4k stars) — Slice 13.
  - Config: `lmcache.yaml` (path `/var/cache/lmcache`, max 50 GB on NVMe, `chunk_size: 256` tokens, `enable_async_save: true`).
  - Wired into vLLM via `--kv-transfer-config '{"kv_connector":"LMCacheConnectorV1","kv_role":"kv_both"}'`.
  - Env: `LMCACHE_LOCAL_DISK_PATH`, `LMCACHE_MAX_LOCAL_DISK_GB` (third-party canonical naming, no `AURA_` prefix).
  - 3–10× TTFT reduction on long-context (per LMCache benchmarks, production at GCP / GMI / CoreWeave).

**File Storage:**
- Local filesystem, layered by purpose (prd.md §Persistence note):
  - `$AURA_RUN_DIR/` (default `~/.aura/run/`) — runtime sidecar artifacts.
    - `conversations/<conv_id>/<tool_call_id>.result` — `ToolResult` spillover when payload >`AURA_CONTEXT_PREVIEW_CAP_BYTES=2048`. Lifetime tied to conversation row (cascade on `aura chat delete`).
    - `conversations/<conv_id>/<seq>.content` — `conversation_turns` content spillover (>`AURA_CONVERSATION_TURN_CAP_BYTES=262144`, 256 KiB).
    - `conversations/<conv_id>/workspace/` — Slice 2b sandbox workspace mount (RW, owner `uid:gid 65532`, quota `AURA_SANDBOX_WORKSPACE_MAX_BYTES=104857600` = 100 MiB).
    - `tmp/<unix-ts>-<rand4>.<ext>` — one-off scratch, TTL 24 h, swept at boot.
    - Boot: warn if `$AURA_RUN_DIR > AURA_RUN_DIR_WARN_THRESHOLD_BYTES=1073741824` (1 GiB).
  - `$AURA_PROFILE_DIR/<identity_id>/Agent.md` (default `~/.aura/agents/`) — per-identity user profile (Slice 10).
  - `~/.aura/pyscripts/<id>/` — Slice 7e executable code snippets (multi-lang via SKILL.md `type: snippet`).
  - `$AURA_SKILLS_DIR/` — skills instruction tree (Slice 7).
  - `~/.aura/backups/postgres/` — `pg_dump` daily 03:00, retention 14 days.
  - `~/.aura/backups/neo4j/` — `neo4j-admin database dump` daily 03:30, retention 7 days.
- **Forbidden by discipline:** no knowledge in Postgres, no scheduler tasks in Neo4j, no markdown wiki anywhere, no vector index outside Neo4j.

**Caching:**
- Provider-aware **prompt cache** (KV cache) is the responsibility of Slice 4 prompt builder — stable-prefix discipline, zero `Messages[0]` mutation. Implementation lives in `internal/llm` (prompt builder), not the wire client.
- Anthropic-style ephemeral `cache_control` passthrough supported on the wire (`internal/llm/openai_compat/client.go` test fixture).
- DeepSeek auto-cache: native, no wire-level work beyond stable prefix.
- LMCache for local vLLM (see above).

---

## Authentication & Identity

### Current implementation

None.

### Planned (per prd.md)

**User identity:**
- Aura-native, table `aura.identities` (Slice 1.7). Single-user mode hardcodes identity name `local` with a constant UUID seed (`LocalIdentityID() uuid`). Capability grants table `aura.capability_grants` scaffolds future multi-user (wildcard `match-all` semantics).
- Per-identity `Agent.md` profile at `$AURA_PROFILE_DIR/<id>/Agent.md` (Slice 10).
- Telegram user binding via `aura.telegram_accounts.telegram_user_id → identities.id` (Slice 9a). Onboarding via QR + deep-link `t.me/<bot>?start=<onboarding_token>` (1 h TTL in `aura.telegram_setup_pending`).

**API authentication (Aura serving):**
- **Out of scope** for current PRD horizon. AG-UI endpoint (`POST /agent/run`, `GET /threads/<id>/messages`) binds to `127.0.0.1:9080` local-only. Setup Wizard binds to `127.0.0.1:9081` (override `AURA_SETUP_BIND` for remote QR scan). Future slice will add bearer token + identity FK on `conversations` (prd.md §Slice 8 acceptance "Auth dell'endpoint Out of scope esplicito").

**Third-party API authentication:**
- OpenRouter: API key in header, supplied via `OPENROUTER_API_KEY`.
- Telegram: bot token via `TELEGRAM_BOT_TOKEN` (Setup Wizard validates with `getMe`).
- Neo4j: bolt-level user/password via `NEO4J_USER` / `NEO4J_PASSWORD`.
- Postgres: DSN-embedded credentials via `AURA_DB_URL` + `POSTGRES_PASSWORD`.
- Sidecar HTTP endpoints (`aura-llama-embed`, `aura-llama-multimodal`, `aura-vllm-chat`, `markitdown`): dummy `no-key` API keys, isolated on Docker bridge network bound to `127.0.0.1`.

---

## Monitoring & Observability

### Current implementation

None. No log framework, no metrics, no error tracking.

### Planned (per prd.md)

**Error Tracking:**
- Not provisioned. Out of scope for slices 0.5 → 13.

**Logs:**
- Stdlib `log/slog` expected (not yet codified in PRD; convention only).
- Audit tables in Postgres provide forensic traceability per domain: `aura.skill_audit` (Slice 7), `aura.ingest_audit` (Slice 11b), `aura.knowledge_migrations` (Slice 0.7), `aura.task_runs` (Slice 6).
- Risk-Based Governance (cross-cutting section) emits audit rows + Notifier alerts at threshold `AURA_RISK_ALERT_THRESHOLD=risky`.

**Streaming events (observability surface):**
- AG-UI protocol (Slice 8) emits standardized event types over SSE: `RUN_STARTED`, `STEP_STARTED`/`FINISHED`, `TEXT_MESSAGE_*`, `TOOL_CALL_*`, `REASONING_*`, `STATE_SNAPSHOT`, `STATE_DELTA` (RFC 6902 JSONPatch), `MESSAGES_SNAPSHOT`, `RUN_FINISHED` (with `outcome.type ∈ {success, interrupted, errored}`), `RUN_ERROR`. ~17–25 standardized types.

**Health checks:**
- Docker Compose healthchecks on every stateful service: `pg_isready` (Postgres), `cypher-shell ... RETURN 1` (Neo4j), `curl /health` (multimodal), `curl /v1/models` (vLLM). Aura container `depends_on: condition: service_healthy`.

---

## CI/CD & Deployment

### Current implementation

**Hosting:** none.
**CI Pipeline:** none. `.git`-tracked workspace only.
**Build:** `go build ./...` + `go vet ./...` (CLAUDE.md §Post-edit validation).

### Planned (per prd.md)

**Hosting:**
- Self-hosted mini-PC (16-core, 32 GB target). Single-binary `aura serve` orchestrates the compose fleet. No managed cloud target.

**CI Pipeline:**
- Required by Gate 2 (Implementation Q&A) and Gate 3 (DoD) per prd.md §Slice Q&A discipline: `go vet ./...`, `go build ./...`, `go test ./...`, `go test -race ./...`, build-tag-gated integration tests (`db_integration`, `neo4j_integration`, `sandbox_integration`, `multimodal_integration`).
- Coverage thresholds at Gate 3 DoD: ≥75% unit, ≥60% integration, ≥70% mutation testing kill rate (spot-check).
- `goleak.VerifyNone(t)` in `TestMain` per package.
- `sqlc generate` golden test — CI fails if `internal/db/sqlc/` is out-of-sync with committed SQL.

**Deployment:**
- Docker Compose stack (`compose.yaml`) per host. Slice 9a Setup Wizard bootstraps Telegram first-run via web UI on `127.0.0.1:9081`.
- No OTA update, no tray icon, no Windows native runtime (all out of scope).

---

## Environment Configuration

### Current implementation

No env vars consumed. The skeleton's `cmd/aura/main.go` only branches on `os.Args`.

### Planned (per prd.md)

**Required secrets (must be supplied before boot):**
- `OPENROUTER_API_KEY` — remote LLM access (forwarded to `AURA_LLM_API_KEY`).
- `POSTGRES_PASSWORD` — Postgres auth (default `changeme`, must change).
- `NEO4J_PASSWORD` — Neo4j auth (default `changeme`, must change).
- `TELEGRAM_BOT_TOKEN` — supplied via Setup Wizard after first boot (no env required at boot if `--no-telegram`).

**Required operational env vars:**
- `AURA_DB_URL` (no default).
- All other env vars have safe defaults (see prd.md §4233 for the ~60-entry catalog).

**Secrets location:**
- `.env` file at repo root, loaded via `godotenv` (already in `.gitignore`).
- `.env.example` committed with placeholder empty values.
- CI grep enforces `.env` presence in `.gitignore` at Gate 1.
- Future: secrets store for Setup Wizard `POST /setup/token` persistence.

---

## Webhooks & Callbacks

### Current implementation

None.

### Planned (per prd.md)

**Incoming:**
- **Telegram polling** (not webhook) — `tele.Bot` long-poll mode (Slice 9b). Telegram updates flow through `internal/channels/telegram/bot.go`.
- **AG-UI gateway** (Slice 8):
  - `POST /agent/run` — SSE stream response. Body: `{threadId, runId?, messages[]}`. Path configurable via `AURA_AGUI_PATH_RUN` (default `/agent/run`).
  - `GET /threads/<id>/messages` — JSON `MESSAGES_SNAPSHOT` for UI rehydration.
  - CORS: permissive in dev (`AURA_AGUI_CORS_PERMISSIVE=1` → `*`), default permissive for `127.0.0.1` origin.
- **Setup Wizard** (Slice 9a) — `127.0.0.1:9081`:
  - `GET /setup` — embedded HTML/CSS/JS page.
  - `POST /setup/token` — paste bot token, validate via Telegram `getMe`.
  - `POST /setup/onboard-link` — generate onboarding UUID + QR SVG + deep link.
  - `GET /setup/events` — SSE stream of onboarding completions (poll DB every 2 s).
  - `GET /setup/status` — `{bot_configured, account_count, last_activity}`.

**Outgoing:**
- **OpenRouter** — `POST https://openrouter.ai/api/v1/chat/completions` (SSE stream).
- **vLLM local** — `POST http://aura-vllm-chat:8083/v1/chat/completions` (Slice 13).
- **Gemma 4 multimodal** — `POST http://aura-llama-multimodal:8082/v1/audio/transcriptions` (voice), `POST .../v1/chat/completions` (photo description).
- **`aura-llama-embed`** — `POST http://aura-llama-embed:8081/v1/embeddings` (batch 32).
- **SearXNG** — `GET ${SEARXNG_URL}/search?q=...&format=json`.
- **markitdown sidecar** — `POST .../convert` (document conversion).
- **Sandbox sidecar** — `POST http://127.0.0.1:18901/exec/python`, `/exec/shell`, `/session/<id>/exec/<lang>` (Slice 2b).
- **Notifier** (out of scope target, planned via Slice 6 scheduler + Telegram) — IMMEDIATE alert at risk tier ≥ `AURA_RISK_ALERT_THRESHOLD=risky`.

---

## MCP (Model Context Protocol) Servers

### Current implementation

None. No `mcp.json` exists in the working tree.

### Planned (per prd.md)

- **`mcp-neo4j-cypher`** — Apache 2.0 — Aura's sole interface to Neo4j (no native Go driver).
  - Transport: stdio subprocess, spawned by Aura at boot, lifecycle coupled to main process.
  - Distribution: required on host PATH (`pip install mcp-neo4j-cypher`). Fail-fast at boot with clear error if not found (prd.md §Slice 0.7 Open Question 1; bundling deferred as scope creep).
  - Configured via `Config.Neo4j.MCPBinary` (default `mcp-neo4j-cypher`), `Neo4jConfig.BoltURL`, `Neo4jConfig.User`, `Neo4jConfig.Password`.
  - Used for: Cypher migrations (`internal/knowledge/migrate.go`), application queries (`internal/knowledge/client.go` `Cypher(ctx, query, params) ([]Record, error)`), all knowledge-facing tools in future slices.

---

*Integration audit: 2026-05-28*
