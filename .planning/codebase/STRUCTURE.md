# Codebase Structure

**Analysis Date:** 2026-09-07

## Directory Layout

```
Aura/
├── cmd/                # Binaries (680 files) — every executable entry point
│   ├── aura/           # Main CLI + daemon (243 .go, 23,125 non-test LOC)
│   ├── arcadedb-mcp/   # Memory MCP server (36 .go, 2,713 LOC)
│   ├── aura-media-index/       # Media indexer helper (2 .go, 273 LOC)
│   ├── aura-filecard/          # File card helper (1 .go, 64 LOC)
│   └── aura-ingest-supervisor/ # Ingest supervisor helper (1 .go, 72 LOC)
├── internal/           # All library code — 68 top-level packages, 5,044 files
├── web/                # React + Vite cockpit (1,018 files, 560 .ts/.tsx in src/)
├── internal/webui/dist # Committed Vite build, //go:embed'd into the binary
├── scripts/            # Gates, evals, fixtures, installers (208 files)
├── docs/               # Design notes, audits, quality snapshot (98 files)
├── services/ingest/    # Python ingest sidecar (27 files)
├── packages/create-aura/ # npx installer package (37 files)
├── spikes/             # Throwaway experiments (38 files)
├── finetune/           # Model fine-tuning assets (17 files)
├── observability/      # Collector / dashboards config (16 files)
├── docker/             # Dockerfiles + entrypoints (13 files)
├── deploy/, caddy/, searxng/  # Deployment and sidecar service config
├── artifacts/, backups/, dist/, public/  # Build and runtime output
├── prd.md              # Architectural source of truth
├── CLAUDE.md, AGENTS.md # Agent-facing project rules
├── Makefile            # quality / quality-full / coverage gates
├── compose.yaml        # ArcadeDB + sidecar stack
└── sqlc.yaml           # sqlc codegen config
```

**Measured Go totals:** 67,703 non-test LOC across `cmd/` + `internal/`, and
23,554 LOC of `_test.go` in the same tree.

## Directory Purposes

**`cmd/aura/`:**
- Purpose: the single production binary — CLI sub-commands and the daemon.
- Contains: the composition roots. `serve.go` and ~45 `serve_*.go` siblings wire
  the daemon; `chat_boot.go` is the shared sub-root; `chat*.go` is the REPL.
- Key files: `main.go`, `serve.go` (`bootServe`), `chat_boot.go`
  (`bootServeChatEnv`), `serve_channels.go`, `serve_webui.go`, `tools.go`.

**`cmd/arcadedb-mcp/`:**
- Purpose: the MCP server the agent uses to reach its own memory.
- Key files: `main.go`, `tool_memory.go`, `tool_memory_recall.go`,
  `tool_memory_graph.go`, `tool_forget.go`, `auth.go`, `tenant.go`.

**`internal/agent/`** (351 files, 24,928 non-test LOC — the largest package):
- Purpose: the agent runtime contract and the LLM tool-dispatch loop.
- Subpackages: `tools/` (the model-callable surface), `mcptools/` (MCP → tool
  bridge), `prompt/` (system-prompt builder, cache-stable), `workflow/`,
  `display/`, `panicobs/`, `agenttest/` (test-only helpers, excluded from the
  coverage denominator).
- Key files: `agent.go`, `event.go`, `budget.go`, `llm_agent.go`,
  `llm_agent_dispatch.go`, `llm_agent_round.go`, `tools/registry.go`,
  `tools/spec.go`, `tools/manifest.go`.

**`internal/agui/`** (214 files, 17,185 LOC):
- Purpose: the AG-UI HTTP + SSE gateway and the whole REST cockpit surface.
- Key files: `server.go`, `server_sse.go`, `server_run.go`, `translator.go`,
  `fanout.go`, `auth.go`, plus one `*_api.go` per REST domain (conversations,
  governance, settings, share, assets, audit, voice, onboarding).

**`internal/arcadedb/`** (126 files, 11,883 LOC):
- Purpose: the bitemporal memory graph — schema, recall, vector search, provenance.
- Key files: `memory.go` (schema + model doc), `memory_recall.go`,
  `memory_vector.go`, `memory_graph_temporal.go`, `memory_supersede.go`,
  `tenant.go`, `client.go`.

**`internal/db/`** (78 files, 11,957 LOC):
- Purpose: Postgres access.
- Contains: `migrations/` (golang-migrate pairs, currently through `0119`),
  `queries/` (sqlc input), `sqlc/` (generated client — never hand-edited).
- Key files: `db.go`, `migrate.go`, `tx.go`, `rls.go`.
- **Next migration number is `ls internal/db/migrations/ | tail -1` + 1 — never
  deduced from a document.**

