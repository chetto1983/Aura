---
last_mapped_commit: 5adb3d49b9b8cd7ea4f872fbdb7199b4021c9f5c
---
# Codebase Structure

**Analysis Date:** 2026-08-13

## Directory Layout

```
Aura/
├── cmd/
│   ├── aura/                  # Main daemon/CLI binary — hand-rolled verb dispatcher (NOT cobra)
│   ├── aura/conversations/    # (sub-package under cmd/aura for conversation-scoped helpers)
│   ├── arcadedb-mcp/          # MCP server exposing the ArcadeDB memory graph + retrieval to LLMs
│   └── aura-filecard/         # Standalone CLI wrapping internal/documents/filecard
├── internal/                  # ~68 packages, the whole application (see table below)
├── services/ingest/           # Python CocoIndex app: S3(Garage) → extract/chunk/embed → ArcadeDB
├── finetune/                  # Python fine-tuning pipeline (separate from the runtime)
├── web/                       # React + Vite cockpit SPA source (internal/webui embeds its build)
├── docker/                    # Per-service Dockerfiles (aura, aura-sandbox, aura-ingest, arcadedb, garage, ...)
├── deploy/                    # systemd unit files
├── caddy/                     # Reverse-proxy Caddyfile
├── observability/             # Grafana dashboards, Prometheus rules, Tempo config, runbooks
├── scripts/                   # Bash/Python/Go operational scripts (coverage gates, smoke tests, migrations helpers)
├── docs/                      # Design docs, ADRs, audit reports, quality snapshot
├── spikes/                    # Throwaway measurement spikes (cocoindex, retrieval benchmarks)
├── searxng/                   # SearXNG config for the web_search tool's backend
├── public/                    # Static marketing/readme assets
├── .planning/                 # GSD workflow state (regenerated per milestone; see CLAUDE.md)
├── .claude/                   # GSD commands/agents/hooks + installed skills
└── prd.md, CLAUDE.md          # Root-level living specs
```

## Directory Purposes

**`cmd/aura`:**
- Purpose: The single production binary. Every verb (`serve`, `chat`, `shell`, `db`, `identity`, `mcp`, `memory`, `docs`, `skills`, `retention`, `task`, `paused-states`, `swarm-demo`, `web`, `doctor`, `tools`, `config`, `version`, `toolpipe`) is a hand-rolled subcommand.
- Contains: Composition roots (`main.go`, `serve*.go`, `chat*.go`), tool-registry wiring, HTTP-layer glue for AG-UI (`serve_webui*.go`, `serve_agui.go`), asset/document processing workers, idempotency middleware.
- Key files: `main.go` (top-level dispatch + `buildBaseRegistryWithHandles`, the SHARED tool-registry composition root every boot path funnels through), `serve.go` (daemon lifecycle), `chat.go` (REPL), `db.go` (migrate/ping/status/reset).

**`cmd/arcadedb-mcp`:**
- Purpose: MCP server process exposing memory (facts) and document-retrieval/graph-schema tools over the Model Context Protocol.
- Contains: `main.go` (server boot + tenant wiring), one `tool_*.go` per MCP tool group (`tool_memory.go`, `tool_forget.go`, `tool_browse.go`, `tool_graph_schema.go`, `tool_memory_maintenance.go`), `tenant.go` (per-identity HMAC-derived credential + database routing).

**`internal/agent`:**
- Purpose: The agent runtime core — the open `Agent` interface, `Budget`, `Event`, and `LlmAgent` (the concrete tool-dispatch loop).
- Contains: `agent.go` (contract), `budget*.go` (resource control), `event*.go` (wire shape), `llm_agent*.go` (~30 files, the loop split by concern: `_dispatch`, `_tool`, `_finalize`, `_pause`, `_retry`, `_truncation`, `_verification`, `_promote`, `_reasoning`), `hooks*.go` (extension seam), `verification_*.go` (evidence ledger / verify-on-stop gate).
- Subpackages: `internal/agent/prompt` (request builder + reasoning classifier), `internal/agent/tools` (46 built-in tools), `internal/agent/mcptools` (MCP-to-tool bridge), `internal/agent/display` (typed tool-result display payloads), `internal/agent/mcp` (nothing — see `internal/mcp` instead), `internal/agent/agenttest` (test-support fakes, excluded from coverage floor).

