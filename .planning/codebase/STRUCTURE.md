# Codebase Structure

**Analysis Date:** 2026-07-04

## Directory Layout

```
Aura/
├── cmd/aura/                # single binary entry point — CLI dispatch + composition roots
│   ├── main.go               # subcommand dispatch table + shared registry builders
│   ├── serve.go / serve_*.go # `aura serve` daemon boot, HTTP mounts, shutdown
│   ├── chat.go / chat_*.go   # `aura chat` interactive REPL (multi-thread conversations)
│   ├── shell.go              # `aura shell` REPL against the raw agent loop
│   └── *.go                  # one file per subcommand (db, neo4j, identity, profile, mcp, ...)
├── internal/                 # all application code (51 packages, ~71k LOC non-test)
│   ├── agent/                 # Agent interface, Budget, LlmAgent, Event model, hooks
│   │   ├── tools/               # Tool interface, Registry, deferred specs, built-in tools
│   │   ├── mcptools/             # MCP-server-to-Tool mounting/adaptation
│   │   ├── workflow/             # workflow-parent agent composition helpers
│   │   ├── prompt/               # system-prompt assembly
│   │   ├── display/               # tool-result → display-payload projection
│   │   ├── panicobs/              # panic-site observability tagging
│   │   └── agenttest/             # shared test doubles for agent-consuming packages
│   ├── agui/                  # AG-UI HTTP/SSE gateway (the cockpit's only backend surface)
│   ├── runner/                 # per-turn orchestration (Runner.Turn/SubmitAnswers)
│   ├── swarm/                   # fan-out sub-agent orchestration (swarm_spawn backing)
│   ├── gateway/                 # policy-enforcement point (risk classify/decide/approve/ledger)
│   ├── mcp/                     # MCP client transport (stdio/HTTP), SSRF guard
│   ├── llm/                     # LLM client abstraction (`openai_compat/` backend)
│   ├── config/                  # env-driven Config resolution + validation + runtime profiles
│   ├── db/                       # sqlc client, migrations, raw queries
│   │   ├── sqlc/                   # generated query code — DO NOT hand-edit
│   │   ├── migrations/             # golang-migrate SQL, numbered 0001-0011
│   │   └── queries/                 # sqlc source .sql files
│   ├── knowledge/               # Neo4j schema/migration + read-only GraphView
│   ├── neostore/                 # Neo4j driver-adjacent store helpers
│   ├── conversations/            # conversation/turn persistence, FTS, cl100k tokenization
│   ├── documents/                # ingest pipeline: extract → chunk → embed → index → GraphRAG
│   ├── cron/                      # scheduler: claim/dispatch/heartbeat/recover
│   │   └── handlers/                # per-TaskKind handler implementations
│   ├── channels/                 # channel abstraction + registry
│   │   └── telegram/                # Telegram bot: dispatch, render, onboarding, HITL
│   ├── identity/                 # identity + capability_grants (RBAC)
│   ├── identityctx/               # request-scoped identity context helper
│   ├── webauth/                   # Authula-backed cockpit auth provider
│   ├── askuser/                    # HITL pause/resume primitive (ask_user)
│   ├── toolinvocations/            # append-only tool-dispatch ledger (gateway's store)
│   ├── skills/                    # skill-library self-extension (skill_create/skill tool backing)
│   │   └── embed/                   # embedded default skill assets
│   ├── skilladapters/              # skill-tool ↔ gateway/registry adapters
│   ├── settings/                   # cockpit Settings page backing store
│   ├── setup/                      # setup-wizard (loopback :9081) backing
│   ├── onboarding/                  # onboarding-wizard session/provisioning
│   ├── profile/                     # filesystem Agent.md profile store
│   ├── objectstore/                  # S3-compatible object storage client
│   ├── multimodal/                  # image/audio pre-processing for multimodal turns
│   ├── reasoningfifo/, reasoningstore/, reasoningtrace/, reasoninglearn/  # adaptive-reasoning tier subsystem
│   ├── toolselectstore/, toolselectlearn/  # tool-selection learning subsystem
│   ├── semindex/                    # semantic tool-search index
│   ├── rerank/                       # retrieval re-ranking
│   ├── scoring/                      # risk-tier scoring (consumed by gateway)
│   ├── cachemetrics/                  # prompt-cache hit/miss metrics
│   ├── obs/                            # OpenTelemetry tracing setup
│   ├── secret/                          # secret redaction/handling helpers
│   ├── canonicaljson/                    # deterministic JSON canonicalization
│   ├── boundedbuffer/, envutil/, pgnumeric/  # small focused utility packages
│   ├── web/                              # web_search/web_fetch tool backend (SearXNG client)
│   ├── webui/                             # embeds `web/dist` static assets into the Go binary
│   ├── eval/                               # offline evaluation harness (RAGAS etc.)
│   └── activelearn/                        # active-learning loop for classifiers
├── web/                        # React/Vite SPA — SEPARATE npm module, own package.json
│   ├── src/
│   │   ├── chat/                  # assistant-ui-based chat surface, SSE adapter
│   │   ├── governance/              # governance-board pages (MCP/skills/scheduler)
│   │   ├── graph/                    # sigma.js graph explorer
│   │   ├── onboarding/                 # onboarding wizard UI
│   │   ├── settings/                    # Settings page UI
│   │   ├── shell/                        # AppShell layout
│   │   ├── conversations/                 # conversation list/branch UI
│   │   ├── approvals/                      # HITL approval center UI
│   │   ├── documents/                       # document catalog UI
│   │   ├── auth/                             # login/session UI
│   │   ├── api/                               # typed fetch clients over the AG-UI REST API
│   │   ├── lib/, theme/, i18n/, a11y/          # cross-cutting frontend helpers
│   │   └── routes/                              # route-level page components
│   ├── e2e/                       # Playwright end-to-end tests
│   └── tokens/                     # design-token generation (theme build step)
├── docker/                     # Dockerfiles + source for sidecar services (agent-memory, garage, markitdown, mcp-neo4j-cypher)
├── finetune/                   # Python fine-tuning pipeline (separate from the Go runtime)
├── scripts/                    # shell/Go helper scripts (coverage gate, eval harness, fixtures)
├── docs/                        # design docs, audit reports, deployment notes
├── .planning/                    # GSD workflow state (phases, spikes, codebase maps, milestones)
├── .github/workflows/            # CI pipelines
├── prd.md                        # the PRD — truth-source for architecture decisions (561 KB)
├── CLAUDE.md                     # project-wide behavioral rules for AI agents
├── compose*.yaml                 # Docker Compose stacks (base, GPU, gVisor, cloud, LLM sidecar)
├── Makefile                      # quality/coverage/test gate targets
└── go.mod                        # module github.com/chetto1983/aura, Go 1.26.4
```

