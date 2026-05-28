# Codebase Structure

**Analysis Date:** 2026-05-28

> Two layouts coexist: the **current skeleton** (what `ls` actually shows today) and the **target layout** (what prd.md prescribes for the 14+ atomic slices). Section headers label which is which.

## Directory Layout — Current State (today)

```text
Aura/                                # repo root
├── .claude/                         # GSD framework installation (committed)
│   ├── .gsd-profile                 # selected profile
│   ├── agents/                      # 39 GSD sub-agent prompts (gsd-*.md)
│   ├── gsd-file-manifest.json
│   ├── gsd-install-state.json
│   ├── hooks/                       # gsd-check-update.js, gsd-graphify-update.sh
│   ├── package.json                 # GSD npm package metadata
│   └── settings.json
├── .git/
├── .gitignore                       # 53 lines, excludes /data /runtime-workspace /aura.db .env /.worktrees /.planning/tmp
├── .planning/                       # GSD planning workspace (being initialized now)
│   └── codebase/                    # ← this document and ARCHITECTURE.md land here
├── CLAUDE.md                        # 130-line project guidance (rewrite 2026-05-27)
├── README.md                        # 30 lines, links to cmd/aura/main.go + internal/agent/loop.go
├── cmd/
│   └── aura/
│       └── main.go                  # 90 LOC — subcommand router + stubClient
├── go.mod                           # `module github.com/chetto1983/aura` + `go 1.23`
├── internal/
│   ├── agent/
│   │   ├── loop.go                  # 131 LOC — Loop.Turn, MaxSteps=8, history append
│   │   └── tools/
│   │       ├── manifest.go          # 57 LOC — Render + RenderText, alphabetical sort
│   │       ├── search.go            # 94 LOC — ToolSearch built-in hook
│   │       ├── spec.go              # 61 LOC — Spec, Tool interface, Registry
│   │       └── text_response.go     # 44 LOC — TextResponse terminal tool
│   ├── llm/
│   │   └── client.go                # 78 LOC — Client interface + Chunk/Message/ToolCall/ToolDef
│   ├── sandbox/
│   │   └── sandbox.go               # 36 LOC — Runner interface + Stub
│   └── swarm/
│       └── swarm.go                 # 42 LOC — Coordinator interface + Stub, MaxSpawnDepth=3
└── prd.md                           # 4401 lines, 14+ slice plan (Slice 0.5 → 13)
```

**Total Go LOC today:** 543 (per `wc -l` across all `.go` files under `cmd/` + `internal/`).

**Notable absences vs CLAUDE.md / prd.md:**
- No `compose.yaml` file (referenced in CLAUDE.md §Persistence and prd.md Slice 0.5/0.7/9c/13)
- No `internal/db/`, `internal/knowledge/`, `internal/config/`, `internal/scoring/` directories
- No `internal/conversations/`, `internal/identity/`, `internal/cron/`, `internal/skills/`, `internal/web/`, `internal/memory/`, `internal/onboarding/`, `internal/channels/`, `internal/agui/`, `internal/setup/`
- No `sandbox/` infra directory (Dockerfile + seccomp.json + sidecar.py)
- No `Makefile`, no `sqlc.yaml`, no `lmcache.yaml`, no `.env.example`
- No `mcp.json` (referenced by CLAUDE.md §Persistence)

## Directory Layout — Target State (per prd.md)

