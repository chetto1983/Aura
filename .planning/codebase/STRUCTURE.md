# Codebase Structure

**Analysis Date:** 2026-08-25

## Directory Layout

```text
Aura/
├── cmd/
│   ├── aura/                 # Main CLI and long-lived daemon composition root
│   ├── arcadedb-mcp/         # Streamable HTTP MCP server for graph memory
│   └── aura-filecard/        # Document-card subprocess helper
├── internal/                 # Private Go runtime, domains, adapters, and persistence
│   ├── agent/                # Agent contract, LLM loop, prompts, tools, MCP bridge, workflows
│   ├── agui/                 # AG-UI SSE bridge and cockpit REST API
│   ├── runner/               # Durable per-conversation turn orchestration
│   ├── gateway/              # Tool policy, approval, idempotency, reconciliation
│   ├── cron/                 # Scheduler core plus kind-specific handlers
│   ├── channels/telegram/    # Telegram transport, rendering, HITL, media
│   ├── db/                   # pgx, migrations, SQL sources, generated sqlc
│   ├── arcadedb/             # Graph memory and document-index client
│   ├── sandbox/usersandbox/  # Per-identity sandbox port and Docker backend
│   └── webui/dist/           # Committed generated Vite bundle embedded by Go
├── web/                      # React 19 + TypeScript + Vite cockpit source/tooling
│   ├── src/                  # Feature-grouped application source
│   ├── e2e/                  # Playwright scenarios
│   ├── tokens/               # Theme token generator/source
│   └── public/               # Web public assets
├── services/ingest/          # Python/CocoIndex object-store reconciliation sidecar
├── docker/                   # Image build contexts for Aura and appliance sidecars
├── caddy/                    # Reverse-proxy appliance configuration
├── searxng/                  # Search-sidecar configuration
├── observability/            # Prometheus, Tempo, Grafana, and runbooks
├── deploy/                   # systemd unit files
├── scripts/                  # Quality gates, smoke/E2E tests, drills, installers, evals
├── docs/                     # Architecture, operations, audits, quality attestations
├── finetune/                 # Function-calling dataset/export/training support
├── spikes/                   # Committed empirical prototypes and findings
├── public/                   # Release-facing public assets
├── .github/workflows/        # CI/release pipelines
├── .planning/                # Tracked GSD state, maps, phases, research, handoffs
├── .claude/                  # Tracked project-local agent workflow/tooling bundle
├── .agents/skills/           # Local untracked skill installation/indexes
├── go.mod                    # Go module and toolchain contract
├── Makefile                  # Build, test, quality, migration, and appliance targets
├── sqlc.yaml                 # SQL source → generated Go configuration
├── compose.yaml              # Full appliance topology
└── CLAUDE.md                 # Canonical repository rules
```

## Directory Purposes

**`cmd/aura/`:**
- Purpose: Own executable-only command dispatch, dependency composition, cross-package adapters, HTTP route mounting, and daemon lifecycle.
- Contains: Concern-split `*.go` files grouped by command (`chat_*.go`, `mcp_*.go`, `serve_*.go`, `document_*_wiring.go`).
- Key files: `cmd/aura/main.go`, `cmd/aura/chat_boot.go`, `cmd/aura/serve.go`, `cmd/aura/serve_agui.go`, `cmd/aura/serve_webui.go`.

**`cmd/arcadedb-mcp/`:**
- Purpose: Run Aura's model-facing graph-memory MCP server as a separate process.
- Contains: Tenant resolution plus one `tool_*.go` file per MCP capability.
- Key files: `cmd/arcadedb-mcp/main.go`, `cmd/arcadedb-mcp/tenant.go`, `cmd/arcadedb-mcp/tool_memory.go`.

**`cmd/aura-filecard/`:**
- Purpose: Expose the reusable Go filecard implementation to the Python ingestion process.
- Contains: One minimal flag-parsing entry point.
- Key files: `cmd/aura-filecard/main.go`, implementation in `internal/documents/filecard/`.

**`internal/agent/`:**
- Purpose: Own the transport-neutral agent execution model.
- Contains: `Agent`, `InvocationContext`, `Event`, `Budget`, concern-split `llm_agent_*.go`, hooks, tracing, verification, and display normalization.
- Key files: `internal/agent/agent.go`, `internal/agent/event.go`, `internal/agent/budget.go`, `internal/agent/llm_agent.go`.

