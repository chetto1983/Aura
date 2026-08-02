# Codebase Structure

**Analysis Date:** 2026-08-02

## Directory Layout

```text
Aura/
|-- cmd/
|   |-- aura/                 # Main CLI, daemon, and process composition roots
|   `-- arcadedb-mcp/         # Standalone long-term-memory MCP server
|-- internal/
|   |-- agent/                # Agent contract, LLM loop, prompts, tools, workflows
|   |-- agui/                 # AG-UI SSE bridge and cockpit REST handlers
|   |-- runner/               # Durable turn orchestration and HITL resume
|   |-- gateway/              # Tool policy, approval, reservation, reconciliation
|   |-- llm/                  # Provider-neutral LLM API and OpenAI-compatible client
|   |-- mcp/                  # MCP transports and managed-server control plane
|   |-- db/                   # pgx/sqlc, migrations, transactions, DB operations
|   |-- arcadedb/             # ArcadeDB HTTP client and memory operations
|   |-- conversations/        # Conversation/history/context management
|   |-- documents/            # Document catalog, ingestion jobs, open/search flows
|   |-- assets/               # Upload/finalize/process asset lifecycle
|   |-- objectstore/          # S3/filesystem blob abstraction and Garage admin
|   |-- cron/                 # Scheduler, store, dispatch, notifications, handlers
|   |-- channels/             # Channel registry and Telegram adapter
|   |-- skills/               # Skill loader, lifecycle, validation, audit adapters
|   |-- sandbox/              # Per-user sandbox routing and Docker backend
|   `-- ...                   # Focused domain and cross-cutting utility packages
|-- web/
|   |-- src/                  # React cockpit source, organized by feature
|   |-- e2e/                  # Playwright end-to-end tests
|   |-- public/               # Web assets copied by Vite
|   |-- tokens/               # Theme-token source/generator
|   `-- package.json          # Frontend scripts and dependencies
|-- internal/webui/dist/      # Generated, committed SPA embedded by Go
|-- finetune/                 # Python fine-tuning/export tooling
|-- docker/                   # Service image build contexts
|-- deploy/                   # systemd units
|-- caddy/                    # Reverse-proxy configuration
|-- observability/            # Grafana, Prometheus, Tempo, and runbooks
|-- searxng/                  # SearXNG service configuration
|-- scripts/                  # Quality gates, smoke tests, drills, eval helpers
|-- docs/                     # Architecture, ADRs, audits, runbooks, research
|-- spikes/                   # Experimental benchmarks and investigations
|-- .github/                  # CI workflows, CodeQL, templates, ownership
|-- .claude/                  # Committed GSD/agent workflow assets
|-- .agents/skills/           # Project skills used by agents
|-- .planning/codebase/       # Generated codebase maps
|-- prd.md                    # Product/architecture truth source
|-- CLAUDE.md                 # Binding repository rules
|-- go.mod                    # Go module and dependency manifest
|-- Makefile                  # Build, test, coverage, and quality entry points
|-- sqlc.yaml                 # SQL-to-Go generation configuration
`-- compose.yaml              # Local service topology
```

### Internal Package Map

