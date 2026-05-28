# Technology Stack

**Analysis Date:** 2026-05-28

> Aura is a tabula-rasa rewrite (commit `pre-rewrite-2026-05-27` preserves the prior implementation).
> This document distinguishes **Current implementation** (what is committed in HEAD today, ~633 LOC Go skeleton) from **Planned target** (what `prd.md` schedules across slices 0.5 → 13).

---

## Languages

### Current implementation

**Primary:**
- Go 1.23 — declared in `go.mod`. Used for every file under `cmd/` and `internal/` (`cmd/aura/main.go`, `internal/agent/loop.go`, `internal/llm/client.go`, `internal/sandbox/sandbox.go`, `internal/swarm/swarm.go`, `internal/agent/tools/*.go`).

### Planned (per prd.md)

**Primary:**
- Go 1.23+ — kept. Required by Slice 0.9 for `iter.Seq2[K,V]` range-over-func streaming in the `Agent` runtime abstraction (prd.md §Slice 0.9 Pre-requisiti).

**Secondary:**
- Python (CPython 3.12 slim) — sandbox sidecar (`sandbox/sidecar.py`, ~150 LOC stdlib `http.server` + `subprocess.run`, no pip deps). prd.md §Slice 2 file targets.
- Cypher — Neo4j migrations under `internal/knowledge/migrations/*.cypher` (prd.md §Slice 0.7 file targets, §Slice 11a schema).
- SQL — Postgres queries under `internal/db/queries/*.sql` (sqlc source) and migrations `internal/db/migrations/*.up.sql` / `*.down.sql` (prd.md §Slice 0.5 file targets).
- YAML — `compose.yaml` (renamed from `sandbox/compose.yaml` in PRD references), `sqlc.yaml`, `lmcache.yaml` (prd.md §Slice 13).
- Shell — `Makefile` targets (`make sqlc`, `make db-up`, `make neo4j-up`).

---

## Runtime

### Current implementation

**Environment:**
- Go runtime, single binary expected at `/aura` (per `.gitignore`).
- No containers wired yet — no `Dockerfile`, no `compose.yaml`, no `mcp.json` exist at HEAD.

**Package Manager:**
- Go modules (`go.mod`). Module path `github.com/chetto1983/aura`.
- No `go.sum` committed (no external dependencies pulled yet).
- Lockfile: not applicable for Go modules baseline; will materialize when Slice 0.5 lands.

### Planned (per prd.md)

**Environment:**
- Single Go binary `aura` on the host (target: 16-core mini-PC, 16-32 GB RAM, Linux Docker-capable). Aura process orchestrates a fleet of containers via Docker Compose.
- All stateful services run in containers under `compose.yaml`:
  - `aura-postgres` (`postgres:17-alpine`, ~250 MB idle, bolt `5432`) — Slice 0.5
  - `aura-neo4j` (`neo4j:5-community`, APOC + GDS + HNSW vector index, ~1.5–2 GB idle, bolt `7687`, browser `7474`) — Slice 0.7
  - `aura-llama-embed` (embeddinggemma CPU 4 threads, OpenAI-compat, port `8081`, ~600 MB) — Slice 0.7
  - `aura-sandbox` (Python 3.12 slim, seccomp + ulimit + `network_mode: none`, port `18901`) — Slice 2a
  - `searxng` (`searxng/searxng` image, ~150 MB) — Slice 5
  - `aura-llama-multimodal` (`ghcr.io/ggml-org/llama.cpp:server`, Gemma 4 E4B Q4 + mmproj, port `8082`, ~3 GB) — Slice 9c
  - `markitdown` sidecar (~150 MB, document → markdown converter) — Slice 9c
  - `aura-vllm-chat` (`vllm/vllm-openai:latest` + LMCache disk-tier, port `8083`, ~7 GB Gemma 3 12B Q5) — Slice 13 (gated on GPU; CPU fallback path "13-bis" reuses `aura-llama-multimodal`)
- Health gating: Aura container `depends_on: condition: service_healthy` for `postgres`, `neo4j`, `aura-llama-embed` (prd.md §Slice 0.5 acceptance row 78).

**Package Manager:**
- Go modules with `go.sum` checked in (target).
- Python sidecar uses **stdlib only** — no pip deps, no `requirements.txt`. Intentional, per Slice 2 Open Question 1 (`sandbox/sidecar.py` is 1 file, no compile step).

---

## Frameworks

### Current implementation