## Directory Purposes

**`cmd/aura/`:**
- Purpose: the ONLY entry point binary; every subcommand's composition root lives here
- Contains: one `<subcommand>.go` + matching `<subcommand>_test.go` per CLI verb; shared boot helpers (`chat_boot.go` → `bootChatEnv`, `serve_bootstrap.go`)
- Key files: `main.go` (dispatch + `buildBaseRegistry*`), `serve.go` (daemon), `chat_boot.go` (shared boot env)

**`internal/agent/`:**
- Purpose: the core agent-runtime contract and its concrete LLM-driven implementation
- Contains: `Agent`/`InvocationContext`/`Budget`/`Event` types (`agent.go`, `budget.go`, `event.go`), the `LlmAgent` implementation split across ~20 `llm_agent_*.go` files by concern (construct, dispatch, retry, pause, reasoning, truncation, finalize)
- Key files: `agent.go` (the open interface), `llm_agent.go` (top-level type), `llm_agent_dispatch.go` (tool-call dispatch through the gateway)

**`internal/agent/tools/`:**
- Purpose: the Tool interface, the immutable-per-run Registry, the deferred-spec manifest mechanism, and every built-in tool implementation
- Contains: one file per tool (`fs_read.go`, `shell_exec.go`, `skill.go`, `document_search.go`, ...), `spec.go` (Spec/Deferred/Mutating/Multiplexed metadata), `registry.go` (Registry + `Without`)
- Key files: `spec.go`, `registry.go`, `manifest.go` (LLM-visible manifest rendering)

**`internal/agui/`:**
- Purpose: the ONLY HTTP surface the web cockpit talks to — AG-UI protocol translation + REST endpoints for governance/settings/onboarding/documents/graph
- Contains: `server.go` (Mux + SSE run handler), `translator.go` (Event → AG-UI protocol), one `<feature>_api.go` per REST subtree (conversations, approvals, assets, documents, graph, governance, settings, onboarding, connect)
- Key files: `server.go`, `translator.go`, `types.go`

**`internal/gateway/`:**
- Purpose: the single Policy Enforcement Point for mutating tool dispatch
- Contains: `gateway.go` (struct + vocabulary), `classify.go` (risk tiering), `decide.go` (PEP logic), `approve.go`/`approvals.go` (HITL approval routing + ledger), `reserve.go` (idempotency reservation), `reconcile.go` (crash-orphan recovery)
- Key files: `gateway.go`, `decide.go`