**`internal/runner/`** (105 files, 5,688 LOC):
- Purpose: per-turn orchestration and durability.
- Key files: `runner.go`, `runner_deps.go`, `runner_persist.go`,
  `runner_resume.go`, `runner_memory_capture.go`, `runner_history.go`.

**`internal/channels/`** (85 files, 5,997 LOC):
- Purpose: the daemon channel contract and its implementations. Telegram is the
  only channel implemented today — there is no WhatsApp package.
- Key files: `channel.go`, `registry.go`, `telegram/bot.go`,
  `telegram/bot_dispatch.go`, `telegram/bot_dispatch_turn.go`,
  `telegram/renderer.go`, `telegram/agui_subscriber.go`.

**`internal/mcp/`** (82 files, 5,524 LOC):
- Purpose: MCP client transport, OAuth, SSRF/egress policy, tool result mapping.
- Subpackages: `manager/` (server lifecycle), `mcpenv/`.
- Key files: `sdkclient.go`, `oauth_flows.go`, `ssrf.go`, `egress_policy.go`.

**`internal/conversations/`** (89 files, 5,717 LOC): durable conversation store,
context budgeting, compaction, sidecars. Key: `store.go`, `compaction.go`,
`context_budget.go`.

**`internal/cron/`** (77 files, 4,066 LOC): the durable scheduler. Key:
`scheduler.go`, `dispatch.go`, `store.go`, `claim.go`, `recover.go`.

**`internal/llm/`** (58 files, 3,961 LOC): provider-neutral client, model
catalog, pricing, capability probes. Key: `client.go`, `runtime.go`,
`capabilities.go`, `model_catalog.go`.

**`internal/skills/`** (58 files, 3,912 LOC): the self-extension system — catalog,
install, validate, materialize, write. Ships built-in skills under
`internal/skills/embed/`. Key: `loader.go`, `installer.go`, `writer.go`,
`validator.go`, `catalog_store.go`.

**`internal/swarm/`** (47 files, 3,324 LOC): delegation and sub-agent spawn. Key:
`swarm.go`, `delegation_run.go`, `delegation_queue.go`, `swarm_depth.go`.

**`internal/sandbox/usersandbox/`** (31 files, 2,189 LOC): the per-user Docker
sandbox. Key: `router.go`, `docker_backend.go`, `spec.go`, `egress.go`, `reap.go`.

**Other notable packages:** `documents/` (5,201), `assets/` (2,851),
`gateway/` (2,146), `config/` (2,113), `share/` (1,798), `web/` (1,717 — SearXNG
search + fetch tools), `objectstore/` (1,692), `retention/` (1,662),
`idempotency/` (1,430), `webauth/` (1,312), `obs/` (1,121). Small leaf helpers:
`envutil/`, `idroot/`, `pgnumeric/`, `boundedbuffer/`, `reasoningfifo/`,
`canonicaljson/`, `procgroup/`, `redact/`, `scoring/`, `bm25/`.

**`web/`:**
- Purpose: the React cockpit, built by Vite and committed into
  `internal/webui/dist` for the single-binary embed.
- Contains: `src/` (560 `.ts`/`.tsx` across `chat/`, `conversations/`,
  `governance/`, `settings/`, `admin/`, `graph/`, `approvals/`, `onboarding/`,
  `files/`, `audit/`, `health/`, `shell/`, `theme/`, `a11y/`, `i18n/`),
  `e2e/` (Playwright), `tokens/`, `scripts/`.
- Key files: `src/main.tsx`, `src/AppShell.tsx`, `src/routes/`, `vite.config.ts`,
  `playwright.config.ts`, `stryker.config.json`.

## Key File Locations

**Entry Points:**
- `cmd/aura/main.go`: CLI dispatch for every sub-command.
- `cmd/aura/serve.go`: `bootServe` — the daemon composition root.
- `cmd/aura/chat_boot.go`: `bootServeChatEnv` — the shared sub-root.
- `cmd/arcadedb-mcp/main.go`: the memory MCP server.
- `web/src/main.tsx`: the web cockpit root.

**Configuration:**
- `internal/config/`: `AURA_*` env loading and validation.
- `compose.yaml`: ArcadeDB and sidecar stack.
- `sqlc.yaml`, `Makefile`, `lefthook.yml`, `.golangci.yml`.
- `scripts/coverage_package_policy.json`: per-package coverage contract.

**Core Logic:**
- `internal/agent/llm_agent.go` + `llm_agent_*.go`: the agent loop.
- `internal/agent/tools/`: every model-callable capability.
- `internal/runner/runner.go`: turn orchestration.
- `internal/arcadedb/memory*.go`: the memory model.

**Testing:**
- Co-located `*_test.go` beside every implementation file.
- `internal/dbtest/`, `internal/agent/agenttest/`: shared harnesses.
- `web/e2e/`: Playwright specs.
- `scripts/`: coverage, mutation, deadcode, dup, and smoke gates.

## Naming Conventions

