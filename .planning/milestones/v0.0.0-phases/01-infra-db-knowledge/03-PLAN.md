---
phase: 1
slug: infra-db-knowledge
plan: 03
type: execute
slice: 0.7
title: "Slice 0.7 — Neo4j Community + mcp-neo4j-cypher + embed sidecar"
wave: 2
depends_on:
  - 02
files_modified:
  - cmd/aura/main.go
  - cmd/aura/neo4j.go
  - internal/config/config.go
  - internal/knowledge/config.go
  - internal/knowledge/client.go
  - internal/knowledge/migrate.go
  - internal/knowledge/ping.go
  - internal/knowledge/status.go
  - internal/knowledge/reset.go
  - internal/knowledge/migrations/0001_init.cypher
  - internal/knowledge/client_unit_test.go
  - internal/knowledge/client_test.go
  - compose.yaml
  - Makefile
  - .env.example
  - scripts/neo4j_smoke.sh
  - scripts/fixtures/neo4j-smoke/01_amatriciana.md
  - scripts/fixtures/neo4j-smoke/02_duomo_milano.md
  - scripts/fixtures/neo4j-smoke/03_fiat_panda.md
  - scripts/fixtures/neo4j-smoke/04_nome_della_rosa.md
  - scripts/fixtures/neo4j-smoke/05_espresso_napoletano.md
  - scripts/fixtures/neo4j-smoke/queries.txt
  - prd.md
  - .planning/ROADMAP.md
autonomous: false
requirements:
  - INFRA-02
tags:
  - neo4j
  - mcp
  - embeddings
  - graph
  - infra
user_setup:
  - service: mcp-neo4j-cypher
    why: "MCP subprocess required on host PATH; no bundling in Phase 1 (PRD OQ 0.7 #1)"
    env_vars:
      - name: AURA_MCP_NEO4J_CYPHER_BIN
        source: "Path to mcp-neo4j-cypher binary (defaults to 'mcp-neo4j-cypher' on PATH after `pip install mcp-neo4j-cypher==0.6.0`)"
    install_command: "pip install mcp-neo4j-cypher==0.6.0"
  - service: neo4j-community
    why: "Bolt + APOC + GDS plugin for HNSW vector index"
    env_vars:
      - name: NEO4J_USER
        source: ".env (defaults to neo4j)"
      - name: NEO4J_PASSWORD
        source: ".env (operator must set; do NOT commit real password)"
  - service: aura-llama-embed
    why: "OpenAI-compat embeddings server; sidecar boots embeddinggemma-300m-Q8_0 (768d native)"
    env_vars:
      - name: AURA_EMBED_BASE_URL
        source: ".env (default http://127.0.0.1:8081)"
      - name: AURA_EMBED_DIMENSIONS
        source: ".env (Amendment #18 contract — must be 768)"
must_haves:
  truths:
    - "Operator runs `aura neo4j migrate` and observes `0001_init.cypher` applied; re-run prints 0 newly applied (idempotent via aura.knowledge_migrations check) — ROADMAP SC#4 first part"
    - "Operator runs `aura neo4j ping` and observes: mcp-neo4j-cypher subprocess returns Neo4j server version 5.26.x AND embed sidecar `/v1/embeddings` round-trip returns 768d (Pattern 5 dim probe, NOT `/health → {dim}` per Pitfall #5) — ROADMAP SC#4 (amended)"
    - "Operator runs `bash scripts/neo4j_smoke.sh` and observes recall@5 = 5/5 on Italian fixture corpus, p95 vector search ≤ 30ms — ROADMAP SC#5"
    - "Operator runs `aura neo4j migrate` against an MCP subprocess that crashes mid-call and observes Aura process exit with wrapped error referencing D-06 policy (no auto-restart, no graceful degrade)"
    - "Operator runs `aura neo4j ping` against a sidecar returning 384d (mocked) and observes fail-fast with literal error containing `embedding sidecar returned dim=384 but AURA_EMBED_DIMENSIONS=768 — refuse to start (Pitfall #7 silent corruption); see prd.md amendment #18 swap runbook` — Pattern 5 + Pitfall #5 mitigation"
    - "Operator runs `aura neo4j status` and observes the `aura.knowledge_migrations` rows tabulated (read via Slice 0.5 sqlc-generated `ListAppliedKnowledgeMigrations`); exit code 0"
    - "Operator inspects `compose.yaml` and observes 3 services [postgres, neo4j, aura-llama-embed] with loopback-only port maps + healthchecks + named volumes (no bind-mounts per feedback_sqlite_wal_windows_corruption)"
    - "PRD amendment #18 (one-line correction) lands in this slice: Slice 0.7 acceptance row 182 `aura knowledge ping validates sidecar /health returns {dim:768}` → `aura neo4j ping validates sidecar /v1/embeddings round-trip returns 768d (Pattern 5)`"
    - "ROADMAP amendment lands in this slice: Phase 1 SC#4 `aura knowledge ping` → `aura neo4j ping` per D-05"
    - "mcp-neo4j-cypher upstream license verified Apache 2.0 (or compatible permissive — MIT/BSD-3-Clause); evidence captured in commit body via `gh api repos/neo4j-contrib/mcp-neo4j/contents/LICENSE` output"
  artifacts:
    - path: "compose.yaml"
      provides: "Extended with neo4j 5.26.26-community + aura-llama-embed services (postgres already there from Slice 0.5)"
      contains: "neo4j:5.26.26-community"
    - path: "Makefile"
      provides: "Extended with neo4j-up, neo4j-migrate (depends on db-migrate), neo4j-status, neo4j-reset, smoke targets"
      contains: "neo4j-migrate:"
    - path: ".env.example"
      provides: "Extended with Neo4j + embed env keys"
      contains: "AURA_EMBED_DIMENSIONS=768"
    - path: "internal/knowledge/client.go"
      provides: "MCP subprocess JSON-RPC client per Pattern 3"
      contains: "exec.CommandContext"
    - path: "internal/knowledge/migrate.go"
      provides: "Cypher migration runner reading aura.knowledge_migrations via Slice 0.5 sqlc"
      contains: "ListAppliedKnowledgeMigrations"
    - path: "internal/knowledge/migrations/0001_init.cypher"
      provides: "Chunk constraint + HNSW vector index (768d M=32 ef=200) + fulltext index — D-08 minimal scope"
      contains: "CREATE VECTOR INDEX chunk_embedding"
    - path: "internal/knowledge/ping.go"
      provides: "MCP ping + embed sidecar dim self-test (Pattern 5)"
      contains: "embedding sidecar returned dim="
    - path: "scripts/neo4j_smoke.sh"
      provides: "Italian fixture smoke harness with recall@5=5/5 + p95≤30ms assertions"
      contains: "recall@5"
    - path: "scripts/fixtures/neo4j-smoke/queries.txt"
      provides: "5 IT queries with expected top-1 doc IDs"
      contains: "01_amatriciana"
    - path: "cmd/aura/neo4j.go"
      provides: "runNeo4j(args) inner switch over migrate|ping|status|reset|cypher"
      contains: "case \"migrate\":"
  key_links:
    - from: "cmd/aura/main.go"
      to: "cmd/aura/neo4j.go"
      via: "case \"neo4j\": runNeo4j(os.Args[2:])"
      pattern: "case \"neo4j\":"
    - from: "internal/knowledge/migrate.go"
      to: "internal/db/sqlc.Queries"
      via: "ListAppliedKnowledgeMigrations + RecordKnowledgeMigration"
      pattern: "sqlc\\.New\\(.*\\)\\.(List|Record)"
    - from: "internal/knowledge/client.go"
      to: "mcp-neo4j-cypher subprocess"
      via: "exec.CommandContext stdio JSON-RPC"
      pattern: "exec\\.CommandContext"
    - from: "internal/knowledge/ping.go"
      to: "aura-llama-embed sidecar"
      via: "POST /v1/embeddings + len(data[0].embedding) check"
      pattern: "/v1/embeddings"
    - from: "internal/knowledge/migrations/0001_init.cypher"
      to: "Neo4j HNSW vector index"
      via: "MCP write_neo4j_cypher subprocess call"
      pattern: "CREATE VECTOR INDEX chunk_embedding"
---

<objective>
Stand up Neo4j 5.26.26-community + APOC + GDS + `mcp-neo4j-cypher` MCP subprocess + `aura-llama-embed` (embeddinggemma-300m, OpenAI-compat) sidecar, wired through the new `aura neo4j {migrate|ping|status|reset}` CLI surface. Land the minimal `0001_init.cypher` (D-08 scope: `:Chunk(id)` UNIQUE + HNSW 768d cosine M=32 ef_construction=200 + fulltext index) and the Italian smoke fixture that proves recall@5 = 5/5 + p95 ≤ 30ms on a deterministic 5-doc corpus. Slice 0.7 closes Phase 1.