**`internal/db/`:**
- Purpose: Postgres access layer — sqlc-generated queries over pgx/v5, golang-migrate migrations
- Contains: `sqlc/` (generated, do not hand-edit), `migrations/` (0001-0011, numbered sequentially — see PRD §Persistence for the authoritative slot map), `queries/` (source `.sql` sqlc compiles from)
- Key files: `db.go` (pool construction), `migrations/*.sql`

**`internal/documents/`:**
- Purpose: the document ingestion + retrieval pipeline (upload → extract → chunk → embed → BM25/vector index → GraphRAG)
- Contains: `service.go` (facade), `worker.go`/`jobs_worker.go` (async job processing), `extractor.go`/`extract_client.go` (markitdown sidecar client), `indexer.go`, `retrieve.go`, `search.go`, `graphrag.go`
- Key files: `service.go`, `worker.go`

**`internal/channels/telegram/`:**
- Purpose: the Telegram Bot API adapter — the one non-web channel wired into `aura serve`
- Contains: `bot.go` (bot construction), `bot_dispatch*.go` (update routing split by concern: auth, asset, callbacks, HITL, turn), `renderer.go`/`mdv2.go`/`tables.go` (MarkdownV2 rendering), `voice.go`/`tts.go` (audio), `onboarding.go`
- Key files: `bot.go`, `bot_dispatch_turn.go`, `renderer.go`

**`internal/config/`:**
- Purpose: single source of truth for env-var-driven configuration, validated at every boot path
- Contains: `config.go` (the `Config` struct + `Load`/`LoadDB`), `config_env.go` (env parsing helpers), `config_validate.go` (fail-fast validation), `config_runtimeprofile.go` (dev/server_production profile gating), `config_mcp.go` (MCP server/policy config), `config_routes.go`, `config_knobs.go`
- Key files: `config.go`, `config_validate.go`

**`internal/cron/`:**
- Purpose: the scheduler — task claim/dispatch/heartbeat/recover, backed by Postgres
- Contains: `scheduler.go` (tick loop), `dispatch.go` (per-TaskKind routing), `claim.go`/`heartbeat.go`/`recover.go` (distributed-claim safety), `store.go`/`store_runs.go` (persistence), `handlers/` (concrete TaskKind implementations)
- Key files: `scheduler.go`, `dispatch.go`

**`internal/knowledge/`:**
- Purpose: Neo4j schema ownership + the read-only GraphView normalizer the cockpit graph explorer consumes
- Contains: `schema.go`/`migrate.go` (Cypher migration 0001), `client.go` (driver lifecycle), `graphview.go`/`graphview_normalize.go`/`graphview_intent.go` (query normalization for LLM/UI consumption)
- Key files: `client.go`, `graphview.go`

**`web/` (separate module):**
- Purpose: the React/Vite single-page cockpit application; built independently and embedded into the Go binary via `internal/webui`
- Contains: feature-first directories (`chat/`, `governance/`, `graph/`, `settings/`, `onboarding/`) plus cross-cutting `lib/`, `theme/`, `i18n/`, `api/`
- Key files: `src/AppShell.tsx`, `src/main.tsx`, `vite.config.ts` (not read in this pass — see `web/package.json` for scripts)

## Key File Locations

**Entry Points:**
- `cmd/aura/main.go`: process entry, subcommand dispatch, shared registry builders
- `cmd/aura/serve.go`: `aura serve` daemon boot/shutdown
- `internal/agui/server.go`: HTTP mux + `POST /agent/run` SSE handler

**Configuration:**
- `internal/config/config.go`: the `Config` struct and `Load`/`LoadDB` entry points
- `internal/config/config_validate.go`: fail-fast validation run at every boot
- `.env.example`: the full env-var catalog with placeholder values (never read `.env` itself — it holds live secrets)
- `.golangci.yml`: lint configuration (33 linters, dupl threshold 100, `_test.go` excluded)

**Core Logic:**
- `internal/agent/agent.go`: the `Agent` interface and `InvocationContext`/`Budget` contract
- `internal/agent/llm_agent.go` + `llm_agent_*.go`: the concrete leaf agent implementation
- `internal/gateway/gateway.go` + `decide.go`: the policy enforcement point
- `internal/runner/runner.go`: per-turn orchestration

**Testing:**
- Co-located `*_test.go` next to every source file (white-box `package x`, occasional black-box `package x_test`)
- `internal/agent/agenttest/`: shared test doubles for packages consuming the agent runtime
- `scripts/fixtures/`: fixture data for cache-invariant and Neo4j-smoke tests
- `web/e2e/`: Playwright end-to-end specs for the SPA

## Naming Conventions