```text
internal/
|-- agent/
|   |-- tools/                # Built-in tools and tool registry/spec
|   |-- prompt/               # Stable prompt builder and reasoning routing
|   |-- workflow/             # Sequential, parallel, and loop composition
|   |-- mcptools/             # MCP-to-Tool bridge and mount lifecycle
|   |-- display/              # Transport-neutral structured display payloads
|   |-- panicobs/             # Bounded panic observability
|   `-- agenttest/            # Shared test fakes, not production behavior
|-- agui/                     # HTTP APIs split one concern per *_api.go file
|-- arcadedb/                 # Client, memory, vector, schema, tenant helpers
|-- askuser/                  # Durable human-input pauses
|-- assets/                   # Asset service, processors, upload lifecycle
|-- breakglass/               # Audited recovery/escape-hatch integration
|-- channels/telegram/        # Telegram bot, renderers, HITL, media, onboarding
|-- config/                   # Environment/config parsing and profile validation
|-- conversations/           # Store, branching, history ladder, sidecar GC
|-- cron/handlers/            # Concrete task-kind handlers
|-- db/
|   |-- migrations/           # Paired zero-padded up/down SQL migrations
|   |-- queries/              # Handwritten sqlc query sources by domain
|   `-- sqlc/                 # Generated, committed Go data-access code
|-- documents/                # Catalog/service/store/jobs/open/orphan concerns
|-- gateway/                  # classify/decide/approve/reserve/reconcile split
|-- identity/                 # Identities and capability grants
|-- identityctx/              # Request-context identity carrier
|-- llm/openai_compat/        # Streaming OpenAI-compatible transport
|-- mcp/manager/              # Managed MCP recipes/config/runtime/audit
|-- objectstore/garageadmin/  # Garage provisioning/admin client
|-- obs/                      # OpenTelemetry and Prometheus setup/helpers
|-- retention/                # Retention planning/execution workers
|-- runner/                   # Turn, persistence, session, resume, delete concerns
|-- sandbox/usersandbox/      # Docker-backed identity sandbox implementation
|-- share/                    # Internal/public share lifecycle and snapshots
|-- skills/                   # Skill discovery, lifecycle, catalog, audit
|-- swarm/                    # Bounded multi-agent fan-out
|-- web/                      # SSRF-hardened search/fetch client
|-- webauth/                  # Authula wrapper
`-- webui/                    # Embedded generated frontend distribution
```

### Frontend Feature Map

```text
web/src/
|-- main.tsx                  # React root, providers, and top-level routes
|-- AppShell.tsx              # Authenticated cockpit shell and surface switching
|-- api/                      # Cross-feature HTTP/idempotency helpers
|-- chat/                     # Assistant runtime, SSE, tools, artifacts, voice
|-- conversations/            # Sidebar, search, lifecycle, export
|-- documents/                # Document library workspace and API
|-- governance/               # MCP, skills, and scheduler administration
|-- graph/                    # ArcadeDB graph/schema explorer
|-- settings/                 # Model, capability, Telegram, and share settings
|-- approvals/                # Pending approval state and resolution UI
|-- admin/                    # Current identity/capability API hooks
|-- audit/                    # Operator audit view
|-- onboarding/               # First-run and identity provisioning flows
|-- auth/                     # Login/bootstrap/password-reset UI clients
|-- shell/                    # Responsive shell, drawers, dock, surface state
|-- components/ui/            # Local shadcn/Radix primitives
|-- components/skeleton/      # Shared loading shells
|-- routes/                   # Route-level login/share/404 views
|-- a11y/                     # Accessibility helpers
|-- i18n/                     # i18next setup and split locale resources
|-- styles/                   # Tailwind entry plus theme/motion/atmosphere CSS
|-- theme/                    # Theme and density application
|-- lib/                      # Small shared utilities
`-- test/                     # Vitest browser/test setup
```

## Directory Purposes

**`cmd/aura/`:**
- Purpose: Own executable-only orchestration and concrete dependency injection for the main binary.
- Contains: Subcommand handlers, chat/serve composition roots, HTTP parent-mux wiring, scheduler adapters, channel/provisioning adapters.
- Key files: `cmd/aura/main.go`, `cmd/aura/chat_boot.go`, `cmd/aura/serve.go`, `cmd/aura/serve_agui.go`, `cmd/aura/serve_webui.go`, `cmd/aura/serve_dispatch.go`

**`cmd/arcadedb-mcp/`:**
- Purpose: Build the separate Streamable-HTTP MCP server for long-term memory.
- Contains: Process lifecycle, tenant resolution, memory tools, graph-schema tool.
- Key files: `cmd/arcadedb-mcp/main.go`, `cmd/arcadedb-mcp/tenant.go`, `cmd/arcadedb-mcp/tool_memory.go`, `cmd/arcadedb-mcp/tool_graph_schema.go`