None. The skeleton defines its own thin interfaces:
- `llm.Client` (`internal/llm/client.go:66`) — streaming chunk channel.
- `agent.Loop` (`internal/agent/loop.go:25`) — conversation lifecycle, `MaxSteps=8` default.
- `tools.Tool` + `tools.Registry` (`internal/agent/tools/spec.go:30`) — deferred-tool dispatch pattern.
- `sandbox.Runner` + `sandbox.Stub` (`internal/sandbox/sandbox.go:13`) — placeholder.
- `swarm.Coordinator` + `swarm.Stub` (`internal/swarm/swarm.go:23`) — placeholder.

### Planned (per prd.md)

**Core (Aura-native, no external framework):**
- `agent.Agent` interface — base for all runtime work (LLM agents, scheduler handlers, swarm workers, skills). Pattern **stolen, not imported** from `google/adk-go` v1.3.0 (prd.md §Slice 0.9 disclaimer: 35 GCP/OTel/Gemini deps deemed footprint-unacceptable; Aura reimplements ~380 LOC of the same shape).
- `agent.InvocationContext` + `agent.Event` + `agent.Actions{Escalate, StateDelta, ArtifactDelta}` — streaming events via `iter.Seq2[*Event, error]`.
- Built-in workflow agents: `SequentialAgent`, `LoopAgent`, `ParallelAgent` (`internal/agent/workflow/*.go`).

**LLM transport:**
- OpenAI-compat wire layer at `internal/llm/openai_compat/client.go` (~280 LOC, SSE parser, tool-call delta accumulator). Wraps `net/http` stdlib; no SDK dependency.
- Default provider: **OpenRouter** (base URL `https://openrouter.ai/api/v1`), default model `deepseek/deepseek-v4-flash:exacto` (DeepSeek-V4 routed via OpenRouter, prd.md §Slice 1 Open Question 1).
- Provider-aware KV cache discipline lives in the **prompt builder** (Slice 4), not in the wire client.

**Persistence (Postgres):**
- `jackc/pgx/v5` — pure-Go driver, no CGO. Connection pool via `pgxpool.Pool` (`MaxConns=10`, `MinConns=1`, idle 30 s). Slice 0.5.
- `sqlc` — SQL-first codegen for type-safe queries; output to `internal/db/sqlc/`. `sqlc.yaml` v2 config (engine postgresql, json_tags, emit_interface, emit_exact_table_names).
- `golang-migrate/migrate/v4` — file-based versioned migrations under `internal/db/migrations/` (`*.up.sql` / `*.down.sql`), embedded via `embed.FS`.

**Persistence (Neo4j):**
- Neo4j 5.x Community + APOC + GDS plugins (`NEO4J_PLUGINS='["apoc","graph-data-science"]'`).
- Vector index: built-in HNSW Apache Lucene, **768-dim cosine** (no MRL truncation).
- **`mcp-neo4j-cypher`** MCP server (Apache 2.0) — subprocess stdio spawned by Aura. **No native Go Neo4j driver** by deliberate discipline (prd.md §Slice 0.7 stack: "tutto accesso Neo4j passa da MCP"). Audit of applied Cypher migrations lives in Postgres `aura.knowledge_migrations`.

**Web tooling (Slice 5):**
- `github.com/go-shiori/go-readability` — readability extractor.
- `github.com/JohannesKaufmann/html-to-markdown/v2` — HTML → markdown converter.

**Telegram channel (Slice 9b):**
- `gopkg.in/telebot.v4` — bot framework (`tele.Bot` polling).
- `github.com/eekstunt/telegramify-markdown-go` — Markdown → Telegram MarkdownV2 safe escaping (with custom port fallback if license/maintenance fails pre-merge check).
- `github.com/skip2/go-qrcode` — QR code SVG generation for setup wizard.
- `github.com/mdp/qrterminal/v3` — ASCII QR for console (`aura serve` first-boot fallback).

**AG-UI gateway (Slice 8):**
- `github.com/ag-ui-protocol/ag-ui/sdks/community/go` — AG-UI protocol Go SDK (MIT). Exposes `RUN_STARTED` / `TEXT_MESSAGE_*` / `TOOL_CALL_*` / `REASONING_*` / `STATE_DELTA` / `RUN_FINISHED` / `RUN_ERROR` event types over SSE.

**Config / env loading:**
- `github.com/joho/godotenv` — `.env` file loader (Slice 1 file targets). Load order: built-in default → `.env` → `$AURA_CONFIG_DIR/llm.json` → env vars.

