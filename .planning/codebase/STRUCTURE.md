# Codebase Structure

**Analysis Date:** 2026-05-27

## Directory Layout

```
D:/Aura/
├── cmd/                          # All executable binaries (Go main packages)
│   ├── aura/                     # Main binary: bot + dashboard + agent loop
│   ├── aura_mcp_server/          # Aura-as-MCP-server (exposes Aura tools to other agents)
│   ├── aura-init-models/         # One-shot init container: fetch model GGUFs
│   ├── bench_ctx/                # Context-window microbench
│   ├── build_icon/               # Windows tray icon builder
│   ├── chat/                     # Standalone chat REPL
│   ├── debug_*/                  # 10 debug harnesses (llm, searxng, ingest, tools, xlsx, pdf, docx, backup, qdrant, reconcile, common, convdump, telegram)
│   ├── module_health/            # Module health check tool
│   ├── probe_chat/               # E2E probe driver (canonical Aura tester)
│   ├── probe_doc/                # Document generation probe
│   ├── probe_ingest_e2e/         # Ingestion E2E probe
│   ├── probe_reasoning/          # Reasoning-mode probe
│   ├── probe_telegram_ui/        # Telegram CDP UI probe
│   ├── probe_webfetch/           # Web fetch probe
│   ├── quality_bench/            # Quality benchmark runner
│   └── seed_e2e_env/             # E2E environment seeding helper
├── internal/                     # All Aura library code (non-public Go packages)
│   ├── agent/                    # Agent runtime (loop, executor, governance, tools)
│   ├── agentcore/                # Invocation type (the per-turn descriptor)
│   ├── agentnote/                # Per-conversation working-plan note store
│   ├── api/                      # HTTP/JSON API + embedded SPA (100 files)
│   ├── audio/                    # Voice mode policy (off/voice_only/all)
│   ├── backup/                   # Garage S3 backup/export
│   ├── budget/                   # Cost budget tracking
│   ├── channels/                 # Channel adapters (telegram, web, cron, silent, askuser)
│   ├── chat/                     # Channel-neutral Hub + AgentLoop adapter + types
│   ├── concurrency/              # User gates, queue trackers
│   ├── config/                   # Env + SQLite settings loader + applier + bootstrap
│   ├── conversation/             # Sliding window, prompt overlays, compaction, summarizer
│   ├── cron/                     # Scheduler + agent jobs + maintenance + WAL checkpoint
│   ├── ctxmetrics/               # Context-window metrics
│   ├── db/                       # SQLite open/close/recovery + 29 migrations
│   ├── dbrecovery/               # Corruption repair (FTS5 + run_events)
│   ├── files/                    # Workspace file generators (xlsx, docx, pdf)
│   ├── httputil/                 # Shared HTTP client
│   ├── identity/                 # Channel-neutral principal + capability grants
│   ├── install/                  # Model download + SHA-256 verify (init-models)
│   ├── learning/                 # Lesson promoter, reflection fork, user-memory writer
│   ├── llm/                      # LLM client (OpenAI-compat chat + streaming + tool fragments)
│   │   ├── whisper/              # Whisper STT sidecar client
│   │   └── pockettts/            # Pocket-TTS sidecar client
│   ├── logging/                  # zap-backed slog + secret sanitizer + daily writer
│   ├── mcp/                      # MCP client (stdio + Streamable-HTTP), policy, watcher
│   ├── opsfile/                  # Operational-memory ingestor
│   ├── probe/                    # Document inspect probe lib (docinspect)
│   ├── release/                  # Version, commit, build-date constants
│   ├── sandbox/                  # Python sandbox process runner
│   ├── secrets/                  # SQLite secrets store
│   ├── skills/                   # Anthropic-style SKILL.md loader + catalog client
│   ├── storage/                  # All persistence wrappers
│   │   ├── freshness/            # Projection-state recency tracking
│   │   ├── memoryindex/          # Compact memory mirror (SQLite + Qdrant)
│   │   ├── qdrant/               # Qdrant HTTP client
│   │   ├── reindex/              # Background reindex worker
│   │   ├── runs/                 # Run + Event + Question lifecycle store
│   │   ├── search/               # Embed cache + Qdrant repo + FTS5 syncer
│   │   ├── sources/              # Source ingestion: store/ocr/ingest/markitdown
│   │   └── sweep/                # LRU-age sweep policy
│   ├── stringx/                  # Dedup + diacritics helpers
│   ├── swarm/                    # AuraBot sub-agent manager + runner + plan + tool policy
│   ├── telegram/                 # gopkg.in/telebot.v4 wrapper (bot, handlers, voice, docs)
│   ├── testutil/                 # Test DB helpers
│   ├── tokenjuice/               # Tool output compaction (port from openhuman GPLv3 concepts)
│   ├── tray/                     # Windows tray icon + Browser launcher
│   ├── wiki/                     # Markdown wiki store: writes, graph, FTS5, dedup, hygiene
│   └── workspace/                # Sandboxed workspace root for file tools
├── web/                          # React 19 dashboard (Vite + TypeScript)
│   ├── e2e/                      # Playwright E2E tests
│   ├── public/                   # Static assets
│   ├── scripts/                  # i18n check, timezone check, audit
│   └── src/
│       ├── components/           # 24 React components (panels, drawer, shell, sidebar, chat/, ui/)
│       ├── pages/                # Chat.tsx, Quarantine.tsx (router-mounted)
│       ├── hooks/                # Custom React hooks
│       ├── i18n/                 # Locale bundles (it/en)
│       ├── lib/                  # Helpers
│       ├── types/                # api.ts (TypeScript API client types)
│       └── api.ts                # API client surface
├── docker/                       # Sidecar Dockerfiles
│   ├── garage-init/              # Garage S3 bucket bootstrap
│   ├── init-models/              # aura-init-models sidecar (fetch GGUFs)
│   ├── markitdown/               # markitdown-mcp sidecar (xlsx/docx/pptx/...)
│   ├── pocket-tts/               # Kyutai Pocket-TTS sidecar (Italian TTS)
│   ├── secrets/                  # aura-secrets init script
│   └── whisper/                  # whisper.cpp HTTP server sidecar
├── runtime-workspace/            # Default PROMPT_OVERLAY_PATH + workspace root
│   ├── AGENT.md                  # Operational contract overlay
│   ├── SOUL.md                   # Persona overlay
│   ├── HEARTBEAT.md              # Heartbeat task config
│   ├── mcp.json                  # MCP server registry
│   ├── data/                     # Per-install data
│   ├── inbox/                    # Inbox staging dir
│   ├── notes/                    # Free-form notes
│   ├── skills/                   # Installed skills
│   ├── sources/                  # Default SOURCES_PATH
│   ├── wiki/                     # Default WIKI_PATH (Markdown KB)
│   └── tmp/                      # Scratch dir
├── runtime/                      # Compiled-in runtime artifacts
│   └── mcp/aura-calculator-mcp/  # Bundled MCP server (SymPy + NumPy + SciPy)
├── data/                         # Per-deployment state (gitignored)
│   ├── aura.db                   # SQLite (default DB_PATH)
│   ├── embeddinggemma-300m-Q4_0.gguf  # Embed model
│   ├── ggml-small.bin            # Whisper model
│   ├── logs/                     # Daily-rotated logs
│   └── secrets/                  # Secrets dropped by aura-secrets sidecar
├── garage/                       # Garage S3 daemon state
├── scripts/                      # Dev/ops PowerShell + bash scripts
│   ├── ralph/                    # Ralph autonomous loop driver + prd.json
│   └── *.ps1, *.sh               # check-file-size, registry-diff, test runners
├── docs/                         # Markdown design docs, plans, syntheses
├── agents/                       # GSD agent definitions (markdown)
├── skills/                       # Project-level skills (empty by default)
├── tmp/                          # Scratch (git-tracked for some artifacts)
├── compose.yaml                  # Local docker compose stack
├── compose.image.yaml            # Image-based compose (no local build)
├── compose.prod.yaml             # Production compose overlay
├── Dockerfile                    # Main Aura binary image
├── Dockerfile.test               # CI test image
├── Makefile                      # Build, test, web-build, download-models, compose-up
├── go.mod / go.sum               # Go modules (`go 1.26.2`, `module github.com/aura/aura`)
├── package.json / .nvmrc         # (none at root; web/ has its own)
├── lefthook.yml                  # Pre-commit hooks
├── .golangci.yml                 # Linter config
├── CLAUDE.md                     # Agent operating instructions (primary)
├── AGENTS.md                     # Project agent manifest
├── PRD.md / VISION.md            # Product spec + vision
├── README.md / INSTALL.md / CONTRIBUTING.md
├── License.md
├── mcp.example.json              # MCP config template
├── skills-lock.json              # Skills install lockfile
└── gsd-file-manifest.json        # GSD planning manifest
```