**Files:**
- Go: `snake_case.go`, one primary concern per file, `<concern>_test.go` for its unit tests, `<concern>_internal_test.go` for white-box tests of unexported behavior, `<concern>_integration_test.go` for `//go:build integration`-tagged tests requiring live Postgres/Neo4j
- Split-by-concern within a package for large files: e.g. `llm_agent_dispatch.go`, `llm_agent_retry.go`, `llm_agent_pause.go` rather than one giant `llm_agent.go` — this is the "refactor on touch, LOC ≤600" rule from CLAUDE.md applied structurally
- Frontend: `PascalCase.tsx` for React components, `camelCase.ts` for plain modules/hooks

**Directories:**
- `internal/<domain>/` — one Go package per bounded concern; sub-packages only when a concern has its own adapters/subordinate types worth isolating (e.g. `internal/agent/tools`, `internal/agent/mcptools`)
- `internal/channels/<channel>/` — one directory per external channel implementation (currently only `telegram`)
- `internal/cron/handlers/` — one sub-package for concrete task-kind handlers, kept separate from `internal/cron` core to avoid the `agent/tools` ↔ `cron` import cycle (see ARCHITECTURE.md "Circular imports")

## Where to Add New Code

**New CLI subcommand:**
- Add `cmd/aura/<name>.go` with a `run<Name>(args []string)` function; register it in the `switch` in `cmd/aura/main.go`; add the verb to `usage()`
- Tests: `cmd/aura/<name>_test.go` alongside it

**New tool (agent-dispatchable capability):**
- Implementation: `internal/agent/tools/<name>.go` implementing the `Tool` interface, with a package-level `Spec` constant/builder
- Register it in `buildBaseRegistryWithHandles` (`cmd/aura/main.go`) — set `Deferred: true` if the description/schema is long (mandatory convention)
- Tests: `internal/agent/tools/<name>_test.go`; add a golden-manifest entry if it changes the default manifest shape (`builtin_spec_golden_test.go`)

**New MCP-mounted integration:**
- Config: extend `internal/config/config_mcp.go` (server/policy resolution)
- Mounting logic: `internal/agent/mcptools/` if a new mount strategy is needed (most integrations reuse `MountServer`/`MountManagedServer`)

**New Postgres table/query:**
- Migration: new numbered file in `internal/db/migrations/` (golang-migrate, sequential — check the PRD §Persistence "Migration numbering" section for the current highest slot before allocating a new one)
- Queries: add `.sql` to `internal/db/queries/`, regenerate `internal/db/sqlc/` via `sqlc generate` (see `sqlc.yaml`) — never hand-edit generated files

**New AG-UI REST endpoint (cockpit feature):**
- Handler + routes: `internal/agui/<feature>_api.go`, registered via a `register<Feature>Routes(mux)` call added to `Server.Mux()` in `server.go`
- Auth mount: the actual `RequireAuth`/`RequireCapability` wrapping happens at the parent mux in `cmd/aura/serve_webui.go`, not inside `internal/agui`
- Frontend: matching feature directory under `web/src/<feature>/` + typed client in `web/src/api/`

**New scheduler task kind:**
- Handler: `internal/cron/handlers/<kind>.go`
- Wiring: the composition root in `cmd/aura/serve.go` adapts the handler map into `cron.Dispatch` (handlers cannot import `internal/agent/tools` directly — see the import-cycle note in ARCHITECTURE.md)

**Utilities:**
- Small, focused, dependency-free helpers go in their own top-level `internal/<name>/` package (e.g. `internal/envutil`, `internal/boundedbuffer`, `internal/canonicaljson`) rather than a generic `internal/utils` grab-bag — this codebase has no such catch-all package; follow that precedent for new cross-cutting helpers

## Special Directories

**`internal/db/sqlc/`:**
- Purpose: sqlc-generated Postgres query bindings
- Generated: Yes (`sqlc generate`, config in `sqlc.yaml`)
- Committed: Yes

**`internal/webui/dist/`:**
- Purpose: embedded static build of the `web/` SPA, served by the Go binary
- Generated: Yes (`web` build output copied/embedded)
- Committed: check `.gitignore` before assuming — treat as a build artifact, do not hand-edit

**`web/node_modules/`, `.planning/`, `docs/audit/`:**
- `web/node_modules/`: npm-managed, generated, not committed
- `.planning/`: GSD workflow state (phases, spikes, codebase maps) — committed, hand-maintained + tool-generated
- `docs/audit/`: audit findings referenced by CLAUDE.md's `AUDIT` rule — committed, human/agent-authored

**Stray root-level build artifacts observed:**
- `aura` (93 MB binary), `cover_gate.out*` (coverage run output) were present at the repository root at analysis time — these are local build/test artifacts, not source; confirm `.gitignore` coverage before committing anything at the repo root.

---

*Structure analysis: 2026-07-04*