**Testing:**
- Stdlib `testing` + `go test -race`.
- `go.uber.org/goleak` — `goleak.VerifyNone(t)` in `TestMain` to assert zero residual goroutines per prd.md §Slice 1 acceptance.
- Build tags for integration tiers that need sidecars: `//go:build db_integration`, `//go:build neo4j_integration`, `//go:build sandbox_integration`, `//go:build multimodal_integration`.
- Property-based and mutation testing called out in prd.md §Test discipline rigorosa (mutation ≥70% killed at Gate 3).

**Build/Dev:**
- `Makefile` (Slice 0.5) — `make sqlc`, `make db-up`, `make db-migrate`, `make db-reset`, `make neo4j-up`, `make neo4j-migrate`.
- Docker Compose `compose.yaml` (renamed from `sandbox/compose.yaml`) orchestrates the sidecar fleet.

---

## Key Dependencies

### Current implementation

None — `go.mod` has zero `require` directives. Module is bootstrap-only.

### Planned (per prd.md)

**Critical:**
- `github.com/jackc/pgx/v5` — Postgres driver + pool. Performance leader for Postgres in Go. (Slice 0.5)
- `github.com/golang-migrate/migrate/v4` — schema migrations engine. (Slice 0.5)
- `github.com/joho/godotenv` — `.env` loader. (Slice 1)
- `gopkg.in/telebot.v4` — Telegram bot library. (Slice 9b)
- `github.com/ag-ui-protocol/ag-ui/sdks/community/go` — AG-UI protocol SDK. (Slice 8)

**Infrastructure (codegen / tooling, not runtime):**
- `sqlc` (`cmd/sqlc` v2 binary) — invoked via `make sqlc`, generates `internal/db/sqlc/`. (Slice 0.5)
- `mcp-neo4j-cypher` — Python MCP server installed on PATH (`pip install mcp-neo4j-cypher`); subprocess-spawned by Aura. **Required on host PATH**, fail-fast at boot with clear error if missing (prd.md §Slice 0.7 Open Question 1). (Slice 0.7)

**HTTP/parsing helpers:**
- `github.com/go-shiori/go-readability` — readability extractor for `web_fetch`. (Slice 5)
- `github.com/JohannesKaufmann/html-to-markdown/v2` — markdown converter. (Slice 5)
- `github.com/eekstunt/telegramify-markdown-go` — Telegram MarkdownV2 safe escaping. (Slice 9b)
- `github.com/skip2/go-qrcode` — QR SVG. (Slice 9a)
- `github.com/mdp/qrterminal/v3` — ASCII QR. (Slice 9a)

**Testing:**
- `go.uber.org/goleak` — goroutine leak detection. (Slice 1 acceptance)

---

## Configuration

### Current implementation

**Environment:**
- Not wired. `cmd/aura/main.go` exposes only `tools` and `chat <msg>` subcommands; `serve` and `shell` print `"TODO: implemented by the agent-loop and CLI slices"` (`cmd/aura/main.go:37`).
- The skeleton `chatOnce` instantiates a `stubClient` (`cmd/aura/main.go:74`) that hard-codes a `text_response` tool call, with no env reading at all.

**Build:**
- `go.mod` only — no `Makefile`, no `compose.yaml`, no `Dockerfile`, no `tsconfig`-equivalent.

**Forbidden file inspection:** `.gitignore` lists `/.env` and `/.env.local` as ignored, but no `.env` exists in the working tree. Their existence and contents are out-of-scope for this audit by policy.

### Planned (per prd.md)

**Naming convention:**
- Aura-controlled env vars use `AURA_<DOMAIN>_<UNIT>` (e.g. `AURA_SWARM_MAX_DEPTH`, `AURA_LLM_TOTAL_TIMEOUT_SEC`, `AURA_CONTEXT_PREVIEW_CAP_BYTES`).
- Third-party libs / sidecars keep upstream-canonical names: `TELEGRAM_BOT_TOKEN`, `OPENROUTER_API_KEY`, `MULTIMODAL_*`, `LLAMA_*`, `LMCACHE_*`, `NEO4J_PASSWORD`, `POSTGRES_PASSWORD`.
- Unit suffix required for caps: `_BYTES`, `_SEC`, `_MS`, `_HR`, `_DAYS`, `_USD_DAY` etc. (prd.md §Naming convention 4334).

**Load order** (Slice 1 §Open Question 1):
1. Built-in defaults (`Provider="openrouter"`, `Model="deepseek/deepseek-v4-flash:exacto"`, etc.).
2. `.env` file via `godotenv` (keys like `OPENROUTER_API_KEY` → `AURA_LLM_API_KEY`).
3. JSON file `$AURA_CONFIG_DIR/llm.json` (default `~/.aura/llm.json`), `Save()`able from a future dashboard.
4. Env vars (`AURA_LLM_*`, `AURA_RUN_DIR`, …).

