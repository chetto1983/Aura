# Phase 1: Infra DB + Knowledge - Context

**Gathered:** 2026-05-29
**Status:** Ready for planning

<domain>
## Phase Boundary

Stand up the two stateful substrates every later phase will depend on:

- **Postgres 17-alpine** (Slice 0.5) with schema `aura.*`, `pgx/v5` pool, `sqlc` v2 codegen, `golang-migrate/v4` embedded file migrations, and **role separation** (`aura_app` runtime vs `aura_migrate` DDL). Two migrations land: `0001_init` (empty schema) and `0002_knowledge_migrations` (Cypher audit table consumed by Slice 0.7).
- **Neo4j 5.26-community LTS** (Slice 0.7) with APOC + GDS + HNSW 768d cosine vector index + fulltext index on `:Chunk`, fronted exclusively by the `mcp-neo4j-cypher` MCP server (stdio subprocess, required on host PATH, no native Go driver). Cypher migrations are `.cypher` files under `internal/knowledge/migrations/`, executed via MCP, with success/failure recorded in Postgres `aura.knowledge_migrations`.
- **`aura-llama-embed` sidecar** (embeddinggemma-300m CPU 4 threads, OpenAI-compat `:8081`) ships **inside this phase** so the `AURA_EMBED_DIMENSIONS=768` env contract is enforced end-to-end at boot via the sidecar's `/health` `{dim}` response — without the sidecar the dim assertion is theoretical.

The deliverable is **infrastructure**: code under `internal/db/`, `internal/knowledge/`, `internal/config/`, plus `compose.yaml`, `Makefile`, `sqlc.yaml`, `.env.example`, and the CLI surface `aura db {migrate|ping|status|reset}` + `aura neo4j {migrate|ping|status|reset}`. Phase 1 stops the moment a fresh `git clone` + `cp .env.example .env` + `make db-up neo4j-up && aura db migrate && aura neo4j migrate && aura db ping && aura neo4j ping` goes green.

**Explicitly NOT in Phase 1:**
- Application tables beyond `0001_init` + `0002_knowledge_migrations` — every domain table (`paused_states`, `conversations`, `conversation_turns`, `skill_audit`, `scheduler_tasks`, etc.) lands in its owning later slice
- The `backup_postgres` / `backup_neo4j` cron handlers — they're Phase 10 (Slice 6b); only the env/path catalog and the manual restore-drill script land here
- Any `:Document`, `:Entity`, `:Community`, `:AgentEpisode`, `:AgentInsight` Neo4j labels — only `:Chunk` lands in `0001_init.cypher` (the rest are Slice 11a)
- Agent runtime, LLM client, ToolResult pattern — all Phase 2+

</domain>

<decisions>
## Implementation Decisions

### Commit Atomicity (discussed)
- **D-01:** Phase 1 ships as **two atomic slice commits** in dependency order, NOT a single phase commit:
  1. `slice 0.5: postgres + sqlc + pgx + golang-migrate infrastructure` — Postgres up, role separation enforced, migrations 0001 + 0002 applied, `make sqlc` green, `db_integration` tests passing, `.env.example` + `compose.yaml` + `Makefile` materialised. Gate 2 verify between commits.
  2. `slice 0.7: neo4j community + mcp-neo4j-cypher + cypher migrations` — Neo4j 5.26-community + APOC + GDS + embed sidecar containers, `mcp-neo4j-cypher` subprocess wiring, `internal/knowledge/{client,config,migrate,ping}.go`, Cypher `0001_init` + Italian smoke fixture, `neo4j_integration` tests passing, `aura neo4j ping` reports server version + sidecar dim.
- **Rationale:** PRD `§Slice Q&A discipline → 1 slice = 1 commit` rule + user's `feedback_one_module_per_slice` ("Un modulo per slice, andiamo calmi") + ROADMAP entry explicitly notes the two slices are "parallelizable — independent stores" (no inter-slice code dep, only Slice 0.7's audit table reads/writes Postgres). Slice 0.7 also writes to `aura.knowledge_migrations` so it has a soft dep on Slice 0.5's `0002` migration being there — order is fixed as 0.5 → 0.7, NOT parallel waves.
- **Implication for planner:** PLAN.md should carry two top-level slice sections (`Slice 0.5 — Postgres` then `Slice 0.7 — Neo4j`), each with its own task list, acceptance checklist, and commit template (copied from PRD lines 109-125 and 215-235 respectively). Each slice independently completes Gate 2 (`go vet + build + test + race` green for its package boundary) before the next starts.