**Files:**
- Concern-split: `<subject>_<concern>.go` — e.g. `llm_agent_dispatch.go`,
  `memory_graph_temporal.go`, `serve_webui_routes.go`,
  `bot_dispatch_callbacks.go`. This is a direct consequence of the 600-LOC ceiling.
- Tests: `<file>_test.go` beside the source.
- Tiered tests carry the tier in the name and a build tag:
  `*_integration_test.go`, `*_db_integration_test.go`, `*_docker_test.go`,
  `*_live_test.go`, `*_smoke_test.go`.
- Migrations: `NNNN_snake_case_description.{up,down}.sql`.
- Generated code lives only in `internal/db/sqlc/*.sql.go` and is never edited.

**Directories:**
- Lowercase, single word, no underscores: `arcadedb`, `mcpregistry`,
  `usersandbox`, `identityctx`.
- Nested subpackages only where a real boundary exists
  (`internal/agent/tools`, `internal/mcp/manager`, `internal/sandbox/usersandbox`).

**Go identifiers:** exported `PascalCase`, unexported `camelCase`; constructors
are `New...`; consumer-side interfaces are narrow and declared in the consuming
package (`runner.ConversationStore`, `tools.capabilityChecker`).

## File Size Convention — current state

The ≤600 LOC ceiling is enforced by `scripts/check-file-size.sh` (wired into
`make quality` and lefthook pre-push).

**It currently holds for every hand-written Go file.** The only files above 600
LOC are sqlc-generated and exempt:

| File | LOC |
|------|-----|
| `internal/db/sqlc/ingestion_jobs.sql.go` | 933 |
| `internal/db/sqlc/assets.sql.go` | 907 |
| `internal/db/sqlc/conversation_turns.sql.go` | 834 |
| `internal/db/sqlc/conversations.sql.go` | 771 |
| `internal/db/sqlc/models.go` | 727 |
| `internal/db/sqlc/querier.go` | 645 |

Zero hand-written violators. When a file approaches the ceiling, split it into a
`<name>_<concern>.go` sibling in the same commit (refactor-on-touch).

## Where to Add New Code

**New agent tool:**
- Implementation: `internal/agent/tools/<name>.go`, with its `Spec` constant in
  the same file. Set `Deferred: true` for anything with a long description or a
  non-trivial schema; set `Mutating: true` if it can change host state.
- Registration: the composition root in `cmd/aura/` (`tools.go` /
  `serve_adapters.go`) — never a package-level init.
- Tests: `internal/agent/tools/<name>_test.go`.

**New channel:**
- Implementation: `internal/channels/<name>/`, implementing
  `channels.Channel` from `internal/channels/channel.go`.
- Build the fanout per turn, inside the turn handler.
- Wiring: `cmd/aura/serve_channels.go`.

**New HTTP endpoint (cockpit):**
- Handler: `internal/agui/<domain>_api.go`.
- Route registration: `cmd/aura/serve_webui_routes.go` or `serve_agui.go`.
- Frontend: `web/src/<domain>/` plus a client in `web/src/api/`.

**New memory capability:**
- Query/traversal: `internal/arcadedb/memory_<concern>.go`.
- LLM-facing tool: `cmd/arcadedb-mcp/tool_memory_<concern>.go`.

**New database table or column:**
- Migration: `internal/db/migrations/` — number from
  `ls internal/db/migrations/ | tail -1`, both `.up.sql` and `.down.sql`.
- Query: `internal/db/queries/<table>.sql`, then regenerate `internal/db/sqlc/`.

**New scheduled job:**
- Handler: `internal/cron/` + wiring in `cmd/aura/serve.go` (`buildDispatch`),
  plus a `seed<Name>` idempotent seeder and a widened `kind` CHECK migration.

**Shared helpers:**
- Small cross-cutting utilities get their own leaf package under `internal/`
  (`envutil`, `canonicaljson`, `redact`); never a `utils` grab-bag.

## Special Directories

**`internal/webui/dist`:**
- Purpose: the committed Vite build embedded via `//go:embed all:dist`.
- Generated: yes — `npm run build` in `web/`.
- Committed: yes (required for the single-binary build).

**`internal/db/sqlc/`:**
- Purpose: sqlc-generated Postgres client.
- Generated: yes. Committed: yes. Excluded from the coverage denominator.

**`internal/skills/embed/`:**
- Purpose: built-in skills shipped inside the binary (e.g. `memory-aura/SKILL.md`).
- Committed: yes.

**`.planning/`:**
- Purpose: GSD planning artifacts. Tracked in git except `.planning/tmp/` and
  `.planning/graphs/*`.

**`spikes/`, `finetune/`, `artifacts/`, `backups/`, `dist/`:**
- Purpose: experiments, training assets, and build/runtime output. Not part of the
  production dependency graph.

---

*Structure analysis: 2026-09-07*