```text
Aura/
├── .claude/                                 # unchanged
├── .env.example                             # (Slice 0.5) POSTGRES_PASSWORD, OPENROUTER_API_KEY, NEO4J_PASSWORD, etc.
├── .gitignore
├── .planning/                               # GSD workspace (gitignored except /codebase/)
├── CLAUDE.md
├── Makefile                                 # (Slice 0.5) make sqlc, make db-{up,migrate,reset}, make neo4j-{up,migrate,reset}
├── README.md
├── cmd/
│   └── aura/
│       ├── main.go                          # subcommand router (Cobra wiring)
│       ├── chat.go                          # aura chat {list|resume|new|archive|delete|rename} (Slice 1.8)
│       └── paused_states.go                 # aura paused-states {list|purge} (Slice 1.8)
├── compose.yaml                             # docker compose at root (per CLAUDE.md §Persistence)
├── go.mod
├── go.sum
├── internal/
│   ├── agent/
│   │   ├── agent.go                         # ~80 LOC — Agent interface, InvocationContext (Slice 0.9)
│   │   ├── event.go                         # ~70 LOC — Event, Actions, LLMResponse (Slice 0.9)
│   │   ├── llm_agent.go                     # ~480 LOC — LlmAgent impl (Slice 1, renamed from loop.go)
│   │   ├── llm_agent_pause.go               # split if llm_agent.go > 600 LOC (Slice 1.5/1.8)
│   │   ├── llm_agent_history.go             # split if needed (Slice 1.8)
│   │   ├── pending.go                       # ~95 LOC — PausedState type + sqlc adapter (Slice 1.5)
│   │   ├── tools/
│   │   │   ├── action.go                    # ~90 LOC — ActionRouter (introduced Slice 6)
│   │   │   ├── ask_user.go                  # ~180 LOC — ask_user + ErrAwaitingUserInput sentinel (Slice 1.5)
│   │   │   ├── execute.go                   # ~140 LOC — sandbox tool, Deferred=true (Slice 2)
│   │   │   ├── ingest.go                    # ~90 LOC — memory ingestion (Slice 11)
│   │   │   ├── manifest.go                  # existing
│   │   │   ├── memory.go                    # ~150 LOC — memory.search/recall/forget (Slice 11)
│   │   │   ├── read_tool_output.go          # ~80 LOC — sidecar reader, non-deferred (Slice 1)
│   │   │   ├── result.go                    # ~60 LOC — ToolResult type (Slice 1)
│   │   │   ├── search.go                    # existing (tool_search)
│   │   │   ├── spec.go                      # existing, signature change in Slice 1
│   │   │   ├── task.go                      # ~140 LOC — scheduler tool, ActionRouter (Slice 6)
│   │   │   ├── text_response.go             # existing
│   │   │   └── web_*.go                     # web_search.go, web_fetch.go (Slice 5)
│   │   └── workflow/
│   │       ├── loop.go                      # ~40 LOC — LoopAgent (Slice 0.9)
│   │       ├── parallel.go                  # ~70 LOC — ParallelAgent errgroup+ackChan (Slice 0.9)
│   │       └── sequential.go                # ~30 LOC — SequentialAgent (Slice 0.9)
│   ├── agui/                                # AG-UI gateway (Slice 8)
│   │   ├── client.go                        # ~80 LOC — Go SDK community wrapper
│   │   ├── emitter.go                       # ~+50 diff — in-process fanout API (Slice 9b add)
│   │   ├── fanout.go                        # ~80 LOC — multi-subscriber distribution (Slice 8)
│   │   ├── server.go                        # ~200 LOC — HTTP server, SSE
│   │   ├── translator.go                    # ~180 LOC — Event → AG-UI mapping
│   │   └── types.go                         # ~80 LOC — RunAgentInput parser
│   ├── askuser/
│   │   └── cli.go                           # ~120 LOC — CLI Responder renderer (Slice 1.5)
│   ├── channels/                            # Channel framework (Slice 9a)
│   │   ├── channel.go                       # ~70 LOC — Channel interface
│   │   ├── registry.go                      # ~100 LOC — StartAll/StopAll orchestration
│   │   ├── cli/
│   │   │   └── cli.go                       # ~150 LOC — CLI as Channel (migrated from cmd/aura/chat.go)
│   │   └── telegram/                        # Slice 9b + 9c
│   │       ├── agui_subscriber.go           # ~140 LOC — subscribe to agui.Fanout
│   │       ├── bot.go                       # ~100 LOC — tele.Bot wrapper
│   │       ├── commands.go                  # ~170 LOC — 8 MVP commands bot-intercept
│   │       ├── config.go                    # ~60 LOC — BotToken, bind addr
│   │       ├── documents.go                 # ~160 LOC — markitdown tiered sync/async (9c)
│   │       ├── handlers.go                  # ~+50 diff — auto ingest.file on doc attach (Slice 11)
│   │       ├── hitl.go                      # ~150 LOC — ask_user → InlineKeyboard / ForceReply
│   │       ├── onboarding.go                # ~80 LOC — /start <token> matcher
│   │       ├── photo.go                     # ~90 LOC — Gemma 4 multimodal photo (9c)
│   │       ├── renderer.go                  # ~250 LOC — AG-UI events → Telegram messages
│   │       ├── status_pane.go               # ~180 LOC — Pattern B status pane
│   │       ├── store.go                     # ~80 LOC — sqlc adapter telegram_accounts + setup_pending
│   │       └── voice.go                     # ~150 LOC — Gemma 4 multimodal STT (9c)
│   ├── config/
│   │   └── config.go                        # ~110 LOC — root composite Config{LLM, DB, RunDir, ToolPreviewCap}
│   ├── conversations/                       # Slice 1.8
│   │   ├── budget.go                        # ~50 LOC — L2 hard cap (Slice 1.8 #17)
│   │   ├── cleanup.go                       # ~70 LOC — boot orphan scan + tmp/ TTL
│   │   ├── microcompact.go                  # ~60 LOC — L1 tool turn eviction
│   │   ├── sidecar.go                       # ~40 LOC — content > 64 KiB spillover
│   │   ├── store.go                         # ~120 LOC — sqlc adapter, atomic AppendTurn
│   │   ├── title.go                         # ~60 LOC — LLM-generated auto-title best-effort
│   │   └── types.go                         # ~50 LOC — Conversation, Turn domain types
│   ├── cron/                                # Scheduler (Slice 6)
│   │   ├── handlers/
│   │   │   ├── agent_job.go                 # ~120 LOC — AgentJobAgent (impl agent.Agent)
│   │   │   ├── handler.go                   # ~50 LOC — Handler = type alias of agent.Agent + metadata
│   │   │   ├── reminder.go                  # ~70 LOC — ReminderAgent
│   │   │   └── backup_postgres.go / backup_neo4j.go  # Slice 6b
│   │   ├── scheduler.go                     # ~180 LOC — tick loop + crash-recovery
│   │   ├── store.go                         # ~80 LOC — sqlc adapter
│   │   └── types.go                         # ~100 LOC — Task, TaskKind, ScheduleKind, Status enums
│   ├── db/                                  # Postgres infra (Slice 0.5)
│   │   ├── config.go                        # ~40 LOC — DBConfig
│   │   ├── db.go                            # ~90 LOC — pgxpool.Pool open + ping
│   │   ├── migrate.go                       # ~80 LOC — golang-migrate wrapper with embed.FS
│   │   ├── schema.sql                       # ~20 LOC — source of truth for sqlc
│   │   ├── migrations/                      # *.up.sql / *.down.sql numbered
│   │   │   ├── 0001_init.up.sql             # CREATE SCHEMA aura;
│   │   │   ├── 0002_knowledge_migrations.up.sql   # Slice 0.7
│   │   │   ├── 0003_paused_states.up.sql    # Slice 1.5
│   │   │   ├── 0004_identity.up.sql         # Slice 1.7
│   │   │   ├── 0005_conversations.up.sql    # Slice 1.8 (renamed from 0005_scheduler)
│   │   │   ├── 0006_scheduler.up.sql        # Slice 6
│   │   │   ├── 0007_skill_audit.up.sql      # Slice 7
│   │   │   ├── 0008_telegram.up.sql         # Slice 9a
│   │   │   ├── 0009_profile_audit.up.sql    # Slice 10
│   │   │   ├── 0010_sandbox_sessions.up.sql # Slice 2b
│   │   │   ├── 0011_ingest_audit.up.sql     # Slice 11
│   │   │   └── 0013_local_llm.up.sql        # Slice 13
│   │   ├── queries/                         # sqlc source files (1 file ≈ 1 domain area)
│   │   │   ├── conversations.sql            # Slice 1.8
│   │   │   ├── conversation_turns.sql       # Slice 1.8
│   │   │   ├── identities.sql               # Slice 1.7
│   │   │   ├── ingest_audit.sql             # Slice 11
│   │   │   ├── knowledge_migrations.sql     # Slice 0.7
│   │   │   ├── local_llm_cost.sql           # Slice 13
│   │   │   ├── local_llm_sessions.sql       # Slice 13
│   │   │   ├── paused_states.sql            # Slice 1.5
│   │   │   ├── profile_audit.sql            # Slice 10
│   │   │   ├── sandbox_sessions.sql         # Slice 2b
│   │   │   ├── scheduler.sql                # Slice 6
│   │   │   ├── skill_audit.sql              # Slice 7
│   │   │   ├── telegram_accounts.sql        # Slice 9a
│   │   │   └── telegram_setup_pending.sql   # Slice 9a
│   │   └── sqlc/                            # generated code (committed; CI golden test)
│   ├── identity/                            # Slice 1.7
│   │   ├── capability.go                    # ~60 LOC — HasCapability + wildcard
│   │   ├── store.go                         # ~80 LOC — sqlc adapter
│   │   └── types.go                         # ~40 LOC — Identity + Kind enum
│   ├── knowledge/                           # Neo4j infra (Slice 0.7)
│   │   ├── client.go                        # ~80 LOC — MCP subprocess + Cypher wrapper
│   │   ├── config.go                        # ~40 LOC — Neo4jConfig
│   │   ├── migrate.go                       # ~90 LOC — *.cypher migrations + audit in PG
│   │   ├── ping.go                          # ~30 LOC — RETURN 1 health check
│   │   └── migrations/                      # numbered Cypher migrations
│   │       ├── 0001_init.cypher             # constraints + HNSW vector + fulltext (Slice 0.7)
│   │       └── 0002_memory_schema.cypher    # full schema (Slice 11a)
│   ├── llm/                                 # Slice 1 + 4 + 13
│   │   ├── client.go                        # existing interface
│   │   ├── cost_tracker.go                  # ~100 LOC — sqlc adapter local_llm_cost (Slice 13)
│   │   ├── offline_detector.go              # ~80 LOC — TCP dial poller (Slice 13)
│   │   ├── openai_compat/
│   │   │   ├── client.go                    # ~280 LOC — SSE parser + tool-call accumulator (Slice 1)
│   │   │   ├── client_test.go               # ~120 LOC — golden SSE fixtures
│   │   │   ├── models.go                    # ~+30 LOC — ContextWindow + MaxOutputTokens lookup
│   │   │   └── testdata/                    # SSE fixtures
│   │   ├── prompt/                          # Slice 4 KV cache builder
│   │   │   ├── builder.go                   # provider-aware cache_control injection
│   │   │   ├── cache.go                     # stable-prefix discipline helpers
│   │   │   └── builder_test.go
│   │   └── router.go                        # ~150 LOC — LLMRouter (Slice 13)
│   ├── memory/                              # Slice 11
│   │   ├── agent/
│   │   │   ├── insight.go                   # ~200 LOC — cross-conv pattern analyzer (11e)
│   │   │   └── journal.go                   # ~150 LOC — post-conv :AgentEpisode (11e)
│   │   ├── graph/
│   │   │   ├── community.go                 # ~200 LOC — Leiden via GDS (11c)
│   │   │   └── memify.go                    # ~250 LOC — prune/strengthen/derive (11e)
│   │   ├── ingest/
│   │   │   ├── audit.go                     # ~50 LOC — sqlc adapter ingest_audit
│   │   │   ├── chunker.go                   # ~120 LOC — recursive semantic
│   │   │   ├── embedder.go                  # ~100 LOC — batch aura-llama-embed
│   │   │   ├── entity_extractor.go          # ~180 LOC — mem0 2-fase
│   │   │   └── pipeline.go                  # ~150 LOC — Cognify 6-stage orchestration
│   │   └── retrieval/
│   │       ├── global_search.go             # ~70 LOC — GraphRAG global pattern
│   │       ├── recall.go                    # ~80 LOC — entity-based recall
│   │       ├── rerank.go                    # ~100 LOC — LLM tier=worker rerank
│   │       └── search.go                    # ~150 LOC — hybrid BM25+HNSW+graph
│   ├── onboarding/                          # Slice 10
│   │   ├── extractor.go                     # ~150 LOC — LLM-driven fact extraction
│   │   ├── injector.go                      # ~80 LOC — Agent.md as 2nd system message
│   │   ├── interview.go                     # ~80 LOC — LoopAgent[InterviewStepAgent]
│   │   ├── steps.go                         # ~120 LOC — InterviewStepAgent + SummaryConfirmAgent
│   │   ├── store.go                         # ~100 LOC — filesystem ~/.aura/agents/<id>/
│   │   └── updater.go                       # ~200 LOC — hybrid auto-update (mem0 ADD-only)
│   ├── sandbox/                             # Slice 2a + 2b
│   │   ├── config.go                        # ~50 LOC — AURA_SANDBOX_URL, timeouts
│   │   ├── docker.go                        # ~220 LOC — DockerRunner HTTP client (2a)
│   │   ├── network.go                       # ~80 LOC — allowlist + iptables hooks (2b)
│   │   ├── sandbox.go                       # existing Stub (replaced in 2a)
│   │   ├── sessions.go                      # ~150 LOC — SessionManager (2b)
│   │   └── workspace.go                     # ~80 LOC — workspace mount manager (2b)
│   ├── scoring/                             # Risk-Based governance
│   │   └── scoring.go                       # ~100 LOC — ComputeTaskTier / ComputeSkillTier
│   ├── setup/                               # Slice 9a setup wizard
│   │   ├── handlers.go                      # ~200 LOC — POST /setup/* endpoints
│   │   ├── page.html                        # ~250 LOC — embedded HTML+CSS+JS dark theme
│   │   ├── qr.go                            # ~50 LOC — SVG QR generation
│   │   ├── server.go                        # ~150 LOC — HTTP server on 127.0.0.1:9081
│   │   └── types.go                         # ~40 LOC
│   ├── skills/                              # Slice 7 a-e
│   │   ├── audit.go                         # ~70 LOC — sqlc adapter skill_audit
│   │   ├── catalog.go                       # ~140 LOC — skills.sh fetch + parse
│   │   ├── deleter.go                       # ~70 LOC — FS remove + Invalidate
│   │   ├── installer.go                     # ~140 LOC — npx skills add (--ignore-scripts)
│   │   ├── loader/
│   │   │   ├── cache.go                     # ~80 LOC — sync.RWMutex + TTL 1s
│   │   │   ├── filesystem.go                # ~100 LOC — FS scan multi-root
│   │   │   ├── loader.go                    # ~60 LOC — coordinator
│   │   │   └── parser.go                    # ~80 LOC — YAML frontmatter
│   │   ├── paths.go                         # ~40 LOC — SanitizeName chokepoint
│   │   ├── types.go                         # ~50 LOC — Skill struct
│   │   ├── validator.go                     # ~120 LOC — single source of truth
│   │   ├── writer.go                        # ~120 LOC — atomic write pending/ → active/
│   │   └── snippet.go                       # Slice 7e — executable code snippets
│   ├── swarm/                               # Slice 3
│   │   ├── bus.go                           # shared message bus + DM-by-ID
│   │   ├── coordinator.go                   # Spawn/Talk/Join, tier model
│   │   ├── swarm.go                         # existing Stub (replaced in Slice 3)
│   │   └── tier.go                          # chat/reasoning/worker classification
│   └── web/                                 # Slice 5
│       ├── config.go                        # ~40 LOC — SearXNGURL, timeouts, allowlist
│       ├── fetcher.go                       # ~120 LOC — SSRF-defended Fetch
│       ├── html.go                          # ~90 LOC — readability + html→markdown
│       └── searxng.go                       # ~130 LOC — Query client
├── lmcache.yaml                             # ~25 LOC — LMCache disk-tier config (Slice 13)
├── mcp.json                                 # MCP server registration (created on `aura init`)
├── prd.md
├── sandbox/                                 # sandbox sidecar materials (Slice 2)
│   ├── Dockerfile                           # ~30 LOC — python:3.12-slim + bash + non-root user
│   ├── compose.yaml                         # ~25 LOC initial, grows per slice (postgres/neo4j/embed/multimodal/vllm)
│   ├── seccomp.json                         # ~80 LOC — default-deny + allow list
│   └── sidecar.py                           # ~150 LOC — stdlib http.server, /exec/python /exec/shell /session/{id}
└── sqlc.yaml                                # ~30 LOC — sqlc v2 config (Slice 0.5)
```