### Claude's Discretion (defaulted, downstream-overridable)

These were not selected for discussion. Defaults chosen using PRD literal text + CLAUDE.md persistence section + memory priors. Each is overridable by the planner if research surfaces a concrete reason; otherwise they hold.

- **D-02 — Compose file path:** `./compose.yaml` at **repo root**, not `sandbox/compose.yaml`.
  - **Why:** CLAUDE.md §Persistence references `compose.yaml` (root); `.planning/codebase/STRUCTURE.md` target layout shows the file at root; the PRD's `sandbox/compose.yaml` reference is vestigial from the pre-rewrite scaffolding (the prior implementation had a `sandbox/` infra dir that no longer applies). Slice 0.5 plan should ship a PRD-amendment note (one-line) updating `§Slice 0.5/0.7 File targets` rows to `compose.yaml (root)`.
- **D-03 — Embed sidecar scope:** `aura-llama-embed` container **ships in Slice 0.7** with healthcheck + dim self-test, NOT deferred to Phase 15.
  - **Why:** PRD §Slice 0.7 acceptance row 182 explicitly requires `aura knowledge ping` to validate sidecar `/health` returns `{"dim":768}` matching `AURA_EMBED_DIMENSIONS`. Without the running sidecar, the embedding-dim contract (Amendment #18, Pitfall #7 — silent retrieval corruption) is theoretical. ROADMAP Phase 1 SC#4 reinforces this. RAM impact (~600 MB idle) already budgeted in PRD §Slice 0.5 RAM table.
- **D-04 — Neo4j smoke fixture corpus:** Tiny (~5-doc) deterministic **Italian fixture committed under `scripts/fixtures/neo4j-smoke/*.md` + companion seed Cypher**. Embed/ingest pipeline reads fixtures → calls embed sidecar → upserts `:Chunk` nodes → runs the 5 known-answer queries and asserts recall@5 = 5/5 + p95 ≤ 30ms.
  - **Why:** The spike corpus at `D:/tmp/aura-neo4j-spike-2026-05-27/` is volatile (host temp, gets swept) and cannot anchor a CI-runnable smoke. Generating from a seed at runtime adds non-determinism. A small committed Italian corpus (reviewable, diffable, replayable) gives the smoke a stable anchor and lets later slices reuse the same fixture for regression. Italian content is non-negotiable per the recall measurement context.
- **D-05 — Subcommand naming:** Keep `aura neo4j {migrate|ping|status|reset}` literal per PRD §Slice 0.7 file targets row.
  - **Why:** PRD is the source of truth; the ROADMAP Phase 1 SC#4 mention of `aura knowledge ping` is a ROADMAP typo (no PRD body uses `aura knowledge`). Slice 0.7 plan should ship a one-line ROADMAP correction (`aura knowledge ping` → `aura neo4j ping` in Phase 1 SC#4) as part of the Slice 0.7 commit. Future "swap Neo4j for X" is too speculative to drive naming now.
- **D-06 — `mcp-neo4j-cypher` mid-runtime crash policy:** **Fail the Aura process** (no restart-once-then-fail, no graceful-degrade). PRD says "lifecycle coupled to main process"; the runtime contract is that the MCP subprocess is part of Aura's process tree.
  - **Why:** Restart-once masks infra rot (the third crash will look like the first one). Graceful-degrade is meaningless when every later phase that touches knowledge will silently no-op. Phase 10 scheduler (Slice 6) can later add a watchdog handler that auto-restarts Aura at the orchestrator level if needed; that's a deliberate Phase 10 decision, not implicit Phase 1 policy.
- **D-07 — `AURA_DB_URL` vs `AURA_DB_MIGRATE_URL` carriage:** Both URLs live on `Config.DB` (`URL string`, `MigrateURL string`). `aura serve` and `aura db ping` use `Config.DB.URL` (role `aura_app`). `aura db migrate` uses `Config.DB.MigrateURL` (role `aura_migrate`). If `MigrateURL` is empty when `aura db migrate` runs → fail-fast with the exact PRD-mandated error message: `AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17`. `aura serve` does NOT require `MigrateURL` to be set.
  - **Why:** PRD §Slice 0.5 amendment #17 row 50 specifies this exact behavior; capturing it now avoids the planner re-deriving it.
- **D-08 — Initial Cypher schema scope:** `0001_init.cypher` lands **only** the `:Chunk(id)` UNIQUE constraint + `chunk_embedding` HNSW 768d cosine vector index + `chunk_text` fulltext index. All other labels (`:Document`, `:Entity`, `:Community`, `:AgentEpisode`, `:AgentInsight`, `:UserConversation`, `:UserSnippet`) are deferred to their owning slices (11a/11b/11c/11e/7e respectively). PRD §Slice 0.7 acceptance row 178 explicit.
  - **Why:** Premature schema = premature commit to relationship cardinality that later slices will reshape. Keep `0001` minimal; let owning slices add their own constraints + indices in their `000N` Cypher migrations.

### Pre-locked by Project Discipline (carry forward, no discussion)

These are PRD/PROJECT/CLAUDE.md locks that planner + researcher MUST honor:

- Postgres 17-alpine, named volume `aura_aura-postgres`, healthcheck `pg_isready`, port 5432
- `jackc/pgx/v5` + `pgxpool.Pool` (`MaxConns=10`, `MinConns=1`, idle 30s)
- `sqlc` v2 codegen (engine postgresql, `json_tags`, `emit_interface`, `emit_exact_table_names`, output `internal/db/sqlc/`); `make sqlc` regen + CI golden test
- `golang-migrate/migrate/v4` with `embed.FS`, file-based `*.up.sql` / `*.down.sql`
- Schema `aura.*` (NOT `public`); no soft-delete default
- Role separation `aura_app` (INSERT/SELECT on audit, INSERT/SELECT/UPDATE/DELETE on mutable, **NO TRUNCATE, NO DROP**) vs `aura_migrate` (ALL inc. DDL, TRUNCATE)
- Migration `0001_init` = `CREATE SCHEMA IF NOT EXISTS aura; SET search_path TO aura, public;` + role grants
- Migration `0002_knowledge_migrations` = `(version int pk, name text, applied_at timestamptz default now(), checksum text)` + sqlc queries `RecordKnowledgeMigration` + `ListAppliedKnowledgeMigrations`
- `internal/config` stays a thin root composite `Config{LLM, DB, Neo4j, RunDir, ToolPreviewCap}`; per-subsystem config (sandbox/web/...) lives in the owning package
- Neo4j 5.26-community LTS pinned (Amendment #2 — avoids CalVer ambiguity post-5.x)
- `NEO4J_PLUGINS='["apoc","graph-data-science"]'`; healthcheck `cypher-shell ... RETURN 1`
- `mcp-neo4j-cypher` required on host PATH (no bundling), subprocess stdio, fail-fast at boot if missing, 10s connect timeout
- No native Go Neo4j driver — discipline ban
- Embedding 768 native (NO MRL truncation); `AURA_EMBED_DIMENSIONS=768` env; boot self-test exits code 78 with exact message on mismatch
- Embedding-model swap runbook section lands in Slice 0.7 PRD body (already there, lines 183-184) — referenced from migration code as comment
- Named volumes only — no bind-mounts on Windows (`feedback_sqlite_wal_windows_corruption` rationale extended to Neo4j)
- Build tags: `//go:build db_integration` (Slice 0.5 tests), `//go:build neo4j_integration` (Slice 0.7 tests)
- `goleak.VerifyNone(t)` in `TestMain` for both packages
- Test discipline at Gate 3: coverage ≥75% unit / ≥60% integration, mutation ≥70% killed, race detector clean
- File targets ≤600 LOC each (god-class ban); refactor-on-touch
- Commit templates: copy verbatim from PRD lines 109-125 (Slice 0.5) and 215-235 (Slice 0.7); both include `Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>` trailer
- Conversation/CONTEXT/PLAN output language for user-facing prose: Italian where natural, English for code/paths/identifiers (per `feedback_all_prompts_in_english_only` — prompts in EN, output in IT where applicable)

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### PRD (source of truth, locked)
- `prd.md` §Slice 0.5 (lines 21-127) — Postgres goal, stack, smoke, acceptance, file targets, open questions, RAM budget, backup strategy, commit template
- `prd.md` §Slice 0.7 (lines 129-237) — Neo4j goal, stack, smoke, acceptance, file targets, open questions, commit template, embedding-model swap runbook
- `prd.md` Amendment #2 — Neo4j 5.26-community LTS pin (avoids CalVer ambiguity post-5.x)
- `prd.md` Amendment #17 — Role separation `aura_app` vs `aura_migrate` + TRUNCATE trigger discipline
- `prd.md` Amendment #18 — `AURA_EMBED_DIMENSIONS` env contract + boot self-test (Pitfall #7 silent corruption prevention)
- `prd.md` §Naming convention (~line 4334) — `AURA_<DOMAIN>_<UNIT>` env discipline, unit suffixes (`_BYTES`, `_SEC`, `_MS`, ...)
- `prd.md` §Slice Q&A discipline — 3 gates (DoR / Implementation / DoD) with thresholds

### Project planning (this repo)
- `.planning/ROADMAP.md` Phase 1 entry (lines 52-63) — 5 Success Criteria (idempotent migrate, role-separation enforcement, restore <90s, `aura knowledge ping` returns version + dim, smoke recall@5 5/5 + p95 ≤30ms)
- `.planning/REQUIREMENTS.md` INFRA-01 (line 18) and INFRA-02 (line 19) — slice-mapped acceptance summaries
- `.planning/PROJECT.md` — substrate identity, constraints, Key Decisions table
- `.planning/STATE.md` — current position; Phase 0 complete (6/6)
- `.planning/codebase/STACK.md` — Planned target stack (lines 17-235; pgx/sqlc/golang-migrate/Neo4j/MCP details aligned with PRD)
- `.planning/codebase/INTEGRATIONS.md` — Planned external integrations (Postgres details lines 92-107; Neo4j details lines 108-119; MCP details lines 294-307)
- `.planning/codebase/ARCHITECTURE.md` — Current vs Target state distinction, runtime layer description
- `.planning/codebase/STRUCTURE.md` — Target directory layout (notes `compose.yaml` at root; lines 56-80)
- `CLAUDE.md` §Persistence — `compose.yaml` at root, named volumes mandatory
- `CLAUDE.md` §Env vars — `AURA_<DOMAIN>_<UNIT>` convention, exceptions for upstream-canonical names (`NEO4J_PASSWORD`, `POSTGRES_PASSWORD`)
- `CLAUDE.md` §Behavioral rules — no god class (≤600 LOC), refactor on touch, 3-strike rule, deferred-tool pattern
- `CLAUDE.md` §Post-edit validation — Gate 2 commands (`go vet + build + test + race`)

### Memory priors that constrain decisions
- `feedback_one_module_per_slice` — drives D-01 (two atomic commits)
- `feedback_embedding_backend_stays_mistral` — drives embed sidecar = `embeddinggemma-300m` via llama.cpp, Neo4j HNSW (not Qdrant), 768 native (no MRL); validates D-03
- `project_graph_memory_core_strategy` — Neo4j + MCP wins; wiki markdown deprecated
- `project_neo4j_spike_2026-05-27` — validates Neo4j+MCP stack choice; recall@5 5/5 + p95 22-30ms on Italian corpus; source for D-04 fixture content
- `feedback_minipc_cpu_budget` — embed sidecar ≤4 threads, no busy-loop polling; constrains sidecar healthcheck cadence
- `feedback_docker_compose_run_msys_path_mangling` — Git Bash `docker compose run` mangles `/workspace/...` paths; use PowerShell or `MSYS_NO_PATHCONV=1` in dev docs
- `feedback_preserve_docker_build_cache` — never `docker builder prune` by default; cold rebuild is 45-60 min for Aura stack
- `feedback_groundtruth_before_planning_2026-05-27` — researcher MUST probe live Postgres/Neo4j docs and `mcp-neo4j-cypher` Apache 2.0 PyPI release notes before planning, not infer from PRD alone
- `feedback_master_direct_workflow` — commit on master, no feature branches/PRs unless explicitly asked
- `feedback_planner_researcher_must_be_opus` — planner + researcher MUST run on Opus; verify via `init.plan-phase` JSON before kicking off
- `reference_aura_implementation_loop_skill` — bounded-slice execution skill at `D:/Aura/.codex/skills/aura-implementation-loop/`; available to executor

### Drifts to flag during planning
- `prd.md` §Slice 0.5 row `sandbox/compose.yaml (diff)` — vestigial; D-02 requires PRD-amendment line in Slice 0.5 commit updating to `compose.yaml (root)`
- `prd.md` §Slice 0.7 row `sandbox/compose.yaml (diff)` — same vestigial pattern; same amendment scope
- `.planning/ROADMAP.md` Phase 1 SC#4 `aura knowledge ping` — typo; D-05 requires ROADMAP one-line fix `aura knowledge ping` → `aura neo4j ping` as part of Slice 0.7 commit
- `prd.md` §Slice 0.9 Pre-requisiti row "Go 1.25+" — confirms Go version target across the project; Phase 1 `go.mod` should already be on Go 1.25 per Phase 0 Amendment #1 (verify in planner)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets

**From the 633-LOC skeleton (commit `af4ca65c`):**
- `internal/agent/loop.go` (131 LOC) — **does NOT touch DB or Neo4j**; safe to leave untouched in Phase 1. Will be rewritten in Phase 2 (Slice 0.9 Agent interface).
- `internal/llm/client.go` (78 LOC) — **does NOT touch DB or Neo4j**; safe in Phase 1. Phase 3 owns the rewrite.
- `internal/agent/tools/{spec,manifest,search,text_response}.go` (~256 LOC total) — Tool/Registry pattern stays put; not consumed by Phase 1.
- `cmd/aura/main.go` (90 LOC) — current subcommand router is the integration point. Phase 1 adds two new top-level subcommands (`db`, `neo4j`) under the existing dispatcher. Existing `tools`, `chat`, `serve`, `shell` cases stay intact (`serve`/`shell` are still `TODO` stubs).

**Effectively a greenfield phase:**
- No `internal/config`, `internal/db`, `internal/knowledge` exist today — all created fresh
- No `compose.yaml`, no `Makefile`, no `sqlc.yaml`, no `.env.example` exist today — all created fresh
- `go.mod` has zero `require` directives — Phase 1 introduces the first external deps (`pgx/v5`, `golang-migrate/v4`, `embed` is stdlib; `sqlc` is a binary, not a Go require)

### Established Patterns

**From the skeleton (must respect):**
- Deferred-tool pattern (`tools.Spec.Deferred bool`, `internal/agent/tools/spec.go:30`) — Phase 1 introduces no tools, so this is informational only
- Module path is `github.com/chetto1983/aura`; new packages live under `internal/`
- Go 1.23 in current `go.mod` — bump to **Go 1.25** is a Phase 0 amendment (#1); verify the bump landed before Slice 0.5 starts

**From CLAUDE.md §Post-edit validation (must run on every edit):**
- `go vet ./...`
- `go build ./...`
- `go test ./internal/<package>/`
- `go test -race ./internal/<package>/`

### Integration Points

- `cmd/aura/main.go` subcommand dispatcher — Phase 1 adds `db` and `neo4j` cases
- `internal/config/config.go` (new) — root composite consumed by both `internal/db` and `internal/knowledge`; loaded by every `cmd/aura/*` subcommand
- `.env` / `.env.example` (new) — secrets loaded via `godotenv` at the start of `cmd/aura/main.go`'s subcommand handlers (load order spec'd in PRD Slice 1 OQ1; Phase 1 only consumes `POSTGRES_PASSWORD`, `AURA_DB_URL`, `AURA_DB_MIGRATE_URL`, `NEO4J_PASSWORD`, `NEO4J_USER`, `AURA_EMBED_DIMENSIONS`)
- `compose.yaml` (new, root) — Phase 1 adds 3 services: `postgres`, `neo4j`, `aura-llama-embed`. Aura binary's `depends_on: service_healthy` is set up on all 3 for future `aura serve` (but `aura serve` is still a stub — Phase 12 owns it)
- `Makefile` (new) — targets `make sqlc`, `make db-up`, `make db-migrate`, `make db-reset`, `make neo4j-up`, `make neo4j-migrate`, `make neo4j-reset`
- `internal/db/migrations/` (new) — `0001_init.up.sql` / `0001_init.down.sql` (empty schema + roles) + `0002_knowledge_migrations.up.sql` / `0002_knowledge_migrations.down.sql` (Cypher audit table)
- `internal/db/queries/` (new) — sqlc source SQL; `knowledge_migrations.sql` for the 2 Slice 0.7 queries
- `internal/knowledge/migrations/` (new) — `0001_init.cypher` with `:Chunk` constraint + vector + fulltext index

</code_context>

<specifics>
## Specific Ideas

- **Smoke fixture content (D-04):** Italian corpus of ~5 short documents covering distinguishable topics so the 5 known-answer queries are deterministic. Suggested seed (planner can refine): (1) breve nota sulla pasta amatriciana; (2) descrizione del Duomo di Milano; (3) scheda Fiat Panda 30 history; (4) sinossi *Il nome della rosa* di Eco; (5) note sul caffè espresso napoletano. Five queries map 1:1 to the 5 docs; expected: top-1 hit per query, recall@5 = 5/5 trivially achieved on a 5-doc corpus. The point isn't retrieval difficulty — it's exercising the embed-sidecar→Neo4j-HNSW round-trip end-to-end with non-toy Italian text. Planner free to revise content.
- **CLI subcommand conventions:** `aura db migrate` and `aura neo4j migrate` exit non-zero on failure, print human-readable + machine-parseable status line on success (e.g. `ok: 2 migrations applied (0001_init, 0002_knowledge_migrations)`). `aura db status` and `aura neo4j status` print the table from `aura.knowledge_migrations` for Neo4j; Postgres status prints from `schema_migrations` (golang-migrate's own table).
- **Restore drill script:** `scripts/restore_drill.sh` lives in Slice 0.5 commit; takes a `pg_dump`-style file path arg, calls `pg_restore` (or `psql` for plain SQL), times the operation, asserts < 90s (ROADMAP SC#3). Backup itself stays in Phase 10.

</specifics>

<deferred>
## Deferred Ideas

- **Domain tables** — every `aura.*` table beyond `0001_init` + `0002_knowledge_migrations` lands in its owning slice (paused_states/conversations/identities/scheduler_tasks/skill_audit/etc). Phase 1 does not pre-create them. (Phases 2, 4, 10, 11)
- **Cron backup handlers** — `backup_postgres` + `backup_neo4j` `TaskKind` handlers materialise in Phase 10 (Slice 6b) once the scheduler exists. Phase 1 ships only the manual restore drill. (Phase 10)
- **Full Neo4j schema** — `:Document`, `:Entity`, `:Community`, `:AgentEpisode`, `:AgentInsight`, `:UserConversation`, `:UserSnippet` labels + their constraints/indices/relations deferred to the owning slice. (Phase 11 for memory; Phase 11/7e for snippet)
- **`aura knowledge ingest` / GraphRAG retrieval** — entire memory subsystem is Phase 15 (Slice 11a-e). Phase 1 only proves the substrate works (smoke), not the consuming app.
- **MCP server subprocess watchdog** — D-06 hard-fails on crash; a graceful auto-restart watchdog could come from Phase 10 scheduler later. Logged here so it isn't lost.
- **Multi-database Neo4j** — Community edition only supports `neo4j` default database; multi-DB (`CREATE DATABASE aura`) requires Enterprise. Locked out by license choice. Not revisited unless Enterprise license enters scope.
- **`aura init-models` bundle distribution of `mcp-neo4j-cypher`** — PRD OQ 1 dismisses bundling as scope creep. Not revisited in Phase 1.
- **Per-conversation Postgres connection pooling tuning** — current `MaxConns=10` is a sensible default; Phase 1 doesn't tune. Phase 10 scheduler workload may surface contention and prompt a tuning slice.

</deferred>

---

*Phase: 1-Infra-DB-Knowledge*
*Context gathered: 2026-05-29*