**`internal/agent/`:**
- Purpose: Hold the transport-neutral agent kernel.
- Contains: Open `Agent` contract, `InvocationContext`, `Budget`, event model, LLM loop, hooks, prompt construction, tools, workflow composition.
- Key files: `internal/agent/agent.go`, `internal/agent/event.go`, `internal/agent/budget.go`, `internal/agent/llm_agent.go`, `internal/agent/tools/spec.go`

**`internal/runner/`:**
- Purpose: Turn the stateless per-round agent into a durable conversation application service.
- Contains: Narrow dependency interfaces, history loading, per-thread locks, event persistence, pause/resume, deletion, auto-title workers.
- Key files: `internal/runner/interfaces.go`, `internal/runner/runner.go`, `internal/runner/runner_persist.go`, `internal/runner/runner_resume.go`, `internal/runner/resume_committer.go`

**`internal/agui/`:**
- Purpose: Provide the authenticated cockpit's protocol and REST server implementation.
- Contains: AG-UI translator/SSE, conversation/assets/documents/approvals/share/graph/governance/settings/onboarding/admin APIs, readiness, strict decoding.
- Key files: `internal/agui/server.go`, `internal/agui/server_run.go`, `internal/agui/translator.go`, `internal/agui/server_sse.go`, `internal/agui/auth.go`

**`internal/gateway/` and `internal/agent/tools/`:**
- Purpose: Define available model actions and enforce runtime-profile policy before execution.
- Contains: Tool specs/registry/implementations, risk classification, approval handling, mutation reservation/replay, orphan reconciliation.
- Key files: `internal/agent/tools/spec.go`, `internal/agent/tools/registry.go`, `internal/gateway/decide.go`, `internal/gateway/reserve.go`

**`internal/db/`:**
- Purpose: Centralize Postgres connectivity, schema evolution, transaction boundaries, and generated data access.
- Contains: `pgxpool` open/ping, embedded migrations, `WithTx`/RLS transactions, handwritten SQL, sqlc output.
- Key files: `internal/db/db.go`, `internal/db/migrate.go`, `internal/db/tx.go`, `internal/db/queries/`, `internal/db/sqlc/`

**Domain packages under `internal/`:**
- Purpose: Keep business rules and persistence adapters focused by capability.
- Contains: Conversation/history code in `internal/conversations/`, document catalog/jobs in `internal/documents/`, asset processing in `internal/assets/`, schedules in `internal/cron/`, skills in `internal/skills/`, shares in `internal/share/`, identity in `internal/identity/`, retention in `internal/retention/`.
- Key files: `internal/conversations/store.go`, `internal/documents/catalog_service.go`, `internal/assets/service.go`, `internal/cron/scheduler.go`, `internal/skills/loader.go`, `internal/share/service.go`

**`internal/mcp/` and `internal/arcadedb/`:**
- Purpose: Isolate external capability protocols behind internal clients.
- Contains: stdio/HTTP MCP transport, SSRF/egress controls, managed configuration, ArcadeDB REST operations, per-identity memory helpers.
- Key files: `internal/mcp/transport.go`, `internal/mcp/http_client.go`, `internal/mcp/manager/config.go`, `internal/arcadedb/client.go`, `internal/arcadedb/memory.go`

**`web/src/`:**
- Purpose: Implement the cockpit as a feature-organized React SPA.
- Contains: Router/providers, responsive application shell, chat streaming, feature workspaces, shared UI primitives, styles, unit tests.
- Key files: `web/src/main.tsx`, `web/src/AppShell.tsx`, `web/src/chat/ExternalStoreChat.tsx`, `web/src/chat/sseAdapter.ts`, `web/src/queryClient.ts`

**`scripts/`:**
- Purpose: Make repository quality, smoke, coverage, security, release, and operational checks executable.
- Contains: Shell/Python/Go test harnesses and gate scripts.
- Key files: `scripts/coverage_gate.sh`, `scripts/check-file-size.sh`, `scripts/agui_boundary_check.sh`, `scripts/web_dist_freshness.sh`, `scripts/quality_snapshot_gate.sh`