**Key configs required** (selected high-impact, full ~60-entry index at prd.md §4233):
- `OPENROUTER_API_KEY` (secret, required) — forwarded to `AURA_LLM_API_KEY`.
- `AURA_LLM_BASE_URL` (default `https://openrouter.ai/api/v1`).
- `AURA_LLM_MODEL` (default `deepseek/deepseek-v4-flash:exacto`).
- `POSTGRES_PASSWORD` (secret, default `changeme` must change).
- `NEO4J_PASSWORD` (secret, default `changeme` must change), `NEO4J_USER=neo4j`.
- `TELEGRAM_BOT_TOKEN` (secret, supplied via Setup Wizard at `http://127.0.0.1:9081/setup`).
- `AURA_RUN_DIR` (default `~/.aura/run/`) — sidecar tool result files and conversation spillover.
- `AURA_CONFIG_DIR` (default `~/.aura/`).
- `AURA_PROFILE_DIR` (default `~/.aura/agents/`) — per-identity `Agent.md` profile.
- `AURA_SETUP_BIND` (default `127.0.0.1:9081`).
- `AURA_SWARM_MAX_DEPTH` (default `3`).
- `AURA_SANDBOX_*` family — `URL`, `TIMEOUT_SEC`, `SESSION_TTL_SEC`, `MAX_CONCURRENT_SESSIONS`, `WORKSPACE_MAX_BYTES`, `NETWORK_ALLOW_HOSTS`.
- `LMCACHE_LOCAL_DISK_PATH` (default `/var/cache/lmcache`), `LMCACHE_MAX_LOCAL_DISK_GB` (default `50`) — Slice 13.

**Build config files (planned):**
- `compose.yaml` — Docker Compose for the whole sidecar fleet (extended by each slice).
- `sqlc.yaml` — sqlc v2 codegen config (Slice 0.5).
- `lmcache.yaml` — LMCache disk-tier config (Slice 13: `chunk_size: 256`, `enable_async_save: true`).
- `Makefile` — targets per slice (`make sqlc`, `make db-up`, `make neo4j-up`, …).
- `sandbox/seccomp.json` — Docker seccomp profile (default-deny, allow-list syscalls, block `socket`/`connect`/`bind`/`mount`/`unshare`/`ptrace`/`clone(CLONE_NEWNET)`).
- `sandbox/Dockerfile` — `FROM python:3.12-slim`, non-root `uid:gid 65532:65532`.
- `.env.example` — committed template with placeholder empty values.

---

## Platform Requirements

### Current implementation

**Development:**
- Any platform with Go 1.23+ installed. `go build ./...` and `go vet ./...` are the only mandated checks (CLAUDE.md §Post-edit validation).

**Production:**
- Not defined. Binary expected at `/aura` per `.gitignore`.

### Planned (per prd.md)

**Development:**
- Go 1.23+ toolchain.
- Docker / Docker Compose for the sidecar fleet.
- Python 3 on PATH (only to install `mcp-neo4j-cypher`; the sidecar itself runs in a container).
- Linux preferred for seccomp; Docker Desktop on Windows tolerated for development but **no native Windows runtime** (Slice 2 Open Question 5: "Aura runs in container or against a Docker sidecar. Punto."). Named volumes are mandatory — no bind mounts on Windows (carries `feedback_sqlite_wal_windows_corruption.md` rationale).

**Production:**
- 16-core mini-PC, 32 GB RAM target (16 GB minimum). Cumulative idle ~5.7–6.2 GB by end of Slice 7; peak ~7 GB under load. Slice 13 (vLLM + LMCache) adds another ~5–7 GB and is gated on **GPU availability** (open question pre-merge: vLLM CPU is 5–10× slower than llama.cpp CPU, with fallback path "13-bis" reusing `aura-llama-multimodal` for chat).
- Storage: NVMe required for LMCache disk-tier (50 GB at `/var/cache/lmcache`).
- Network: outbound HTTPS to OpenRouter (`https://openrouter.ai`). Optional Telegram polling (egress to `api.telegram.org`).
- Backup: `pg_dump` + `neo4j-admin database dump` cronned via Slice 6b scheduler (`backup_postgres` daily 03:00, `backup_neo4j` daily 03:30, retention 14 / 7 days).

---

*Stack analysis: 2026-05-28*