## Directory Purposes — Current State

**`cmd/aura/`:**
- Purpose: Single binary entry point
- Contains: `main.go` only (90 LOC)
- Key files: `cmd/aura/main.go` — subcommand router, registry builder, stubClient implementation

**`internal/agent/`:**
- Purpose: Conversation lifecycle + tool dispatch
- Contains: Loop struct + tools subpackage
- Key files: `internal/agent/loop.go` (Loop.Turn, MaxSteps=8), `internal/agent/tools/spec.go` (Tool interface), `internal/agent/tools/manifest.go` (alphabetical sort for cache stability)

**`internal/llm/`:**
- Purpose: Provider-neutral streaming contract
- Contains: Interface + types only, no implementation
- Key files: `internal/llm/client.go` — `Client.Stream(ctx, req) (<-chan Chunk, error)`

**`internal/sandbox/`:**
- Purpose: Isolated execution stub
- Contains: Runner interface + Stub
- Key files: `internal/sandbox/sandbox.go` — Stub returns "not yet implemented"

**`internal/swarm/`:**
- Purpose: Parallel agents coordinator stub
- Contains: Coordinator interface + Stub
- Key files: `internal/swarm/swarm.go` — Stub + `MaxSpawnDepth = 3` constant

**`.claude/`:**
- Purpose: GSD (Get Shit Done) framework installation
- Contains: 39 sub-agent prompts under `agents/`, hooks, settings
- Generated: No (committed per `.gitignore` policy `# Claude Code: GSD installation is committed`)
- Committed: Yes (except `cache/`, `sessions/`, `.local-state/`)