**`internal/agent/tools`:**
- Purpose: Every built-in tool the agent loop can dispatch, plus the deferred-tool manifest machinery.
- Contains: One file per tool (`shell_exec.go`, `read_file.go`, `document_search.go`, `document_open.go`, `send_file.go`, `skill*.go`, `ask_user.go`, ...), `spec.go` (`Tool`/`Spec`/`Registry` contract), `manifest.go` (LLM-visible rendering + `tool_search`), `registry.go` (`Without` — clone-minus-names for swarm children).

**`internal/agent/prompt`:**
- Purpose: The single chokepoint assembling the wire `llm.Request` from history + registry + config + volatile `Budget` hints; also the local embedding-based reasoning-tier classifier.
- Key files: `builder.go` (`PromptBuilder.Build/BuildWithReasoningTier/BuildWithReasoningOverride`), `cache_anthropic.go` (provider `cache_control` injection), `reasoning_classifier.go`, `reasoning_router.go`, `reasoning_policy.go`.

**`internal/runner`:**
- Purpose: Channel-agnostic orchestration — the SOLE driver of `agent.Run` per turn, and the SOLE writer of `paused_states`.
- Contains: `runner.go` (`Runner`, `Deps`, `Turn`/`TurnBranch`), `runner_context.go` (context-config + memory digest wiring), `runner_history.go`, `runner_persist.go`, `runner_resume*.go`, `runner_session.go` (per-thread locks + live-turn cancel registry), `runner_verification.go`.

**`internal/conversations`:**
- Purpose: The Postgres-backed conversation Store PLUS the deterministic context ladder (L1/L2/L2.4/L2.5) and compaction.
- Contains: `store*.go` (CRUD, branching, search, purge), `context.go` (the ladder), `compaction.go` (L2.4 LLM summarizer interface), `context_rot.go`/`context_tail.go`, `tiktoken.go` (token counting), `sweeper.go`, `orphan_scan.go`, `title.go` (auto-title).

**`internal/documents`:**
- Purpose: The document catalog, ingestion-job bookkeeping, and the retrieval cascade (card + lexical + dense legs).
- Contains: `service.go` (`IngestPath`, catalog registration, card write), `jobs_worker.go`/`jobs_store.go` (durable ingestion job queue), `retrieval.go`/`retrieval_cards.go`/`retrieval_rank.go` (the fused cascade), `catalog_*.go` (Postgres-backed catalog store), `open.go` (`document_open` resolution), `embedder.go`.
- Subpackages: `internal/documents/filecard` (structural card builder).

**`internal/arcadedb`:**
- Purpose: Typed HTTP/Cypher client for ArcadeDB, plus the memory and document-retrieval domain logic that runs against it.
- Contains: `client.go` (HTTP transport), `memory.go`/`memory_vector.go`/`memory_provenance.go`/`memory_backfill.go` (bitemporal fact graph), `document_retrieval.go`/`document_cards.go` (the `RetrievalControlPlane`/`RetrievalProjection` implementations `internal/documents` consumes), `tenant.go`/`tenant_clients.go` (per-identity database + credential), `embedding.go`, `schema.go`, `forget.go`, `browse.go`, `studio_graph.go`.

**`internal/agui`:**
- Purpose: The AG-UI protocol HTTP/SSE gateway — the cockpit's transport layer.
- Contains: `server*.go` (mux + boot), `server_run*.go` (`POST /agent/run`), `translator.go` (`agent.Event` → AG-UI wire), `conversations_api.go`, `governance_*api.go`, `onboarding_*.go`, `share_*.go`, `voice_api.go`, `assets_api*.go`, `auth*.go`.