**`internal/agent/tools/`:**
- Purpose: Implement built-in tools and the dynamic/deferred registry.
- Contains: One tool or tool concern per file, registry/spec/result helpers, shell/background execution, sandbox adapters, document/file/web/skill/scheduling tools.
- Key files: `internal/agent/tools/spec.go`, `internal/agent/tools/registry.go`, `internal/agent/tools/search.go`, `internal/agent/tools/shell_exec.go`.

**`internal/agent/mcptools/`:**
- Purpose: Adapt advertised MCP tools into Aura's registry and supervise live mounts.
- Contains: Mount/retry logic, bridge policies, risk/deferral, view hydration, and redial supervision.
- Key files: `internal/agent/mcptools/mount.go`, `internal/agent/mcptools/bridge.go`, `internal/agent/mcptools/bridge_supervisor.go`.

**`internal/agent/workflow/` and `internal/swarm/`:**
- Purpose: Compose agents sequentially, in parallel, in loops, or as bounded ephemeral worker fan-out.
- Contains: Workflow agent implementations and swarm runner/adapters.
- Key files: `internal/agent/workflow/sequential.go`, `internal/agent/workflow/parallel.go`, `internal/agent/workflow/loop.go`, `internal/swarm/swarm.go`, `internal/swarm/runner_adapter.go`.

**`internal/runner/`:**
- Purpose: Own durable interactive-turn orchestration around fresh `LlmAgent` instances.
- Contains: Per-thread locks, turn assembly, model-context splitting, pause/resume, deletion, persistence, context management, stores/interfaces.
- Key files: `internal/runner/runner.go`, `internal/runner/runner_session.go`, `internal/runner/runner_resume.go`, `internal/runner/runner_persist.go`.

**`internal/agui/`:**
- Purpose: Serve the AG-UI protocol and the cockpit's authenticated REST API.
- Contains: `Server`, route-specific `*_api.go` handlers, SSE translator/writer, auth middleware, detached run registry, consumer-declared service interfaces.
- Key files: `internal/agui/server.go`, `internal/agui/server_run.go`, `internal/agui/translator.go`, `internal/agui/auth.go`.

**`internal/webui/`:**
- Purpose: Embed and serve the compiled browser application as a leaf Go package.
- Contains: `embed.go` plus generated, committed `dist/` assets.
- Key files: `internal/webui/embed.go`, `internal/webui/dist/index.html`.

**`web/src/`:**
- Purpose: Implement the operator cockpit.
- Contains: Feature directories (`chat`, `approvals`, `conversations`, `files`, `governance`, `graph`, `onboarding`, `settings`, `shell`) plus shared UI, theme, i18n, API, and tests.
- Key files: `web/src/main.tsx`, `web/src/AppShell.tsx`, `web/src/chat/ExternalStoreChat.tsx`, `web/src/queryClient.ts`.

**`internal/gateway/`:**
- Purpose: Centralize agent-tool policy enforcement and durable mutation reservation.
- Contains: Risk classification, decisions, approvals/grants, reservations, scopes, guards, and crash reconciliation.
- Key files: `internal/gateway/gateway.go`, `internal/gateway/decide.go`, `internal/gateway/reserve.go`, `internal/gateway/guard.go`.

**`internal/sandbox/usersandbox/`:**
- Purpose: Abstract one identity-scoped execution box and provide the Docker-backed implementation.
- Contains: `Backend` port, router, sandbox spec, Docker lifecycle/exec/egress/materialization, path translation, and idle reaping.
- Key files: `internal/sandbox/usersandbox/backend.go`, `internal/sandbox/usersandbox/router.go`, `internal/sandbox/usersandbox/docker_backend.go`.

**`internal/cron/`:**
- Purpose: Persist, claim, recover, schedule, dispatch, and notify recurring/one-shot work.
- Contains: Core scheduler/store/dispatch types at package root; concrete task kinds under `internal/cron/handlers/`.
- Key files: `internal/cron/scheduler.go`, `internal/cron/store.go`, `internal/cron/dispatch.go`, `internal/cron/handlers/agentjob.go`.