**`.planning/`:**
- Purpose: GSD planning workspace
- Contains: `codebase/` (this directory), eventually `phases/`, `nyquist/`
- Generated: Yes
- Committed: Partially — `/tmp/` excluded via `.gitignore` line 33, `/codebase/` IS committed

## Key File Locations — Current State

**Entry Points:**
- `cmd/aura/main.go`: subcommand router (today only `tools` + `chat` are wired)

**Configuration:**
- `go.mod`: minimal module declaration, `go 1.23`
- `.gitignore`: excludes runtime artifacts (`/data/`, `/runtime-workspace/`, `/garage/`, `/aura.db*`, `/.env`, `/.worktrees/`, `/.planning/tmp/`)
- `CLAUDE.md`: project guidance (rewrite 2026-05-27)
- `README.md`: 30-line summary linking to `cmd/aura/main.go` + `internal/agent/loop.go`

**Core Logic:**
- `internal/agent/loop.go`: 131 LOC, `Loop.Turn`, MaxSteps=8, append-only Messages slice
- `internal/agent/tools/spec.go`: 61 LOC, `Tool` interface + `Registry`
- `internal/agent/tools/manifest.go`: 57 LOC, `Render()` returns alphabetically sorted slice for cache stability

**Testing:**
- No `*_test.go` files yet (skeleton stage)
- Slice 1 will add `internal/llm/openai_compat/client_test.go` + `testdata/` SSE fixtures