**`docs/`:**
- Purpose: Store maintained architecture, capability, ADR, audit, deployment, research, and runbook material.
- Contains: Narrative documentation and evidence artifacts; it does not replace `prd.md` as the truth source.
- Key files: `docs/ARCHITECTURE.md`, `docs/TECHNICAL_OVERVIEW.md`, `docs/CAPABILITIES.md`, `docs/adr/`, `docs/audit/`, `docs/runbooks/`

## Key File Locations

**Entry Points:**
- `cmd/aura/main.go`: Main binary entry and subcommand dispatch.
- `cmd/aura/serve.go`: Long-running daemon lifecycle.
- `cmd/arcadedb-mcp/main.go`: Memory MCP sidecar entry.
- `web/src/main.tsx`: Browser application entry.

**Configuration:**
- `CLAUDE.md`: Binding repository implementation and validation rules.
- `prd.md`: Product and architecture truth source; amend before implementation that deviates from it.
- `internal/config/config.go`: Application configuration aggregation/loading.
- `internal/config/config_validate.go`: Profile-aware validation.
- `go.mod`: Go version and module dependencies.
- `web/package.json`: Frontend runtime/build/test dependencies and scripts.
- `web/components.json`: shadcn aliases, CSS entry, icon library, and non-RSC mode.
- `sqlc.yaml`: SQL generation inputs/outputs.
- `.golangci.yml`: Go lint configuration.
- `Makefile`: Repository quality/build commands.
- `compose.yaml`: Local multi-service topology; inspect only non-sensitive sections when changing deployment wiring.

**Core Logic:**
- `internal/runner/runner.go`: One user turn through history, fresh-agent construction, and event persistence.
- `internal/agent/llm_agent.go`: LLM/tool execution loop.
- `internal/gateway/decide.go`: Tool policy decision chokepoint.
- `internal/agui/server_run.go`: Cockpit run ingress.
- `internal/cron/scheduler.go`: Background task execution.
- `internal/documents/catalog_service.go`: Document catalog business service.
- `internal/objectstore/types.go`: Blob-storage port.
- `internal/arcadedb/memory.go`: Long-term memory operations.

**Persistence:**
- `internal/db/migrations/`: Paired schema migrations; the directory determines the next slot.
- `internal/db/queries/`: Handwritten domain SQL consumed by sqlc.
- `internal/db/sqlc/`: Generated Go query/model code.
- `internal/db/tx.go`: Atomic and identity-scoped transaction seams.

**Frontend:**
- `web/src/AppShell.tsx`: Authenticated shell and feature-surface composition.
- `web/src/chat/ExternalStoreChat.tsx`: assistant-ui external-store integration.
- `web/src/chat/sseAdapter.ts`: AG-UI SSE parser/reducer/pump.
- `web/src/components/ui/`: Shared local shadcn components.
- `web/src/styles/index.css`: Tailwind/theme CSS entry configured by `web/components.json`.
- `internal/webui/embed.go`: Go embed boundary for the built SPA.

**Testing:**
- `internal/**/**/*_test.go`: Co-located Go unit tests by package.
- `internal/**/*_integration_test.go`: Co-located tagged/live integration tests.
- `cmd/aura/*_test.go`: Composition-root and CLI behavior tests.
- `web/src/**/*.test.ts` and `web/src/**/*.test.tsx`: Co-located frontend unit/component tests.
- `web/src/**/__tests__/`: Feature-grouped frontend tests.
- `web/e2e/`: Playwright end-to-end tests.
- `scripts/*smoke*.sh` and `scripts/*gate*`: Cross-process smoke and quality gates.

## Naming Conventions