## Directory Purposes

**`cmd/aura/`:**
- Purpose: Process entry point, composition root, lifecycle.
- Contains: `main.go` (boot), `app.go` (DI), `app_wire.go` (chat Hub wiring), `chat_hub.go`, `mcp_runtime.go`, `web_chat*.go` (web dashboard chat endpoint helpers), `migrate_sources.go`, `secrets_boot.go`, `bootstrap_defaults.go`.
- Key files: `main.go:202` (`startAura`), `app.go:152` (`newApp`).

**`cmd/probe_chat/`:**
- Purpose: Canonical E2E probe — drives Aura through the chat API and verifies ground truth (SQLite + filesystem + artifact bytes).
- Contains: `cases.go` (1511 LOC test catalog), `client.go`, `runner.go`, `phase07[d-f].go`, `qa_phase_*.go`, `live_db_*.go`.
- Note: `cases.go` is large by design (test data); consolidation into shared helpers is in progress.

**`internal/agent/`:**
- Purpose: One agent turn — LLM round + tool dispatch + governance + finalize.
- Contains: `loop.go` (the 594-LOC loop), `executor.go` (parallel tool fan-out), `runtime.go`, `governance/` (budget, dedup, payload summarizer, history hygiene, repeated-lookup), `tools/` (registry, sets, swarm, attempts), `agents/summarizer/`, `agentdef/` (declarative sub-agent specs).
- Key files: `loop.go`, `executor.go`, `runtime.go`, `state.go`, `session.go`, `task.go`, `tools/registry/registry.go`.