## Key File Locations — Target State (per prd.md File targets sections)

**Entry Points:**
- `cmd/aura/main.go`: Cobra setup, sub-command routing
- `cmd/aura/chat.go`: `aura chat {list|resume|new|archive|delete|rename}` (Slice 1.8)
- `cmd/aura/paused_states.go`: `aura paused-states {list|purge}` (Slice 1.8)

**Configuration:**
- `internal/config/config.go`: root composite Config (~110 LOC). Per-subsystem configs live in their own packages (`internal/web/config.go`, `internal/sandbox/config.go`, `internal/knowledge/config.go`, etc.) — explicit non-god-class rule (Slice 0.5 acceptance)
- `.env.example`: template with `POSTGRES_PASSWORD=changeme`, `OPENROUTER_API_KEY=`, `NEO4J_PASSWORD=changeme`
- `sqlc.yaml`: sqlc v2 config (engine postgresql, queries dir, output dir, emit_interface)
- `sandbox/compose.yaml`: docker compose with healthchecks, depends_on conditions, named volumes
- `lmcache.yaml`: LMCache disk-tier configuration (50 GB cache, chunk_size 256)
- `mcp.json`: MCP server registrations (created by `aura init`)
- `Makefile`: `make sqlc`, `make db-{up,migrate,reset}`, `make neo4j-{up,migrate,reset}`

**Core Logic (Runtime layer):**
- `internal/agent/agent.go`: `Agent` interface + `InvocationContext` + builder helpers (Slice 0.9)
- `internal/agent/event.go`: `Event` + `Actions` + `LLMResponse` (Slice 0.9)
- `internal/agent/llm_agent.go`: `LlmAgent` implementing `Agent` (Slice 1)
- `internal/agent/workflow/{sequential,loop,parallel}.go`: built-in workflow agents (Slice 0.9)
- `internal/agent/tools/`: registry + ToolResult + ActionRouter + ask_user + execute + task + skill + memory + web tools

**Persistence layer:**
- `internal/db/db.go`: `Open(ctx, cfg) (*pgxpool.Pool, error)` with ping at boot
- `internal/db/migrate.go`: `golang-migrate` wrapper with `embed.FS` migrations
- `internal/db/queries/*.sql`: sqlc source files, one per domain area
- `internal/db/migrations/NNNN_*.up.sql` / `.down.sql`: numbered migrations
- `internal/knowledge/client.go`: MCP-neo4j-cypher stdio subprocess wrapper
- `internal/knowledge/migrate.go`: Cypher migrations with audit row in Postgres
- `internal/knowledge/migrations/*.cypher`: Cypher schema migrations