**Files:**
- Use lowercase snake-case Go filenames, with concern suffixes for splits: `internal/agent/llm_agent_dispatch.go`, `internal/runner/runner_resume.go`, `cmd/aura/serve_webui_routes.go`.
- Keep the executable package as `package main` under `cmd/<binary>/`; name each command/wiring file after the subcommand or concern, as in `cmd/aura/chat.go` and `cmd/aura/serve_agui.go`.
- Name Go tests `<source>_test.go`; use explicit live-tier suffixes such as `_integration_test.go` and build tags where the test requires Postgres, ArcadeDB, or Docker.
- Name migrations `NNNN_description.up.sql` and `NNNN_description.down.sql`, matching files such as `internal/db/migrations/0085_document_digest_gin_fastupdate_off.up.sql`.
- Name React components and route views in PascalCase: `web/src/chat/ExternalStoreChat.tsx`, `web/src/routes/SharePage.tsx`.
- Name React hooks with `use` plus PascalCase intent: `web/src/conversations/useConversations.ts`, `web/src/shell/useSurfaceRestore.ts`.
- Name frontend API/model/helper modules in camelCase by domain: `web/src/documents/documentApi.ts`, `web/src/governance/governanceApi.ts`, `web/src/chat/toolGrouping.ts`.
- Name local shadcn primitives in lowercase/kebab style under `web/src/components/ui/`, for example `confirm-dialog.tsx` and `native-select.tsx`.
- Split oversized frontend concerns beside the owner rather than creating a generic utilities dump: `web/src/chat/ExternalStoreChat_assets.ts`, `web/src/chat/sseAdapter_frames.ts`.

**Directories:**
- Use short lowercase Go package directories under `internal/`, such as `runner`, `gateway`, `documents`, and `toolinvocations`.
- Group code by capability first; nested subpackages mark a real boundary, as in `internal/agent/tools/`, `internal/cron/handlers/`, and `internal/channels/telegram/`.
- Group frontend code by product feature under `web/src/`; reserve `web/src/components/ui/`, `web/src/lib/`, and `web/src/api/` for genuinely cross-feature code.
- Keep tests co-located with the package/feature; do not create a repository-wide unit-test tree outside `web/e2e/` and script-level harnesses.

## Where to Add New Code

**New Backend Feature:**
- Primary code: create or extend a focused `internal/<domain>/` package; follow examples in `internal/documents/`, `internal/share/`, and `internal/retention/`.
- Composition: inject concrete dependencies from a focused `cmd/aura/<feature>.go` or `cmd/aura/serve_<feature>.go`; shared daemon/chat assembly belongs in `cmd/aura/chat_boot.go`.
- Tests: co-locate `*_test.go` in the same `internal/<domain>/` package, adding a tagged `_integration_test.go` only for live infrastructure behavior.

**New CLI Command:**
- Implementation: add `cmd/aura/<command>.go` with a `run<Command>` entry, then add the explicit dispatch case and usage text in `cmd/aura/main.go`.
- Tests: add `cmd/aura/<command>_test.go`; reuse composition helpers rather than constructing a parallel runtime.

**New HTTP/API Endpoint:**
- Handler: place domain behavior in a focused `internal/agui/<feature>_api.go`; keep reusable domain rules in `internal/<domain>/`.
- Internal route: register it from `internal/agui/server.go` or a focused `register*Routes` method.
- Authenticated mount: add the exact path/prefix and capability wrapper to `cmd/aura/serve_webui.go`; keep shared route constants in `cmd/aura/serve_webui_routes.go` or the matching focused `serve_webui_*.go`.
- Tests: add `internal/agui/<feature>_api_test.go` plus a parent-mux reachability/capability test in `cmd/aura/serve_webui*_test.go` when authorization changes.

**New Agent Tool:**
- Implementation: add `internal/agent/tools/<name>.go` implementing `tools.Tool`.
- Registration: wire it in `buildBaseRegistryWithHandles` in `cmd/aura/main.go`; keep heavy descriptions/schemas `Deferred: true` and ensure mutating operation metadata is complete.
- External integration: declare a narrow consumer interface in the tool file and inject its adapter from `cmd/aura/`; do not import a high-level domain solely to construct it.
- Tests: add `internal/agent/tools/<name>_test.go` and gateway/registry tests if classification or mutation metadata changes.