**`internal/channels/`:**
- Purpose: Define fail-soft daemon channel lifecycle and delivery fan-out.
- Contains: Channel/Deliverer interfaces and registry at root; Telegram implementation under `internal/channels/telegram/`.
- Key files: `internal/channels/channel.go`, `internal/channels/registry.go`, `internal/channels/telegram/bot.go`.

**`internal/db/`:**
- Purpose: Own Postgres infrastructure and generated query plumbing.
- Contains: Pool/migration/RLS/transaction logic, paired SQL migrations, handwritten query definitions, and generated sqlc bindings.
- Key files: `internal/db/db.go`, `internal/db/migrate.go`, `internal/db/rls.go`, `internal/db/migrations/`, `internal/db/queries/`, `internal/db/sqlc/`.

**Domain packages under `internal/`:**
- Purpose: Keep one product capability per package with its own store/service/types and narrow dependencies.
- Contains: `internal/conversations/`, `internal/identity/`, `internal/assets/`, `internal/documents/`, `internal/skills/`, `internal/onboarding/`, `internal/settings/`, `internal/share/`, `internal/retention/`, and related small packages.
- Key files: Constructor/store/service files inside each directory, such as `internal/conversations/store.go`, `internal/assets/service.go`, `internal/identity/store.go`.

**External adapter packages under `internal/`:**
- Purpose: Keep protocols and provider details out of domain/runtime code.
- Contains: `internal/llm/`, `internal/mcp/`, `internal/arcadedb/`, `internal/objectstore/`, `internal/web/`, `internal/multimodal/`, `internal/embeddings/`.
- Key files: `internal/llm/client.go`, `internal/mcp/sdkclient.go`, `internal/arcadedb/client.go`, `internal/objectstore/types.go`.

**`services/ingest/`:**
- Purpose: Reconcile identity-scoped object-store files into the ArcadeDB document index.
- Contains: CocoIndex flow, extraction/conversion, chunking, identity/source mapping, ArcadeDB schema, and pytest coverage.
- Key files: `services/ingest/app.py`, `services/ingest/arcade.py`, `services/ingest/extract.py`, `services/ingest/tests/`.

**`docker/`, `caddy/`, `searxng/`, `observability/`, `deploy/`:**
- Purpose: Package and operate the appliance around the binaries.
- Contains: Docker build contexts, proxy/search configs, dashboards/scrape/trace configs/runbooks, and systemd units.
- Key files: `docker/aura/Dockerfile`, `caddy/Caddyfile`, `searxng/settings.yml`, `observability/prometheus/prometheus.yml`, `deploy/aura.service`.

**`scripts/`:**
- Purpose: Provide executable evidence for quality, security, coverage, integration, release, restore, and performance requirements.
- Contains: Shell, Python, Go, and PowerShell gates plus `scripts/eval/` and deterministic fixtures.
- Key files: `scripts/coverage_gate.sh`, `scripts/agui_smoke.sh`, `scripts/quality_snapshot_gate.sh`, `scripts/restore_drill.sh`.

**`docs/`:**
- Purpose: Preserve product architecture, operator procedures, audit findings, and measured quality state.
- Contains: Architecture/overview/capabilities, runbooks, audits, ADR-like reports, and `docs/aura-quality-snapshot.md`.
- Key files: `docs/ARCHITECTURE.md`, `docs/TECHNICAL_OVERVIEW.md`, `docs/aura-quality-snapshot.md`, `docs/audit/`.

**`.planning/`:**
- Purpose: Store tracked GSD project state and generated codebase intelligence.
- Contains: Project/roadmap/state/config, phases, handoffs, research, intel, and `.planning/codebase/`.
- Key files: `.planning/STATE.md`, `.planning/ROADMAP.md`, `.planning/codebase/ARCHITECTURE.md`.

## Key File Locations

**Entry Points:**
- `cmd/aura/main.go`: Top-level CLI dispatch.
- `cmd/aura/serve.go`: Long-lived daemon lifecycle.
- `cmd/arcadedb-mcp/main.go`: Memory MCP HTTP server.
- `cmd/aura-filecard/main.go`: Filecard helper.
- `services/ingest/app.py`: Python ingestion process.
- `web/src/main.tsx`: Browser entry point.