**Capabilities layer:**
- `internal/sandbox/docker.go`: HTTP client → sidecar (Slice 2a)
- `internal/sandbox/sessions.go`: session-bound containers (Slice 2b)
- `internal/swarm/coordinator.go`: Spawn/Talk/Join + tier model (Slice 3)
- `internal/cron/scheduler.go`: tick loop + crash-recovery (Slice 6)
- `internal/cron/handlers/<kind>.go`: one file per TaskKind, each implementing `agent.Agent`
- `internal/skills/loader/{filesystem,parser,cache,loader}.go`: 4-way split (Slice 7a)
- `internal/skills/validator.go`: single source of truth
- `internal/skills/paths.go`: `SanitizeName` chokepoint (audit P0)
- `internal/web/{searxng,fetcher,html}.go`: web tools (Slice 5)
- `internal/memory/{ingest,graph,retrieval,agent}/*.go`: memory subsystem (Slice 11)
- `internal/onboarding/{interview,steps,store,injector,extractor,updater}.go`: onboarding (Slice 10)

**Client layer:**
- `internal/channels/channel.go`: Channel interface
- `internal/channels/registry.go`: StartAll/StopAll
- `internal/channels/cli/cli.go`: CLI as Channel (migrated from `cmd/aura/chat.go`)
- `internal/channels/telegram/*.go`: bot + renderer + HITL + commands + multimodal handlers
- `internal/agui/server.go`: HTTP server `POST /agent/run` (SSE) + `GET /threads/<id>/messages`
- `internal/setup/server.go`: setup wizard on `127.0.0.1:9081`

**Testing:**
- `internal/llm/openai_compat/client_test.go`: golden SSE fixtures (Slice 1)
- `internal/llm/openai_compat/testdata/`: SSE response captures
- `internal/agent/workflow/workflow_test.go`: workflow agent tests + escalation (Slice 0.9)
- `internal/conversations/store_test.go`: build tag `db_integration` (Slice 1.8)
- `internal/identity/store_test.go`: build tag `db_integration` (Slice 1.7)
- `internal/sandbox/docker_test.go`: build tag `sandbox_integration` (Slice 2a)
- `internal/sandbox/sessions_test.go`: build tag `sandbox_integration` (Slice 2b)
- `internal/knowledge/client_test.go`: build tag `neo4j_integration` (Slice 0.7)
- `internal/channels/telegram/*_test.go`: build tag for multimodal `multimodal_integration` (Slice 9c)

## Naming Conventions

**Files (Go source):**
- snake_case: `llm_agent.go`, `ask_user.go`, `read_tool_output.go`, `agui_subscriber.go`, `status_pane.go`, `entity_extractor.go`, `paused_states.go`
- Stuttering with package OK if it improves clarity: `internal/sandbox/sandbox.go`, `internal/swarm/swarm.go`, `internal/skills/skills.go` (none today, but pattern allowed)
- Test files: `*_test.go` co-located in same package
- Build-tagged files: standard `//go:build <tag>` directive at top, no filename suffix (e.g., `client_test.go` with `//go:build db_integration`)

**Files (SQL):**
- Migrations: `NNNN_<descriptor>.up.sql` + matching `.down.sql`. Numbering monotonic, 4-digit zero-padded (`0001_init.up.sql`, `0011_ingest_audit.up.sql`). Renumber if a slice is inserted out of order — `aura.knowledge_migrations` audit table catches mismatches.
- Queries: snake_case domain name, `internal/db/queries/<domain>.sql` (one file per concern: `paused_states.sql`, `identities.sql`, `conversations.sql`, `scheduler.sql`, `skill_audit.sql`)
- Cypher: `NNNN_<descriptor>.cypher` under `internal/knowledge/migrations/`

**Files (Skills):**
- `~/.aura/skills/active/<skill-name>/SKILL.md`: regex `^[a-z0-9-]+$`, length 1-64, no reserved (`init`, `delete`, `.`, `..`). Single chokepoint `internal/skills/paths.go:SanitizeName`.

**Files (Onboarding profile):**
- `~/.aura/agents/<identity_id>/`:
  - `Agent.md` — user-facing markdown
  - `preferences.json` — structured prefs (lang, timezone, voice_mode, tone)
  - `metadata.json` — version, timestamps, observation_counts
  - `changelog.md` — append-only audit log

**Files (Runtime artifacts):**
- `$AURA_RUN_DIR/conversations/<conv_id>/<tool_call_id>.result` — ToolResult sidecar (Slice 1)
- `$AURA_RUN_DIR/conversations/<conv_id>/<seq>.content` — Conversation turn content spillover (Slice 1.8)
- `$AURA_RUN_DIR/conversations/<conv_id>/workspace/` — Sandbox session workspace mount (Slice 2b)
- `$AURA_RUN_DIR/tmp/<unix-ts>-<rand4>.<ext>` — Oneoff scratch, 24h TTL

**Directories:**
- `internal/` for Go packages (Go convention: not importable outside module root)
- `cmd/<binary>/` for each binary entry point (Go convention)
- `sandbox/` (NOT `internal/sandbox/sandbox/`) at repo root for sidecar infra (Dockerfile, compose, sidecar.py, seccomp.json). Separate from Go code.

**Packages (Go):**
- One concept per package: `agent`, `llm`, `sandbox`, `swarm`, `conversations`, `identity`, `knowledge`, `cron`, `skills`, `web`, `memory`, `onboarding`, `channels`, `agui`, `setup`, `scoring`, `config`, `db`
- Sub-packages allowed for clear concerns: `agent/tools/`, `agent/workflow/`, `skills/loader/`, `llm/openai_compat/`, `llm/prompt/`, `memory/{ingest,graph,retrieval,agent}/`, `channels/{cli,telegram}/`, `cron/handlers/`, `db/{queries,migrations,sqlc}/`