**New MCP Integration:**
- Generic transport behavior: extend `internal/mcp/`.
- Managed recipes/configuration: extend `internal/mcp/manager/` and mount through `internal/agent/mcptools/`.
- Aura-owned sidecar tool: add one `tool_<name>.go` under `cmd/arcadedb-mcp/` and register it in `newServer` in `cmd/arcadedb-mcp/main.go`.

**New Database Behavior:**
- Schema: add the next paired migration in `internal/db/migrations/`, taking the next number from the live directory listing.
- Queries: add or change domain SQL in `internal/db/queries/<domain>.sql`, then regenerate committed output in `internal/db/sqlc/` using `sqlc.yaml`.
- Transactional orchestration: reuse `internal/db/tx.go`; keep domain conversion in its store package instead of editing generated sqlc files.
- Tests: add co-located unit and `db_integration` tests under `internal/db/` or the owning domain package.

**New Frontend Feature:**
- Primary code: add a focused folder under `web/src/<feature>/` with components, API client, view-model helpers, and co-located tests.
- Shell surface: lazy-load large workspaces from `web/src/AppShell.tsx`; add a top-level URL only through `web/src/main.tsx`.
- Shared UI: use existing `web/src/components/ui/` components first; shadcn additions follow `web/components.json` and shared style changes go through `web/src/styles/index.css` or its imported focused sheets.
- Server state: use `web/src/queryClient.ts` and feature hooks; streaming chat state belongs in `web/src/chat/`.

**Utilities:**
- Go helpers: keep them inside the owning package unless at least two independent packages need a small, stable abstraction; existing cross-package utilities live in focused packages such as `internal/canonicaljson/`, `internal/envutil/`, and `internal/boundedbuffer/`.
- Frontend helpers: keep feature helpers beside their consumers; use `web/src/lib/` or `web/src/api/` only for cross-feature primitives.

## Special Directories

**`internal/webui/dist/`:**
- Purpose: Built Vite distribution embedded into the Go binary by `internal/webui/embed.go`.
- Generated: Yes, from `web/` via its build pipeline.
- Committed: Yes; current Git index contains the embedded distribution.

**`internal/db/sqlc/`:**
- Purpose: Generated Go models and query methods from `internal/db/queries/`.
- Generated: Yes, through `sqlc.yaml`.
- Committed: Yes; edit the SQL source and regenerate rather than hand-editing this directory.

**`internal/db/migrations/`:**
- Purpose: Ordered PostgreSQL schema history embedded by `internal/db/migrate.go`.
- Generated: No; migrations are intentionally authored as up/down pairs.
- Committed: Yes.

**`.planning/`:**
- Purpose: GSD-generated project planning, maps, graphs, phases, and verification artifacts.
- Generated: Yes, by GSD commands including the current codebase map.
- Committed: No tracked files in the current worktree at analysis time; workflow commands manage its lifecycle.

**`.claude/`:**
- Purpose: Repository-local GSD commands, agents, hooks, skills, and settings.
- Generated: Tool-managed but repository-local.
- Committed: Yes.

**`runtime-workspace/`:**
- Purpose: Local working directory exposed to runtime tools when configured.
- Generated: Runtime/local state.
- Committed: No tracked files in the current worktree.

**`dist/`:**
- Purpose: Local cross-platform release artifacts produced by the release pipeline.
- Generated: Yes.
- Committed: No tracked files in the current worktree.

**`web/coverage/`, `web/test-results/`, `web/.stryker-tmp/`:**
- Purpose: Frontend coverage, Playwright output, and mutation-test workspaces.
- Generated: Yes.
- Committed: No; treat as disposable test output.

**`artifacts/`, `output/`, `graphify-out/`:**
- Purpose: Local audit, design, graph, and workflow outputs.
- Generated: Yes or task-produced.
- Committed: No tracked files in the current worktree at analysis time.

---

*Structure analysis: 2026-08-02*