Purpose: The graph + vector substrate that every memory and skills phase consumes. The dim self-test enforced here (Pattern 5 / Amendment #18 / Pitfall #5) is the only place the `AURA_EMBED_DIMENSIONS=768` contract becomes operational — without it the Phase 15 ingest pipeline could silently corrupt the index. D-06 hard-fail policy on MCP subprocess crash is locked in here so later phases inherit it. The Italian smoke fixture (D-04) is the regression anchor for all future retrieval work.

Output: ~280 src LOC + ~120 test LOC across `internal/knowledge/` + `cmd/aura/neo4j.go`; 1 Cypher migration; 5 Italian fixture docs + queries.txt + smoke script; compose extension (neo4j + aura-llama-embed services); Makefile extension (neo4j-* + smoke targets); .env.example extension (Neo4j + embed keys); config composite extension (Neo4j sub-struct); 3 documentation amendments (PRD acceptance row 182 fix, ROADMAP Phase 1 SC#4 `knowledge`→`neo4j` fix, mcp-neo4j-cypher license evidence in commit body).

This plan is closed by a single atomic commit per D-01: `slice 0.7: neo4j community + mcp-neo4j-cypher + cypher migrations`.

**This plan is non-autonomous** because it includes a blocking human-verify checkpoint for the mcp-neo4j-cypher license verification (Pitfall #6 / OWASP supply-chain discipline).
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@d:\Aura\.planning\PROJECT.md
@d:\Aura\.planning\ROADMAP.md
@d:\Aura\.planning\STATE.md
@d:\Aura\.planning\REQUIREMENTS.md

# Phase context (mandatory)
@d:\Aura\.planning\phases\01-infra-db-knowledge\01-CONTEXT.md
@d:\Aura\.planning\phases\01-infra-db-knowledge\01-RESEARCH.md
@d:\Aura\.planning\phases\01-infra-db-knowledge\01-PATTERNS.md
@d:\Aura\.planning\phases\01-infra-db-knowledge\01-VALIDATION.md

# Source of truth + Slice 0.5 handoff
@d:\Aura\prd.md
@d:\Aura\CLAUDE.md
@d:\Aura\.planning\phases\01-infra-db-knowledge\01-02-SUMMARY.md

# Skeleton integration points
@d:\Aura\cmd\aura\main.go
@d:\Aura\internal\config\config.go
@d:\Aura\compose.yaml
@d:\Aura\Makefile
@d:\Aura\.env.example
</context>

<threat_model>

## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| Aura process → mcp-neo4j-cypher subprocess | stdio JSON-RPC channel; subprocess executes Cypher with full Neo4j privileges; same UID as Aura |
| Aura process → aura-llama-embed sidecar | localhost HTTP; sidecar exposes `/v1/embeddings` and `/health` over loopback only |
| Aura process → Neo4j bolt port | via MCP only (no direct driver per discipline ban); credentials in `${NEO4J_PASSWORD}` |
| Cypher migration body → Neo4j | trusted body (.cypher file in repo, embedded into binary); no user input interpolation in Phase 1 |
| Smoke fixture text → embed sidecar | scripted ingest of repo-committed fixtures; trust boundary applies only to operator-modified fixtures |
| `gh api repos/neo4j-contrib/mcp-neo4j/contents/LICENSE` evidence path | GitHub API → operator workstation → commit body; treated as trusted source |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-1.07-01 | Tampering | MCP subprocess Cypher invocation | mitigate | All Cypher calls go through MCP `tools/call → read_neo4j_cypher / write_neo4j_cypher` with `params` map; never string-interpolate user input into query body. Phase 1 only runs embed.FS-loaded migration Cypher (no user input) — threat contained but pattern locked for Phase 11+. `TestCypherClient_RejectsConcatenatedInjection` (unit) asserts the client API does not expose a string-concat path. |
| T-1.07-02 | Information disclosure | aura-llama-embed sidecar `/v1/embeddings` and `/health` ports | mitigate | compose ports bound to `127.0.0.1:8081:8081` (loopback only — never published to LAN). Healthcheck path is internal-only. `TestComposeYAML_LoopbackOnly` (smoke) greps compose for any non-loopback port binding and fails on hit. |
| T-1.07-03 | Information disclosure | Neo4j admin ports (7474 HTTP + 7687 bolt) | mitigate | compose ports `127.0.0.1:7474:7474` + `127.0.0.1:7687:7687` loopback only. HNSW index data never leaves localhost in Phase 1. Same compose grep gate as T-1.07-02. |
| T-1.07-04 | Information disclosure | MCP subprocess stdout/stderr in error paths | mitigate | `internal/knowledge/client.go` error wrap discipline: on any `cmd.Stderr` capture, the wrapped error includes only the first 200 bytes of stderr (truncated) AND runs through a `redactNeo4jSecrets()` helper that masks any substring matching `password=...`, `pass:...`, or the literal value of `cfg.Password` (if non-empty). `TestClient_StderrRedaction` (unit) asserts password in stderr is masked in error. |
| T-1.07-05 | Tampering of stored data | Sidecar returns wrong embedding dim → silent HNSW corruption | mitigate | Pattern 5 dim probe in `internal/knowledge/ping.go` POSTs dummy input to `/v1/embeddings`, asserts `len == AURA_EMBED_DIMENSIONS`; fails Aura boot on mismatch with literal error string (Pitfall #5/#7). Asserted by `TestPingEmbed_DimMismatch` (unit, mocked HTTP). |
| T-1.07-06 | Elevation of privilege | mcp-neo4j-cypher subprocess running as Aura UID | accept | Subprocess inherits Aura's UID by exec.Command default; reducing to non-root requires container deployment refactor (Phase 12+ scope). Documented; not blocking. |
| T-1.07-07 | Denial of service / Tampering | mcp-neo4j-cypher crash mid-call | mitigate | D-06 policy: fail Aura process. Client returns wrapped error referencing D-06; the calling subcommand exits non-zero. `TestMCPCrash_FailsAura` (integration) kills MCP subprocess and asserts next Cypher call returns error containing "MCP may have crashed — D-06 policy". NO restart-once, NO graceful degrade. |
| T-1.07-SC | Tampering | mcp-neo4j-cypher upstream supply chain (license unknown per Pitfall #6) | mitigate | **Blocking human-verify checkpoint** (gate=blocking-human, NOT auto-advanceable) verifies upstream LICENSE via `gh api repos/neo4j-contrib/mcp-neo4j/contents/LICENSE` BEFORE the slice commit. If license is NOT Apache 2.0 / MIT / BSD-3-Clause, escalates to PRD-amendment trigger (discipline ban on native Go drivers locks Aura in). |
| T-1.07-08 | Tampering | Neo4j healthcheck races MCP subprocess connect (Pitfall #3) | mitigate | compose healthcheck uses `cypher-shell -u neo4j -p $$NEO4J_PASSWORD --database neo4j 'RETURN 1'` (NOT `nc -z 7687`); compose `depends_on: { neo4j: { condition: service_healthy } }`. Client retry loop in `internal/knowledge/client.go` honors `cfg.ConnectTimeoutSec=10` exponential backoff on first call. `TestClient_RetryOnConnectionRefused` (integration) verifies retry behavior. |
| T-1.07-09 | Information disclosure | Plaintext `NEO4J_PASSWORD` in compose.yaml or logs | mitigate | compose uses `${NEO4J_PASSWORD:?NEO4J_PASSWORD required in .env}` fail-fast interpolation. `.env.example` ships `changeme` placeholder. `redactNeo4jSecrets` helper covers logs. Grep gate: compose.yaml contains zero hardcoded password values. |

**Block-on threshold:** `high`. T-1.07-SC is a **blocking-human checkpoint** (cannot auto-advance even if `workflow.auto_advance=true`). T-1.07-01/02/03/04/05/07/08/09 are mitigations requiring automated test evidence before commit. T-1.07-06 accepted with rationale.

</threat_model>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Compose + Makefile + .env.example extensions + config composite + Neo4j config + Italian smoke fixture</name>
  <files>
    compose.yaml,
    Makefile,
    .env.example,
    internal/config/config.go,
    internal/knowledge/config.go,
    scripts/fixtures/neo4j-smoke/01_amatriciana.md,
    scripts/fixtures/neo4j-smoke/02_duomo_milano.md,
    scripts/fixtures/neo4j-smoke/03_fiat_panda.md,
    scripts/fixtures/neo4j-smoke/04_nome_della_rosa.md,
    scripts/fixtures/neo4j-smoke/05_espresso_napoletano.md,
    scripts/fixtures/neo4j-smoke/queries.txt
  </files>
  <read_first>
    @d:\Aura\.planning\phases\01-infra-db-knowledge\01-RESEARCH.md (§Code Examples lines 919-976 `compose.yaml` neo4j + aura-llama-embed blocks, lines 1016-1040 `Makefile` Slice 0.7 appendix, lines 1053-1066 `.env.example` Slice 0.7 appendix, lines 1184-1192 `queries.txt`; §Common Pitfalls #3 healthcheck race, #4 Cypher syntax drift, #5 dim mismatch, #7 MSYS path mangling, #8 Community single-DB; §Assumptions A3 cypher-shell in image, A4 HF model first-boot download),
    @d:\Aura\.planning\phases\01-infra-db-knowledge\01-PATTERNS.md (lines 428-444 compose critical config; lines 447-457 Makefile Slice 0.7 targets; lines 471-481 .env.example Slice 0.7 keys; lines 495-518 internal/knowledge/config.go shape; lines 650-666 fixture content suggestions),
    @d:\Aura\.planning\phases\01-infra-db-knowledge\01-CONTEXT.md (§Decisions D-02 compose at root, D-03 sidecar in this slice, D-04 fixture content, D-08 minimal schema, line 117 mini-PC CPU budget),
    @d:\Aura\compose.yaml (Slice 0.5 starting state — postgres-only),
    @d:\Aura\Makefile (Slice 0.5 starting state — db-* + restore-drill targets),
    @d:\Aura\.env.example (Slice 0.5 starting state — Postgres keys only),
    @d:\Aura\internal\config\config.go (Slice 0.5 starting state — DB-only composite),
    @d:\Aura\prd.md (§Slice 0.7 lines 129-237 — Neo4j goal, stack, file targets; §Amendment #18 dim contract; §Amendment #2 5.26-community LTS)
  </read_first>
  <behavior>
    - `compose.yaml` extends Slice 0.5 form by appending two services after the existing `postgres` block: `neo4j` and `aura-llama-embed`. Updates the `volumes:` declaration block to include `aura-neo4j`, `aura-neo4j-plugins`, `aura-llama-embed`. Does NOT modify the existing `postgres` block (refactor-on-touch only applies to touched scope; this is additive extension).
    - `neo4j` service: image `neo4j:5.26.26-community` (Amendment #2 pin), `NEO4J_AUTH: neo4j/${NEO4J_PASSWORD:?NEO4J_PASSWORD required in .env}`, `NEO4J_PLUGINS: '["apoc","graph-data-science"]'`, memory env (`NEO4J_dbms_memory_heap_initial__size: 512m`, `NEO4J_dbms_memory_heap_max__size: 1G`, `NEO4J_dbms_memory_pagecache_size: 512m`), `NEO4J_dbms_security_procedures_unrestricted: 'apoc.*,gds.*'`, ports `127.0.0.1:7474:7474` + `127.0.0.1:7687:7687` (loopback only), named volumes `aura-neo4j:/data` + `aura-neo4j-plugins:/plugins`, healthcheck `cypher-shell -u neo4j -p $$NEO4J_PASSWORD --database neo4j 'RETURN 1' || exit 1` (Pitfall #3 — NOT `nc -z 7687`), interval 10s, timeout 10s, retries 10, **start_period: 40s** (APOC + GDS auto-download).
    - `aura-llama-embed` service: image `ghcr.io/ggml-org/llama.cpp:server`, command `--hf-repo ggml-org/embeddinggemma-300M-GGUF --hf-file embeddinggemma-300M-Q8_0.gguf --embeddings --host 0.0.0.0 --port 8081 -t 4` (Mini-PC CPU budget per `feedback_minipc_cpu_budget`), port `127.0.0.1:8081:8081`, named volume `aura-llama-embed:/root/.cache/llama.cpp`, healthcheck `curl -sf http://localhost:8081/health || exit 1` (sidecar process probe; dim assertion is Go-side per Pattern 5), interval 10s, timeout 5s, retries 12, **start_period: 60s** (HF first-boot download).
    - `Makefile` extends with: `neo4j-up` (docker compose up neo4j aura-llama-embed + wait healthy), `neo4j-migrate` (depends on db-migrate per slice dependency), `neo4j-status`, `neo4j-reset` (guarded by `AURA_RESET_YES=1`), `smoke` (depends on neo4j-migrate; runs `bash scripts/neo4j_smoke.sh`).
    - `.env.example` appends Neo4j + embed keys: `NEO4J_USER=neo4j`, `NEO4J_PASSWORD=changeme`, `AURA_NEO4J_BOLT_URL=bolt://127.0.0.1:7687`, `AURA_NEO4J_DATABASE=neo4j` (Pitfall #8 — Community single-DB), `AURA_MCP_NEO4J_CYPHER_BIN=mcp-neo4j-cypher` (must be on PATH; install hint comment), `AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC=10`, `AURA_EMBED_BASE_URL=http://127.0.0.1:8081`, `AURA_EMBED_DIMENSIONS=768` (Amendment #18 contract).
    - `internal/config/config.go`: append `Neo4j knowledge.Config` field to the composite `Config` struct; extend `Load()` to populate Neo4j config from env. Comment in struct documents Slice 0.7 addition.
    - `internal/knowledge/config.go` (NEW): package `knowledge`; struct `Config{ BoltURL string; User string; Password string; Database string; MCPBinary string; ConnectTimeoutSec int; EmbedURL string; EmbedDimensions int }` per PATTERNS.md lines 502-514.
    - Italian fixture corpus (5 *.md docs under `scripts/fixtures/neo4j-smoke/`): each doc 100-200 words of legible Italian prose about its respective topic (amatriciana, duomo di milano, fiat panda 30, il nome della rosa, espresso napoletano). Content must be distinguishable enough that the 5 known-answer queries return the correct top-1 (recall@5 = 5/5 trivially achievable on a 5-doc corpus per CONTEXT.md row 178).
    - `queries.txt` format `query|expected_id` per line per RESEARCH §Code Examples lines 1186-1192 verbatim seed.
    - Tests: `internal/knowledge/config_test.go` (unit, no build tag) with `TestLoad_EnvOverrides`, `TestLoad_DefaultsApplied`, `TestEmbedDimensions_RequiredNonZero`. Integration tests defer to Task 2.
    - File LOC budgets: compose.yaml extension adds ~50 lines (total ~80 across postgres + neo4j + embed), Makefile extension adds ~25 lines (total ~65), .env.example extension adds ~12 lines (total ~25), internal/config/config.go grows by ~30 LOC, internal/knowledge/config.go ~40 LOC, each fixture .md ~150 words ≤ 30 lines. All ≤600.
  </behavior>
  <action>
    Extend the Slice 0.5 scaffolding with Neo4j + embed sidecar infrastructure + smoke fixture corpus. Surgical additions to compose.yaml / Makefile / .env.example / config.go — do NOT modify the postgres-related content.

    1. Edit `compose.yaml`: append the `neo4j` and `aura-llama-embed` service blocks per RESEARCH §Code Examples lines 919-969 verbatim. Update `volumes:` declaration to include `aura-neo4j`, `aura-neo4j-plugins`, `aura-llama-embed`. Critical settings:
       - `neo4j.image: neo4j:5.26.26-community` (Amendment #2)
       - `neo4j.environment.NEO4J_AUTH: neo4j/${NEO4J_PASSWORD:?NEO4J_PASSWORD required in .env}` (fail-fast)
       - `neo4j.environment.NEO4J_PLUGINS: '["apoc","graph-data-science"]'`
       - `neo4j.environment.NEO4J_dbms_security_procedures_unrestricted: 'apoc.*,gds.*'`
       - `neo4j.ports: ["127.0.0.1:7474:7474", "127.0.0.1:7687:7687"]` (loopback only, T-1.07-03)
       - `neo4j.healthcheck.test: ["CMD-SHELL", "cypher-shell -u neo4j -p $$NEO4J_PASSWORD --database neo4j 'RETURN 1' || exit 1"]` (Pitfall #3, T-1.07-08)
       - `neo4j.healthcheck.start_period: 40s` (APOC + GDS download)
       - `aura-llama-embed.image: ghcr.io/ggml-org/llama.cpp:server`
       - `aura-llama-embed.command`: --hf-repo + --hf-file + --embeddings + --host 0.0.0.0 + --port 8081 + `-t 4` (Mini-PC CPU budget)
       - `aura-llama-embed.ports: ["127.0.0.1:8081:8081"]` (T-1.07-02)
       - `aura-llama-embed.healthcheck.start_period: 60s` (HF first-boot)

    2. Edit `Makefile`: append Slice 0.7 targets per RESEARCH §Code Examples lines 1016-1040 verbatim:
       - `neo4j-up:` runs `docker compose up -d neo4j aura-llama-embed`, waits for both to report healthy via `docker compose ps --format json | grep -q '"Health":"healthy"'` until-loop.
       - `neo4j-migrate: db-migrate neo4j-up` — depends on Slice 0.5's db-migrate (the `aura.knowledge_migrations` audit table must exist); runs `go run ./cmd/aura neo4j migrate`.
       - `neo4j-status:` runs `go run ./cmd/aura neo4j status`.
       - `neo4j-reset:` guarded by `AURA_RESET_YES=1`; runs `go run ./cmd/aura neo4j reset --yes`.
       - `smoke: neo4j-migrate` runs `bash scripts/neo4j_smoke.sh`.
       - Update `help` target to include the 5 new lines.

    3. Edit `.env.example`: append Neo4j + embed keys per PATTERNS.md line 479 + RESEARCH §Code Examples lines 1053-1066:
       ```
       # Slice 0.7
       # Neo4j
       NEO4J_USER=neo4j
       NEO4J_PASSWORD=changeme                                                                               # required, must change
       AURA_NEO4J_BOLT_URL=bolt://127.0.0.1:7687
       AURA_NEO4J_DATABASE=neo4j                                                                             # Community = single DB only (Pitfall #8)

       # mcp-neo4j-cypher subprocess
       AURA_MCP_NEO4J_CYPHER_BIN=mcp-neo4j-cypher                                                            # must be on PATH (pip install mcp-neo4j-cypher==0.6.0)
       AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC=10

       # Embed sidecar
       AURA_EMBED_BASE_URL=http://127.0.0.1:8081
       AURA_EMBED_DIMENSIONS=768                                                                             # Amendment #18 — boot self-test asserts this (Pitfall #5)
       ```

    4. Create `internal/knowledge/config.go` per PATTERNS.md lines 502-514: package `knowledge`; struct `Config{ BoltURL, User, Password, Database, MCPBinary, EmbedURL string; ConnectTimeoutSec, EmbedDimensions int }`. No methods — pure data.

    5. Edit `internal/config/config.go`: import `"github.com/chetto1983/aura/internal/knowledge"`; add `Neo4j knowledge.Config` field to the `Config` struct; extend `Load()` to populate Neo4j config:
       ```go
       Neo4j: knowledge.Config{
           BoltURL:           envDefault("AURA_NEO4J_BOLT_URL", "bolt://127.0.0.1:7687"),
           User:              envDefault("NEO4J_USER", "neo4j"),
           Password:          os.Getenv("NEO4J_PASSWORD"),
           Database:          envDefault("AURA_NEO4J_DATABASE", "neo4j"),
           MCPBinary:         envDefault("AURA_MCP_NEO4J_CYPHER_BIN", "mcp-neo4j-cypher"),
           ConnectTimeoutSec: envIntDefault("AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC", 10),
           EmbedURL:          envDefault("AURA_EMBED_BASE_URL", "http://127.0.0.1:8081"),
           EmbedDimensions:   envIntDefault("AURA_EMBED_DIMENSIONS", 768),
       },
       ```

    6. Create 5 Italian fixture documents under `scripts/fixtures/neo4j-smoke/`:
       - `01_amatriciana.md`: ~150 words on pasta amatriciana — origin (Amatrice, Lazio), ingredients (guanciale, pecorino romano, pomodoro, peperoncino), cooking time (~10 minutes spaghetti al dente), brief technique.
       - `02_duomo_milano.md`: ~150 words on Duomo di Milano — Gothic cathedral, dedicated to Santa Maria Nascente, construction started 1386, primary architect Simone da Orsenigo + sequence of Lombard/French/Italian architects, Madonnina statue atop main spire.
       - `03_fiat_panda.md`: ~150 words on Fiat Panda — first generation 1980, designed by Giorgetto Giugiaro/Italdesign, "Panda 30" trim with 652cc air-cooled twin engine, transverse mounting, FF layout, longevity in production.
       - `04_nome_della_rosa.md`: ~150 words on *Il nome della rosa* by Umberto Eco — published 1980, set in 1327 Benedictine abbey, protagonists Guglielmo da Baskerville + novice Adso da Melk, semiotic medieval mystery.
       - `05_espresso_napoletano.md`: ~150 words on espresso napoletano — preparation conventions, water temperature 88-92°C, extraction time 25-30s, ~25ml output, naturally sweet crema, served in pre-heated cup.
       Each doc must be plain prose Italian (no markdown bullets unless natural) so the embed model picks up semantically distinct content.

    7. Create `scripts/fixtures/neo4j-smoke/queries.txt` per RESEARCH §Code Examples lines 1186-1192 verbatim (5 lines, `query|expected_id` format):
       ```
       quanto tempo cuoce la pasta amatriciana|01_amatriciana
       chi ha progettato il duomo di milano|02_duomo_milano
       qual è la cilindrata della fiat panda 30|03_fiat_panda
       qual è il nome del monaco protagonista del romanzo di eco|04_nome_della_rosa
       come si prepara il caffè espresso napoletano|05_espresso_napoletano
       ```

    8. Create `internal/knowledge/config_test.go` with 3 unit tests using `t.Setenv`: TestLoad_DefaultsApplied (verify defaults when env unset), TestLoad_EnvOverrides (verify env values override defaults), TestEmbedDimensions_RequiredNonZero (verify behavior when AURA_EMBED_DIMENSIONS=0 — should default to 768 via envIntDefault).

    9. Post-edit Gate 2: `go vet ./...`, `go build ./...`, `go test ./internal/config/... ./internal/knowledge/... -race -count=1`.

    10. Verify no port maps in compose.yaml expose to non-loopback (T-1.07-02 / T-1.07-03 grep gate): `grep -E '^\s+- "[^1][^.]' compose.yaml | grep -cv '127.0.0.1' = 0`.
  </action>
  <verify>
    <automated>cd d:/Aura && go vet ./... && go build ./... && go test ./internal/config/... ./internal/knowledge/... -race -count=1</automated>
    <commands>
      - `test -f d:/Aura/internal/knowledge/config.go` (exit 0)
      - `grep -q 'neo4j:5.26.26-community' d:/Aura/compose.yaml` (Amendment #2 pin)
      - `grep -q 'ggml-org/embeddinggemma-300M-GGUF' d:/Aura/compose.yaml` (sidecar correct model)
      - `grep -q '127.0.0.1:7687:7687' d:/Aura/compose.yaml` (T-1.07-03 loopback)
      - `grep -q '127.0.0.1:8081:8081' d:/Aura/compose.yaml` (T-1.07-02 loopback)
      - `grep -q "cypher-shell -u neo4j" d:/Aura/compose.yaml` (Pitfall #3 healthcheck not nc -z)
      - `grep -q 'NEO4J_PASSWORD required in .env' d:/Aura/compose.yaml` (T-1.07-09 fail-fast)
      - `grep -q 'start_period: 40s' d:/Aura/compose.yaml` (APOC + GDS auto-download)
      - `grep -q 'start_period: 60s' d:/Aura/compose.yaml` (HF first-boot)
      - `grep -q '-t' d:/Aura/compose.yaml && grep -A1 '\-t' d:/Aura/compose.yaml | grep -q '4'` (Mini-PC CPU budget = 4 threads)
      - `grep -q '^neo4j-migrate:' d:/Aura/Makefile`
      - `grep -q '^smoke:' d:/Aura/Makefile`
      - `grep -q 'AURA_EMBED_DIMENSIONS=768' d:/Aura/.env.example` (Amendment #18)
      - `grep -q 'AURA_NEO4J_DATABASE=neo4j' d:/Aura/.env.example` (Pitfall #8)
      - `grep -q 'AURA_MCP_NEO4J_CYPHER_BIN=mcp-neo4j-cypher' d:/Aura/.env.example`
      - `grep -q 'Neo4j knowledge.Config' d:/Aura/internal/config/config.go` (composite extended)
      - For each fixture: `test -s d:/Aura/scripts/fixtures/neo4j-smoke/0X_*.md && wc -w d:/Aura/scripts/fixtures/neo4j-smoke/0X_*.md` reports between 80 and 300 words
      - `wc -l d:/Aura/scripts/fixtures/neo4j-smoke/queries.txt` reports 5
      - `head -1 d:/Aura/scripts/fixtures/neo4j-smoke/queries.txt | grep -q '01_amatriciana'`
      - Loopback-only grep gate: `grep -E '^\s+-\s+"[^"]+:[0-9]+:[0-9]+"' d:/Aura/compose.yaml | grep -cv '127.0.0.1' = 0`
      - `wc -l d:/Aura/compose.yaml d:/Aura/Makefile d:/Aura/.env.example d:/Aura/internal/knowledge/config.go d:/Aura/internal/config/config.go` — every value ≤ 600
    </commands>
  </verify>
  <done>
    compose.yaml at repo root contains 3 services [postgres, neo4j, aura-llama-embed] all with loopback-only ports + named volumes + fail-fast env interpolation + healthchecks. Makefile extended with neo4j-* + smoke targets. .env.example extended with Neo4j + embed keys including `AURA_EMBED_DIMENSIONS=768` Amendment #18 contract. `internal/config/config.go` composite now carries Neo4j sub-struct. `internal/knowledge/config.go` materialized with 8-field struct per Pattern 3 Client signature. 5 Italian fixture docs + queries.txt committed. `go vet + build + test -race` exits 0 on `./internal/config/... ./internal/knowledge/...`. T-1.07-02 / T-1.07-03 / T-1.07-09 mitigations verified via grep gates.
  </done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: MCP subprocess client + Cypher migration runner + 0001_init.cypher + ping/status/reset + smoke harness + integration tests + CLI dispatcher</name>
  <files>
    internal/knowledge/client.go,
    internal/knowledge/migrate.go,
    internal/knowledge/migrations/0001_init.cypher,
    internal/knowledge/ping.go,
    internal/knowledge/status.go,
    internal/knowledge/reset.go,
    internal/knowledge/client_unit_test.go,
    internal/knowledge/client_test.go,
    cmd/aura/main.go,
    cmd/aura/neo4j.go,
    scripts/neo4j_smoke.sh
  </files>
  <read_first>
    @d:\Aura\.planning\phases\01-infra-db-knowledge\01-RESEARCH.md (§Pattern 3 lines 434-534 MCP subprocess JSON-RPC client; §Pattern 4 lines 537-565 Cypher migration with Postgres audit table; §Pattern 5 lines 587-621 PingEmbed dim self-test; §Code Examples lines 838-859 0001_init.cypher verbatim; §Code Examples lines 1118-1182 neo4j_smoke.sh verbatim; §Common Pitfalls #3/#4/#5/#6/#8; §Assumption Log A6 namespace flag, A10 JSON-RPC envelope shape — Wave 0 manual probe required),
    @d:\Aura\.planning\phases\01-infra-db-knowledge\01-PATTERNS.md (lines 84-165 cmd/aura/main.go surgical extension for neo4j case; lines 521-538 client.go shape with D-06 fail-fast; lines 542-567 migrate.go audit-table consumption; lines 571-583 0001_init.cypher D-08 minimal scope; lines 587-605 ping.go literal error string; lines 619-647 client_test.go mandatory structure; lines 696-709 literal error messages are LOAD-BEARING),
    @d:\Aura\.planning\phases\01-infra-db-knowledge\01-CONTEXT.md (§Decisions D-05 subcommand naming, D-06 hard-fail policy, D-08 minimal Cypher schema),
    @d:\Aura\cmd\aura\main.go (Slice 0.5 starting state — `case "db":` already added by Slice 0.5; Slice 0.7 inserts `case "neo4j":`),
    @d:\Aura\internal\db\sqlc\knowledge_migrations.sql.go (Slice 0.5 starting state — `RecordKnowledgeMigration` + `ListAppliedKnowledgeMigrations` generated bindings ready for consumption)
  </read_first>
  <behavior>
    - `internal/knowledge/client.go` per RESEARCH §Pattern 3 lines 434-534. Public `Client` struct holds `cmd *exec.Cmd`, `stdin io.WriteCloser`, `stdout *bufio.Reader`, `mu sync.Mutex` (serialize req/resp on single stdio pipe), `nextID atomic.Int64`. Public `Open(ctx context.Context, cfg *Config) (*Client, error)`: builds `exec.CommandContext(ctx, cfg.MCPBinary, args...)` with stdio flags `--db-url`/`--username`/`--password`/`--database`/`--transport stdio`. Opens StdinPipe + StdoutPipe; starts. On spawn failure returns `fmt.Errorf("spawn %s: %w (PATH check: pip install mcp-neo4j-cypher==0.6.0)", cfg.MCPBinary, err)` LITERAL (PATTERNS.md line 705). Public `Cypher(ctx context.Context, query string, params map[string]any, write bool) (json.RawMessage, error)` — serialized via mu; encodes JSON-RPC 2.0 envelope; tool name `read_neo4j_cypher` (write=false) or `write_neo4j_cypher` (write=true); on send/recv error returns wrapped error containing `MCP may have crashed — D-06 policy: fail Aura process` LITERAL. Public `Close() error` — closes stdin + waits for child exit. Private `redactNeo4jSecrets(s string) string` masks `cfg.Password` substring + `password=...` / `pass:...` regex matches (T-1.07-04 mitigation).
    - `internal/knowledge/migrate.go` per RESEARCH §Pattern 4 lines 537-565 + PATTERNS.md lines 542-567. `//go:embed migrations/*.cypher` declares `cypherFS embed.FS`. Public `Migrate(ctx context.Context, mcp *Client, pool *pgxpool.Pool) (int, error)`:
      1. Read `cypherFS.ReadDir("migrations")`; sort numerically by leading `000N` prefix.
      2. For each file: compute SHA-256 of body bytes.
      3. Query `aura.knowledge_migrations` via `sqlc.New(pool).ListAppliedKnowledgeMigrations(ctx)` (consumes Slice 0.5 generated bindings).
      4. If version already applied: compare stored checksum vs computed; if mismatch → return error `migration NNNN checksum mismatch (history corruption)`; if match → skip.
      5. Execute new migration via `mcp.Cypher(ctx, body, nil, write=true)`. Each Cypher file may have multiple statements; split by `;` or use Cypher transaction wrapping per Neo4j 5.26 conventions.
      6. On success, call `sqlc.New(pool).RecordKnowledgeMigration(ctx, sqlc.RecordKnowledgeMigrationParams{ Version: int32(version), Name: name, Checksum: sha })`.
      7. Return count of newly-applied.
    - `internal/knowledge/migrations/0001_init.cypher` per RESEARCH §Code Examples lines 838-859 verbatim. Three statements, all idempotent via `IF NOT EXISTS` (Pitfall #4 — 5.26 supports IF NOT EXISTS on CONSTRAINT, VECTOR INDEX, FULLTEXT INDEX):
      ```cypher
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
      D-08 scope: ONLY these three artifacts. NO `:Document`, `:Entity`, `:Community`, `:AgentEpisode`, `:AgentInsight`, `:UserConversation`, `:UserSnippet` — those land in Phase 11/15.
    - `internal/knowledge/ping.go` per RESEARCH §Pattern 5 lines 587-621 + PATTERNS.md lines 587-605. Public `Ping(ctx context.Context, mcp *Client, cfg *Config) error`:
      1. MCP ping: `mcp.Cypher(ctx, "CALL dbms.components() YIELD name, versions, edition", nil, write=false)`; parse response; expect `name=Neo4j Kernel`, version starts with `5.26`. If 0 rows or version mismatch → error.
      2. Embed dim self-test (Pattern 5): POST `{"input":"ping","model":"embedding"}` to `cfg.EmbedURL + "/v1/embeddings"`; decode response; assert `len(data[0].embedding) == cfg.EmbedDimensions`. If mismatch return LITERAL error: `fmt.Errorf("embedding sidecar returned dim=%d but AURA_EMBED_DIMENSIONS=%d — refuse to start (Pitfall #7 silent corruption); see prd.md amendment #18 swap runbook", actual, expectedDim)`. T-1.07-05 mitigation.
    - `internal/knowledge/status.go`: public `Status(ctx context.Context, pool *pgxpool.Pool) ([]MigrationRow, error)` — calls `sqlc.New(pool).ListAppliedKnowledgeMigrations(ctx)` (consumes Slice 0.5 generated bindings); returns rows for CLI tabulation.
    - `internal/knowledge/reset.go`: public `Reset(ctx context.Context, mcp *Client, pool *pgxpool.Pool) error` — guarded; DROP all indexes + constraints via MCP, runs `MATCH (n) DETACH DELETE`, then re-runs `Migrate()`. Dev only.
    - **B1 file split (Nyquist <30s feedback latency)**: unit-safe tests (mocked HTTP only, no live Neo4j/sidecar required) live in `internal/knowledge/client_unit_test.go` WITHOUT any build tag so they compile + run in the per-task quick gate. Live-container tests (require running Neo4j + mcp-neo4j-cypher + aura-llama-embed) live in `internal/knowledge/client_test.go` with `//go:build neo4j_integration` (or `//go:build neo4j_integration && db_integration` for audit-row checks). BOTH files declare `TestMain` calling `goleak.VerifyTestMain(m)` — but only one `TestMain` per build (the integration-tagged TestMain is excluded from the unit build by tag, and vice versa: the unit TestMain must NOT have a build tag, and the integration TestMain must have the `neo4j_integration` tag so a single build never has two TestMain symbols).
    - `internal/knowledge/client_unit_test.go` (NO build tag — unit-safe): `TestPingEmbed_DimMismatch` (mocked httptest.NewServer returns hardcoded 384-element embedding; asserts literal error string verbatim), `TestClient_StderrRedaction` (induce stderr capture with password substring; assert masked in error — T-1.07-04), `TestCypherClient_RejectsConcatenatedInjection` (assert the public client API does not expose a query-string concat path — T-1.07-01), `TestComposeYAML_LoopbackOnly` (reads compose.yaml file from repo; greps for non-127.0.0.1 host port maps; pure I/O, no containers — T-1.07-02/03). All four are pure unit tests (no subprocess spawn, no docker container).
    - `internal/knowledge/client_test.go` with `//go:build neo4j_integration` tag (combine with `db_integration` for audit-row checks). `TestMain` calls `goleak.VerifyTestMain(m)`. Live tests per PATTERNS.md lines 626-642:
      - `TestPing_ReturnsServerVersion` (integration) — live MCP + Neo4j; server version 5.26.x.
      - `TestPingEmbed_Live` (integration) — live aura-llama-embed sidecar; dim = 768.
      - `TestCypherMigrate_Idempotent` (integration) — re-run returns 0 newly-applied.
      - `TestCypherMigrate_WritesAuditRow` (integration, build tags `neo4j_integration && db_integration`) — verify aura.knowledge_migrations row exists after migrate.
      - `TestMCPCrash_FailsAura` (integration) — `cmd.Process.Kill()`; next Cypher call returns wrapped error containing literal `MCP may have crashed — D-06 policy`.
      - `TestInitCypher_AllArtifactsPresent` (integration) — `SHOW INDEXES` + `SHOW CONSTRAINTS`; verify chunk_id constraint + chunk_embedding HNSW index (M=32, ef_construction=200, dimensions=768, similarity=cosine) + chunk_text fulltext index all present.
      - (Note: `TestPingEmbed_DimMismatch`, `TestClient_StderrRedaction`, `TestCypherClient_RejectsConcatenatedInjection`, and `TestComposeYAML_LoopbackOnly` are unit-safe and live in `client_unit_test.go` WITHOUT a build tag — per B1 file split for Nyquist <30s feedback latency.)
    - `cmd/aura/main.go` surgical edit per PATTERNS.md lines 129-134: insert ONE new case `case "neo4j": runNeo4j(os.Args[2:])` between existing `"db"` and `"shell"/"serve"` branches. Update usage line to `usage: aura {serve|shell|chat <msg>|tools|db <sub>|neo4j <sub>}`. Do NOT touch tools/chat/shell/serve/db branches.
    - `cmd/aura/neo4j.go` NEW file in `package main`. Public `runNeo4j(args []string)` with inner switch over `migrate|ping|status|reset|cypher`:
      - `migrate`: opens Postgres pool (cfg.DB.URL — aura_app role) + MCP client (cfg.Neo4j); calls `knowledge.Migrate(ctx, mcp, pool)`; prints "ok: N migrations applied" or "ok: no pending migrations" (idempotent).
      - `ping`: opens MCP + calls `knowledge.Ping(ctx, mcp, &cfg.Neo4j)`; prints "ok: Neo4j 5.26.x + embed sidecar dim 768".
      - `status`: opens Postgres pool; calls `knowledge.Status`; tabulates.
      - `reset`: requires `--yes` + `AURA_RESET_YES=1`; calls `knowledge.Reset`.
      - `cypher`: read or write subcommand for smoke harness use; takes Cypher query string + --param key=value flags; calls `mcp.Cypher` directly; prints raw JSON response. Format: `aura neo4j cypher {read|write} "QUERY" [--param k=v ...]`.
    - `scripts/neo4j_smoke.sh` per RESEARCH §Code Examples lines 1118-1182 verbatim. `set -euo pipefail`. Iterates fixtures *.md → embed via curl POST `/v1/embeddings` → asserts dim match → upserts via `go run ./cmd/aura neo4j cypher write "MERGE (c:Chunk {id: $id}) SET c.text = $text, c.embedding = $emb" --param id=... --param text=... --param emb=...`. Then iterates queries.txt → embed query → `aura neo4j cypher read "CALL db.index.vector.queryNodes('chunk_embedding', 5, $q) YIELD node RETURN node.id AS id LIMIT 1" --param q=...` → tracks hit_count + latencies. Asserts `hit_count == 5` (ROADMAP SC#5 recall@5) + `p95 ≤ 30ms`. Documents `jq` requirement in preamble comment.
    - File LOC budgets: client.go ~140, migrate.go ~120, ping.go ~80, status.go ~40, reset.go ~50, 0001_init.cypher ~25, client_unit_test.go ~100 (B1), client_test.go ~200 live-tagged (split further into migrate_test.go / ping_test.go if exceeds 400, none expected to exceed 600), cmd/aura/neo4j.go ~120 (slightly larger because of cypher subcommand), scripts/neo4j_smoke.sh ~65. All ≤600.
    - **W4 executor note (optional internal split):** Task 2 is on the upper end of plan-task scope (~10 files / ~860 LOC). The atomic-commit contract (D-01) requires all artifacts land in ONE commit, so structural splitting at the plan level is forbidden. However, the executor MAY group the work into two internal sub-batches as a cognitive-load aid — sub-batch 2a = MCP client (`client.go`) + `0001_init.cypher` + the B1 unit-safe tests (`client_unit_test.go`); sub-batch 2b = migrate + ping + status + reset + `neo4j.go` subcommand + smoke harness + integration tests (`client_test.go`). Both sub-batches stage together for the single D-01 commit; this is purely a sequencing aid for the executor, not a plan restructure.
    - Wave 0 manual probe (PATTERNS.md line 535 / RESEARCH Assumption A10): BEFORE writing client.go, spawn `mcp-neo4j-cypher --transport stdio` against the running Neo4j container locally, send a literal `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_neo4j_cypher","arguments":{"query":"RETURN 1 AS one","params":{}}}}` request, capture response shape. Align Go decoder accordingly if envelope differs from RESEARCH Pattern 3. Document probe output in commit body.
  </behavior>
  <action>
    Build the MCP subprocess client + Cypher migration runner + dim self-test + CLI dispatcher + Italian smoke harness. Every Go file greenfield except `cmd/aura/main.go` (surgical extension only).

    **PREREQUISITE (manual, per W5 + Assumption A10):** Step 1 below is a Wave 0 hand-probe — an operator-driven one-time cross-checking exercise that an autonomous executor cannot perform unattended. The operator MUST run step 1 first (with `make db-up neo4j-up` containers warm), capture the JSON-RPC response envelope, and paste the captured shape into the slice commit body BEFORE the automated steps 2-14 are executed. See `01-VALIDATION.md` §Manual-Only Verifications row "Wave 0 MCP JSON-RPC envelope probe" and RESEARCH §Assumption A10 for the exact protocol. Once the probe is done, an autonomous executor can complete steps 2-14 unattended.

    1. **Wave 0 manual probe** (Assumption A10 — RESEARCH risk; **manual prerequisite per W5**): With `make db-up neo4j-up` running, spawn `mcp-neo4j-cypher --transport stdio --db-url bolt://127.0.0.1:7687 --username neo4j --password $NEO4J_PASSWORD --database neo4j` in a shell; send a `tools/call` request as JSON-RPC; capture the response envelope shape. Adjust the Go `rpcResp` struct in client.go if the envelope differs from RESEARCH §Pattern 3 lines 486-495. Document the captured response in the slice commit body.

    2. Create `internal/knowledge/client.go` per RESEARCH §Pattern 3 lines 434-534. Public `Client` struct with stdin/stdout/mutex/atomic ID; `Open(ctx, *Config)` spawns subprocess with literal install-hint error on spawn failure (PATTERNS.md line 705); `Cypher(ctx, query, params, write)` serialized via `c.mu.Lock()`; on send/recv error wraps with literal `MCP may have crashed — D-06 policy: fail Aura process` (PATTERNS.md line 532 + D-06 contract); `Close()` closes stdin + waits child. Implement private `redactNeo4jSecrets(s string) string` that masks `cfg.Password` substring + regex `password=[^\s&]+` (T-1.07-04 mitigation). Apply to all error wraps that might include stderr capture.

    3. Create `internal/knowledge/migrations/0001_init.cypher` per RESEARCH §Code Examples lines 838-859 verbatim. Three statements separated by `;`. `IF NOT EXISTS` on all three (Pitfall #4). D-08 scope lock: NO other labels.

    4. Create `internal/knowledge/migrate.go` per RESEARCH §Pattern 4 lines 537-565 + PATTERNS.md lines 547-567. `//go:embed migrations/*.cypher` declares `cypherFS embed.FS`. Public `Migrate(ctx, *Client, *pgxpool.Pool) (int, error)`:
       - Read embed.FS, sort numerically by filename prefix.
       - SHA-256 each body.
       - Query `aura.knowledge_migrations` via `sqlc.New(pool).ListAppliedKnowledgeMigrations(ctx)` (Slice 0.5 generated bindings).
       - Skip applied; checksum mismatch → error "migration NNNN checksum mismatch (history corruption)".
       - For new: execute via `mcp.Cypher(ctx, body, nil, write=true)`. Each Cypher file may contain multiple statements separated by `;` — split by semicolon outside of string literals; for Phase 1's 0001_init.cypher this is straightforward (no string literals contain `;`).
       - On success: call `sqlc.New(pool).RecordKnowledgeMigration(ctx, sqlc.RecordKnowledgeMigrationParams{Version, Name, Checksum})`.
       - Return count of newly-applied.

    5. Create `internal/knowledge/ping.go` per RESEARCH §Pattern 5 lines 587-621 + PATTERNS.md lines 587-605. Public `Ping(ctx, *Client, *Config) error`:
       - First sub-check (MCP/Neo4j): `mcp.Cypher(ctx, "CALL dbms.components() YIELD name, versions, edition", nil, write=false)`; parse response; assert version starts with `"5.26"`. Print server version on success.
       - Second sub-check (embed dim self-test, Pattern 5 / T-1.07-05): `http.NewRequestWithContext(ctx, POST, cfg.EmbedURL+"/v1/embeddings", ...)`; decode response; if `len(data) == 0 || len(data[0].embedding) != cfg.EmbedDimensions` return `fmt.Errorf("embedding sidecar returned dim=%d but AURA_EMBED_DIMENSIONS=%d — refuse to start (Pitfall #7 silent corruption); see prd.md amendment #18 swap runbook", actual, expectedDim)` LITERAL.

    6. Create `internal/knowledge/status.go` (~40 LOC) — `Status(ctx, *pgxpool.Pool) ([]knowledge.MigrationRow, error)`; uses Slice 0.5 sqlc bindings.

    7. Create `internal/knowledge/reset.go` (~50 LOC) — `Reset(ctx, *Client, *pgxpool.Pool) error`; guarded; DROP indexes + constraints via MCP, `MATCH (n) DETACH DELETE`, re-Migrate.

    8. Create the test files using the **B1 split for Nyquist <30s feedback latency**:
       - `internal/knowledge/client_unit_test.go` (NO build tag — unit-safe, runs in every per-task quick gate). `TestMain` calls `goleak.VerifyTestMain(m)`. Tests: `TestPingEmbed_DimMismatch` (use `httptest.NewServer` to mock embed sidecar returning hardcoded 384-element embedding; assert literal error string verbatim — Pattern 5 / T-1.07-05), `TestClient_StderrRedaction` (T-1.07-04), `TestCypherClient_RejectsConcatenatedInjection` (T-1.07-01), `TestComposeYAML_LoopbackOnly` (read compose.yaml from repo; grep host port maps; T-1.07-02/03). No subprocess spawn, no docker container — all four MUST pass with just `go test ./internal/knowledge/...` (no tag).
       - `internal/knowledge/client_test.go` with `//go:build neo4j_integration` tag (combine with `db_integration` for audit-row tests like `TestCypherMigrate_WritesAuditRow`). `TestMain` calls `goleak.VerifyTestMain(m)`. Live tests: `TestPing_ReturnsServerVersion`, `TestPingEmbed_Live`, `TestCypherMigrate_Idempotent`, `TestCypherMigrate_WritesAuditRow`, `TestMCPCrash_FailsAura` (use a manual MCP subprocess + `cmd.Process.Kill()` to force crash mid-test; assert next call returns wrapped error containing literal D-06 string), `TestInitCypher_AllArtifactsPresent`.
       - Only ONE `TestMain` per build: the unit `TestMain` lives in `client_unit_test.go` (no tag), the integration `TestMain` lives in `client_test.go` (tagged). Tags ensure a single build has exactly one `TestMain` symbol — no duplicate-symbol link errors.

    9. Edit `cmd/aura/main.go` surgically per PATTERNS.md lines 129-134: insert ONE new case `case "neo4j": runNeo4j(os.Args[2:])` between existing `"db"` (added by Slice 0.5) and `"shell", "serve":` branches. Update `usage()` line to `usage: aura {serve|shell|chat <msg>|tools|db <sub>|neo4j <sub>}`. DO NOT touch any other branch.

    10. Create `cmd/aura/neo4j.go` NEW file in `package main`. Public `runNeo4j(args []string)` with subcommand switch over `migrate|ping|status|reset|cypher`. Each branch loads config, opens MCP + Postgres pool as needed, calls knowledge package, prints result + exits. `cypher` subcommand supports read/write nested switch and `--param k=v` repeated flags; routes to `mcp.Cypher` directly. The `cypher` subcommand is needed by `scripts/neo4j_smoke.sh` to upsert chunks + run vector queries from shell.

    11. Create `scripts/neo4j_smoke.sh` per RESEARCH §Code Examples lines 1118-1182 verbatim. `chmod +x`. Preamble comment documents `jq` requirement (`choco install jq` or `pacman -S jq`); `set -euo pipefail` fails-loud if missing.

    12. Post-edit Gate 2 (mandatory): `go vet ./...`, `go build ./...`, `go test ./internal/config/... ./internal/db/... ./internal/knowledge/... -race -count=1` (unit only). Container-gated integration: `make neo4j-migrate && go test -race -tags 'db_integration neo4j_integration' ./internal/knowledge/... -count=1`.

    13. Smoke harness execution (Gate 3 evidence): `make smoke` — must print final line `ok: recall@5 = 5/5, p95 = N ms` with N ≤ 30. Capture into commit body.

    14. Coverage verification: `go test ./internal/knowledge/... -tags 'db_integration neo4j_integration' -race -coverprofile=cover.out && go tool cover -func=cover.out | grep total:` returns ≥ 0.60 (integration); pure unit ≥ 0.75.
  </action>
  <verify>
    <automated>cd d:/Aura && go vet ./... && go build ./... && go test ./internal/knowledge/... -race -count=1 -run "TestPingEmbed_DimMismatch|TestClient_StderrRedaction|TestCypherClient_RejectsConcatenatedInjection|TestComposeYAML_LoopbackOnly"</automated>
    <commands>
      - `test -f d:/Aura/internal/knowledge/client.go && test -f d:/Aura/internal/knowledge/migrate.go && test -f d:/Aura/internal/knowledge/ping.go && test -f d:/Aura/internal/knowledge/migrations/0001_init.cypher && test -f d:/Aura/internal/knowledge/client_unit_test.go && test -f d:/Aura/internal/knowledge/client_test.go` (exit 0 — B1 split: unit-safe file present alongside live-tagged file)
      - `test -f d:/Aura/cmd/aura/neo4j.go && test -f d:/Aura/scripts/neo4j_smoke.sh && test -x d:/Aura/scripts/neo4j_smoke.sh` (exit 0)
      - `grep -q 'CREATE VECTOR INDEX chunk_embedding' d:/Aura/internal/knowledge/migrations/0001_init.cypher` (D-08 minimal scope)
      - `grep -q 'vector.dimensions.*768' d:/Aura/internal/knowledge/migrations/0001_init.cypher` (Amendment #18 dim contract)
      - `grep -q 'vector.hnsw.m.*32' d:/Aura/internal/knowledge/migrations/0001_init.cypher` (Amendment #20 M=32)
      - `grep -q 'vector.hnsw.ef_construction.*200' d:/Aura/internal/knowledge/migrations/0001_init.cypher` (Amendment #20 ef=200)
      - `grep -q 'CREATE FULLTEXT INDEX chunk_text' d:/Aura/internal/knowledge/migrations/0001_init.cypher`
      - `grep -q 'CREATE CONSTRAINT chunk_id' d:/Aura/internal/knowledge/migrations/0001_init.cypher`
      - D-08 scope guard: `grep -cE '(Document|Entity|Community|AgentEpisode|AgentInsight|UserConversation|UserSnippet)' d:/Aura/internal/knowledge/migrations/0001_init.cypher` returns 0
      - `grep -q '//go:embed migrations/\*\.cypher' d:/Aura/internal/knowledge/migrate.go`
      - `grep -q 'ListAppliedKnowledgeMigrations' d:/Aura/internal/knowledge/migrate.go` (Slice 0.5 sqlc consumption)
      - `grep -q 'RecordKnowledgeMigration' d:/Aura/internal/knowledge/migrate.go`
      - `grep -q 'MCP may have crashed — D-06 policy: fail Aura process' d:/Aura/internal/knowledge/client.go` (D-06 literal)
      - `grep -q 'PATH check: pip install mcp-neo4j-cypher==0.6.0' d:/Aura/internal/knowledge/client.go` (install hint per PATTERNS.md line 705)
      - `grep -q 'embedding sidecar returned dim=' d:/Aura/internal/knowledge/ping.go`
      - `grep -q 'refuse to start (Pitfall #7 silent corruption)' d:/Aura/internal/knowledge/ping.go` (literal error contract per PATTERNS.md line 704)
      - `grep -q 'amendment #18 swap runbook' d:/Aura/internal/knowledge/ping.go`
      - `grep -q 'case "neo4j":' d:/Aura/cmd/aura/main.go` (dispatcher extended)
      - `grep -q 'usage: aura {serve|shell|chat <msg>|tools|db <sub>|neo4j <sub>}' d:/Aura/cmd/aura/main.go`
      - `grep -q '//go:build neo4j_integration' d:/Aura/internal/knowledge/client_test.go` (live-tagged file)
      - `grep -L '//go:build' d:/Aura/internal/knowledge/client_unit_test.go | grep -q client_unit_test.go` (unit-safe file has NO build tag — B1)
      - `grep -q 'goleak.VerifyTestMain' d:/Aura/internal/knowledge/client_test.go`
      - `grep -q 'goleak.VerifyTestMain' d:/Aura/internal/knowledge/client_unit_test.go`
      - `grep -q 'TestPingEmbed_DimMismatch' d:/Aura/internal/knowledge/client_unit_test.go` (moved per B1)
      - `grep -q 'TestClient_StderrRedaction' d:/Aura/internal/knowledge/client_unit_test.go` (moved per B1)
      - `grep -q 'TestCypherClient_RejectsConcatenatedInjection' d:/Aura/internal/knowledge/client_unit_test.go` (moved per B1)
      - `grep -q 'TestComposeYAML_LoopbackOnly' d:/Aura/internal/knowledge/client_unit_test.go` (moved per B1)
      - `grep -q 'TestMCPCrash_FailsAura' d:/Aura/internal/knowledge/client_test.go`
      - `grep -q 'TestInitCypher_AllArtifactsPresent' d:/Aura/internal/knowledge/client_test.go`
      - `grep -q 'recall@5' d:/Aura/scripts/neo4j_smoke.sh`
      - `grep -q 'p95' d:/Aura/scripts/neo4j_smoke.sh`
      - `bash -n d:/Aura/scripts/neo4j_smoke.sh` (shell syntax)
      - Container-gated: `cd d:/Aura && make neo4j-migrate` exits 0 (Pattern 4 audit-row write succeeds)
      - Container-gated: re-run `make neo4j-migrate` exits 0 with stdout containing "0 newly applied" or "no pending" (ROADMAP SC#4 idempotency)
      - Container-gated: `cd d:/Aura && go test -race -tags 'db_integration neo4j_integration' ./internal/knowledge/... -count=1` exits 0
      - Container-gated: `cd d:/Aura && make smoke` exits 0 with stdout containing `ok: recall@5 = 5/5, p95 = ` followed by a number ≤ 30 (ROADMAP SC#5)
      - `wc -l d:/Aura/internal/knowledge/*.go d:/Aura/internal/knowledge/migrations/0001_init.cypher d:/Aura/cmd/aura/neo4j.go d:/Aura/scripts/neo4j_smoke.sh` — every value ≤ 600
    </commands>
  </verify>
  <done>
    `internal/knowledge/` package operational with MCP subprocess client (Pattern 3), Cypher migration runner consuming Slice 0.5 sqlc bindings (Pattern 4), and embed dim self-test (Pattern 5). `0001_init.cypher` lands the D-08 minimal scope (chunk_id UNIQUE + HNSW 768d M=32 ef=200 cosine + chunk_text fulltext). CLI `aura neo4j {migrate|ping|status|reset|cypher}` operational. `make smoke` returns recall@5 = 5/5 + p95 ≤ 30ms on Italian corpus (ROADMAP SC#5 ✓). All literal error strings (D-06 MCP crash, Pattern 5 dim mismatch, install hint) asserted by tests. No file exceeds 600 LOC. INFRA-02 coverage thresholds met. Wave 0 MCP manual-probe output documented in commit body.
  </done>
</task>

<task type="checkpoint:human-verify" gate="blocking-human">
  <name>Task 3: License legitimacy gate — verify mcp-neo4j-cypher Apache 2.0</name>
  <what-built>
    Slice 0.7 introduces a runtime dependency on `mcp-neo4j-cypher` 0.6.0 — installed via `pip` from PyPI. RESEARCH Pitfall #6 + Open Question 1 document that the PyPI metadata `License:` field is empty, the LICENSE file did NOT respond to WebFetch during research, and the upstream license is claimed (by PRD + CONTEXT.md) to be Apache 2.0 but **not verified** by upstream metadata. Per OWASP supply-chain discipline + CLAUDE.md PRD-first principle, this is a `[ASSUMED]` package that requires explicit checkpoint verification before merging.

    The full upstream package legitimacy posture was captured in the Package Legitimacy Audit table in RESEARCH.md lines 161-172. mcp-neo4j-cypher is the ONE entry there flagged as `Approved with caveat — see Pitfall #6`.
  </what-built>
  <how-to-verify>
    Run the following commands and capture output into the slice commit body as evidence:

    1. Query GitHub for the upstream LICENSE file:
       ```bash
       gh api repos/neo4j-contrib/mcp-neo4j/contents/LICENSE \
         | jq -r '.content' | base64 -d | head -10
       ```
       Expected: literal text `Apache License` on the first line (or equivalent header for Apache 2.0).

    2. Verify the install-time package source matches:
       ```bash
       pip show mcp-neo4j-cypher | grep -E '^(Name|Version|Home-page|Author):'
       ```
       Expected: `Name: mcp-neo4j-cypher`, `Version: 0.6.0`, Home-page pointing to `neo4j-contrib/mcp-neo4j` (or related neo4j-contrib org repo).

    3. (Optional sanity) Run `go-licenses report ./...` if installed; the result should NOT flag mcp-neo4j-cypher (it won't appear there because it's a Python subprocess, not a Go dep) — but the report should be clean for the Go-side deps (pgx/v5, golang-migrate, godotenv, goleak, uuid).

    Decision rule:
    - If the LICENSE first line says `Apache License` (or `MIT License` or `BSD 3-Clause License` — any of these three permissive licenses is acceptable per OWASP guidance): **APPROVE the slice commit**, capture the output in commit body.
    - If the LICENSE says anything else (GPL, AGPL, LGPL, proprietary, or the file is still 404): **HALT the slice commit**, escalate to PRD-amendment scope — the discipline ban on native Go Neo4j drivers (CLAUDE.md §Project scope row 14) locks Aura into this subprocess; a non-permissive license would require either re-evaluating the discipline ban OR finding an alternative Cypher-MCP server.

    Slice 0.7 commit body MUST include:
    ```
    mcp-neo4j-cypher license: Apache 2.0 (or MIT / BSD-3-Clause) verified via gh api repos/neo4j-contrib/mcp-neo4j/contents/LICENSE on YYYY-MM-DD.
    First line of LICENSE file: "Apache License"
    pip show mcp-neo4j-cypher Home-page: https://github.com/neo4j-contrib/mcp-neo4j
    ```

    Resume signal: type "approved: license verified Apache 2.0" (or MIT/BSD-3 substitution) OR "halt: license is <X> — escalating to PRD amendment".
  </how-to-verify>
  <resume-signal>
    Operator types one of:
    - `approved: license verified Apache 2.0` (or MIT / BSD-3-Clause)
    - `halt: license is <X> — escalating to PRD amendment`

    No auto-advance allowed. This checkpoint is blocking-human per OWASP supply-chain discipline + RESEARCH Pitfall #6 explicit recommendation.
  </resume-signal>
  <done>
    License evidence captured in slice commit body. If approved, slice commit proceeds to Task 4. If halted, planner is notified and PRD-amendment workflow takes over.
  </done>
</task>

<task type="auto">
  <name>Task 4: PRD amendment (acceptance row 182) + ROADMAP amendment (SC#4 naming) + slice commit</name>
  <files>
    prd.md,
    .planning/ROADMAP.md
  </files>
  <read_first>
    @d:\Aura\prd.md (§Slice 0.7 acceptance section around row 182 — the `aura knowledge ping validates sidecar /health returns {"dim":768}` claim that needs correction; §Amendment #18 dim contract for cross-reference),
    @d:\Aura\.planning\ROADMAP.md (Phase 1 SC#4 line 61 — the `aura knowledge ping` literal that needs correction per D-05),
    @d:\Aura\.planning\phases\01-infra-db-knowledge\01-CONTEXT.md (§Decisions D-05 subcommand naming; §Drifts to flag during planning),
    @d:\Aura\.planning\phases\01-infra-db-knowledge\01-RESEARCH.md (§Summary risk 1 — PRD acceptance row 182 is wrong; §Pattern 5 dim probe replacement)
  </read_first>
  <behavior>
    - PRD amendment (one-line edit): locate `prd.md` §Slice 0.7 acceptance row ~182 that claims `aura knowledge ping validates sidecar /health returns {"dim":768} matching AURA_EMBED_DIMENSIONS`. Replace the substring `/health returns {"dim":768}` with `/v1/embeddings round-trip returns 768d (Pattern 5 dim probe)`. Final form: `aura neo4j ping validates sidecar /v1/embeddings round-trip returns 768d (Pattern 5 dim probe) matching AURA_EMBED_DIMENSIONS`. Rationale: literal `/health` response is `{"status":"ok"}` not `{"dim":N}` (RESEARCH §Pattern 5 ground truth; Pitfall #5 mitigation).
    - PRD amendment ALSO updates `aura knowledge ping` → `aura neo4j ping` in the same row if the text says `aura knowledge ping` (cross-check with CONTEXT.md D-05).
    - ROADMAP amendment (one-line edit): locate `.planning/ROADMAP.md` Phase 1 SC#4 line containing `aura knowledge ping`. Replace `aura knowledge ping` with `aura neo4j ping`. Same row: the `/health` claim also needs correction here if present — replace `/health` returns `{"dim":768}` with `/v1/embeddings round-trip returns 768d`. Per D-05.
    - These are PRD/ROADMAP corrections, NOT code edits. They land in the same atomic slice commit per D-01 (the Slice 0.7 commit body lists them in the PRD-amendment / ROADMAP-amendment scope blocks).
    - Final slice commit per D-01 + PRD §Slice 0.7 commit template (lines 215-235):
      ```
      slice 0.7: neo4j community + mcp-neo4j-cypher + cypher migrations

      [body: enumerate every artifact created; note D-05 ROADMAP correction (aura knowledge ping → aura neo4j ping); note PRD acceptance row 182 amendment (/health → {dim} replaced with /v1/embeddings round-trip per Pattern 5); note T-1.07-SC license evidence (mcp-neo4j-cypher Apache 2.0 verified via gh api ...); note D-06 hard-fail policy enforced in client.go; note T-1.07-02 / T-1.07-03 loopback-only port gates verified; note Pattern 3 Wave-0 MCP probe captured envelope shape <paste minimal JSON-RPC response>; note D-08 minimal Cypher schema scope honored; note RESEARCH §Pattern 3/4/5 anchors and PATTERNS.md analogs followed]

      Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
      ```
    - Update `.planning/STATE.md` `current_position` if convention requires (orchestrator may handle this in phase-complete pass — confirm via execution context).
  </behavior>
  <action>
    Land the two documentation amendments + the atomic slice commit.

    1. Edit `prd.md`: locate the Slice 0.7 acceptance row that mentions `/health returns {"dim":768}` (approximately line 182 per CONTEXT.md `<canonical_refs>`). Replace the substring as follows:
       - From: `aura knowledge ping ... /health returns {"dim":768} matching AURA_EMBED_DIMENSIONS`
       - To: `aura neo4j ping ... /v1/embeddings round-trip returns 768d (Pattern 5 dim probe) matching AURA_EMBED_DIMENSIONS`
       Use `grep -n 'sidecar.*health' prd.md` first to locate the exact line; verify it's the §Slice 0.7 row, not a different slice's acceptance.

    2. Edit `.planning/ROADMAP.md`: locate Phase 1 SC#4 line containing `aura knowledge ping` (line ~61). Replace `aura knowledge ping` with `aura neo4j ping` per D-05. If the same SC#4 row mentions `/health` returning `{"dim":768}`, also correct it to `/v1/embeddings round-trip returns 768d` consistent with the PRD amendment.

    3. Verify both files:
       - `grep -c 'aura knowledge ping' .planning/ROADMAP.md` returns 0
       - `grep -c 'aura neo4j ping' .planning/ROADMAP.md` returns ≥ 1
       - `grep -c '/health returns.*dim' prd.md` returns 0 (the wrong claim removed)
       - `grep -c '/v1/embeddings round-trip returns 768d' prd.md` returns ≥ 1

    4. Update `.planning/phases/01-infra-db-knowledge/01-VALIDATION.md` per-task verification table — fill rows for Task 1, Task 2, Task 3 (checkpoint), Task 4 with: Plan ID `03`, Wave `2`, Requirement `INFRA-02`, Threat Refs (per task), Test Type, Automated Command (from `<verify>` above), File Exists ✅, Status ⬜ pending → updated to ✅ green after smoke runs cleanly. Set `nyquist_compliant: true` in VALIDATION.md frontmatter once all rows have automated commands OR Wave 0 dependencies listed AND Task 2 smoke run is green.

    5. Run final Gate 3 evidence collection:
       - `cd d:/Aura && make neo4j-migrate` exits 0
       - `cd d:/Aura && make smoke` exits 0; capture stdout `ok: recall@5 = 5/5, p95 = N ms`
       - `cd d:/Aura && go test -race -tags 'db_integration neo4j_integration' ./internal/... -count=1` exits 0
       - Coverage report: `go test -tags 'db_integration neo4j_integration' -coverprofile=cover.out ./internal/knowledge/... && go tool cover -func=cover.out | tail -1` ≥ 60%

    6. Stage and commit the slice atomically (single commit per D-01) including:
       - All Slice 0.7 Go source files
       - 0001_init.cypher migration
       - compose.yaml + Makefile + .env.example extensions
       - Italian fixture corpus + queries.txt + neo4j_smoke.sh
       - PRD amendment (acceptance row 182) — this commit
       - ROADMAP amendment (Phase 1 SC#4) — this commit
       - VALIDATION.md per-task table fills

       Commit message body MUST include the items listed in <behavior> step 4 PLUS the captured license evidence from Task 3 PLUS the Wave 0 MCP probe envelope sample from Task 2 step 1.

    7. DO NOT `git push`. Master-direct workflow per memory `feedback_master_direct_workflow`: commit on master locally; user pushes when ready.
  </action>
  <verify>
    <automated>cd d:/Aura && ! grep -q 'aura knowledge ping' .planning/ROADMAP.md && grep -q 'aura neo4j ping' .planning/ROADMAP.md && grep -q '/v1/embeddings round-trip returns 768d' prd.md</automated>
    <commands>
      - `grep -c 'aura knowledge ping' d:/Aura/.planning/ROADMAP.md` returns 0 (D-05 applied)
      - `grep -c 'aura neo4j ping' d:/Aura/.planning/ROADMAP.md` returns ≥ 1
      - `grep -c '/health returns.*dim' d:/Aura/prd.md` returns 0 (wrong claim removed)
      - `grep -c '/v1/embeddings round-trip returns 768d' d:/Aura/prd.md` returns ≥ 1 (Pattern 5 replacement in place)
      - `grep -c 'aura knowledge ping' d:/Aura/prd.md` returns 0 (Slice 0.7 acceptance corrected if the wrong subcommand was there)
      - `git log -1 --format='%s' | grep -q '^slice 0.7:'` (slice atomic commit landed; subject matches PRD template)
      - `git log -1 --format='%B' | grep -q 'Co-Authored-By: Claude Opus 4.7 (1M context)'` (trailer present)
      - `git log -1 --format='%B' | grep -qi 'Apache' || git log -1 --format='%B' | grep -qiE '(MIT|BSD-3)'` (license evidence in body)
      - `git log -1 --format='%B' | grep -q 'Pattern 5'` (PRD amendment scope in body)
      - `git log -1 --format='%B' | grep -q 'D-05'` (ROADMAP amendment scope in body)
      - VALIDATION.md frontmatter check: `grep -q '^nyquist_compliant: true' d:/Aura/.planning/phases/01-infra-db-knowledge/01-VALIDATION.md`
    </commands>
  </verify>
  <done>
    PRD amendment landed: Slice 0.7 acceptance row 182 corrected from `/health → {"dim":768}` to `/v1/embeddings round-trip returns 768d (Pattern 5 dim probe)`. ROADMAP amendment landed: Phase 1 SC#4 `aura knowledge ping` → `aura neo4j ping` per D-05. VALIDATION.md per-task table filled; `nyquist_compliant: true` set. Single atomic Slice 0.7 commit on master with: code + cypher + compose/Makefile/.env extensions + smoke harness + 2 doc amendments + Co-Authored-By trailer + license evidence + Wave 0 probe sample. Phase 1 is COMPLETE.
  </done>
</task>

</tasks>

<verification>

## Slice 0.7 Phase Verification (after all 4 tasks complete)

```bash
cd d:/Aura

# Quick gate (per-task, no containers)
go vet ./...
go build ./...
go test ./internal/... -race -count=1   # unit-only, build-tag-excluded integration skips

# Full integration gate (containers warm)
make db-up neo4j-up
go test -tags 'db_integration neo4j_integration' -race -count=1 ./internal/...
# Expected: all knowledge tests PASS — TestPing_ReturnsServerVersion, TestPingEmbed_DimMismatch (unit, mocked),
#   TestPingEmbed_Live, TestCypherMigrate_Idempotent, TestCypherMigrate_WritesAuditRow, TestMCPCrash_FailsAura,
#   TestInitCypher_AllArtifactsPresent, TestClient_StderrRedaction, TestCypherClient_RejectsConcatenatedInjection,
#   TestComposeYAML_LoopbackOnly

# CLI smoke
go run ./cmd/aura neo4j migrate   # exits 0, "ok: 1 migration applied (0001_init)"
go run ./cmd/aura neo4j migrate   # exits 0, "no pending migrations" (ROADMAP SC#4 first part)
go run ./cmd/aura neo4j ping      # exits 0, "ok: Neo4j 5.26.x + embed sidecar dim 768" (ROADMAP SC#4 second part)
go run ./cmd/aura neo4j status    # exits 0, tabulates aura.knowledge_migrations rows

# Italian smoke (ROADMAP SC#5)
make smoke
# Expected: "ok: recall@5 = 5/5, p95 = N ms" with N ≤ 30

# Pattern 5 dim mismatch (T-1.07-05)
# Mocked via TestPingEmbed_DimMismatch — assertion verbatim error string

# D-06 fail-fast policy (T-1.07-07)
# Verified by TestMCPCrash_FailsAura — kills MCP, asserts D-06 literal in error

# Loopback-only ports (T-1.07-02 / T-1.07-03)
grep -E '^\s+-\s+"[^"]+:[0-9]+:[0-9]+"' compose.yaml | grep -cv '127.0.0.1'   # returns 0

# Documentation amendments (D-02, D-05, Pattern 5 PRD correction)
grep -c 'sandbox/compose.yaml' prd.md                                          # returns 0
grep -c 'aura knowledge ping' .planning/ROADMAP.md                             # returns 0
grep -c '/v1/embeddings round-trip returns 768d' prd.md                        # returns ≥ 1

# Structural: every Slice 0.7 file ≤600 LOC
wc -l internal/knowledge/*.go internal/knowledge/migrations/*.cypher cmd/aura/neo4j.go scripts/neo4j_smoke.sh
# Expected: every value ≤ 600

# Wave 0 manual probe (PATTERNS.md line 535)
# Documented in commit body; not re-runnable as automated assertion

# License evidence (T-1.07-SC)
# Captured in commit body via gh api; verified by `git log -1 --format='%B' | grep -qiE '(Apache|MIT|BSD-3)'`
```

</verification>

<success_criteria>

Slice 0.7 closes when:

- [ ] All artifacts in `files_modified` exist at the specified paths
- [ ] `go vet ./... && go build ./... && go test ./internal/... -race` exits 0 (unit gate)
- [ ] `go test -tags 'db_integration neo4j_integration' -race ./internal/...` exits 0 (integration gate, requires containers)
- [ ] Coverage ≥ 75% unit on `internal/knowledge`; ≥ 60% integration on `internal/knowledge` (PRD Gate 3 thresholds)
- [ ] `aura neo4j migrate` idempotent (ROADMAP SC#4 first part)
- [ ] `aura neo4j ping` returns Neo4j 5.26.x version + embed sidecar dim 768 verification via Pattern 5 probe (ROADMAP SC#4 second part — corrected per Pattern 5 PRD amendment)
- [ ] `make smoke` returns recall@5 = 5/5 + p95 ≤ 30ms on Italian fixture corpus (ROADMAP SC#5)
- [ ] Literal D-06 error string asserted by `TestMCPCrash_FailsAura`
- [ ] Literal Pattern 5 dim-mismatch error string asserted by `TestPingEmbed_DimMismatch`
- [ ] Literal install-hint error asserted by client tests
- [ ] T-1.07-02 / T-1.07-03 loopback-only port grep gate passes
- [ ] T-1.07-04 stderr redaction asserted
- [ ] T-1.07-05 dim self-test asserted
- [ ] T-1.07-07 D-06 fail-fast asserted
- [ ] T-1.07-SC license verification checkpoint approved (blocking-human gate cleared; evidence in commit body)
- [ ] D-08 minimal Cypher scope honored (only `:Chunk` artifacts; no `:Document`/`:Entity`/etc.)
- [ ] PRD amendment landed: Slice 0.7 acceptance row 182 corrected
- [ ] ROADMAP amendment landed: Phase 1 SC#4 `knowledge` → `neo4j`
- [ ] D-02 amendment carried forward (verified by `grep -c 'sandbox/compose.yaml' prd.md` returns 0)
- [ ] Wave 0 manual MCP probe output captured in commit body
- [ ] No file in this slice exceeds 600 LOC (god-class ban)
- [ ] Single atomic commit per D-01 with PRD-verbatim Slice 0.7 commit template + Co-Authored-By trailer
- [ ] `01-VALIDATION.md` per-task table updated; `nyquist_compliant: true` set in frontmatter

Phase 1 closes when both Slice 0.5 and Slice 0.7 are complete and the 5 ROADMAP Phase 1 Success Criteria (SC#1-5) are all green:
- SC#1: idempotent `aura db migrate` ✓ (Slice 0.5)
- SC#2: role separation enforced ✓ (Slice 0.5)
- SC#3: restore drill < 90s ✓ (Slice 0.5)
- SC#4: `aura neo4j ping` returns Neo4j version + sidecar dim ✓ (Slice 0.7, corrected per Pattern 5)
- SC#5: Italian smoke recall@5 = 5/5 + p95 ≤ 30ms ✓ (Slice 0.7)

</success_criteria>

<output>
On task completion, write `.planning/phases/01-infra-db-knowledge/03-04-SUMMARY.md` documenting:
- Artifacts created and LOC actually used per file
- Test results (which tests run, coverage %, any flakes — especially TestPingEmbed_Live which depends on first-boot HF model download timing)
- T-1.07-SC license verification evidence (exact LICENSE first line, gh api command output)
- Wave 0 MCP probe captured response envelope (verbatim JSON-RPC sample)
- D-05 + Pattern 5 PRD/ROADMAP amendment line-level diffs
- Commit SHA + body
- Any deviations from this plan (especially: Cypher migration statement splitting behavior if 0001_init.cypher needed transactional wrapping; mcp-neo4j-cypher CLI flag drift if `0.6.x` patch released during execution)
- Phase 1 closure summary: all 5 ROADMAP SC marked ✓; Phase 2 (Agent Cornerstone) is the next phase per ROADMAP execution order
</output>