**Environment variables:**
- Convention: `AURA_<DOMAIN>_<UNIT>` uppercase snake_case
- Examples:
  - `AURA_LLM_BASE_URL`, `AURA_LLM_API_KEY`, `AURA_LLM_MODEL`
  - `AURA_DB_URL`, `AURA_RUN_DIR`, `AURA_CONFIG_DIR`
  - `AURA_CONTEXT_PREVIEW_CAP_BYTES=2048`
  - `AURA_CONVERSATION_TURN_CAP_BYTES=65536`
  - `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS=10`
  - `AURA_RUN_DIR_WARN_THRESHOLD_BYTES=1073741824`
  - `AURA_SANDBOX_URL`, `AURA_SANDBOX_TIMEOUT_SEC`, `AURA_SANDBOX_SESSION_TTL_SEC=1800`, `AURA_SANDBOX_MAX_CONCURRENT_SESSIONS=5`, `AURA_SANDBOX_NETWORK_ALLOW_HOSTS`, `AURA_SANDBOX_WORKSPACE_MAX_BYTES=104857600`
  - `AURA_TELEGRAM_STATUS_THROTTLE_MS=1500`, `AURA_TELEGRAM_CONTENT_THROTTLE_MS=500`, `AURA_TELEGRAM_CHAT_RATE_LIMIT_MS=1000`
  - `AURA_PROFILE_CERTAINTY_N=3`
  - `AURA_MEMORY_CHUNK_SIZE_TOKENS=512`, `AURA_MEMORY_CHUNK_OVERLAP_TOKENS=64`, `AURA_MEMORY_COMMUNITY_INTERVAL_HR=24`, `AURA_MEMORY_INSIGHT_INTERVAL_MIN=60`, `AURA_MEMORY_MEMIFY_INTERVAL_HR=24`
  - `AURA_RISK_ALERT_THRESHOLD=risky`
  - `AURA_SKILL_BODY_CAP_BYTES=32768`
  - `AURA_CHANNEL_<NAME>_ENABLED` (default true)
  - `AURA_SETUP_BIND=127.0.0.1:9081`
  - `AURA_AGUI_CORS_PERMISSIVE=1`
- External provider envs follow their convention without AURA prefix: `POSTGRES_PASSWORD`, `NEO4J_USER`, `NEO4J_PASSWORD`, `NEO4J_PLUGINS`, `TELEGRAM_BOT_TOKEN`, `OPENROUTER_API_KEY`

**Postgres tables and schema:**
- Schema: `aura` (dedicated, not `public`). Set in `0001_init.up.sql`. Search path: `SET search_path TO aura, public;`
- Table names: plural snake_case under `aura.<plural>`: `aura.conversations`, `aura.conversation_turns`, `aura.paused_states`, `aura.identities`, `aura.capability_grants`, `aura.scheduler_tasks`, `aura.skill_audit`, `aura.profile_audit`, `aura.ingest_audit`, `aura.agent_job_runs`, `aura.telegram_accounts`, `aura.telegram_setup_pending`, `aura.sandbox_sessions`, `aura.local_llm_sessions`, `aura.local_llm_cost`, `aura.knowledge_migrations`
- Primary keys: `uuid` with `gen_random_uuid()` default unless natural key (e.g. `telegram_accounts.telegram_user_id BIGINT PRIMARY KEY`)
- Enums: `text` column + `CHECK (col IN (...))` (not Postgres ENUM type — easier migration)
- Timestamps: `timestamptz NOT NULL DEFAULT now()` for created/updated columns

**Neo4j labels and relations:**
- PascalCase labels: `:Document`, `:Chunk`, `:Entity`, `:Community`, `:UserConversation`, `:UserSnippet`, `:AgentEpisode`, `:AgentInsight`
- SCREAMING_SNAKE_CASE relations: `:HAS_CHUNK`, `:MENTIONS`, `:RELATED_TO`, `:IN_COMMUNITY`, `:CONTAINS`, `:DISCUSSED`, `:CITES`, `:USED_SNIPPET`, `:LEARNED`, `:HANDLED`, `:DERIVED_FROM`
- Properties: camelCase or snake_case allowed, prd uses snake_case more often (`mention_count`, `first_seen_at`, `last_mentioned_at`, `agent_kind`)

**Test build tags:**
- `db_integration` — requires Postgres container
- `neo4j_integration` — requires Neo4j container
- `sandbox_integration` — requires sandbox sidecar container
- `multimodal_integration` — requires Gemma 4 multimodal sidecar
- Pattern: `//go:build <tag>` at file top. Tests skip cleanly in CI without sidecar.

## Where to Add New Code

**New Tool (LLM-facing):**
- Implementation: `internal/agent/tools/<name>.go`
- Tool spec metadata constant defined in the same file
- Big tools (long Description, complex Parameters): set `Spec.Deferred=true`
- Small built-in tools (terminal `text_response`, hook `tool_search`, `ask_user`, `read_tool_output`): `Spec.Deferred=false`
- Tests: `internal/agent/tools/<name>_test.go` co-located
- Register in `cmd/aura/main.go:buildRegistry()` (or future `internal/agent/tools/manifest.go` registration hook)