**Configuration:**
- `internal/config/`: Typed environment configuration and profile validation.
- `go.mod`, `go.sum`: Go module/toolchain/dependency lock.
- `web/package.json`, `web/package-lock.json`: Frontend dependency and script contract.
- `web/tsconfig.json`, `web/vite.config.ts`, `web/eslint.config.js`: Frontend compiler/build/lint configuration.
- `sqlc.yaml`: SQL generation mapping.
- `.golangci.yml`, `.editorconfig`, `lefthook.yml`: Repository style and local gates.
- `compose.yaml`: Appliance topology; do not read secret-bearing runtime environment files.

**Core Logic:**
- `internal/runner/runner.go`: Durable turn lifecycle.
- `internal/agent/llm_agent.go`: Model/tool loop.
- `internal/agent/tools/spec.go`: Tool and registry contracts.
- `internal/gateway/decide.go`: Tool policy decision point.
- `internal/agui/server_run.go`: Web-to-runner request boundary.
- `internal/cron/scheduler.go`: Scheduled execution lifecycle.
- `internal/arcadedb/memory.go`: Long-term memory operations.
- `internal/arcadedb/document_retrieval.go`: Hybrid document candidate retrieval.

**Persistence:**
- `internal/db/migrations/`: Paired Postgres migration source of truth.
- `internal/db/queries/`: Handwritten sqlc SQL source.
- `internal/db/sqlc/`: Generated Go bindings; regenerate instead of editing.
- `internal/db/tx.go`, `internal/db/rls.go`: Transaction and tenant-scope infrastructure.
- `internal/objectstore/types.go`: Blob-store port.
- `internal/arcadedb/tenant_clients.go`: Per-identity graph-client resolution.

**Testing:**
- `internal/**/*_test.go`, `cmd/**/*_test.go`: Co-located Go unit/integration tests.
- `web/src/**/*.test.ts(x)`, `web/src/**/__tests__/`: Co-located Vitest tests.
- `web/e2e/`: Playwright browser tests.
- `services/ingest/tests/`: Python ingestion tests.
- `scripts/*smoke*`, `scripts/*gate*`, `scripts/*eval*`: Live/system validation and policy gates.

**Canonical Guidance:**
- `CLAUDE.md`: Binding repository rules.
- `prd.md`: Measured product decisions and acceptance contracts.
- `.agents/skills/spike-findings-Aura/SKILL.md`: Aura-specific spike index; confirm any historical finding against current code.
- `.agents/skills/golang-project-layout/SKILL.md`: Local Go placement guidance (`cmd/` composition, `internal/` implementation, co-located tests).

## Naming Conventions

**Files:**
- Go production files use lowercase snake case by concern: `serve_memory_readiness.go`, `llm_agent_retry.go`, `document_retrieval.go`.
- Go tests are co-located and end `_test.go`; build-tag tiers often encode the tier in the file name, such as `*_integration_test.go` or Docker-specific files under `internal/sandbox/usersandbox/`.
- React components and route/workspace files use PascalCase: `AppShell.tsx`, `ExternalStoreChat.tsx`, `GovernanceWorkspace.tsx`.
- TypeScript hooks/utilities/API modules use camelCase: `useRunUsageOwner.ts`, `sseResume.ts`, `governanceApi.ts`.
- Python modules/tests use snake case: `services/ingest/source.py`, `services/ingest/tests/test_extract.py`.
- SQL migrations use four-digit numeric prefixes and paired `.up.sql`/`.down.sql` files in `internal/db/migrations/`.
- sqlc queries group by domain noun in `internal/db/queries/`, generating a matching file in `internal/db/sqlc/`.

**Directories:**
- Go package directories are lowercase and match package names: `internal/toolinvocations/`, `internal/reasoningtrace/`, `internal/sandbox/usersandbox/`.
- Domain subpackages are introduced only for a real boundary or concern, such as `internal/agent/tools/`, `internal/cron/handlers/`, and `internal/objectstore/garageadmin/`.
- Frontend directories group by product feature (`web/src/chat/`, `web/src/governance/`) and use `components/`, `routes/`, `api/`, or `__tests__/` only where the feature benefits from the split.

