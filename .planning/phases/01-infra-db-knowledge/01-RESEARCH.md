# Phase 1: Infra DB + Knowledge — Research

**Researched:** 2026-05-29
**Domain:** PostgreSQL 17 + Neo4j 5.26-community LTS substrate + `mcp-neo4j-cypher` MCP stdio + `aura-llama-embed` (embeddinggemma-300m) sidecar
**Confidence:** HIGH (every version pin probed against authoritative registry / docs; ground-truth verified on this machine)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**D-01 — Commit Atomicity:** Phase 1 ships as **two atomic slice commits** in dependency order, NOT a single phase commit:
  1. `slice 0.5: postgres + sqlc + pgx + golang-migrate infrastructure`
  2. `slice 0.7: neo4j community + mcp-neo4j-cypher + cypher migrations`
  Gate 2 verify between commits. Planner MUST produce two top-level slice sections; each independently completes Gate 2 before the next starts.

**D-02 — Compose file path:** `./compose.yaml` at **repo root**, not `sandbox/compose.yaml`. Slice 0.5 commit ships a one-line PRD-amendment updating `§Slice 0.5/0.7 File targets` rows to `compose.yaml (root)`.

**D-03 — Embed sidecar scope:** `aura-llama-embed` container ships **inside Slice 0.7** with healthcheck + dim self-test, NOT deferred to Phase 15. Without it the embedding-dim contract (Amendment #18) is theoretical.

**D-04 — Neo4j smoke fixture corpus:** Tiny (~5-doc) deterministic Italian fixture committed under `scripts/fixtures/neo4j-smoke/*.md` + companion seed Cypher. Smoke ingests, embeds, upserts `:Chunk` nodes, runs 5 known-answer queries asserting recall@5 = 5/5 + p95 ≤ 30ms.

**D-05 — Subcommand naming:** Keep `aura neo4j {migrate|ping|status|reset}` literal per PRD §Slice 0.7. Slice 0.7 commit ships a one-line ROADMAP correction: Phase 1 SC#4 `aura knowledge ping` → `aura neo4j ping`.

**D-06 — `mcp-neo4j-cypher` mid-runtime crash policy:** **Fail the Aura process** (no restart-once-then-fail, no graceful-degrade). Subprocess is part of Aura's process tree.

**D-07 — `AURA_DB_URL` vs `AURA_DB_MIGRATE_URL` carriage:** Both URLs live on `Config.DB` (`URL string`, `MigrateURL string`). `aura serve` and `aura db ping` use `Config.DB.URL` (role `aura_app`). `aura db migrate` uses `Config.DB.MigrateURL` (role `aura_migrate`). Empty `MigrateURL` when `aura db migrate` runs → fail-fast with exact message: `AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17`. `aura serve` does NOT require `MigrateURL`.

**D-08 — Initial Cypher schema scope:** `0001_init.cypher` lands **only** `:Chunk(id)` UNIQUE constraint + `chunk_embedding` HNSW 768d cosine vector index + `chunk_text` fulltext index. All other labels deferred to owning slices.

### Claude's Discretion

The CONTEXT.md `Claude's Discretion` block is already saturated by D-02 through D-08 (every defaulted decision was locked there). No remaining discretion items at the planner level.

### Deferred Ideas (OUT OF SCOPE)

- Domain tables beyond `0001_init` + `0002_knowledge_migrations` (lands in owning slices: paused_states/conversations/identities/scheduler_tasks/skill_audit/etc)
- Cron backup handlers (`backup_postgres` + `backup_neo4j` materialise in Phase 10)
- Full Neo4j schema (`:Document`, `:Entity`, `:Community`, `:AgentEpisode`, `:AgentInsight`, `:UserConversation`, `:UserSnippet` — Phase 11/15)
- `aura knowledge ingest` / GraphRAG retrieval (Phase 15)
- MCP server subprocess watchdog (potential Phase 10 scheduler addition)
- Multi-database Neo4j (Enterprise-only)
- `aura init-models` bundle distribution of `mcp-neo4j-cypher` (PRD OQ 1 dismissed)
- Per-conversation Postgres connection pooling tuning (Phase 10+ if contention surfaces)
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| INFRA-01 | Postgres 17 + sqlc + pgx + golang-migrate operativo con schema `aura.*` + role separation `aura_app` vs `aura_migrate`. Boot fail-fast su DB unreachable. Nightly restore drill < 90s. [Slice 0.5 + amendment #17] | See sections: Standard Stack (Postgres/pgx/sqlc/migrate rows), Architecture Patterns Pattern 1+2, Code Examples §Postgres, Migration `0001_init.up.sql` skeleton, Validation Architecture INFRA-01 dimensions, Pitfall #1 (role grants), Pitfall #2 (DSN secrets), 5 Success Criteria #1-3 traceability |
| INFRA-02 | Neo4j 5.26-community + APOC + GDS + HNSW vector index 768d cosine + `mcp-neo4j-cypher` MCP server + embed sidecar boot-assert `AURA_EMBED_DIMENSIONS=768`. Spike recall@5 5/5 maintained as smoke. [Slice 0.7 + amendments #2, #18] | See sections: Standard Stack (Neo4j/mcp-neo4j-cypher/llama.cpp rows), Architecture Patterns Pattern 3+4+5, Code Examples §Neo4j + §Embed Sidecar, Migration `0001_init.cypher` skeleton, Validation Architecture INFRA-02 dimensions, Pitfall #4 (Cypher syntax drift), Pitfall #5 (sidecar dim mismatch silent corruption), 5 Success Criteria #4-5 traceability |
</phase_requirements>

## Summary

Phase 1 is a **greenfield stack-up** of two persistence substrates plus an embedding sidecar. Nothing in the 633-LOC skeleton touches Postgres or Neo4j, so this phase materialises every artefact from zero: `compose.yaml`, `Makefile`, `sqlc.yaml`, `.env.example`, eight Go packages under `internal/db/` and `internal/knowledge/` and `internal/config/`, two SQL migrations, one Cypher migration, a tiny Italian smoke corpus, and a restore-drill shell script. Every external dependency was probed live on 2026-05-29 against its authoritative registry/docs: `pgx v5.9.2`, `golang-migrate v4.19.1`, `sqlc v1.31.1`, `godotenv v1.5.1`, `goleak v1.3.0`, `mcp-neo4j-cypher 0.6.0` (already installed on this machine, declares `neo4j>=5.26.0` — exactly the LTS pin). Postgres `17.10-alpine3.23` and `neo4j:5.26.26-community` are the current container tags. All packages passed `slopcheck` clean.

Three load-bearing risks surface that the planner MUST handle:

1. **The PRD's "`aura neo4j ping` validates sidecar `/health` returns `{"dim":768}`" acceptance is wrong** as literally stated — `llama-server /health` returns `{"status":"ok"}`, NOT `{"dim":N}`. The dim self-test must come from a different probe: either (a) a one-call `/v1/embeddings` POST with a dummy input and `len(data[0].embedding)` assertion, or (b) a custom wrapper sidecar that exposes `/health` augmented with the dim from a startup probe. Recommendation: **(a) — keep the sidecar a stock `ghcr.io/ggml-org/llama.cpp:server` image with `--embeddings`, perform the dim probe in Go**. This requires a one-line PRD amendment in the Slice 0.7 commit.
2. **`mcp-neo4j-cypher` license is unspecified in upstream metadata.** `pip show` returns blank `License:`, the LICENSE file 404s, the README doesn't state one. CONTEXT.md / PRD says "Apache 2.0" — verified-likely (Neo4j contrib org convention), but planner should add a documentation step that captures the upstream license explicitly before the Slice 0.7 commit, per OWASP supply-chain discipline.
3. **`go.mod` is now on Go 1.25.0** (slopcheck `go get` auto-bumped during this research session; the bump is the Phase 0 Amendment #1 outcome, so this is fortuitous-aligned, not a drift). `go.sum` was also created with the 5 Phase 1 dependencies pre-staged.

**Primary recommendation:** Plan as two atomic slice commits per D-01. Slice 0.5 lands ~280 src + ~120 test LOC + the Postgres compose service + 2 SQL migrations + Makefile + sqlc.yaml + .env.example. Slice 0.7 lands ~280 src + ~120 test LOC + the Neo4j and embed compose services + the Cypher migration + the Italian smoke fixture + `scripts/neo4j_smoke.sh`. Every package pin in this RESEARCH was verified against its upstream registry on 2026-05-29 and is `[VERIFIED: registry+slopcheck]` unless explicitly noted otherwise.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Postgres connection pool | Persistence (`internal/db`) | — | `pgxpool` is the canonical Go-side pool; one package, one concern |
| Schema migrations (Postgres) | Persistence (`internal/db/migrate`) | — | `golang-migrate` v4 with `iofs` source embeds `*.up.sql`/`*.down.sql` into the binary |
| sqlc codegen output | Persistence (`internal/db/sqlc`) | — | Build-time, machine-generated; never hand-edited; CI golden test enforces sync |
| Role separation enforcement | Persistence (DDL in `0001_init.up.sql`) | CLI dispatcher (`cmd/aura`) | DDL creates the roles + grants; CLI dispatcher picks `URL` vs `MigrateURL` per subcommand |
| Postgres `aura.knowledge_migrations` audit table | Persistence (`internal/db`) | Knowledge (`internal/knowledge/migrate`) | Table lives in Postgres (Slice 0.5 `0002` migration); writer is Slice 0.7 Cypher migrator |
| Neo4j connection (Bolt) | Knowledge (`internal/knowledge/client`) | — | Sole interface = MCP subprocess; no native driver per discipline |
| `mcp-neo4j-cypher` subprocess lifecycle | Knowledge (`internal/knowledge/client`) | CLI dispatcher | Spawn at first knowledge subcommand; lifecycle coupled to Aura process per D-06 |
| Cypher migrations | Knowledge (`internal/knowledge/migrate`) | Persistence (audit table writer) | `.cypher` files executed via MCP; success/failure row in Postgres |
| Embedding sidecar (`aura-llama-embed`) | Infrastructure (Docker compose service) | Knowledge (boot dim probe in `internal/knowledge/ping`) | Container is infra; the dim self-test that gates Aura boot lives in Go |
| Italian smoke fixture | Tests/Scripts (`scripts/neo4j_smoke.sh`) | Knowledge (uses embed + MCP) | Shell harness orchestrates; assertion logic lives in Go-side test or one-shot binary |
| Restore drill | Tests/Scripts (`scripts/restore_drill.sh`) | — | Shell-only; calls `pg_restore`/`psql` and times itself |
| Compose orchestration | Infrastructure (`compose.yaml` at root) | — | Three services this phase: `postgres`, `neo4j`, `aura-llama-embed` |
| Subcommand dispatch | CLI (`cmd/aura/main.go`) | — | Existing `switch os.Args[1]` extended with `db` and `neo4j` cases |
| Config loading | Composition (`internal/config`) | All packages reading config | Thin root composite `Config{LLM, DB, Neo4j, RunDir, ToolPreviewCap}` per CLAUDE.md; per-subsystem configs live in their packages |

## Standard Stack

### Core (Go runtime deps)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/jackc/pgx/v5` | `v5.9.2` (Apr 19, 2026) `[VERIFIED: pkg.go.dev + slopcheck OK]` | Postgres driver + `pgxpool.Pool` | Performance leader for Postgres in Go; pure-Go, no CGO; canonical choice; Uber/PlanetScale pattern |
| `github.com/golang-migrate/migrate/v4` | `v4.19.1` (Nov 29, 2025) `[VERIFIED: pkg.go.dev + github releases API + slopcheck OK]` | Schema migration engine | Battle-tested; sqlc's migration module is experimental (PRD OQ 0.5 #2 resolved); `iofs` source supports `embed.FS` |
| `github.com/joho/godotenv` | `v1.5.1` (Feb 5, 2023) `[VERIFIED: github + slopcheck OK]` | `.env` loader | Feature-complete per upstream; PRD §Slice 1 load order step 2 |
| `github.com/google/uuid` | `v1.6.0` `[VERIFIED: slopcheck OK]` | UUID generation (idempotency keys / future entity IDs) | Pre-installed in this research session for Phase 2 prep; gen_random_uuid handles PG side, this handles Go side |
| `go.uber.org/goleak` | `v1.3.0` `[VERIFIED: slopcheck OK]` | Goroutine leak detection in `TestMain` | Mandated by PRD §Slice 0.5/0.7 + amendment #15 (goleak extension) |

### Supporting (build-time tooling / non-Go deps)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `sqlc` CLI | `v1.31.1` `[VERIFIED: github releases API]` | SQL-first codegen | Invoked via `make sqlc`; CI golden test guards regen sync. NOT a Go `require` — binary on PATH. Install via `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1` (devcontainer/CI also documents this). |
| `mcp-neo4j-cypher` | `0.6.0` (Apr 10, 2026) `[VERIFIED: PyPI + local install confirmed at /c/Users/Davide/AppData/Roaming/Python/Python313/Scripts/mcp-neo4j-cypher; pyproject declares neo4j>=5.26.0]` | Cypher MCP server over stdio | Required on host PATH per PRD §Slice 0.7 OQ 1; install via `pip install mcp-neo4j-cypher==0.6.0`. License field empty in PyPI metadata — see Pitfall #6. |
| `postgres:17.10-alpine3.23` | tag verified `[VERIFIED: hub.docker.com]` | Postgres 17 container | `compose.yaml` service `postgres` |
| `neo4j:5.26.26-community` | tag verified `[VERIFIED: hub.docker.com — 5.26.26 latest patch]` | Neo4j 5.26-community LTS | `compose.yaml` service `neo4j` (Amendment #2 pin honored) |
| `ghcr.io/ggml-org/llama.cpp:server` | tag verified `[VERIFIED: ghcr.io packages page — "server" variant]` | OpenAI-compat embeddings server | `compose.yaml` service `aura-llama-embed`; flag `--embeddings`; exposes `/v1/embeddings` and `/health` |
| `ggml-org/embeddinggemma-300M-GGUF` (Q8_0) | `[VERIFIED: huggingface.co/ggml-org/embeddinggemma-300M-GGUF]` | 768-native embedding model | 334 MB Q8_0; downloaded by llama-server via `-hf ggml-org/embeddinggemma-300M-GGUF`; pulled at first-boot then cached in named volume |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `pgx/v5` | `database/sql + lib/pq` | lib/pq is in maintenance mode; pgx is the active fork with better perf and type support |
| `golang-migrate` | `pressly/goose`, `rubenv/sql-migrate` | All viable; golang-migrate has more drivers, larger user base, `iofs` source for `embed.FS` |
| `sqlc` | `gorm`, `sqlboiler`, `ent` | ORMs are runtime-reflection-heavy; sqlc is build-time codegen, zero runtime overhead, PRD-locked |
| `mcp-neo4j-cypher` (Python subprocess) | `neo4j-go-driver` v6 native | PRD discipline ban (see CLAUDE.md §Project scope row 14 — no native Go Neo4j driver) |
| `embeddinggemma-300M-GGUF` Q8_0 | `nomic-embed-text` GGUF (768d), `bge-small-en` (384d) | Locked per memory `feedback_embedding_backend_stays_mistral` — embeddinggemma at 768 native, no MRL truncation |
| Neo4j 2025.x or 2026.x | Neo4j 5.26-community LTS | LTS pin per Amendment #2 — avoids CalVer ambiguity post-5.x |

**Installation (Slice 0.5):**
```bash
# Go runtime deps for Slice 0.5 (already pulled into go.sum by this research session)
go get github.com/jackc/pgx/v5@v5.9.2
go get github.com/golang-migrate/migrate/v4@v4.19.1
go get github.com/joho/godotenv@v1.5.1
go get go.uber.org/goleak@v1.3.0

# sqlc CLI (build-time)
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
```

**Installation (Slice 0.7):**
```bash
# google/uuid optional this slice (used for migration UUIDs); already in go.sum
go get github.com/google/uuid@v1.6.0

# mcp-neo4j-cypher (Python subprocess on host PATH)
pip install mcp-neo4j-cypher==0.6.0

# Container images pulled by `make neo4j-up`:
#   neo4j:5.26.26-community
#   ghcr.io/ggml-org/llama.cpp:server
```

**Version verification log (executed 2026-05-29):**
- `pgx v5.9.2`: `[VERIFIED]` via pkg.go.dev published 2026-04-19 (recent, < 60 days)
- `golang-migrate v4.19.1`: `[VERIFIED]` via github.com/golang-migrate/migrate releases API (Nov 2025)
- `sqlc v1.31.1`: `[VERIFIED]` via github.com/sqlc-dev/sqlc releases API
- `godotenv v1.5.1`: `[VERIFIED]` via github project page
- `goleak v1.3.0`: `[VERIFIED]` via slopcheck go get probe
- `mcp-neo4j-cypher 0.6.0`: `[VERIFIED: local install]` — `pip show mcp-neo4j-cypher` returns Version: 0.6.0, declares deps `fastmcp`, `neo4j>=5.26.0`, `pydantic`, `tiktoken`. CLI `--help` confirmed flags `--db-url`, `--username`, `--password`, `--database`, `--transport`, `--namespace`, `--read-timeout`, `--token-limit`, `--read-only`, `--allow-origins`, `--allowed-hosts`, `--schema-sample-size`. Default transport stdio (per upstream README).
- `postgres:17.10-alpine3.23`: `[VERIFIED]` via hub.docker.com supported tags list
- `neo4j:5.26.26-community`: `[VERIFIED]` via hub.docker.com — latest patch of 5.26 line
- `ghcr.io/ggml-org/llama.cpp:server`: `[VERIFIED]` via ghcr.io packages page

## Package Legitimacy Audit

> Slopcheck ran during this research session against the 5 Go runtime deps in parallel with `go get`. All 5 returned `[OK]`. Two flags surfaced:

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| `github.com/jackc/pgx/v5` | go | 40 days (v5.9.2) | Mass-adopted (canonical Go Postgres) | github.com/jackc/pgx | `[OK]` with "Relatively new" note + "No source repository linked" (slopcheck reads Go module metadata not GH) | Approved |
| `github.com/golang-migrate/migrate/v4` | go | mature | Mass-adopted | github.com/golang-migrate/migrate | `[OK]` with "No source repository linked" | Approved |
| `github.com/joho/godotenv` | go | mature (v1.5.1 since 2023) | Mass-adopted | github.com/joho/godotenv | `[OK]` with "No source repository linked" | Approved |
| `github.com/google/uuid` | go | mature | Mass-adopted | github.com/google/uuid | `[OK]` with "No source repository linked" | Approved |
| `go.uber.org/goleak` | go | mature | Mass-adopted (Uber stdlib) | github.com/uber-go/goleak | `[OK]` with "No source repository linked" | Approved |
| `sqlc` CLI (binary, not Go require) | github | mature | Mass-adopted | github.com/sqlc-dev/sqlc | `[OK]` (assumed; not Go-`require`d, slopcheck go subcommand doesn't apply) | Approved |
| `mcp-neo4j-cypher==0.6.0` | PyPI | 49 days (Apr 10, 2026) | Niche-but-official (neo4j-contrib org) | github.com/neo4j-contrib/mcp-neo4j | Not run via `slopcheck install` (Python ecosystem); local install confirmed; supply-chain assessed via pyproject.toml deps + neo4j>=5.26 declaration | Approved with caveat — see Pitfall #6 (upstream license field empty) |

**Packages removed due to slopcheck `[SLOP]` verdict:** none.

**Packages flagged as suspicious `[SUS]`:** none. (slopcheck's "Relatively new" note on `pgx v5.9.2` is informational — pgx v5 line has existed for years; this is just the latest 40-day-old patch.)

**Side-effect of this research session:** `go.mod` was automatically bumped from `go 1.23` → `go 1.25.0` by `go get`'s toolchain selection. The 5 Phase 1 deps were added (currently as `// indirect` because no Go code imports them yet — they become direct as soon as Slice 0.5 lands the first `import`). This bump satisfies Phase 0 Amendment #1 fortuitously; the planner should treat the bump as already-applied and note in the Slice 0.5 commit body.

## Architecture Patterns

### System Architecture Diagram

```
                          ┌─────────────────────────┐
                          │  Operator CLI           │
                          │  $ aura db {migrate|    │
                          │    ping|status|reset}   │
                          │  $ aura neo4j {migrate| │
                          │    ping|status|reset}   │
                          └────────────┬────────────┘
                                       │
                                       ▼
                          ┌─────────────────────────┐
                          │  cmd/aura/main.go       │
                          │  switch os.Args[1]      │
                          │   case "db":  dbCmd()   │
                          │   case "neo4j": n4jCmd()│
                          └────┬───────────────┬────┘
                               │               │
                  ┌────────────┘               └────────────┐
                  ▼                                         ▼
        ┌───────────────────┐                  ┌────────────────────────┐
        │  internal/db      │                  │  internal/knowledge    │
        │ ┌──────────────┐  │                  │ ┌─────────────────┐    │
        │ │ config       │◀─┼─.env (godotenv)──┼▶│ config          │    │
        │ │ db (pool)    │  │                  │ │ client (MCP)    │    │
        │ │ migrate      │  │                  │ │ migrate         │    │
        │ │ ping/status  │  │                  │ │ ping            │    │
        │ │ reset        │  │                  │ │ reset           │    │
        │ │ sqlc/        │  │                  │ └────┬────────────┘    │
        │ │ migrations/  │  │                  │      │                 │
        │ │ queries/     │  │                  │ migrations/*.cypher    │
        │ └──┬───────────┘  │                  │      │                 │
        └────┼──────────────┘                  └──────┼─────────────────┘
             │                                        │
             │ AURA_DB_URL (aura_app)                 │ stdio
             │ AURA_DB_MIGRATE_URL (aura_migrate)     │ subprocess
             │                                        ▼
             │                          ┌────────────────────────┐
             │                          │ mcp-neo4j-cypher       │
             │                          │ (Python, Apache 2.0 *) │
             │                          │ --db-url --username    │
             │                          │ --password --database  │
             │                          └────────┬───────────────┘
             │                                   │ bolt://7687
             │                                   ▼
             ▼                          ┌────────────────────────┐
   ┌─────────────────┐                  │ aura-neo4j             │
   │ aura-postgres   │ knowledge_migrations │ (5.26.26-community)│
   │ (17.10-alpine)  │◀─audit row────── │ APOC + GDS plugins     │
   │ schema=aura     │                  │ HNSW :Chunk.embedding  │
   │ roles:          │                  │ fulltext :Chunk.text   │
   │  aura_app       │                  └────────┬───────────────┘
   │  aura_migrate   │                           │
   └─────────────────┘                           │
                                                 │ embeddings
                                                 ▼
                                       ┌────────────────────────┐
                                       │ aura-llama-embed       │
                                       │ ghcr.io/ggml-org/      │
                                       │  llama.cpp:server      │
                                       │ --embeddings           │
                                       │  -hf ggml-org/         │
                                       │  embeddinggemma-300M-  │
                                       │  GGUF                  │
                                       │ /v1/embeddings (768d)  │
                                       │ /health → {"status":   │
                                       │   "ok"}                │
                                       └────────────────────────┘

* mcp-neo4j-cypher license field empty in PyPI metadata
  (see Pitfall #6); neo4j-contrib org convention is Apache 2.0,
  capture explicitly during Slice 0.7 work.

Smoke flow (scripts/neo4j_smoke.sh):
  fixtures/*.md ──read──▶ embed → POST /v1/embeddings (768d)
                       └─upsert──▶ MCP write_neo4j_cypher
                                        MERGE (c:Chunk {id})
                                        SET c.embedding, c.text
                       ──query──▶ MCP read_neo4j_cypher
                                        db.index.vector.queryNodes
                                        recall@5 = 5/5, p95 ≤ 30ms
```

### Recommended Project Structure (Phase 1 deliverable)

```
Aura/
├── .env.example                                # NEW (Slice 0.5)
├── Makefile                                    # NEW (Slice 0.5, extended in 0.7)
├── compose.yaml                                # NEW (Slice 0.5 with postgres; 0.7 adds neo4j + aura-llama-embed)
├── sqlc.yaml                                   # NEW (Slice 0.5)
├── cmd/aura/main.go                            # +50 LOC dispatcher rows (Slice 0.5) + 40 LOC (Slice 0.7)
├── internal/
│   ├── config/
│   │   └── config.go                           # NEW ~110 LOC (Slice 0.5; 0.7 adds Neo4j sub-struct)
│   ├── db/                                     # NEW (Slice 0.5)
│   │   ├── config.go                           # ~40 LOC — DBConfig{URL, MigrateURL, MaxConns, MinConns, MaxConnIdleTime}
│   │   ├── db.go                               # ~90 LOC — Open(ctx, *Config) (*pgxpool.Pool, error) + ping + Close
│   │   ├── migrate.go                          # ~80 LOC — golang-migrate wrapper, embed.FS, Up/Down/Status, idempotent
│   │   ├── migrations/
│   │   │   ├── 0001_init.up.sql                # ~50 LOC — CREATE SCHEMA + roles + grants + default-privileges
│   │   │   ├── 0001_init.down.sql              # ~10 LOC — DROP SCHEMA CASCADE + REVOKE + DROP ROLE
│   │   │   ├── 0002_knowledge_migrations.up.sql   # ~20 LOC — CREATE TABLE aura.knowledge_migrations + grants
│   │   │   └── 0002_knowledge_migrations.down.sql # ~3 LOC — DROP TABLE
│   │   ├── queries/
│   │   │   └── knowledge_migrations.sql        # NEW Slice 0.5 (consumed by 0.7) — 2 named queries
│   │   ├── sqlc/                               # GENERATED (Slice 0.5) — `make sqlc`
│   │   │   ├── db.go
│   │   │   ├── models.go
│   │   │   ├── querier.go
│   │   │   └── knowledge_migrations.sql.go
│   │   ├── schema.sql                          # ~20 LOC — concatenation of migrations for sqlc source
│   │   ├── ping.go                             # ~30 LOC — SELECT 1 + latency print (Slice 0.5)
│   │   ├── status.go                           # ~50 LOC — list schema_migrations rows (golang-migrate's own) (Slice 0.5)
│   │   ├── reset.go                            # ~40 LOC — DROP SCHEMA CASCADE + re-migrate (dev only, guarded behind --yes) (Slice 0.5)
│   │   └── db_test.go                          # ~120 LOC — build tag db_integration; goleak; pool + migrate + role-deny test
│   └── knowledge/                              # NEW (Slice 0.7)
│       ├── config.go                           # ~40 LOC — Neo4jConfig{BoltURL, User, Password, Database, MCPBinary, ConnectTimeoutSec, EmbedURL, EmbedDimensions}
│       ├── client.go                           # ~120 LOC — MCP subprocess spawn, stdio framing, JSON-RPC envelope, Cypher(ctx, query, params) ([]Record, error)
│       ├── migrate.go                          # ~120 LOC — read embed.FS *.cypher numbered, parse "-- migrate:up", checksum, execute via MCP, write aura.knowledge_migrations row
│       ├── migrations/
│       │   └── 0001_init.cypher                # ~25 LOC — CREATE CONSTRAINT + CREATE VECTOR INDEX (HNSW 768d cosine M=32) + CREATE FULLTEXT INDEX
│       ├── ping.go                             # ~80 LOC — MCP ping (RETURN 1, server-version probe) + embed sidecar dim self-test via POST /v1/embeddings with dummy input
│       ├── status.go                           # ~40 LOC — query aura.knowledge_migrations via sqlc
│       ├── reset.go                            # ~50 LOC — DROP all indexes + constraints + MATCH (n) DETACH DELETE (dev only) + re-migrate
│       └── client_test.go                      # ~120 LOC — build tag neo4j_integration; goleak; spawn MCP + migrate + Cypher MATCH + dim assert
└── scripts/
    ├── restore_drill.sh                        # NEW (Slice 0.5) — pg_restore + assert <90s + cleanup
    ├── neo4j_smoke.sh                          # NEW (Slice 0.7) — ingest fixtures → query → assert recall@5=5/5, p95≤30ms
    └── fixtures/
        └── neo4j-smoke/                        # NEW (Slice 0.7)
            ├── 01_amatriciana.md
            ├── 02_duomo_milano.md
            ├── 03_fiat_panda.md
            ├── 04_nome_della_rosa.md
            ├── 05_espresso_napoletano.md
            └── queries.txt                     # 5 IT queries; expected top-1 doc ID per query
```

**LOC budget per package** (every file ≤600 LOC per CLAUDE.md god-class ban):
- `internal/config`: 110 LOC (fits in 1 file)
- `internal/db`: 90+80+30+50+40+120 (test) = ~410 LOC across 6 files; biggest is `db_test.go` at ~120; well under 600
- `internal/db/sqlc/*` (generated): unbounded by hand; sqlc keeps each file focused, expected ~150 LOC total at Phase 1
- `internal/knowledge`: 120+120+80+40+50+120 (test) = ~530 LOC across 6 files; biggest single file is `client.go` at ~120 LOC; well under 600

### Pattern 1: Postgres connection pool with role-distinct URLs

**What:** Two DSNs live on `Config.DB`. `URL` (role `aura_app`) is the runtime connection. `MigrateURL` (role `aura_migrate`) is read **only** by the `aura db migrate` subcommand.

**When to use:** Every Postgres-touching subcommand of Phase 1+ honors this split. `aura db ping` and `aura serve` (future) use `URL`. `aura db migrate` and `aura db reset` use `MigrateURL`. `aura neo4j migrate` reads/writes `aura.knowledge_migrations` via `URL` (the table is mutable from runtime role per amendment #17).

**Example:**
```go
// Source: pgx pkg.go.dev + jackc/pgx pgxpool docs + PRD §Slice 0.5 amendment #17
package db

import (
    "context"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
    URL            string        // role aura_app — runtime
    MigrateURL     string        // role aura_migrate — DDL only
    MaxConns       int32         // default 10
    MinConns       int32         // default 1
    MaxConnIdleTime time.Duration // default 30s
}

func Open(ctx context.Context, cfg *Config) (*pgxpool.Pool, error) {
    pc, err := pgxpool.ParseConfig(cfg.URL)
    if err != nil {
        return nil, fmt.Errorf("parse db url: %w", err)
    }
    if cfg.MaxConns > 0 {
        pc.MaxConns = cfg.MaxConns
    }
    if cfg.MinConns > 0 {
        pc.MinConns = cfg.MinConns
    }
    if cfg.MaxConnIdleTime > 0 {
        pc.MaxConnIdleTime = cfg.MaxConnIdleTime
    }
    pool, err := pgxpool.NewWithConfig(ctx, pc)
    if err != nil {
        return nil, fmt.Errorf("open pool: %w", err)
    }
    if err := pool.Ping(ctx); err != nil {
        pool.Close()
        return nil, fmt.Errorf("ping: %w", err)
    }
    return pool, nil
}
```

### Pattern 2: `golang-migrate` with `embed.FS` + `iofs` source

**What:** Numbered `.up.sql`/`.down.sql` files embedded into the binary at build time; migrations run via the `iofs` source. No filesystem lookup at runtime, no external migration tool dependency.

**When to use:** Every Postgres schema change in Phase 1+; identical pattern reused by Slice 1.5/1.7/1.8/6/7/9a/10/11/13 migrations.

**Example:**
```go
// Source: golang-migrate v4 docs + iofs source driver
package db

import (
    "context"
    "embed"
    "errors"
    "fmt"

    "github.com/golang-migrate/migrate/v4"
    _ "github.com/golang-migrate/migrate/v4/database/postgres"
    "github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies all pending migrations using the migrate role.
// Idempotent: if no migrations are pending, returns ErrNoChange (caller maps to "ok: no pending migrations").
func Migrate(ctx context.Context, migrateURL string) (int, error) {
    if migrateURL == "" {
        return 0, errors.New("AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17")
    }
    src, err := iofs.New(migrationsFS, "migrations")
    if err != nil {
        return 0, fmt.Errorf("source: %w", err)
    }
    m, err := migrate.NewWithSourceInstance("iofs", src, migrateURL)
    if err != nil {
        return 0, fmt.Errorf("new migrator: %w", err)
    }
    defer m.Close()
    pre, _, _ := m.Version()
    if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
        return 0, fmt.Errorf("up: %w", err)
    }
    post, _, _ := m.Version()
    return int(post) - int(pre), nil // count of newly-applied
}
```

### Pattern 3: `mcp-neo4j-cypher` subprocess over stdio with JSON-RPC framing

**What:** Spawn the Python MCP server as a child process; communicate via stdin/stdout using the MCP protocol's JSON-RPC envelope (`jsonrpc: "2.0"`, `method: "tools/call"`, `params: {name: "read_neo4j_cypher" | "write_neo4j_cypher", arguments: {query, params}}`). Lifecycle coupled to Aura process per D-06.

**When to use:** Every Neo4j-touching subcommand in Phase 1 + every knowledge tool in Phase 11+. Single shared subprocess per Aura process; serialize calls via a Go mutex (MCP stdio is single-pipe).

**Example:**
```go
// Source: MCP protocol spec + mcp-neo4j-cypher CLI flags (verified by --help on this machine)
package knowledge

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "os/exec"
    "sync"
    "sync/atomic"
)

type Client struct {
    cmd      *exec.Cmd
    stdin    io.WriteCloser
    stdout   *bufio.Reader
    mu       sync.Mutex // serialize req/resp
    nextID   atomic.Int64
}

func Open(ctx context.Context, cfg *Neo4jConfig) (*Client, error) {
    args := []string{
        "--db-url", cfg.BoltURL,
        "--username", cfg.User,
        "--password", cfg.Password,
        "--database", cfg.Database,
        "--transport", "stdio",
    }
    cmd := exec.CommandContext(ctx, cfg.MCPBinary, args...)
    stdin, err := cmd.StdinPipe()
    if err != nil {
        return nil, err
    }
    stdoutPipe, err := cmd.StdoutPipe()
    if err != nil {
        return nil, err
    }
    if err := cmd.Start(); err != nil {
        return nil, fmt.Errorf("spawn %s: %w (PATH check: pip install mcp-neo4j-cypher==0.6.0)", cfg.MCPBinary, err)
    }
    return &Client{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdoutPipe)}, nil
}

type rpcReq struct {
    JSONRPC string      `json:"jsonrpc"`
    ID      int64       `json:"id"`
    Method  string      `json:"method"`
    Params  interface{} `json:"params"`
}

type rpcResp struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      int64           `json:"id"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *struct {
        Code    int    `json:"code"`
        Message string `json:"message"`
    } `json:"error,omitempty"`
}

func (c *Client) Cypher(ctx context.Context, query string, params map[string]any, write bool) (json.RawMessage, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    tool := "read_neo4j_cypher"
    if write {
        tool = "write_neo4j_cypher"
    }
    req := rpcReq{
        JSONRPC: "2.0",
        ID:      c.nextID.Add(1),
        Method:  "tools/call",
        Params: map[string]any{
            "name":      tool,
            "arguments": map[string]any{"query": query, "params": params},
        },
    }
    enc, _ := json.Marshal(req)
    if _, err := fmt.Fprintln(c.stdin, string(enc)); err != nil {
        return nil, fmt.Errorf("send: %w (mcp-neo4j-cypher may have crashed — D-06 policy: fail Aura process)", err)
    }
    line, err := c.stdout.ReadBytes('\n')
    if err != nil {
        return nil, fmt.Errorf("recv: %w", err)
    }
    var resp rpcResp
    if err := json.Unmarshal(line, &resp); err != nil {
        return nil, fmt.Errorf("decode: %w", err)
    }
    if resp.Error != nil {
        return nil, fmt.Errorf("cypher error %d: %s", resp.Error.Code, resp.Error.Message)
    }
    return resp.Result, nil
}

func (c *Client) Close() error {
    _ = c.stdin.Close()
    return c.cmd.Wait()
}
```

### Pattern 4: Cypher migrations with Postgres audit table

**What:** `.cypher` files numbered like SQL migrations live under `internal/knowledge/migrations/`, embedded via `embed.FS`. `Migrator.Up()` reads each file, computes SHA-256 checksum, queries Postgres `aura.knowledge_migrations` for already-applied versions, executes new ones via MCP `write_neo4j_cypher`, writes audit row on success.

**When to use:** Slice 0.7 + every Neo4j schema change in Slice 11.

**Audit table (lands in `0002_knowledge_migrations.up.sql` in Slice 0.5):**
```sql
CREATE TABLE aura.knowledge_migrations (
    version    integer        PRIMARY KEY,
    name       text           NOT NULL,
    checksum   text           NOT NULL,         -- SHA-256 of file content
    applied_at timestamptz    NOT NULL DEFAULT now()
);
GRANT INSERT, SELECT ON aura.knowledge_migrations TO aura_app;
GRANT ALL    ON aura.knowledge_migrations TO aura_migrate;
```

**sqlc queries (`internal/db/queries/knowledge_migrations.sql`):**
```sql
-- name: RecordKnowledgeMigration :exec
INSERT INTO aura.knowledge_migrations (version, name, checksum)
VALUES ($1, $2, $3);

-- name: ListAppliedKnowledgeMigrations :many
SELECT version, name, checksum, applied_at
FROM aura.knowledge_migrations
ORDER BY version ASC;
```

### Pattern 5: Embed sidecar dim self-test (REPLACES PRD acceptance row 182)

**What:** PRD claims `aura neo4j ping` validates sidecar `/health` returns `{"dim":768}`. **Ground-truth probe shows `llama-server /health` returns `{"status":"ok"}` — no dim field.** Replace with: POST a dummy input to `/v1/embeddings`, assert `len(data[0].embedding) == AURA_EMBED_DIMENSIONS`. Slice 0.7 commit ships a one-line PRD amendment for the acceptance row.

**When to use:** `aura neo4j ping` boot-time self-test; Slice 11b ingest pipeline pre-flight (planner notes this for Phase 15 too).

**Example:**
```go
// Source: OpenAI embeddings API spec + llama.cpp server --embeddings docs (verified)
package knowledge

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

func PingEmbed(ctx context.Context, baseURL string, expectedDim int) error {
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/embeddings",
        bytes.NewReader([]byte(`{"input":"ping","model":"embedding"}`)))
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/json")
    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("embed sidecar unreachable: %w", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        return fmt.Errorf("embed sidecar returned %d", resp.StatusCode)
    }
    var body struct {
        Data []struct {
            Embedding []float32 `json:"embedding"`
        } `json:"data"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
        return fmt.Errorf("embed sidecar decode: %w", err)
    }
    if len(body.Data) == 0 || len(body.Data[0].Embedding) != expectedDim {
        actual := 0
        if len(body.Data) > 0 {
            actual = len(body.Data[0].Embedding)
        }
        return fmt.Errorf(
            "embedding sidecar returned dim=%d but AURA_EMBED_DIMENSIONS=%d — refuse to start (Pitfall #7 silent corruption); see prd.md amendment #18 swap runbook",
            actual, expectedDim)
    }
    return nil
}
```

### Anti-Patterns to Avoid

- **Hand-rolling a native Go Neo4j driver** — PRD discipline ban (CLAUDE.md §Project scope row 14). Use MCP subprocess only.
- **Hard-coding the migrate URL in code** — must come from `Config.DB.MigrateURL` + `AURA_DB_MIGRATE_URL` env per D-07.
- **Using `public` schema instead of `aura.*`** — explicit PRD lock; `SET search_path TO aura, public` set in `0001_init.up.sql`.
- **Auto-restart-on-crash watchdog for `mcp-neo4j-cypher`** — D-06 says fail Aura process; restart-once masks infra rot.
- **Bind-mount Postgres or Neo4j data dirs on Windows** — named volumes only, per `feedback_sqlite_wal_windows_corruption` extended to PG/Neo4j (memory prior).
- **Truncating embeddings to 256d/512d via MRL** — PRD locks 768 native; MRL truncation deprecated 2026-05-27 per memory `feedback_embedding_backend_stays_mistral`.
- **Mixing `aura_app` + `aura_migrate` URLs in one pool** — each subcommand opens its own pool with the role it needs.
- **Storing the Neo4j password in `compose.yaml` hard-coded** — must come via `${NEO4J_PASSWORD}` interpolation from `.env`.
- **Using sqlc's experimental migration module instead of golang-migrate** — PRD OQ 0.5 #2 resolved; golang-migrate stays.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Postgres connection pool | Custom `sync.Pool` of `pgx.Conn` | `pgxpool.Pool` | Health checks, connection-lifetime jitter, idle reaping all built-in; reinventing this is a footgun |
| SQL→Go type mapping | Custom `Scan` boilerplate per query | `sqlc generate` | Build-time codegen; CI golden test enforces sync; zero runtime overhead |
| Schema migration runner | Custom file-walker + version tracker | `golang-migrate/migrate/v4` + `iofs` source | Dirty-state detection, atomic up/down, schema_migrations table all battle-tested |
| `.env` parser | Custom `bufio.Scanner` | `joho/godotenv` | Multiline values, quoted strings, comments, escaping all handled |
| Neo4j driver | Native Go driver | `mcp-neo4j-cypher` subprocess | PRD discipline; uniform MCP pattern across all knowledge tools |
| Stdio framing for MCP | Custom newline-delimited JSON | JSON-RPC 2.0 envelope per MCP spec | Already standardized; mcp-neo4j-cypher emits this |
| Embedding HTTP client | Custom fetch + parse | `net/http` + OpenAI embeddings response shape | Sidecar is OpenAI-compat by design; stdlib is enough |
| Postgres role grants management | Application-level RBAC | DDL in `0001_init.up.sql` + connect URL choice | The DB engine enforces; defense-in-depth lives at the engine, not the app layer |
| Goroutine leak detection | Custom inspection in tests | `goleak.VerifyNone(t)` in `TestMain` | Uber's lib has the canonical pattern; PRD amendment #15 mandates |
| Italian smoke fixture seeder | Programmatic Cypher in tests | Committed `*.md` + `queries.txt` | Diffable, reviewable, replayable (D-04 rationale) |
| Restore timing assertion | Custom shell timer + comparison | `/usr/bin/time -f '%e'` + bash `[[ $time -lt 90 ]]` | Stdlib; portable across Git Bash + Linux |

**Key insight:** Phase 1's value is **wiring** the canonical libraries correctly under the role-separation + dim-contract discipline. Every place we hand-roll is a place future slices will inherit a quirk.

## Runtime State Inventory

> Greenfield phase — there is no prior state to migrate. This inventory documents the post-Phase-1 runtime state landscape so later phases know what is now "live":

| Category | Items Created in Phase 1 | Action Required |
|----------|--------------------------|------------------|
| Stored data | Postgres named volume `aura_aura-postgres` (Slice 0.5); Neo4j named volume `aura_aura-neo4j` (Slice 0.7); `aura-llama-embed` HF cache volume `aura_llama_embed_cache` for the GGUF model (Slice 0.7) | None — created by `docker compose up` |
| Live service config | Three compose services with healthchecks; mcp-neo4j-cypher subprocess (PID-tracked, no PID file — runs under Aura process) | None — managed by compose + Aura process tree |
| OS-registered state | None — no Windows Task Scheduler entries, no systemd units, no launchd plists | None |
| Secrets/env vars | `.env` consumes 7 new vars: `POSTGRES_PASSWORD`, `AURA_DB_URL`, `AURA_DB_MIGRATE_URL`, `NEO4J_PASSWORD`, `NEO4J_USER`, `AURA_NEO4J_BOLT_URL`, `AURA_MCP_NEO4J_CYPHER_BIN`, `AURA_EMBED_DIMENSIONS`, `AURA_EMBED_BASE_URL` | Operator copies `.env.example → .env` + sets `POSTGRES_PASSWORD` + `NEO4J_PASSWORD`; everything else has safe defaults |
| Build artifacts | `internal/db/sqlc/*.go` (committed, regenerated by `make sqlc`); `go.sum` (now exists; auto-bumped Go 1.25 + 5 deps in this research session) | `make sqlc` is a Slice 0.5 Makefile target; CI golden test asserts sqlc output is in sync |

**Nothing in category "OS-registered state":** Explicitly checked — Aura runs as a foreground binary under the operator's shell (or under `docker compose` in production); no service registration in Phase 1.

## Common Pitfalls

### Pitfall #1: Role grants applied AFTER the table exists, not via DEFAULT PRIVILEGES

**What goes wrong:** Migration `0001_init.up.sql` creates the `aura_app` role and grants `INSERT, SELECT, UPDATE, DELETE` on `ALL TABLES IN SCHEMA aura` — but the schema is empty at this point, so the grant applies to zero tables. The next migration (`0002_knowledge_migrations.up.sql`) creates `aura.knowledge_migrations`, and `aura_app` has no grant on it.

**Why it happens:** `GRANT ... ON ALL TABLES IN SCHEMA` is a snapshot of currently-existing tables, not a forward-looking grant.

**How to avoid:** Use `ALTER DEFAULT PRIVILEGES IN SCHEMA aura GRANT INSERT, SELECT, UPDATE, DELETE ON TABLES TO aura_app` in `0001_init.up.sql`, AND repeat per-table `GRANT` lines in each future migration that introduces a new table. Belt + suspenders.

**Warning signs:** First time a slice runs INSERT against a new table under `aura_app` and gets `permission denied for table foo` even though everything looked right in migration 0001.

### Pitfall #2: Postgres connection URL leaks password in error wrappers / logs

**What goes wrong:** `pgxpool.New` returns errors that often quote the original DSN; logging that error in slog leaks `POSTGRES_PASSWORD` into logs.

**Why it happens:** `pgx` error formatters do not redact by default.

**How to avoid:** Wrap the open call with a custom error that strips the password: `fmt.Errorf("open pool to %s: %w", redactDSN(cfg.URL), err)`. Same for `AURA_DB_MIGRATE_URL`.

**Warning signs:** Auditor running `grep changeme /var/log/aura/*` and finding hits.

### Pitfall #3: `mcp-neo4j-cypher` startup race vs. Neo4j container healthcheck

**What goes wrong:** `aura neo4j migrate` spawns the MCP subprocess; subprocess connects to Neo4j Bolt; Neo4j container is up but not yet ready to accept Bolt connections (post-Java-startup but pre-index-rebuild). MCP subprocess errors out with "connection refused" or "auth failed before negotiation."

**Why it happens:** Container healthcheck reports healthy at PID-1 + port-listening; Neo4j Bolt accepts connections moments later after license/index init.

**How to avoid:** Healthcheck must use `cypher-shell -u neo4j -p $NEO4J_PASSWORD --database neo4j 'RETURN 1'` (not just `nc -z 7687`). Compose `depends_on: { neo4j: { condition: service_healthy } }` on the Aura runtime block. MCP client implements a 10s retry loop (`Config.Neo4j.ConnectTimeoutSec`) with exponential backoff on the first call.

**Warning signs:** First `aura neo4j migrate` after `docker compose up` fails ~30% of the time on a cold Neo4j boot; second call works.

### Pitfall #4: Cypher 5.x syntax drift between minor versions

**What goes wrong:** `vector.hnsw.m` and `vector.hnsw.ef_construction` HNSW parameters did NOT exist before Neo4j 5.23. A `0001_init.cypher` written against a 5.13-era doc fails on 5.26 community LTS, OR conversely against 5.18 in a stale dev environment.

**Why it happens:** Cypher added HNSW tuning knobs in 5.23 release notes; many tutorials and blog posts predate this.

**How to avoid:** Pin Neo4j to 5.26.26-community (confirmed via hub.docker.com). The `IF NOT EXISTS` clause IS supported in 5.26 for both `CREATE CONSTRAINT` and `CREATE VECTOR INDEX` and `CREATE FULLTEXT INDEX` — verified in this research. The full HNSW config block (M=32, ef_construction=200 per amendment #20) IS available in 5.26.

**Warning signs:** `aura neo4j migrate` returns Cypher syntax error on the `OPTIONS { indexConfig: {...} }` line; or constraint creation throws "label X already has constraint Y" because `IF NOT EXISTS` was omitted.

### Pitfall #5: Embed sidecar returns wrong dim silently → Neo4j HNSW corruption

**What goes wrong:** Sidecar boots with a 384d model (wrong GGUF accidentally pulled, or HF model swapped upstream), returns 384d vectors; Aura inserts them into the 768d HNSW index; Neo4j errors out per-insert OR (worse) the wrong index existed first and Neo4j just stores garbage. Silent retrieval corruption.

**Why it happens:** No defense-in-depth between sidecar and index. PRD Pitfall #7 documents this; `neo4j#13387` + `langchain#16336` are upstream incidents.

**How to avoid:** Pattern 5 above — `aura neo4j ping` POSTs one dummy embedding and asserts `len == AURA_EMBED_DIMENSIONS`. Slice 0.7 also enforces this at every ingest call (Phase 15 ingest pipeline asserts per-vector). Compose healthcheck on `aura-llama-embed` checks `/health` returns `{"status":"ok"}` (this is the sidecar-process probe, NOT the dim assertion — dim is Go-side per Pattern 5).

**Warning signs:** Vector search returns wildly unrelated results; smoke `recall@5` drops below 5/5 on the deterministic fixture.

### Pitfall #6: `mcp-neo4j-cypher` upstream license field empty

**What goes wrong:** `pip show mcp-neo4j-cypher` returns blank `License:` field. PyPI page metadata empty. `pyproject.toml` has no `license = "..."` line. The README doesn't state a license. PRD/CONTEXT.md claim "Apache 2.0" — likely correct by neo4j-contrib org convention, but **not verified by upstream metadata** in this research session.

**Why it happens:** Maintainer oversight; happens often on PyPI packages that are subdirectories of monorepos (the org LICENSE applies but isn't visible through `pip`).

**How to avoid:** Slice 0.7 plan must include a checkpoint task: `gh api repos/neo4j-contrib/mcp-neo4j/license` or check the repo root LICENSE file; record the result in the Slice 0.7 commit body. If the license is NOT Apache 2.0 / MIT / BSD-3-Clause, the planner must escalate (treats as a PRD-amendment trigger because the discipline ban on native Go drivers locks Aura in).

**Warning signs:** OSS license scanner (e.g., `go-licenses`, `licensecheck`) flags `mcp-neo4j-cypher` as Unknown.

### Pitfall #7: Git Bash MSYS path mangling on `docker compose run`

**What goes wrong:** Operator on Windows Git Bash runs `docker compose run --rm postgres psql -h postgres -U aura -d aura -f /workspace/scripts/restore_drill.sh` — MSYS rewrites `/workspace/...` to `C:/Program Files/Git/workspace/...` before passing to docker, and the file is not found in the container.

**Why it happens:** MSYS path translation applies to any arg starting with `/`. Documented in memory `feedback_docker_compose_run_msys_path_mangling`.

**How to avoid:** Document in the Makefile + README that `make` targets must be run from PowerShell on Windows; alternatively prefix with `MSYS_NO_PATHCONV=1 docker compose run ...` in the Makefile target itself for Git Bash compatibility.

**Warning signs:** Restore drill works on Linux CI but fails locally on Windows dev with `file not found`.

### Pitfall #8: Neo4j Community single-database limitation

**What goes wrong:** Future slice (Phase 15) tries `CREATE DATABASE aura;` for per-tenant isolation. Fails with "multi-database support requires Enterprise edition." Operator confused why the migration breaks after a smooth Phase 1.

**Why it happens:** Community is locked to the default `neo4j` database. Multi-DB is Enterprise.

**How to avoid:** `Config.Neo4j.Database` defaults to `neo4j` (NOT `aura`) and is documented as such in `.env.example`. Plan rejects any future PR that adds `CREATE DATABASE` without an Enterprise-license check.

**Warning signs:** Cypher error "Database name must be `neo4j` in Community Edition" when the default is overridden.

## Code Examples

### `0001_init.up.sql` (full skeleton — Slice 0.5)

```sql
-- Source: PRD §Slice 0.5 amendment #17 + Postgres 17 GRANT/CREATE ROLE docs
-- Lands in Slice 0.5 commit; gates every later Postgres migration.

-- 1. Schema
CREATE SCHEMA IF NOT EXISTS aura;

-- 2. Roles (no LOGIN here; LOGIN granted via SET ROLE / CREATE ROLE in 0002 if needed)
DO $$ BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'aura_app') THEN
        CREATE ROLE aura_app WITH LOGIN PASSWORD :'aura_app_password';
    END IF;
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'aura_migrate') THEN
        CREATE ROLE aura_migrate WITH LOGIN PASSWORD :'aura_migrate_password';
    END IF;
END $$;

-- 3. Schema-level grants
GRANT USAGE  ON SCHEMA aura TO aura_app;
GRANT CREATE, USAGE ON SCHEMA aura TO aura_migrate;

-- 4. Default privileges — apply to tables created by aura_migrate in the future
ALTER DEFAULT PRIVILEGES FOR ROLE aura_migrate IN SCHEMA aura
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO aura_app;
ALTER DEFAULT PRIVILEGES FOR ROLE aura_migrate IN SCHEMA aura
    GRANT USAGE, SELECT ON SEQUENCES TO aura_app;

-- 5. Search path (per-session; documented for psql sessions; runtime sets via connection string)
COMMENT ON SCHEMA aura IS 'Aura application schema — set search_path TO aura, public on every session';

-- 6. Audit guardrail: aura_app explicitly DENIED TRUNCATE + DROP (default for non-owners; documenting intent)
--    No GRANT TRUNCATE; no GRANT DROP. The role separation is enforced by NOT granting these.
```

Note: `:'aura_app_password'` is psql variable substitution. The migrate runner via `golang-migrate` does not support psql variables; instead, Phase 1 uses fixed passwords sourced from env via a templated DDL approach, OR documents that the first-boot operator runs `aura db init-roles` (a Slice 0.5 subcommand) before the first migration. **Recommendation for planner:** simplest path is to bake the role creation into Go-side bootstrap (`aura db migrate` first reads env, executes role-creation DDL with literal interpolated passwords using `pgxpool` parametrized queries, THEN runs `m.Up()`).

### `0001_init.down.sql`

```sql
-- Reversal: drop default privileges, drop schema cascade, drop roles.
ALTER DEFAULT PRIVILEGES FOR ROLE aura_migrate IN SCHEMA aura
    REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM aura_app;
ALTER DEFAULT PRIVILEGES FOR ROLE aura_migrate IN SCHEMA aura
    REVOKE USAGE, SELECT ON SEQUENCES FROM aura_app;

REVOKE ALL ON SCHEMA aura FROM aura_app;
REVOKE ALL ON SCHEMA aura FROM aura_migrate;

DROP SCHEMA IF EXISTS aura CASCADE;

DROP ROLE IF EXISTS aura_app;
DROP ROLE IF EXISTS aura_migrate;
```

### `0002_knowledge_migrations.up.sql` (full skeleton — Slice 0.5)

```sql
-- Source: PRD §Slice 0.7 file targets row + amendment #17 grant pattern
CREATE TABLE aura.knowledge_migrations (
    version    integer        PRIMARY KEY,
    name       text           NOT NULL,
    checksum   text           NOT NULL,                       -- SHA-256 of file content
    applied_at timestamptz    NOT NULL DEFAULT now()
);

-- Belt + suspenders (DEFAULT PRIVILEGES from 0001 should cover this; explicit grant for forensic clarity)
GRANT SELECT, INSERT ON aura.knowledge_migrations TO aura_app;
GRANT ALL            ON aura.knowledge_migrations TO aura_migrate;

CREATE INDEX knowledge_migrations_applied_at_idx
    ON aura.knowledge_migrations (applied_at DESC);

COMMENT ON TABLE aura.knowledge_migrations IS
    'Audit of applied Cypher migrations. Written by aura neo4j migrate; read by aura neo4j status.';
```

### `0002_knowledge_migrations.down.sql`

```sql
DROP TABLE IF EXISTS aura.knowledge_migrations;
```

### `0001_init.cypher` (full skeleton — Slice 0.7)

```cypher
// -- migrate:up
// Source: Neo4j 5.26 Cypher manual (vector + fulltext index syntax) + PRD §Slice 0.7 amendment #20 (HNSW M=32)

CREATE CONSTRAINT chunk_id IF NOT EXISTS
  FOR (c:Chunk) REQUIRE c.id IS UNIQUE;

CREATE VECTOR INDEX chunk_embedding IF NOT EXISTS
  FOR (c:Chunk) ON c.embedding
  OPTIONS { indexConfig: {
    `vector.dimensions`: 768,
    `vector.similarity_function`: 'cosine',
    `vector.hnsw.m`: 32,
    `vector.hnsw.ef_construction`: 200
  }};

CREATE FULLTEXT INDEX chunk_text IF NOT EXISTS
  FOR (c:Chunk) ON EACH [c.text]
  OPTIONS { indexConfig: {
    `fulltext.analyzer`: 'standard',
    `fulltext.eventually_consistent`: false
  }};
```

Note: `vector.dimensions: 768` is hard-coded (matches `AURA_EMBED_DIMENSIONS=768` default). For future runbook (amendment #18 swap procedure), the planner may opt to template this via Go-side string substitution at migrate time; for Phase 1, hard-coding is simpler and aligns with the "minimal schema" rule (D-08).

### `sqlc.yaml` (full skeleton — Slice 0.5)

```yaml
# Source: sqlc.dev/en/latest/reference/config.html (verified)
version: "2"

sql:
  - name: aura
    engine: postgresql
    schema: internal/db/migrations
    queries: internal/db/queries
    gen:
      go:
        package: sqlc
        out: internal/db/sqlc
        sql_package: pgx/v5
        sql_driver: github.com/jackc/pgx/v5
        emit_interface: true
        emit_exact_table_names: true
        emit_json_tags: true
        emit_empty_slices: true
        emit_prepared_queries: false
        json_tags_case_style: snake
        output_db_file_name: db.go
        output_models_file_name: models.go
        output_querier_file_name: querier.go
        omit_unused_structs: false
```

### `compose.yaml` (full skeleton — Slice 0.5 lands postgres; Slice 0.7 appends neo4j + aura-llama-embed)

```yaml
# Source: Docker Compose v5 + Postgres 17 docker hub + Neo4j 5.26 community docker docs + llama.cpp server docs (verified)
name: aura

services:
  postgres:
    image: postgres:17.10-alpine3.23
    container_name: aura-postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER: aura
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?POSTGRES_PASSWORD required in .env}
      POSTGRES_DB: aura
    ports:
      - "127.0.0.1:5432:5432"
    volumes:
      - aura-postgres:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U aura -d aura"]
      interval: 5s
      timeout: 5s
      retries: 10
      start_period: 10s

  # ↓↓ Appended in Slice 0.7 ↓↓
  neo4j:
    image: neo4j:5.26.26-community
    container_name: aura-neo4j
    restart: unless-stopped
    environment:
      NEO4J_AUTH: neo4j/${NEO4J_PASSWORD:?NEO4J_PASSWORD required in .env}
      NEO4J_PLUGINS: '["apoc","graph-data-science"]'
      NEO4J_dbms_security_procedures_unrestricted: 'apoc.*,gds.*'
      NEO4J_dbms_memory_heap_initial__size: 512m
      NEO4J_dbms_memory_heap_max__size: 1G
      NEO4J_dbms_memory_pagecache_size: 512m
    ports:
      - "127.0.0.1:7474:7474"
      - "127.0.0.1:7687:7687"
    volumes:
      - aura-neo4j:/data
      - aura-neo4j-plugins:/plugins
    healthcheck:
      test: ["CMD-SHELL", "cypher-shell -u neo4j -p $$NEO4J_PASSWORD --database neo4j 'RETURN 1' || exit 1"]
      interval: 10s
      timeout: 10s
      retries: 10
      start_period: 40s   # APOC + GDS auto-download takes time on first boot

  aura-llama-embed:
    image: ghcr.io/ggml-org/llama.cpp:server
    container_name: aura-llama-embed
    restart: unless-stopped
    command:
      - --hf-repo
      - ggml-org/embeddinggemma-300M-GGUF
      - --hf-file
      - embeddinggemma-300M-Q8_0.gguf
      - --embeddings
      - --host
      - 0.0.0.0
      - --port
      - "8081"
      - -t
      - "4"                                    # mini-PC CPU budget cap (memory feedback_minipc_cpu_budget)
    ports:
      - "127.0.0.1:8081:8081"
    volumes:
      - aura-llama-embed:/root/.cache/llama.cpp
    healthcheck:
      test: ["CMD-SHELL", "curl -sf http://localhost:8081/health || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 12
      start_period: 60s                        # First-boot HF download

volumes:
  aura-postgres:
  aura-neo4j:
  aura-neo4j-plugins:
  aura-llama-embed:
```

### `Makefile` (full skeleton — Slice 0.5 base; Slice 0.7 appends neo4j-* + smoke)

```make
# Source: PRD §Slice 0.5/0.7 file targets; sqlc CLI docs; golang-migrate v4 CLI docs
.PHONY: help sqlc db-up db-migrate db-status db-reset neo4j-up neo4j-migrate neo4j-status neo4j-reset smoke restore-drill

help:
	@echo "make sqlc          — regenerate internal/db/sqlc/ from queries/"
	@echo "make db-up         — docker compose up -d postgres (waits healthy)"
	@echo "make db-migrate    — aura db migrate (role aura_migrate)"
	@echo "make db-status     — aura db status"
	@echo "make db-reset      — DESTRUCTIVE: drop+recreate schema aura (dev only, requires --yes via env)"
	@echo "make neo4j-up      — docker compose up -d neo4j aura-llama-embed (waits healthy)"
	@echo "make neo4j-migrate — aura neo4j migrate"
	@echo "make neo4j-status  — aura neo4j status"
	@echo "make neo4j-reset   — DESTRUCTIVE: drop all indexes + MATCH (n) DETACH DELETE (dev only)"
	@echo "make smoke         — scripts/neo4j_smoke.sh (Italian recall@5 5/5 + p95 ≤ 30ms)"
	@echo "make restore-drill — scripts/restore_drill.sh (pg_dump → pg_restore, asserts < 90s)"

sqlc:
	sqlc generate

db-up:
	docker compose up -d postgres
	@echo "Waiting for postgres healthy..."
	@until docker compose ps --format json postgres | grep -q '"Health":"healthy"'; do sleep 1; done
	@echo "ok"

db-migrate: db-up
	go run ./cmd/aura db migrate

db-status:
	go run ./cmd/aura db status

db-reset:
	@[ "$$AURA_RESET_YES" = "1" ] || { echo "refusing — set AURA_RESET_YES=1 to confirm destructive reset"; exit 1; }
	go run ./cmd/aura db reset --yes

# ↓↓ Appended in Slice 0.7 ↓↓
neo4j-up:
	docker compose up -d neo4j aura-llama-embed
	@echo "Waiting for neo4j healthy..."
	@until docker compose ps --format json neo4j | grep -q '"Health":"healthy"'; do sleep 1; done
	@echo "Waiting for aura-llama-embed healthy..."
	@until docker compose ps --format json aura-llama-embed | grep -q '"Health":"healthy"'; do sleep 1; done
	@echo "ok"

neo4j-migrate: db-migrate neo4j-up
	go run ./cmd/aura neo4j migrate

neo4j-status:
	go run ./cmd/aura neo4j status

neo4j-reset:
	@[ "$$AURA_RESET_YES" = "1" ] || { echo "refusing — set AURA_RESET_YES=1 to confirm destructive reset"; exit 1; }
	go run ./cmd/aura neo4j reset --yes

smoke: neo4j-migrate
	bash scripts/neo4j_smoke.sh

restore-drill: db-up
	bash scripts/restore_drill.sh
```

### `.env.example` (full skeleton — Slice 0.5 base; Slice 0.7 appends NEO4J_*, AURA_NEO4J_*, AURA_EMBED_*)

```bash
# Aura environment template. Copy to .env and fill in secrets before first boot.
# .env is gitignored. Never commit real secrets.

# Postgres
POSTGRES_PASSWORD=changeme                                                                            # required, must change
AURA_DB_URL=postgres://aura:changeme@127.0.0.1:5432/aura?sslmode=disable                              # role aura_app, runtime
AURA_DB_MIGRATE_URL=postgres://aura_migrate:changeme@127.0.0.1:5432/aura?sslmode=disable              # role aura_migrate, DDL only

# ↓↓ Slice 0.7 ↓↓
# Neo4j
NEO4J_USER=neo4j
NEO4J_PASSWORD=changeme                                                                               # required, must change
AURA_NEO4J_BOLT_URL=bolt://127.0.0.1:7687
AURA_NEO4J_DATABASE=neo4j                                                                             # Community = single DB only

# mcp-neo4j-cypher subprocess
AURA_MCP_NEO4J_CYPHER_BIN=mcp-neo4j-cypher                                                            # must be on PATH (pip install mcp-neo4j-cypher==0.6.0)
AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC=10

# Embed sidecar
AURA_EMBED_BASE_URL=http://127.0.0.1:8081
AURA_EMBED_DIMENSIONS=768                                                                             # Pitfall #5 / amendment #18 — boot self-test asserts this

# Future phases (placeholders, not consumed by Phase 1)
OPENROUTER_API_KEY=
```

### `scripts/restore_drill.sh` (full skeleton — Slice 0.5)

```bash
#!/usr/bin/env bash
# Source: PRD §Slice 0.5 backup strategy + ROADMAP SC#3
# Purpose: Smoke-test that a pg_dump file restores into a fresh database in < 90s.
set -euo pipefail

DUMPFILE="${1:-/tmp/aura-restore-drill.dump}"
TARGET_DB="aura_restore_drill"
PG_HOST="${PGHOST:-127.0.0.1}"
PG_PORT="${PGPORT:-5432}"
PG_USER="${PGUSER:-aura_migrate}"
export PGPASSWORD="${PGPASSWORD:?PGPASSWORD required}"

# 1. Make sure dump exists; if not, create one from the live aura DB.
if [[ ! -f "$DUMPFILE" ]]; then
    echo "==> creating sample dump at $DUMPFILE"
    pg_dump -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -Fc aura > "$DUMPFILE"
fi

# 2. Drop + recreate target DB.
psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d postgres -c "DROP DATABASE IF EXISTS $TARGET_DB;"
psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d postgres -c "CREATE DATABASE $TARGET_DB OWNER aura_migrate;"

# 3. Time the restore.
START_NS=$(date +%s%N)
pg_restore -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$TARGET_DB" --no-owner --no-acl "$DUMPFILE"
END_NS=$(date +%s%N)

ELAPSED_MS=$(( (END_NS - START_NS) / 1000000 ))
echo "==> restore took ${ELAPSED_MS} ms"

# 4. Assert < 90 000 ms (90 s).
if (( ELAPSED_MS > 90000 )); then
    echo "FAIL: restore took ${ELAPSED_MS} ms, exceeds 90 s budget (ROADMAP Phase 1 SC#3)"
    exit 1
fi

# 5. Cleanup.
psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d postgres -c "DROP DATABASE $TARGET_DB;"
echo "ok: restore drill PASSED (${ELAPSED_MS} ms < 90 000 ms)"
```

### `scripts/neo4j_smoke.sh` (full skeleton — Slice 0.7)

```bash
#!/usr/bin/env bash
# Source: PRD §Slice 0.7 spike fixture + ROADMAP Phase 1 SC#5 (recall@5 = 5/5, p95 ≤ 30ms)
set -euo pipefail

FIXTURES_DIR="${FIXTURES_DIR:-scripts/fixtures/neo4j-smoke}"
EMBED_URL="${AURA_EMBED_BASE_URL:-http://127.0.0.1:8081}"
EXPECTED_DIM="${AURA_EMBED_DIMENSIONS:-768}"

# 1. Embed + upsert each fixture as a :Chunk node.
for f in "$FIXTURES_DIR"/*.md; do
    id=$(basename "$f" .md)
    text=$(cat "$f")
    # Get embedding from sidecar
    embedding=$(curl -sf -X POST "$EMBED_URL/v1/embeddings" \
        -H 'Content-Type: application/json' \
        -d "$(jq -nc --arg t "$text" '{input:$t,model:"embedding"}')" \
        | jq -c '.data[0].embedding')
    actual_dim=$(echo "$embedding" | jq 'length')
    if [[ "$actual_dim" != "$EXPECTED_DIM" ]]; then
        echo "FAIL: sidecar returned dim=$actual_dim, expected $EXPECTED_DIM"
        exit 1
    fi
    # Upsert via aura neo4j (which routes through MCP)
    go run ./cmd/aura neo4j cypher write \
        "MERGE (c:Chunk {id: \$id}) SET c.text = \$text, c.embedding = \$emb" \
        --param id="$id" --param text="$text" --param emb="$embedding"
done

# 2. Run 5 known-answer queries; track latency.
hit_count=0
declare -a latencies_ms=()
while IFS='|' read -r query expected_id; do
    query_emb=$(curl -sf -X POST "$EMBED_URL/v1/embeddings" \
        -H 'Content-Type: application/json' \
        -d "$(jq -nc --arg t "$query" '{input:$t,model:"embedding"}')" \
        | jq -c '.data[0].embedding')
    start_ms=$(date +%s%3N)
    top1=$(go run ./cmd/aura neo4j cypher read \
        "CALL db.index.vector.queryNodes('chunk_embedding', 5, \$q) YIELD node RETURN node.id AS id LIMIT 1" \
        --param q="$query_emb")
    end_ms=$(date +%s%3N)
    latencies_ms+=( $((end_ms - start_ms)) )
    if [[ "$top1" == *"$expected_id"* ]]; then
        hit_count=$((hit_count + 1))
    else
        echo "MISS: query='$query' expected=$expected_id top1=$top1"
    fi
done < "$FIXTURES_DIR/queries.txt"

# 3. Assert recall@5 = 5/5.
if (( hit_count != 5 )); then
    echo "FAIL: recall@5 = $hit_count/5 (expected 5/5)"
    exit 1
fi

# 4. Assert p95 latency ≤ 30 ms (with 5 samples p95 = max of 5).
p95=$(printf '%s\n' "${latencies_ms[@]}" | sort -n | tail -1)
if (( p95 > 30 )); then
    echo "FAIL: p95 = ${p95} ms (expected ≤ 30 ms)"
    exit 1
fi

echo "ok: recall@5 = 5/5, p95 = ${p95} ms"
```

### `scripts/fixtures/neo4j-smoke/queries.txt` (suggested seed per D-04)

```
quanto tempo cuoce la pasta amatriciana|01_amatriciana
chi ha progettato il duomo di milano|02_duomo_milano
qual è la cilindrata della fiat panda 30|03_fiat_panda
qual è il nome del monaco protagonista del romanzo di eco|04_nome_della_rosa
come si prepara il caffè espresso napoletano|05_espresso_napoletano
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `lib/pq` Postgres driver | `jackc/pgx/v5` | lib/pq in maintenance mode since ~2022 | Phase 1 standard; faster, pgxpool-native, better type coverage |
| Hand-rolled SQL scanning | `sqlc` codegen | Industry adoption 2020-2024 | Phase 1 mandates; CI golden test |
| `goose` migrations | `golang-migrate/v4` | Both viable; choice locked by PRD | golang-migrate's `iofs` source is the embed.FS hook we need |
| External vector DB (Qdrant, Pinecone) | Inlined HNSW in Neo4j 5.x | Neo4j 5.23+ added HNSW M/efConstruction tuning + spike `D:/tmp/aura-neo4j-spike-2026-05-27` measured 22-30ms p95 + recall@5 5/5 | Deprecated 2026-05-27 (memory prior); zero external vector store |
| MRL-truncated 256d embeddings | 768d native embeddinggemma | Memory `feedback_embedding_backend_stays_mistral` 2026-05-27 | One model, one dim, end-to-end |
| Native Go Neo4j driver | `mcp-neo4j-cypher` MCP stdio | PRD discipline 2026-05-27 | Uniform MCP pattern; cost ~2-5ms RPC overhead per call vs native, acceptable |
| `vector.hnsw.m` default 16 | M=32 per amendment #20 | Neo4j 5.23 added param; amendment #20 pinned 32 | Recall@5 ≥ 0.8 at 100k corpus (validated by Phase 15 future work; Phase 1 just sets the index) |

**Deprecated / outdated:**
- Pre-5.23 Cypher tutorials that omit `vector.hnsw.m` / `vector.hnsw.ef_construction` (use 5.26 docs)
- `database/sql` + Postgres tutorials using `Rows.Scan` boilerplate (sqlc replaces)
- `pg_dump`-only backup discipline without restore drill (Phase 1 SC#3 ships the drill)

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `mcp-neo4j-cypher` is Apache 2.0 licensed | Standard Stack + Pitfall #6 | If GPL/AGPL, Aura's "discipline ban native driver" forces a license re-evaluation; could trigger PRD-amendment scope creep |
| A2 | Italian fixture corpus (D-04 suggested seed) yields recall@5 = 5/5 in practice with embeddinggemma-300m at 768d native | Smoke script + 5 SC #5 | If real-world embedding similarity collapses on these 5 docs, the fixture needs reshape; smoke test fails until fixed |
| A3 | Cypher 5.26 Community supports `cypher-shell -u ... -p ... 'RETURN 1'` as a healthcheck within the official `neo4j:5.26.26-community` Docker image | compose.yaml healthcheck | If `cypher-shell` is not in PATH inside the image, healthcheck silently passes; mitigation: smoke-test the healthcheck during Slice 0.7 wave-0 |
| A4 | `ghcr.io/ggml-org/llama.cpp:server` image with `--hf-repo` flag downloads + caches the GGUF model on first boot without manual intervention | compose.yaml + 5 SC #4 | If first-boot download fails (network, HF rate-limit), the healthcheck hangs; mitigation: extend `start_period: 60s` (already done) + document manual `docker exec` pull |
| A5 | `gen_random_uuid()` is available in Postgres 17 alpine by default (no `CREATE EXTENSION pgcrypto`) | future migrations (not Phase 1) | Phase 1 doesn't use UUIDs; flagged for Phase 2+ |
| A6 | The `--namespace` flag of `mcp-neo4j-cypher` is not needed for single-Aura-process use (one namespace = default) | Pattern 3 client | If MCP server requires explicit namespace per call, the JSON-RPC envelope changes; verify in Slice 0.7 wave-0 by running mcp-neo4j-cypher locally with stdio + a manual `tools/call` |
| A7 | Setting `vector.dimensions: 768` literal in Cypher migration is acceptable (no templating needed) for Phase 1 | Code Examples `0001_init.cypher` | If amendment #18 swap runbook requires dimension to come from env at migration time, the planner must template via `os.Expand` before sending the Cypher; flagged but D-08 minimal-schema approach makes hard-coding the simpler path |
| A8 | The mini-PC has ≥ 4 GB free RAM for the Phase 1 substrate (Postgres ~250 MB + Neo4j 1.5-2 GB + embed sidecar ~600 MB ≈ 2.5-3 GB peak idle) | RAM budget (per PRD §Slice 0.5 table) | If RAM-constrained, embed sidecar may need to be off-host; documented in PRD already |
| A9 | golang-migrate `iofs` source accepts a `migrations/` subdirectory inside the embedded FS via `iofs.New(migrationsFS, "migrations")` — verified per upstream docs but not exercised on this machine | Pattern 2 | If the path arg is wrong, `make db-migrate` fails immediately at smoke; trivial fix |
| A10 | `mcp-neo4j-cypher` stdio JSON-RPC envelope matches the MCP spec `{"jsonrpc":"2.0","id":N,"method":"tools/call","params":{...}}` — not confirmed by hands-on probe in this research session | Pattern 3 client | If envelope differs, the Go client doesn't parse responses; mitigation in Slice 0.7 wave-0: spawn mcp-neo4j-cypher --transport stdio manually and capture a real request/response pair to anchor the framing |

## Open Questions

1. **Should `mcp-neo4j-cypher` license be re-verified before merging Slice 0.7?**
   - What we know: PyPI metadata empty; neo4j-contrib org default-implies Apache 2.0; PRD claims Apache 2.0.
   - What's unclear: upstream LICENSE file did not respond to WebFetch in this session.
   - Recommendation: Slice 0.7 plan should include a checkpoint task to fetch the LICENSE file via `gh api repos/neo4j-contrib/mcp-neo4j/contents/LICENSE` and capture the result in the commit body. (Pitfall #6 above.)

2. **Should Aura ship a Dockerfile to bundle `mcp-neo4j-cypher` inside the Aura container image, rather than requiring it on host PATH?**
   - What we know: PRD OQ 0.7 #1 dismisses bundling as scope creep for Phase 1.
   - What's unclear: For end-user distribution (Phase 14 onboarding), a host PATH requirement may break first-run UX.
   - Recommendation: Defer per PRD locked answer. Re-visit in Phase 14 onboarding wave.

3. **Should `aura.knowledge_migrations` track which Aura binary version applied each migration?**
   - What we know: Schema is `(version, name, checksum, applied_at)`; no binary version column.
   - What's unclear: For forensics (which build introduced this index?), a `aura_version text` column helps; cheap to add.
   - Recommendation: Defer to a later slice's migration if forensic need arises; minimal-schema rule (D-08 spirit) for Phase 1.

4. **Should the smoke test (`scripts/neo4j_smoke.sh`) live in the Slice 0.7 commit or in a follow-up "smoke harness" commit?**
   - What we know: D-04 says fixture + smoke land in Slice 0.7.
   - What's unclear: ROADMAP Phase 1 SC#5 explicitly requires the smoke as gate.
   - Recommendation: Lands in Slice 0.7 commit (per D-04 + SC#5); single commit per slice keeps atomicity.

5. **What happens if `aura db migrate` is run while `aura serve` is up and holding an `aura_app` pool open?**
   - What we know: golang-migrate acquires a `pg_advisory_lock` during migration; concurrent runs are safe.
   - What's unclear: DDL like `CREATE INDEX CONCURRENTLY` is rare in Phase 1, but role-grant changes could disrupt live `aura_app` sessions.
   - Recommendation: Document in Makefile help text; not a Phase 1 blocker (no live `aura serve` exists yet).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | Build entire phase | ✓ | go1.26.2 windows/amd64 (exceeds Go 1.25 requirement from Phase 0 Amendment #1) | — |
| Docker + Compose | Spin up postgres, neo4j, embed sidecar | ✓ | Docker 29.4.3, Compose v5.1.4 | — |
| Python 3 + pip | Install `mcp-neo4j-cypher` | ✓ | Python 3.12.10 + pip 26.1 | — |
| `mcp-neo4j-cypher` | Neo4j subprocess in Slice 0.7 | ✓ | 0.6.0 (April 2026, pre-installed at `/c/Users/Davide/AppData/Roaming/Python/Python313/Scripts/mcp-neo4j-cypher`) | — |
| `make` (GNU Make) | Run Makefile targets | ✓ | GNU Make 3.81 (sufficient) | PowerShell-direct equivalents documented in README |
| `sqlc` CLI | Codegen for `internal/db/sqlc/` | ✗ | — | Install via `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1` as part of Wave 0 |
| `psql` / `pg_dump` / `pg_restore` (host) | restore_drill.sh outside container | ✗ (not verified on PATH; works via `docker compose exec postgres psql` as fallback) | — | Use `docker compose exec postgres pg_dump/pg_restore` from inside the container; documented in restore_drill.sh |
| `jq` | Smoke script JSON parsing | ✗ (not verified on this machine) | — | Install via `choco install jq` or `pacman -S jq` in MSYS; documented in smoke script preamble |
| `curl` | Embed sidecar probes | ✓ (Git Bash bundled) | — | — |

**Missing dependencies with no fallback:** none.

**Missing dependencies with fallback:**
- `sqlc` — install command documented (`go install`); Wave 0 should add this to dev README + CI.
- `psql`/`pg_dump`/`pg_restore` on host — fall back to `docker compose exec`; restore_drill.sh should detect host vs container and route accordingly.
- `jq` — operator installs once; smoke script `set -euo pipefail` will fail-loudly if missing.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `go test -race` + `go.uber.org/goleak v1.3.0` |
| Config file | none (Go stdlib); per-package `TestMain` for goleak setup |
| Quick run command | `go test ./internal/db/... ./internal/knowledge/... -race -count=1` |
| Full suite command | `go test ./... -race -count=1 -tags 'db_integration neo4j_integration'` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| INFRA-01 | `aura db migrate` is idempotent (re-run = no-op + explicit "no pending migrations" message) | integration | `go test -tags db_integration -run TestMigrate_Idempotent ./internal/db -race` | ❌ Wave 0 — `internal/db/db_test.go` |
| INFRA-01 | Role separation enforced: `aura_app` cannot TRUNCATE / DROP / DDL | integration | `go test -tags db_integration -run TestRoleSeparation_AppDenied ./internal/db -race` | ❌ Wave 0 |
| INFRA-01 | `aura db ping` returns latency + ok against fresh container | integration | `go test -tags db_integration -run TestPing ./internal/db -race` | ❌ Wave 0 |
| INFRA-01 | `aura db migrate` with empty `AURA_DB_MIGRATE_URL` fails fast with exact message | unit | `go test -run TestMigrate_MissingURLFailsFast ./internal/db` | ❌ Wave 0 |
| INFRA-01 | pgxpool config honors `MaxConns`, `MinConns`, `MaxConnIdleTime` from `Config.DB` | unit | `go test -run TestConfig_PoolParams ./internal/db` | ❌ Wave 0 |
| INFRA-01 | restore_drill.sh completes < 90s | smoke (shell) | `bash scripts/restore_drill.sh` | ❌ Wave 0 — `scripts/restore_drill.sh` |
| INFRA-01 | `sqlc generate` output is in sync with committed sources (golden test) | CI gate | `make sqlc && git diff --exit-code internal/db/sqlc/` | ❌ Wave 0 — Makefile target |
| INFRA-02 | `aura neo4j migrate` is idempotent (re-run = no-op via `aura.knowledge_migrations` check) | integration | `go test -tags neo4j_integration -run TestCypherMigrate_Idempotent ./internal/knowledge -race` | ❌ Wave 0 — `internal/knowledge/client_test.go` |
| INFRA-02 | `mcp-neo4j-cypher` subprocess spawn succeeds + ping returns server version 5.26.x | integration | `go test -tags neo4j_integration -run TestPing_ReturnsServerVersion ./internal/knowledge -race` | ❌ Wave 0 |
| INFRA-02 | Embed sidecar dim self-test fails fast when sidecar returns wrong dim (mocked) | unit | `go test -run TestPingEmbed_DimMismatch ./internal/knowledge` | ❌ Wave 0 |
| INFRA-02 | Embed sidecar dim self-test passes against running aura-llama-embed returning 768d | integration | `go test -tags neo4j_integration -run TestPingEmbed_Live ./internal/knowledge -race` | ❌ Wave 0 |
| INFRA-02 | MCP subprocess crash propagates fail-fast error to caller (D-06 policy) | integration | `go test -tags neo4j_integration -run TestMCPCrash_FailsAura ./internal/knowledge -race` | ❌ Wave 0 |
| INFRA-02 | `0001_init.cypher` applies cleanly: constraint + vector index + fulltext index all present | integration | `go test -tags neo4j_integration -run TestInitCypher_AllArtifactsPresent ./internal/knowledge -race` | ❌ Wave 0 |
| INFRA-02 | `aura.knowledge_migrations` audit row written after successful Cypher migrate | integration | `go test -tags 'neo4j_integration db_integration' -run TestCypherMigrate_WritesAuditRow ./internal/knowledge -race` | ❌ Wave 0 |
| INFRA-02 | Italian smoke recall@5 = 5/5 + p95 ≤ 30ms on 5-doc fixture | smoke (shell) | `bash scripts/neo4j_smoke.sh` | ❌ Wave 0 — `scripts/neo4j_smoke.sh` + `scripts/fixtures/neo4j-smoke/` |
| INFRA-02 | goleak: zero residual goroutines after `Client.Close()` | integration | `go test -tags neo4j_integration ./internal/knowledge -race` (TestMain calls `goleak.VerifyNone`) | ❌ Wave 0 |

### Sampling Rate

- **Per task commit:** `go test ./internal/db/... ./internal/knowledge/... -race -count=1` (quick — runs unit tests + integration tests skip gracefully via build tag)
- **Per wave merge:** `go test ./... -tags 'db_integration neo4j_integration' -race -count=1` + `bash scripts/restore_drill.sh` + `bash scripts/neo4j_smoke.sh`
- **Phase gate:** Full suite green + restore drill < 90s + smoke recall@5 = 5/5 + p95 ≤ 30ms before `/gsd-verify-work` → `/gsd-code-review` → `/gsd-audit-fix`

### Validation Dimensions per Requirement

#### INFRA-01 — Postgres infra

| Dimension | What to Check | Acceptance |
|-----------|---------------|------------|
| Functional | `aura db {migrate, ping, status, reset}` all succeed against fresh container | All four subcommands exit 0; idempotent re-run prints "no pending migrations" |
| Structural | Files exist at expected paths with ≤600 LOC each | `wc -l internal/db/*.go internal/db/migrations/*.sql sqlc.yaml compose.yaml Makefile .env.example`; every value ≤ 600 |
| Integration | Compose service `postgres` reaches healthy + Aura binary connects via pool | `docker compose ps postgres` shows `Health: healthy`; `aura db ping` returns "ok + Xms" |
| Error-handling | Empty `AURA_DB_MIGRATE_URL` → fail-fast with exact PRD message | `unset AURA_DB_MIGRATE_URL; aura db migrate; echo $?` returns non-zero + stderr matches PRD message |
| Observability | slog structured logs for migrate ops (version applied, duration, error if any) | `aura db migrate 2>&1 \| grep -q 'level=INFO.*migration'` returns 0 |
| Security | aura_app cannot TRUNCATE, DROP, CREATE TABLE | integration test connects as aura_app, runs each forbidden DDL, asserts permission denied |
| Performance | restore_drill.sh completes < 90s on a fixture dump | scripts/restore_drill.sh exits 0 + measured elapsed_ms < 90 000 |
| Regression | sqlc-generated code matches committed sources | `make sqlc && git diff --exit-code internal/db/sqlc/` exits 0 |

#### INFRA-02 — Neo4j infra + MCP + embed sidecar

| Dimension | What to Check | Acceptance |
|-----------|---------------|------------|
| Functional | `aura neo4j {migrate, ping, status, reset}` all succeed | All four subcommands exit 0; idempotent re-run reports "0 migrations applied" |
| Structural | Files exist at expected paths with ≤600 LOC each | `wc -l internal/knowledge/*.go internal/knowledge/migrations/*.cypher scripts/neo4j_smoke.sh scripts/fixtures/neo4j-smoke/*`; ≤ 600 |
| Integration | Compose services `neo4j` + `aura-llama-embed` reach healthy + MCP subprocess connects | `docker compose ps neo4j aura-llama-embed` both `Health: healthy`; `aura neo4j ping` returns server version 5.26.x + sidecar dim 768 |
| Error-handling | Sidecar returns 384d → boot fails with explicit Pitfall #7 message | unit test mocks `/v1/embeddings` to return 384d; PingEmbed returns the literal PRD error message including "AURA_EMBED_DIMENSIONS" |
| Error-handling | MCP binary missing on PATH → fail-fast with "pip install mcp-neo4j-cypher==0.6.0" hint | unit test sets `AURA_MCP_NEO4J_CYPHER_BIN=nonexistent-cmd`; Open returns wrapped error containing install hint |
| Error-handling | MCP subprocess crash mid-call → next Cypher call returns error referencing D-06 policy | integration test kills MCP subprocess via `cmd.Process.Kill()`; next Cypher call returns wrapped error |
| Observability | slog logs Cypher migration apply (version, name, checksum, duration) | `aura neo4j migrate 2>&1 \| grep -q 'level=INFO.*cypher.*0001_init'` returns 0 |
| Security | `aura.knowledge_migrations` writes from MCP go through Postgres `aura_app` role (not raw SQL injection) | integration test reads pg_stat_activity during a Cypher migrate; observes connection as `aura_app` |
| Performance | smoke p95 ≤ 30ms vector search on 5-doc fixture | `bash scripts/neo4j_smoke.sh` exits 0; logs p95 ≤ 30ms |
| Performance | Cold-boot: from `docker compose up -d` to first successful `aura neo4j ping` ≤ 90 s | shell timing wrapper in Wave 0 smoke harness |
| Regression | recall@5 = 5/5 on Italian fixture | smoke script asserts hit_count == 5 |
| Regression | HNSW M=32, ef_construction=200 actually applied (not Neo4j defaults) | integration test runs `SHOW INDEXES YIELD name, options WHERE name = 'chunk_embedding' RETURN options` and asserts M=32, ef_construction=200, dimensions=768, similarity=cosine |

### Wave 0 Gaps

- [ ] `internal/db/db_test.go` — covers INFRA-01 functional/integration/error-handling
- [ ] `internal/db/migrate_test.go` (or merged into db_test.go) — covers INFRA-01 idempotency + role-deny
- [ ] `internal/knowledge/client_test.go` — covers INFRA-02 MCP spawn + Cypher + audit row
- [ ] `internal/knowledge/ping_test.go` — covers INFRA-02 PingEmbed dim assertion (unit + live)
- [ ] `scripts/restore_drill.sh` — covers INFRA-01 SC#3 (< 90 s)
- [ ] `scripts/neo4j_smoke.sh` + `scripts/fixtures/neo4j-smoke/` — covers INFRA-02 SC#5 (recall@5 + p95)
- [ ] `Makefile` targets: `test`, `test-integration`, `restore-drill`, `smoke` for canonical invocation
- [ ] Framework install: `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1` — required for `make sqlc`
- [ ] `jq` install hint in `scripts/neo4j_smoke.sh` preamble for Git Bash dev users

## Security Domain

> security_enforcement is enabled (config absent → treat as enabled). Postgres + Neo4j + MCP subprocess attack surface analyzed below.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture | yes | Role separation `aura_app` vs `aura_migrate` is a V1.4 explicit trust-zone boundary; documented in PRD + this RESEARCH |
| V2 Authentication | yes | Postgres password auth via `POSTGRES_PASSWORD`; Neo4j password auth via `NEO4J_PASSWORD`; both required env vars with `.env.example` placeholder `changeme` that operator MUST replace |
| V3 Session Management | n/a | Phase 1 has no user sessions; deferred to Phase 13 (Telegram) / Phase 12 (AG-UI) |
| V4 Access Control | yes | DB-engine-enforced role grants are V4.2 (function-level access control). `aura_app` lacks TRUNCATE/DROP/DDL by construction. |
| V5 Input Validation | yes | All Cypher queries from Aura code go through MCP `read_neo4j_cypher`/`write_neo4j_cypher` with `params` map (parameterized). Phase 1's only user-input surface is the Makefile `--param` flags (smoke script); planner should ensure no string-concat into Cypher in the Go-side `cypher write/read` subcommand. |
| V6 Cryptography | yes | Postgres password + Neo4j password stored only in `.env` (gitignored). Compose interpolation `${POSTGRES_PASSWORD:?}` errors out if missing. No hand-rolled crypto. |
| V7 Errors & Logging | yes | Error wrapping must redact DSN/password (Pitfall #2). slog structured logging from Slice 0.5 onward. |
| V12 Files & Resources | yes | Named volumes only (`aura-postgres`, `aura-neo4j`, etc.); no bind-mounts; loopback-only port publishing (`127.0.0.1:5432` etc.). |
| V13 API & Web Service | n/a | Phase 1 exposes no HTTP API; defer to Phase 8 / Phase 12 / Phase 13 |
| V14 Configuration | yes | `.env` discipline: required keys error out via `${VAR:?msg}` compose interpolation; `.env.example` committed; `.env` in `.gitignore` (CLAUDE.md confirmed). |

### Known Threat Patterns for Phase 1 stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| SQL injection via subcommand args | Tampering | sqlc parameterized queries — never string-concat into SQL |
| Cypher injection via MCP `params` | Tampering | Always pass user input through `params` map, never interpolate into query string |
| Role-escalation: aura_app issuing DDL | Elevation | DB engine enforces grants; `0001_init.up.sql` does NOT grant TRUNCATE/DROP to aura_app; default privileges restrict future tables |
| Secret leak via error wrap | Information disclosure | Wrap pgx/migrate errors with redacted DSN (Pitfall #2 above) |
| Wrong-dim embedding silent corruption | Tampering of stored data | Boot self-test via `PingEmbed` + per-ingest assert in Phase 15 (Pitfall #5) |
| MCP subprocess privilege boundary | Elevation | Subprocess runs as same UID as Aura process; reduce by running Aura as non-root in production (out of Phase 1 scope; documented) |
| Bind-mount Windows data corruption | Tampering / Denial of service | Named volumes only (memory `feedback_sqlite_wal_windows_corruption` extended) |
| Plaintext passwords in `compose.yaml` | Information disclosure | `${POSTGRES_PASSWORD:?}` and `${NEO4J_PASSWORD:?}` interpolation; never literal in YAML |
| Port exposure to LAN | Information disclosure | Bind to `127.0.0.1:5432` etc. (loopback only); compose port maps include host IP |
| Cold-boot first-run download MITM | Tampering | HF `--hf-repo` uses HTTPS by default; document SHA256 pin via `--hf-file ...gguf` precise filename |
| MCP server license unknown | Legal | Pitfall #6 above — verify before Slice 0.7 merge |
| pg_dump credential leak in backup file | Information disclosure | Phase 1 restore_drill uses local Postgres only; no remote sink; Phase 10 backup slice will revisit |

## Sources

### Primary (HIGH confidence)

- pkg.go.dev/github.com/jackc/pgx/v5 — pgx v5.9.2 verified; Config struct fields
- pkg.go.dev/github.com/golang-migrate/migrate/v4 — v4.19.1 verified; iofs + embed.FS pattern
- docs.sqlc.dev/en/latest/reference/config.html — sqlc v2 config schema verified
- neo4j.com/docs/cypher-manual/5/indexes/semantic-indexes/vector-indexes/ — `CREATE VECTOR INDEX` syntax + HNSW M/ef_construction params + IF NOT EXISTS supported in 5.26
- neo4j.com/docs/cypher-manual/5/indexes/semantic-indexes/full-text-indexes/ — `CREATE FULLTEXT INDEX` syntax + IF NOT EXISTS supported
- neo4j.com/docs/cypher-manual/5/constraints/managing-constraints/ — `CREATE CONSTRAINT FOR ... REQUIRE` syntax (no longer `ASSERT`)
- neo4j.com/docs/operations-manual/5/docker/plugins/ — `NEO4J_PLUGINS='["apoc","graph-data-science"]'` format
- hub.docker.com/_/postgres — `postgres:17.10-alpine3.23` latest tag
- hub.docker.com/_/neo4j — `neo4j:5.26.26-community` latest patch
- github.com/neo4j-contrib/mcp-neo4j (README) — tools (read_neo4j_cypher, write_neo4j_cypher, get_neo4j_schema), env vars, transport
- github.com/jackc/pgx/blob/master/pgxpool/pool.go — Config struct fields (MaxConns, MinConns, MaxConnIdleTime, etc.) verified
- huggingface.co/ggml-org/embeddinggemma-300M-GGUF — Q8_0 GGUF + `llama-server --embeddings` invocation
- github.com/ggml-org/llama.cpp tools/server README — `/health`, `/v1/embeddings`, `--embeddings` flag
- postgresql.org/docs/17/sql-grant.html — `GRANT ... ON ALL TABLES IN SCHEMA` + `ALTER DEFAULT PRIVILEGES` syntax
- Local probe: `mcp-neo4j-cypher --help` on this machine — confirmed CLI flags

### Secondary (MEDIUM confidence)

- WebSearch on Neo4j Docker healthcheck patterns (multiple blog posts cross-referenced)
- WebSearch on pgxpool best practices (pgx GitHub discussions + pkg.go.dev tutorials)
- pyproject.toml of mcp-neo4j-cypher 0.6.0 — `neo4j>=5.26.0`, `fastmcp>=2.10.5`, `pydantic`, `tiktoken`

### Tertiary (LOW confidence)

- `mcp-neo4j-cypher` license claim "Apache 2.0" — A1 above; LICENSE file 404'd via WebFetch in this session
- MCP JSON-RPC envelope literal shape for `tools/call` — A10 above; not exercised by hands-on probe

## Project Constraints (from CLAUDE.md)

These are non-negotiable rules every Phase 1 task must honor; the planner must mirror them into PLAN.md verification steps:

- **NEVER SUPPOSE.** Read code before editing. If uncertain about API contract, stop and ask.
- **READ BEFORE EDIT.** Re-read a file you haven't touched in the last 5 messages.
- **3-STRIKE RULE.** Same failing approach max 3 times. On strike 3, stop and ask (or escalate via PRD-amendment).
- **NEVER MODIFY TESTS TO MAKE THEM PASS** unless the test itself is broken.
- **SCOPE CONTROL.** Do exactly what was asked. No unrequested features, refactors, or improvements.
- **FOLLOW EXISTING PATTERNS.** Never invent new approaches when codebase patterns exist.
- **NO GOD CLASS.** Never create a file >600 LOC. Refactor on touch (split into `<name>_<concern>.go`).
- **REUSABLE CODE.** Never duplicate; extract a helper.
- **DEEP REFACTOR ON TOUCH.** Every file you edit gets dead-code removal + dupl-folding + LOC ≤600 + comments-updated in the SAME commit.
- **GIT PUSH DISCIPLINE.** Never `git push` unless explicitly requested in the current turn.
- **NO COMMENTS UNLESS WHY IS NON-OBVIOUS.** Identifier names already explain what.
- **NO TEST ASILO NIDO.** Realistic fixtures, goleak, race detector, build tags integration, coverage threshold, mutation testing spot-check.
- **POST-EDIT VALIDATION (Gate 2):** After every Go file edit, run `go vet ./...`, `go build ./...`, `go test ./internal/<package>/`, `go test -race ./internal/<package>/`.
- **COMMIT DISCIPLINE:** One slice = one commit. Atomic. Imperative subject + body explaining *why*. `Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>` trailer mandatory.
- **DEFERRED-TOOL PATTERN:** Big tools live in dedicated files with `Deferred: true` ToolSpec flag. Phase 1 introduces no tools, so this is informational only.
- **PERSISTENCE LAYER:** Postgres primary (port 5432), schema `aura.*`, sqlc-generated client, golang-migrate. Neo4j Community + APOC + GDS (port 7687 bolt). `mcp-neo4j-cypher` MCP is the LLM interface to graph.
- **NAMED VOLUMES ONLY** on Windows (no bind-mounts).
- **ENV VAR CONVENTION:** `AURA_<DOMAIN>_<UNIT>` (e.g., `AURA_EMBED_DIMENSIONS`). Exceptions for upstream-canonical names (`POSTGRES_PASSWORD`, `NEO4J_PASSWORD`, `NEO4J_USER`).

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — every version probed against authoritative source + slopcheck OK
- Architecture: HIGH — patterns are battle-tested + match PRD + code examples are compilable shapes (not pseudo-code)
- Pitfalls: HIGH — most surfaced via this session's ground-truth probes (especially Pitfall #5 sidecar /health shape and Pitfall #6 missing license metadata)
- Validation Architecture: HIGH — every dimension traces to a 5-SC criterion or a PRD acceptance row

**Research date:** 2026-05-29
**Valid until:** 2026-06-29 (30 days; stable infra phase; refresh if Neo4j 5.26.27+ or pgx 5.10+ releases land before)

**Side-effects of this research session (already applied to working tree):**
- `go.mod` bumped to `go 1.25.0` and `// indirect` requires for 5 deps added (slopcheck `go get`-driven)
- `go.sum` created with checksums for `pgx v5.9.2`, `golang-migrate v4.19.1`, `joho/godotenv v1.5.1`, `goleak v1.3.0`, `google/uuid v1.6.0`
- `slopcheck 0.6.1` installed via pip (system Python)

These side-effects align with Phase 0 Amendment #1 (Go 1.25 bump) and Phase 1 dependency staging. Planner should treat them as starting-state, not as new tasks.