**`internal/webui`:**
- Purpose: Leaf package embedding and serving the committed Vite build of the cockpit SPA.
- Contains: `embed.go` (`//go:embed all:dist`, SPA-fallback handler), `doc.go` (package doc + boundary-check rationale).
- Special: Enforced as a dependency-closure LEAF by `scripts/agui_boundary_check.sh` — it may import only the standard library, never another `internal/*` package.

**`internal/channels` / `internal/channels/telegram`:**
- Purpose: The daemon channels framework (`Channel` lifecycle interface) and the Telegram bot implementation.
- Contains (telegram): `bot.go` (boot), `bot_dispatch*.go` (inbound routing split by concern: `_turn`, `_auth`, `_callbacks`, `_hitl`, `_asset`), `agui_subscriber.go` (`handleTurn` via the shared Fanout seam), `renderer.go`/`mdv2.go`/`html.go` (Telegram-specific rendering), `voice.go`/`tts.go`, `store.go`.

**`internal/sandbox/usersandbox`:**
- Purpose: Per-identity Docker-backed execution box type layer that `shell_exec`/`fs_*`/`send_file`/`document_open` route into under a strict deployment profile.

**`internal/gateway`:**
- Purpose: Aura's single in-process policy-enforcement point (PEP) over mutating tool calls.
- Contains: `classify.go` (risk classification), plus decision/reservation/ledger logic (grep for `Gateway.Decide`).

**`internal/mcp`:**
- Purpose: The generic stdio/HTTP MCP client (spawns/dials a server, JSON-RPC handshake) that `internal/agent/mcptools` bridges into the tool registry.