**Symbols:**
- Go exported types/functions use PascalCase; unexported functions/variables use camelCase; constructors conventionally use `New` or a precise `New<Type>` in `internal/*`.
- Consumer-facing interfaces name capability, not implementation: `Runner`, `Backend`, `Store`, `Deliverer`, `Handler` in `internal/agui/server.go`, `internal/sandbox/usersandbox/backend.go`, `internal/objectstore/types.go`, and `internal/cron/dispatch.go`.
- React hooks start `use`; query keys are stable exported constants or domain tuples in feature modules under `web/src/`.

## Where to Add New Code

**New CLI Subcommand:**
- Primary dispatch: add the verb in `cmd/aura/main.go`.
- Implementation: create concern-split files such as `cmd/aura/<verb>.go` and `cmd/aura/<verb>_<concern>.go`.
- Domain logic: place reusable behavior in `internal/<domain>/`; keep `cmd/aura/` to parsing, wiring, adapters, and exit-code translation.
- Tests: co-locate `cmd/aura/<verb>_test.go` and add domain tests under `internal/<domain>/`.

**New Daemon Subsystem:**
- Lifecycle/composition: wire into `cmd/aura/serve.go` or a focused `cmd/aura/serve_<subsystem>.go`.
- Reusable service: implement under `internal/<subsystem>/` behind a narrow consumer-side interface.
- Shutdown/readiness: add joined lifecycle logic beside `cmd/aura/serve_lifecycle.go`, `cmd/aura/serve_drain.go`, and `internal/readiness/`.
- Tests: unit-test the internal service and composition adapter separately.

**New Agent Tool:**
- Implementation/spec: add `internal/agent/tools/<name>.go`; split helpers as `<name>_<concern>.go` before 600 LOC.
- Registration: update the shared registry construction in `cmd/aura/main.go` (`buildBaseRegistryWithHandles`) or the appropriate post-store wiring file.
- Policy: set `Mutating`, `Destructive`, `Multiplexed`, and operation metadata in the tool `Spec`; add per-action classification to `internal/gateway/classify.go` when multiplexed.
- Execution boundary: inject sandbox/store/service ports from `cmd/aura/`; do not import high-level domains solely to bypass a consumer seam.
- Tests: `internal/agent/tools/<name>_test.go`, gateway classification tests, and live smoke coverage when external effects exist.

**New MCP Integration:**
- Generic transport/config: `internal/mcp/`.
- Runtime bridge behavior: `internal/agent/mcptools/`.
- Managed recipe/trust/audit: `internal/mcp/manager/` and `internal/mcpregistry/`.
- Composition and live remount: focused `cmd/aura/mcp_*.go` or `cmd/aura/serve_governance*.go`.
- Tests: co-locate protocol/unit tests and add live validation scripts under `scripts/` where the server must be exercised.

**New AG-UI/Cockpit API:**
- Consumer interface and handler: add a focused `internal/agui/<feature>_api.go`; register it from `internal/agui/server.go` or a feature registration helper.
- Concrete adapter: wire in `cmd/aura/serve_<feature>.go` or `cmd/aura/serve_agui.go`.
- Auth/capability mount: register the precise method/path pattern in `cmd/aura/serve_webui.go` or a focused route file such as `serve_webui_share.go`.
- Frontend API/state: add under `web/src/<feature>/`; use `web/src/api/` only for truly cross-feature HTTP primitives.
- Tests: handler tests in `internal/agui/`, component/query tests beside `web/src/<feature>/`, and route-level Playwright coverage in `web/e2e/`.

**New React Surface or Component:**
- Feature workspace: `web/src/<feature>/` with lazy mounting from `web/src/AppShell.tsx` when heavy.
- Shared design-system primitive: `web/src/components/ui/`; do not duplicate an existing primitive.
- App shell/navigation behavior: `web/src/shell/`.
- Theme/tokens: `web/src/theme/`, `web/src/styles/`, and `web/tokens/`.
- Tests: co-locate `*.test.tsx` or use the feature's `__tests__/`; rebuild into `internal/webui/dist/` only after source checks pass.