**`internal/agent/tools/registry/`:**
- Purpose: All LLM-visible tools. One file per tool family with action-enum dispatch.
- Naming: `<tool>.go` + `<tool>_test.go` + helper files (e.g. `wiki.go`, `wiki_diff.go`, `wiki_subgraph.go`, `wiki_path.go`, `wiki_godnodes.go`).
- Key files: `registry.go` (Tool interface + Registry), `text_response.go`, `ask_user.go`, `search.go`, `web.go`, `file.go`, `wiki.go`, `source_unified.go`, `scheduler.go` (task tool), `mcp.go`, `agent_note.go`, `propose_patch.go`, `exec.go`, `skill.go`, `tool_search.go`, `tool_definitions.go`.
- Count: 98 files including tests.

**`internal/api/`:**
- Purpose: HTTP/JSON API + embedded React SPA + setup wizard.
- Naming: One file per resource + write-side split (`<resource>.go` + `<resource>_write.go`).
- Key files: `router.go` (mux + Deps), `auth.go` + `auth/` (bearer middleware), `setup_server.go` + `setup_page.html` (first-run wizard), `chat.go` + `chat_stream.go` (SSE), `wiki.go` + `wiki_write.go`, `sources.go` + `sources_write.go`, `tasks.go` + `tasks_write.go`, `skills.go` + `skills_write.go`, `mcp*.go` (read + setup + write), `settings.go` (526 LOC catalog), `maintenance*.go`, `static.go` (//go:embed all:dist), `health*.go`, `backups.go`, `summaries.go`, `swarm.go`, `pending.go`, `conversations*.go`, `metrics_handler.go`, `tools_direct.go`.
- Count: 100 files.

**`internal/wiki/`:**
- Purpose: Atomic Markdown writes + go-git tracking + FTS5 + graph index + alias index + dedup + hygiene.
- Key files: `store.go` (Store struct), `store_writes.go`, `store_fts5.go`, `store_graph.go`, `graph.go`, `graph_index.go`, `alias_index.go`, `parser.go`, `schema.go`, `dedup.go`, `hubs.go`, `godnodes.go`, `subgraph_render.go`, `surprise.go`, `repairs.go`, `hygiene.go`, `memory_hygiene.go`, `gaps.go`, `questions.go`, `provenance.go`, `source_refs.go`, `renames.go`, `inject_links.go`, `ppr.go` (Personalized PageRank).

**`internal/storage/sources/`:**
- Purpose: Source ingestion pipeline — store, OCR, ingest, markitdown.
- Sub-packages:
  - `store/` — `source.NewStore`, SHA-256 dedupe, file layout `src_<sha16>/`.
  - `ocr/` — Mistral Document AI client (`/v1/ocr`).
  - `markitdown/` — Markitdown HTTP sidecar client (xlsx/docx/pptx/+6).
  - `ingest/` — `pipeline.go` orchestrator + `extractor.go` (LLM-driven), `concept_pages.go`, `entity_pages.go`, `structured.go`.

**`internal/storage/search/`:**
- Purpose: Vector + FTS5 search infrastructure.
- Key files: `qdrant.go` + `qdrant_client.go` + `qdrant_index.go` + `qdrant_hybrid.go` (Qdrant backend), `embed_http.go` + `embed_batch.go` + `embed_cache.go` (embedding pipeline), `fts5_syncer.go` (SQLite mirror sync), `compact_qdrant.go` (compact memory mirror), `wiki_reconcile.go`, `graph_documents.go`, `search.go`, `types.go`.

**`internal/storage/memoryindex/`:**
- Purpose: Compact memory mirror — projects wiki + sources into a recall-optimized SQLite table with optional Qdrant vector overlay.
- Key files: `store.go` + `store_core.go` + `store_search.go` + `store_archive.go` + `store_helpers.go`, `rebuild.go`, `compact_rebuild.go`, `operational_overlay.go`, `priority_section.go`, `collections.go`, `vector_health.go`, `hash.go`, `audit/audit.go`.

**`internal/channels/`:**
- Purpose: Channel adapters around the chat Hub.
- Sub-packages: `telegram/` (8 files — inbound, outbound, invocation_builder, status_pane, fixture), `web/` (chat_client, chat_service, inbound, outbound, sse_chat_client, streaming_outbound), `cron/` (dispatcher, inbound, loop), `silent/` (outbound), `askuser/` (askuser).

**`internal/telegram/`:**
- Purpose: gopkg.in/telebot.v4 wrapper — bot polling, allowlist, commands, document handler, voice handler, conversation snapshots.
- Key files: `bot.go`, `setup.go`, `commands.go`, `handlers.go`, `access.go`, `documents.go`, `voice_handler.go`, `conversation_snapshot.go`, `conversation_terminal.go`, `entity_messages.go`, `entity_markdown.go`, `atomic_tables.go`, `tool_exec_helpers.go`, `deps.go`, `status.go`.

**`internal/db/migrations/`:**
- Purpose: SQLite schema migrations (29 production migrations, m01 → m29).
- Naming: `m<NN>_<description>.go` — sequential, irreversible-by-design.
- Driver: `migrations.go` runs them in order at boot.
- Coverage: core schema (m01), api token expiry (m03), compact memory (m04, m13, m17, m21, m22), tool indexes (m05, m10), run+event foundation (m06, m20), identity (m07, m08), secrets (m09, m18), questions (m11), projection state (m12), proposals (m14, m16), agent notes (m15), tokenjuice (m19), voice (m23, m24), conversation compactions/channel (m27, m28), prompt health (m29).

**`internal/conversation/`:**
- Purpose: Per-turn message history — sliding window, prompt overlay loading, context engine (compaction), summarizer.
- Key files: `context.go` (ContextEngine), `archive.go` + `archive_turns.go` (durable archive), `auto_compact.go`, `compaction_events.go`, `compressor.go` + `compressor_streaming.go`, `engine.go`, `overlay.go` (prompt overlay loader), `system_prompt.go`, `tool_compaction.go`, `summarizer/` (applier, proposals, question_gate, triage, types).

**`internal/cron/`:**
- Purpose: Scheduled tasks (cron-style) + agent job runner + maintenance + WAL checkpoint.
- Key files: `scheduler.go`, `store.go` (594 LOC), `dispatch.go` + `dispatch_handlers.go`, `agent_job.go` + `agentjob_prompt.go`, `issues.go`, `maintenance.go`, `wake.go`, `wal_checkpoint.go`, `backup_verify.go`, `types.go`.

**`internal/mcp/`:**
- Purpose: MCP (Model Context Protocol) client — stdio + Streamable-HTTP transports, JSON-RPC 2.0.
- Key files: `client.go` (523 LOC), `config.go`, `overrides.go`, `policy.go`, `watcher.go` (fsnotify on `mcp.json`).

**`internal/skills/`:**
- Purpose: Anthropic-style SKILL.md loader (frontmatter + content) + admin install/delete + catalog client.
- Key files: `loader.go`, `admin.go`, `catalog.go`, `validation.go`.

**`internal/swarm/`:**
- Purpose: AuraBot sub-agent dispatch (parent → child agent), depth-bounded.
- Key files: `manager.go`, `archetype_runner.go`, `hub_bridge.go`, `nodespec.go`, `plan.go`, `store.go`, `tool_policy.go`, `types.go`.

**`internal/agent/agentdef/`:**
- Purpose: Declarative sub-agent specs (TOML) — orchestrator, reflector, summarizer.
- Key files: `definition.go`, `loader.go`, `registry.go`, `validator.go`, `builtin.go`, `cycle.go`, `delegate.go`, `announce.go`.
- Built-ins: `builtin/{orchestrator,reflector,summarizer}/`.

**`web/src/`:**
- Purpose: React 19 dashboard SPA, embedded into Go binary.
- Components: 24 top-level (panels, chat/, common/, ui/, ErrorBoundary, Login, Markdown, Shell, Sidebar, StderrLogSheet) + Chat.tsx + Quarantine.tsx pages.
- API client: `api.ts` (435 LOC) + `types/api.ts` (704 LOC).

**`docker/`:**
- Purpose: Sidecar Dockerfiles + bootstrap scripts.
- Sub-packages: each sidecar has its own `Dockerfile` (+ `pyproject.toml` for pocket-tts).

**`runtime-workspace/`:**
- Purpose: Default bind-mount target for PROMPT_OVERLAY_PATH + WIKI_PATH + SOURCES_PATH + SKILLS_PATH.
- Default contents shipped by `config.EnsureLayout`.

## Key File Locations

**Entry Points:**
- `cmd/aura/main.go`: Main binary entry (`main` → `startAura`).
- `cmd/aura/app.go`: Composition root (`newApp`).
- `cmd/aura/app_wire.go`: Phase-C wiring (`wireBot`).
- `internal/api/setup_server.go`: First-run setup wizard.
- `cmd/aura_mcp_server/main.go`: Aura-as-MCP-server entry (exposes Aura tools to other agents).

**Configuration:**
- `internal/config/config.go`: Env config struct (590 LOC, all `envconfig` tags).
- `internal/config/applier.go`: SQLite settings overlay onto env config.
- `internal/config/store.go`: SQLite settings repository.
- `internal/config/bootstrap.go`: First-run bootstrap detection.
- `internal/api/settings.go`: Settings catalog (526 LOC, dashboard-facing).
- `compose.yaml`: Local dev stack.
- `runtime-workspace/mcp.json`: MCP server registry (runtime-editable).

**Core Logic:**
- `internal/agent/loop.go`: The agent turn loop (LLM ⇄ tools).
- `internal/agent/executor.go`: Parallel tool dispatcher.
- `internal/agent/tools/registry/registry.go`: Tool interface + Registry.
- `internal/chat/hub.go`: Channel-neutral dispatcher.
- `internal/chat/agentloop.go`: Event translator (agent.Event → chat.OutboundEvent).
- `internal/llm/client.go`: LLM Client interface.
- `internal/llm/openai.go`: OpenAI-compatible HTTP transport.
- `internal/wiki/store.go`: Wiki Store entry.
- `internal/wiki/store_writes.go`: Atomic write + git commit.
- `internal/storage/sources/ingest/pipeline.go`: Ingestion orchestrator.
- `internal/cron/scheduler.go`: Scheduler tick loop.
- `internal/mcp/client.go`: MCP transport.
- `internal/telegram/bot.go`: Telegram polling.

**Testing:**
- `cmd/probe_chat/`: E2E probes (canonical Aura tester).
- `cmd/quality_bench/main.go`: Quality benchmark runner.
- `internal/agent/tools/registry/testdata/retrieval_fixture.jsonl`: Tool eval fixture.
- `internal/storage/sources/ingest/fixtures/`: Ingest test fixtures.
- `internal/db/migrations/testdata/`: Migration test fixtures.
- `internal/conversation/testdata/`: Conversation fixtures.
- `compose.yaml` `test` service: CI test container (`go test ./...`).

## Naming Conventions

**Files:**
- Production Go file: `<topic>.go` (e.g. `loop.go`, `store.go`).
- Test file: `<topic>_test.go` (always in the same package).
- Sub-action file split: `<family>_<action>.go` (e.g. `source_unified.go`, `source_delete.go`, `source_read.go`).
- Migration: `m<NN>_<snake_case_description>.go` (29 sequential numbers).
- React component: `PascalCase.tsx` (e.g. `Sidebar.tsx`, `SettingsPanel.tsx`).
- React helper: `<Component>.helpers.ts`, `<Component>.test.ts`.

**Directories:**
- All-lowercase, no separators: `internal/agent/tools/registry/`, `internal/storage/sources/markitdown/`.
- Sub-packages mirror the responsibility split: `internal/llm/whisper/`, `internal/llm/pockettts/`.
- One Go package per directory (Go convention).

**Functions / Types:**
- Exported types: `PascalCase` (e.g. `Store`, `Registry`, `Hub`).
- Unexported: `camelCase` (e.g. `runLoop`, `agentExecutor`).
- Constructors: `NewX(deps...) (*X, error)` (e.g. `wiki.NewStore`, `tools.NewRegistry`).
- Interfaces ≤3 methods: noun (e.g. `Tool`, `PageReader`).

**Tool names (LLM-facing):**
- `snake_case`, single word when possible (e.g. `search`, `web`, `file`, `task`, `wiki_page`, `text_response`, `ask_user`, `agent_note`, `propose_patch`, `tool_search`, `execute_code`, `execute_shell`, `source`).
- MCP tools: `mcp_<server>_<tool>` (e.g. `mcp_calculator_evaluate`).

## Where to Add New Code

**New LLM tool (action-enum extension):**
- Primary code: `internal/agent/tools/registry/<family>.go` — add a new `action` value to the existing family's switch.
- If no family exists: create `internal/agent/tools/registry/<new_tool>.go` with `Tool` interface (`Name`, `Description`, `Parameters`, `Execute`), implement `Definition()` in `tool_definitions.go`, register in `cmd/aura/app.go:newApp`.
- Tests: `internal/agent/tools/registry/<family>_test.go`.

**New MCP server:**
- Config: add entry to `runtime-workspace/mcp.json` (or `mcp.example.json` for shipping defaults).
- Bundled server: put under `runtime/mcp/<server-name>/` (see `runtime/mcp/aura-calculator-mcp/`).
- No Go code needed for stdio/HTTP MCP servers — `internal/mcp/watcher.go` reloads on file change.

**New skill:**
- Local: `runtime-workspace/skills/<skill-name>/SKILL.md` (Anthropic frontmatter format).
- Catalog (community): use the dashboard `Skills` panel → Install.

**New API endpoint:**
- Read: `internal/api/<resource>.go` — register on the mux in `internal/api/router.go`.
- Write: `internal/api/<resource>_write.go` — admin-gated via `auth.RequireAdmin`.
- Tests: `internal/api/<resource>_test.go`.
- TypeScript types: `web/src/types/api.ts`.
- Frontend client: `web/src/api.ts`.

**New channel adapter:**
- Sub-package: `internal/channels/<name>/inbound.go` + `outbound.go`.
- Adapter types implement `chat.InboundAdapter` + `chat.OutboundAdapter`.
- Register at boot in `cmd/aura/app_wire.go`.

**New migration:**
- Create `internal/db/migrations/m<NN+1>_<description>.go` (next sequential number).
- Add to migration list in `internal/db/migrations/migrations.go`.
- Migrations are irreversible — design for forward compatibility.

**New sidecar service:**
- Dockerfile: `docker/<service>/Dockerfile`.
- Go client: `internal/llm/<service>/client.go` (if LLM-adjacent) or `internal/storage/sources/<service>/client.go` (if ingestion-adjacent).
- Compose entry: add service block in `compose.yaml`.
- Init-models hook (if it ships GGUF/ONNX weights): extend `cmd/aura-init-models/main.go` + `internal/install/`.
- Wire in `cmd/aura/app.go:newApp` with graceful degradation (warn + skip if URL unset).

**New React panel:**
- Component: `web/src/components/<Panel>.tsx`.
- Wire into router/shell: `web/src/components/Shell.tsx`.
- I18n keys: `web/src/i18n/<lang>/<namespace>.json`.

**New scheduled task type:**
- Handler: extend dispatcher in `internal/cron/dispatch_handlers.go`.
- Schema: persist via `internal/cron/store.go`.

**Utilities (shared helpers):**
- Pure string ops: `internal/stringx/`.
- HTTP retry/transport: `internal/httputil/client.go`.
- Sandbox process exec: `internal/sandbox/process_runner.go`.

## Special Directories

**`internal/api/dist/`:**
- Purpose: Vite-built React SPA, embedded via `//go:embed all:dist` in `internal/api/static.go`.
- Generated: Yes (by `npm --prefix web run build`).
- Committed: Generally yes — keeps `go build` self-contained without requiring Node. Empty placeholder is OK; runtime logs a warning when missing but `/api` still serves.

**`internal/agent/agentdef/builtin/`:**
- Purpose: Embedded TOML+Markdown specs for built-in sub-agents (orchestrator, reflector, summarizer).
- Generated: No — hand-authored.
- Loaded via: `agentdef.BuiltinFS` (embed.FS).

**`internal/tokenjuice/builtin/`:**
- Purpose: Default tool-output compaction rule packs (JSON).
- Generated: No — hand-authored per tool family (`git_status.json`, `git_diff_stat.json`, `tests_go_test.json`, `tests_pytest.json`, `install_npm_install.json`, `search_rg.json`, `generic_help.json`, `build_tsc.json`, `fallback.json`).
- Loaded via: `tokenjuice.builtinFS` (embed.FS).

**`runtime-workspace/`:**
- Purpose: Bind-mount target for Docker. Contains AGENT/SOUL/HEARTBEAT overlays + wiki + sources + skills + mcp.json.
- Generated: Bootstrapped on first boot by `config.EnsureLayout`.
- Committed: Default contents are committed; per-install state (wiki pages, sources, skills) is gitignored.

**`data/`:**
- Purpose: Per-deployment state. SQLite DB + downloaded model weights + logs + secrets.
- Generated: Yes — at runtime.
- Committed: No (`.gitignore`).

**`garage/`:**
- Purpose: Garage S3 daemon's on-disk state.
- Generated: Yes.
- Committed: No.

**`web/node_modules/`:**
- Purpose: npm dependencies.
- Generated: Yes (`npm install`).
- Committed: No.

**`scripts/ralph/`:**
- Purpose: Ralph autonomous loop driver scripts + per-phase `prd.json` queues.
- Generated: Mostly hand-authored phase PRDs; completed phases archived as `prd-completed-phase-*.json`.
- Committed: Yes — provides audit trail of phase queues and Ralph completions.

**`.planning/`:**
- Purpose: GSD planning artifacts (codebase maps, QA token, plans).
- Generated: By `/gsd-map-codebase` and related GSD commands.
- Committed: Yes for codebase maps, no for ephemeral tokens.

## Large Files (>600 LOC) Watch List

Per CLAUDE.md's god-class rule (≤600 LOC), the following files are at or above the threshold and warrant refactor consideration on next touch:

**Tests / Test Data (acceptable — not god classes):**
- `cmd/probe_chat/cases.go` — 1511 LOC. Test catalog with many independent case blocks; structural split by category (qa_phase_*.go, skill_cases.go, multiagent_case.go) already in progress.
- `cmd/aura/web_chat_test.go` — 641 LOC. Integration-test surface.

**Frontend (warrants refactor):**
- `web/src/components/MCPPanel.tsx` — 1141 LOC. Splittable by sub-panel (server list, tool list, setup wizard, write actions).
- `web/src/components/SourceInbox.tsx` — 791 LOC. Splittable by phase (upload, OCR view, ingest result).
- `web/src/types/api.ts` — 704 LOC. Auto-generated-like surface; consider codegen + per-resource split.
- `web/src/components/TasksPanel.tsx` — 653 LOC. Splittable into list + detail + form.

**Go quality_bench (acceptable — single-purpose tool):**
- `cmd/quality_bench/main.go` — 790 LOC. Standalone benchmark driver; not on the hot path.

**Production Go files at 600 LOC (boundary cases — refactor on next touch):**
- `internal/api/files.go` — 600 LOC. Files endpoint.
- `internal/channels/telegram/invocation_builder.go` — 596 LOC. Per-turn Invocation builder closure.
- `internal/cron/store.go` — 594 LOC. Scheduler store.
- `internal/agent/loop.go` — 594 LOC. The agent turn loop (heart of the runtime — split with extreme care).
- `internal/config/config.go` — 590 LOC. Env-config struct.

No production file currently exceeds 600 LOC.

---

*Structure analysis: 2026-05-27*