**`internal/swarm`:**
- Purpose: Ephemeral per-call sub-agent fan-out coordinator (the `swarm_spawn` tool's runtime).

**`internal/cron`:**
- Purpose: The `at|every|cron` schedule engine and dispatcher for `reminder`/`agent_job`/`backup_*` task kinds.

**`internal/db`:**
- Purpose: Postgres connectivity — pgxpool open, golang-migrate migrations, sqlc-generated query code.
- Subdirs: `migrations/` (numbered `NNNN_description.{up,down}.sql`, next free number = `ls internal/db/migrations | tail -1` + 1, current floor 0094), `queries/` (hand-written sqlc source `.sql`), `sqlc/` (generated Go, excluded from the coverage floor).

**`internal/identity`, `internal/identityctx`, `internal/idroot`:**
- Purpose: Identity Store (capability grants), the request-scoped identity-id context key, and the traversal-safe per-identity filesystem root helper.

**`internal/llm`:**
- Purpose: The streaming LLM client interface + an OpenAI-compatible implementation (OpenRouter / local llama.cpp), circuit breaker, model capability table.

**`internal/objectstore`:**
- Purpose: Garage/S3-compatible object store client (one bucket per identity) for original document bytes and generated artifacts.

**`internal/webauth`:**
- Purpose: Embeds the Authula (Apache-2.0) Go auth framework for the cockpit's session/login boundary.

**Other single-purpose leaves worth knowing:** `internal/askuser` (HITL pause Store), `internal/breakglass` (offline operator-recovery primitives), `internal/canonicaljson` (deterministic hashing serializer), `internal/gateway`, `internal/idempotency` (mutation replay-safety), `internal/obs` (OTel/logging bootstrap), `internal/readiness`, `internal/redact`, `internal/retention` (storage-lifecycle policy), `internal/secret`, `internal/settings` (cockpit-editable runtime overrides), `internal/skills`/`internal/skilladapters` (the skills system + its composition-root bridge), `internal/toolinvocations`, `internal/tracesink` (encrypted sensitive-trace sink), `internal/web` (the `web_search`/`web_fetch` engine over SearXNG), `internal/multimodal`, `internal/embeddings`.

## Key File Locations

**Entry Points:**
- `cmd/aura/main.go`: Top-level CLI dispatch + the shared tool-registry composition root.
- `cmd/aura/serve.go`: Daemon boot (`aura serve`).
- `cmd/aura/chat.go`: REPL boot (`aura chat`).
- `cmd/arcadedb-mcp/main.go`: MCP server boot.
- `internal/agui/server_run.go`: `POST /agent/run` HTTP handler.
- `internal/channels/telegram/bot_dispatch_turn.go`: Telegram inbound-turn entry point.

**Configuration:**
- `internal/config/`: Thin root composite (`config.Load`/`config.LoadDB`) read by every `cmd/aura` subcommand.
- `.env` (not committed): `AURA_*` env vars + third-party secrets (`TELEGRAM_BOT_TOKEN`, `OPENROUTER_API_KEY`, ...).
- `internal/db/migrations/`: Postgres schema, numbered sequentially.

**Core Logic:**
- `internal/agent/llm_agent.go`: The agent turn loop.
- `internal/conversations/context.go`: The context ladder.
- `internal/documents/retrieval.go`: The retrieval cascade.
- `internal/arcadedb/memory.go`: The bitemporal fact graph model.

**Testing:**
- Co-located `*_test.go` next to the file under test throughout (no separate `tests/` tree in Go code).
- `internal/agent/agenttest`: Shared test-support fakes (e.g. `CountingAgent`), excluded from the coverage floor.
- `services/ingest/tests/`: Python test tree for the CocoIndex app.
- `scripts/coverage_gate.sh`, `scripts/coverage_docker.sh`: The coverage-floor gate scripts (see CLAUDE.md §Quality tooling).

## Naming Conventions

**Files:**
- `<package>.go` for the primary type/entry, then `<name>_<concern>.go` splits once a file nears the 600-LOC ceiling — heavily used in `cmd/aura` (`serve_webui.go`, `serve_webui_auth_config.go`, `serve_webui_composer.go`, `serve_webui_musr.go`, `serve_webui_scheduler.go`, `serve_webui_share.go`, `serve_webui_voice.go`), `internal/agent` (`llm_agent.go` + ~25 `llm_agent_<concern>.go` files), `internal/channels/telegram` (`bot_dispatch.go` + `bot_dispatch_{auth,asset,callbacks,hitl,turn}.go`), `internal/documents` (`catalog_store.go` + `catalog_store_{asset,digest,identity}.go`).
- `<file>_test.go` is the STANDARD unit-test sibling; `<file>_internal_test.go` marks a test that reaches unexported internals of that specific file's concern (e.g. `llm_agent_pause_internal_test.go`); a bare `_test.go` file with no matching non-test file (e.g. `main_test.go`, `cover_test.go`) hosts package-wide test scaffolding or coverage-padding cases.

**Build tags (test-tier gating):**
| Tag | Files (approx.) | Meaning |
|---|---|---|
| `db_integration` | 136 | Requires a live Postgres (`AURA_DB_URL`); the ONLY tag set the coverage gate (`scripts/coverage_gate.sh`) runs by default. |
| `docker_integration` | 11 | Requires a live Docker daemon (`internal/sandbox/usersandbox` DockerBackend); runs in CI (`sandbox-docker-integration` job) but counts ZERO toward the coverage floor. |
| `arcadedb_integration` | 11 | Requires a live ArcadeDB instance; **currently wired into NO CI job and NOT even `go vet`-compiled** — an open gap (see CLAUDE.md §Coverage gate tag set). |
| `!web_integration` | 11 | Excludes the live web-fetch/search suite from the default unit run. |
| `live_e2e` | 4 | Full live end-to-end scenario tests. |
| `windows` / `!windows` | 3 / 3 | OS-specific behavior (process-group handling in `internal/procgroup`). |
| `measure` | 3 | Ad-hoc measurement harnesses (not a correctness gate). |
| `garage_integration` | 3 | Requires a live Garage (S3) instance. |
| `web_integration`, `retrieval_eval`, `reasoning_live`, `whatsapp_integration`, `webauth_integration`, `telegram_integration`, `serve_smoke`, `multimodal_integration`, `live_finalize`, `integrations_integration` | 1-2 each | Narrow live/E2E suites gated behind their respective external dependency being reachable. |

A test carrying a `_integration`/`_live` tag MUST `t.Fatal` (never silently `t.Skip`) when its required env is unset AND `$CI` is set — the no-skip-as-green rule (CLAUDE.md).

**Directories:**
- `internal/<domain>` is a flat, non-nested package per bounded concern (68 top-level packages under `internal/`); the only nested exceptions are `internal/agent/{prompt,tools,mcptools,display,agenttest}`, `internal/channels/telegram`, `internal/db/{migrations,queries,sqlc}`, `internal/sandbox/usersandbox`, `internal/documents/filecard`, `internal/skills/embed/<skill-name>`.
- `internal/db/migrations/NNNN_description.{up,down}.sql`: four-digit zero-padded sequential number, matched `up`/`down` pair; the next number is READ from the directory listing, never computed or copied from documentation.

## Where to Add New Code

**New agent tool:**
- Implementation: `internal/agent/tools/<name>.go` (+ `<name>_test.go`).
- Register it in `cmd/aura/main.go` `buildBaseRegistryWithHandles` (or `buildRegistryWithMCP` if MCP-mounted).
- Set `Deferred: true` in its `Spec` if the description/schema is large (CLAUDE.md's deferred-tool pattern).

**New AG-UI HTTP endpoint:**
- Handler: `internal/agui/<concern>_api.go`.
- Wire into the mux in `internal/agui/server.go`; if it should be excluded from the SPA fallback, add its prefix to the `apiPrefixes` list assembled in `cmd/aura/serve_webui.go`.

**New Postgres table/column:**
- Add a migration `internal/db/migrations/<next-number>_description.{up,down}.sql` (number from `ls internal/db/migrations | tail -1` + 1).
- Add/update the sqlc query in `internal/db/queries/`, regenerate `internal/db/sqlc/`.
- Add the domain Store method in the owning `internal/<domain>/store*.go`.

**New channel (e.g. WhatsApp/Discord):**
- New package `internal/channels/<name>/` implementing `channels.Channel` (`internal/channels/channel.go`).
- Register it in the channel `Registry` construction inside `cmd/aura/serve_channels.go`.

**Utilities/shared helpers:**
- A cross-cutting, dependency-light helper gets its OWN leaf package under `internal/<name>` (the pattern used by `internal/envutil`, `internal/redact`, `internal/canonicaljson`, `internal/pgnumeric`) rather than a grab-bag `internal/utils`.

## Special Directories

**`internal/webui/dist/`:**
- Purpose: Committed Vite production build of the cockpit SPA, embedded via `//go:embed all:dist`.
- Generated: Yes (built via `docker webbuild`, Linux node-24 — building on Windows re-hashes chunks and fails the `web-dist-freshness` CI check).
- Committed: Yes — it must be committed and byte-identical to what a Linux build produces.

**`internal/db/sqlc/`:**
- Purpose: Generated Postgres query code from `internal/db/queries/*.sql`.
- Generated: Yes.
- Committed: Yes; excluded from the `internal/*` LOC/coverage-floor accounting as generated code (~7k LOC per CLAUDE.md).

**`internal/agent/agenttest/`:**
- Purpose: Shared test-support fakes for the agent package tree.
- Generated: No.
- Committed: Yes; excluded from the owned-surface coverage floor (it is test infrastructure, not production logic).

**`.planning/`:**
- Purpose: GSD workflow state (roadmap, phase plans, this codebase map).
- Generated: Yes — deleted at milestone close and regenerated by `/gsd-map-codebase`, `/gsd-ingest-docs`, `/gsd-graphify` at the start of the next milestone.
- Committed: Historically yes at various points, but treated as ephemeral/regenerable — the durable history lives in git commits, not this directory's continuity.

---

*Structure analysis: 2026-08-13*