**New Postgres-Backed Domain:**
- Migration: next paired files in `internal/db/migrations/`, with the next number read from that directory immediately before creation.
- Queries: `internal/db/queries/<domain>.sql`.
- Generated bindings: regenerate `internal/db/sqlc/` via `sqlc.yaml`; never hand-edit it.
- Store/service: `internal/<domain>/`, accepting `*pgxpool.Pool` or a narrow query/transaction port.
- Composition: inject from `cmd/aura/`; transports depend on a consumer interface.
- Tests: co-located unit tests plus `*_integration_test.go` under the owning package or `internal/db/` for migration/RLS contracts.

**New ArcadeDB Memory or Document Capability:**
- Client/domain logic: `internal/arcadedb/<capability>.go` with tenant resolution before every call.
- Model-facing memory tool: `cmd/arcadedb-mcp/tool_<capability>.go`, registered in `cmd/arcadedb-mcp/main.go`.
- Cockpit read adapter: narrow interface in `internal/agui/`, concrete wiring in `cmd/aura/serve_graph_schema.go` or a focused sibling.
- Ingestion schema changes: keep `services/ingest/arcade.py` and `internal/arcadedb/document_schema.go` field-for-field compatible.
- Tests: unit tests plus `arcadedb_integration`/live evidence where engine behavior matters.

**New Scheduled Task Kind:**
- Type/store scheduling contract: `internal/cron/` and a migration/query when persistence shape changes.
- Handler implementation: `internal/cron/handlers/<kind>.go`.
- Adapter/registration: `cmd/aura/serve_dispatch.go`.
- Tests: handler unit tests, scheduler/dispatch tests, and a live scheduler smoke under `scripts/` for external side effects.

**Utilities:**
- Domain-specific helper: keep it in the owning `internal/<domain>/` package.
- Cross-domain pure helper: add a small, narrowly named package directly under `internal/` only after proving two real consumers; existing examples include `internal/canonicaljson/`, `internal/envutil/`, and `internal/boundedbuffer/`.
- Frontend shared helper: `web/src/lib/` only for genuinely cross-feature logic; otherwise keep it beside the feature.

## Special Directories

**`internal/db/sqlc/`:**
- Purpose: Generated Go bindings for `internal/db/queries/`.
- Generated: Yes, via `sqlc.yaml`.
- Committed: Yes.
- Rule: Never edit by hand; change SQL source and regenerate.

**`internal/webui/dist/`:**
- Purpose: Production Vite assets embedded into the Aura Go binary.
- Generated: Yes, by `web/vite.config.ts` and the `web/package.json` build script.
- Committed: Yes.
- Rule: Edit `web/src/`, then rebuild; `internal/webui/embed.go` assumes the tree exists.

**`web/node_modules/`, `web/coverage/`, `web/test-results/`, `web/.stryker-tmp/`:**
- Purpose: Local frontend dependencies and generated quality/test output.
- Generated: Yes.
- Committed: No.

**`artifacts/`, `dist/`, `output/`, `graphify-out/`:**
- Purpose: Local evaluation, release, and analysis outputs.
- Generated: Yes.
- Committed: No.
- Rule: Do not place runtime source or planning truth here.

**`runtime-workspace/`:**
- Purpose: Local runtime workspace mount/staging location.
- Generated: Runtime-managed.
- Committed: No.

**`.planning/`:**
- Purpose: GSD planning and codebase intelligence consumed by later workflows.
- Generated: Mixed; command-managed Markdown/JSON with manual review.
- Committed: Yes, except explicitly ignored temporary/graph output.

**`.agents/skills/`:**
- Purpose: Local skill indexes and references available to agents.
- Generated: Installed/copied by local tooling.
- Committed: No.
- Rule: Treat `CLAUDE.md` and current source as authoritative when a historical skill finding conflicts.

**`.claude/`:**
- Purpose: Project-local workflow, hook, command, and agent tooling.
- Generated: Tool-managed.
- Committed: Yes in substantial part.
- Rule: Application code does not depend on it at runtime.

**`.env` and related environment files:**
- Purpose: Local secret-bearing runtime configuration.
- Generated: Installer/operator-managed.
- Committed: No for live values; `.env.example` documents names only.
- Rule: Never read, quote, or commit live values.

---

*Structure analysis: 2026-08-25*