**New Agent Implementation (Slice 0.9+):**
- Implement `agent.Agent` interface (`Name`, `Description`, `Run(InvocationContext) iter.Seq2[*Event, error]`, `SubAgents`, `FindAgent`)
- File location depends on domain: `internal/cron/handlers/<kind>.go` for scheduler handlers, `internal/onboarding/steps.go` for interview steps, `internal/swarm/` for workers (= reuse `LlmAgent`)
- Emit `Event{Actions.Escalate=true}` to bubble up termination through parent workflow agents

**New Channel (Slice 9a+):**
- Directory: `internal/channels/<name>/`
- Implement `Channel` interface (`Name()`, `Start(ctx, sub)`, `Stop()`, `IsHealthy()`)
- Env var: `AURA_CHANNEL_<NAME>_ENABLED` (default true)
- Add to `internal/channels/registry.go` registration

**New Postgres migration:**
- File: `internal/db/migrations/NNNN_<descriptor>.up.sql` + matching `.down.sql`
- Numbering: monotonic 4-digit zero-padded. Pick the next free number.
- Renumber subsequent migrations if you insert out of sequence (rare, audit caught via `aura.knowledge_migrations` checksum)
- Add `.sql` source under `internal/db/queries/<domain>.sql` for sqlc
- Run `make sqlc` to regenerate code under `internal/db/sqlc/` (CI golden-tests output)

**New Neo4j migration:**
- File: `internal/knowledge/migrations/NNNN_<descriptor>.cypher`
- Frontmatter: `-- migrate:up`
- Audit row goes into Postgres `aura.knowledge_migrations` (centralized audit with golang-migrate)

**New Risk-Based Governance kind:**
- Update mapping in `internal/scoring/scoring.go`
- Update relevant `Compute*Tier` function
- Update audit table in respective domain (e.g., `aura.skill_audit`, `aura.agent_job_runs`)

**New Memory entity type:**
- Update Cypher schema in `internal/knowledge/migrations/<next>.cypher`
- Update entity taxonomy in `internal/memory/ingest/entity_extractor.go`
- Update retrieval handling in `internal/memory/retrieval/search.go`

**Utility / Shared helper:**
- Cross-cutting helpers in their own package, NOT `internal/util/` or `internal/common/` (no god-class package)
- If used by one domain, lives in that domain's package
- If used by 2+ unrelated domains, create a dedicated thin package (e.g., `internal/scoring/` for Risk-Based, `internal/askuser/` for CLI Responder)

**Configuration:**
- Per-subsystem config struct in subsystem's own package: `internal/sandbox/config.go`, `internal/web/config.go`, `internal/knowledge/config.go`, `internal/cron/config.go`, `internal/skills/config.go`
- Root composite `internal/config/config.go` references them via `Config{LLM, DB, RunDir, ToolPreviewCap}` — NO subsystem fields directly under root Config
- Load order (Slice 1): built-in default → `.env` (joho/godotenv) → file JSON (`$AURA_CONFIG_DIR/llm.json`) → env vars (`AURA_*`)

## Special Directories

**`.claude/`:**
- Purpose: GSD framework (Get Shit Done agentic workflow)
- Generated: No
- Committed: Yes (per `.gitignore` comment policy)
- Subdirectories `cache/`, `sessions/`, `.local-state/` are excluded

**`.planning/`:**
- Purpose: GSD planning workspace
- Generated: Partially (`.planning/codebase/` written by mapper agents; `.planning/phases/` by planner; `.planning/tmp/` excluded)
- Committed: Partially — `/tmp/` excluded

**`.worktrees/`:**
- Purpose: Git worktrees for parallel phase execution (GSD convention)
- Generated: Yes
- Committed: No (excluded in `.gitignore`)

**`internal/`:**
- Purpose: Go convention — packages here are NOT importable from outside the module root `github.com/chetto1983/aura`
- Generated: No
- Committed: Yes

**`sandbox/` (target):**
- Purpose: Sandbox sidecar materials (Dockerfile, compose, seccomp profile, sidecar.py)
- Generated: No
- Committed: Yes
- NOT to be confused with `internal/sandbox/` (Go package)

**`internal/db/sqlc/` (target):**
- Purpose: sqlc-generated Go code from `queries/*.sql`
- Generated: Yes (`make sqlc`)
- Committed: Yes — CI golden-tests output sync with current commit

**`~/.aura/` (runtime, outside repo):**
- Purpose: User-scope runtime state
- Subdirectories: `agents/<identity_id>/` (Agent.md per identity), `skills/active/`, `skills/pending/`, `backups/postgres/`, `llm.json` config
- Generated: Yes (at first `aura init` / first run)
- Committed: No (outside repo)

**`$AURA_RUN_DIR/` (runtime, default `~/.aura/run/`):**
- Purpose: Per-conversation runtime artifacts (tool result sidecars, content spillover, workspace mounts)
- Subdirectories: `conversations/<conv_id>/`, `tmp/`
- Lifetime: Cascade-deleted with `aura chat delete <conv_id>`; `tmp/` swept at boot (24h TTL); boot orphan scan
- Generated: Yes
- Committed: No (excluded by `.gitignore` line 24 `/runtime-workspace/` and Slice 1 default `~/.aura/run/` outside repo)

---

*Structure analysis: 2026-05-28*
