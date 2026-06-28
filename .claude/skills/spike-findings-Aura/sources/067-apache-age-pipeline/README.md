---
id: 067-apache-age-pipeline
title: Apache AGE as a Neo4j replacement for the full Aura graph pipeline
date: 2026-06-20
status: VALIDATED (verdict: STAY with Neo4j)
type: standard
tags: [neo4j, apache-age, pgvector, graph, agent-memory, graphrag, gds, migration, due-diligence]
related: docs/research/2026-06-20-neo4j-alternatives-puppygraph-turingdb-apache-age.md
---

# Spike 067 — Apache AGE on the full Aura graph pipeline

## Idea

Companion to the desk-research report (`docs/research/2026-06-20-neo4j-alternatives-puppygraph-turingdb-apache-age.md`), which screened PuppyGraph / TuringDB / Apache AGE and concluded AGE was the only candidate worth a hands-on PoC. This spike **empirically** runs Aura's real graph pipeline against a live Apache AGE + pgvector stack and grades it against that report's §5 success criteria. Operator pointer mid-spike: `https://github.com/rioriost/age_mcp_server` (an AGE MCP server) and "we can modify too `D:\tmp\agent-memory`" (the agent-memory fork is fair game).

**The bar a replacement must clear** (Aura's actual Neo4j usage, ground-truthed from the live `aura-neo4j` container + the code):
1. Durable writable graph store (agent-memory subgraph persists across sessions).
2. Native vector index — 384d cosine HNSW (Granite-97m embeddings; GraphRAG hybrid retrieval). *Note: the live instance has 9 vector indexes; CLAUDE.md's "768d" is stale — the sidecar + migrations are **384d**.*
3. GDS algorithm library — Leiden community detection + PageRank (memory consolidation).
4. Cypher dialect + Bolt protocol — `mcp-neo4j-cypher` (LLM↔graph) + the `neo4j-labs/agent-memory` Bolt-driver fork.

Two consumers touch Neo4j: **(A)** Aura's read-only graph explorer (`internal/knowledge/graphview_intent.go`, 4 compiled queries) and **(B)** the `aura-agent-memory-mcp` fork (`docker/agent-memory/.../graph/queries.py`, ~80 Cypher templates).

## Harness / stack

- **Image:** `Dockerfile.age-pgvector` = `FROM apache/age:latest` (AGE **1.7.0** on **PostgreSQL 18.1**, Debian 13, pgdg apt preconfigured) + `apt-get install postgresql-18-pgvector` → **pgvector 0.8.2**. No source build needed.
- **Container:** throwaway `age-spike` on `127.0.0.1:15432`, no volume. (Removed at spike end; production compose untouched.)
- **Baseline:** the live `aura-neo4j` (5.26.26-community) container, queried in-container via `$AURA_NEO4J_HC_PW` (secret never leaves the container).
- **Embeddings:** real Granite-97m 384d from the live `aura-llama-embed` sidecar (`:8081/v1/embeddings`).
- SQL probes (`01..04`) + an embeddings generator (`03_gen_hybrid_sql.ps1`, invariant-culture floats — host is it-IT) are the artifacts; each runs with `ON_ERROR_STOP off` + `\echo` markers so **every statement's pass/fail is captured ground truth**.
- Docker driven from **PowerShell** (Git-Bash MSYS mangles `/tmp/...` paths and breaks the spaced Docker path — conventions spikes 025/059).

## Results

### Substrate — ✅ trivial
AGE 1.7.0 + pgvector 0.8.2 coexist in one PG18 database (`CREATE EXTENSION age; CREATE EXTENSION vector;`).

### (A) Graph-explorer reads (4 queries) — ⚠ portable WITH rework; the seam breaks, not the bodies
| graphview query | AGE result |
|---|---|
| node/rel projection (`compileSeed/Expand/Overview`) | ❌ as-written (`elementId()`, `apoc.convert.toJson(labels())`) → *invalid indirection*; ✅ **ported** to `id()` + `label()` (singular) + `coalesce()` + `properties()` + `type(r) IN [...]` + `EXISTS{}` (works) + var-length (works) |
| `compileSchema` (`CALL db.labels()/relationshipTypes()/propertyKeys()`) | ❌ none exist → must rewrite against `ag_catalog.ag_label` (and there is no Cypher rel-type/prop-key catalog — full rewrite) |
| **multi-label** nodes (`:Entity:OBJECT:VEHICLE`, POLE+O) | ❌ *syntax error at ":"* — **AGE allows ONE label per node** |
| **param-map** (D-05: `Read(query, params)`) | ❌ `$param` cannot be inlined → requires `PREPARE … cypher(g,$$…$$,$1)` with one agtype blob **and an explicit `AS (col agtype,…)` column list per query** |

Net: the 4 read queries are ~portable (3/4 bodies with moderate rewrite; schema query is a full rewrite), but the **contract changes** (single label not array, numeric `graphid` not string `elementId`) and Aura's generic `knowledge.Client.Read` seam + the `mcp-neo4j-cypher` interface must be **replaced** (no Bolt, no inline params).

### (B) Agent-memory (~80 queries) — ❌ NOT a port, a reimplementation. **9 blocking gaps:**
`datetime()` ❌ · `duration({days})` ❌ · `point()/point.distance` ❌ · `MERGE … ON CREATE/ON MATCH SET` ❌ (*syntax error at "ON"*) · `FOREACH` ❌ · `CALL (a,b){…}` scoped subquery ❌ · `db.index.vector.queryNodes` ❌ (8+ queries) · `CREATE VECTOR INDEX` ❌ · `apoc.map.removeKeys` ❌ · `SHOW INDEXES` ❌. Only var-length paths + basic list fns (`UNWIND/range/collect/head/toLower/substring`) port. Several gaps have **no AGE substitute** (no temporal type, no spatial type) → schema-level workarounds (epoch bigints, SQL `ON CONFLICT` upserts *outside* `cypher()`, all vector search on pgvector).

### Vector / GraphRAG hybrid — ✅ feasible, but re-architected
Real Granite embeddings in pgvector ranked correctly for query *"electric vehicle"*: `a red electric vehicle` 0.0835 < `Tesla…electric car` 0.1251 < `Davide the owner` 0.2238 (cat correctly excluded). The top hit's `id` bridges into AGE to pull its graph neighborhood. **But** it's a **two-step app-level join on a shared business key** (vector search → ids → graph expand), not Neo4j's single composed `db.index.vector.queryNodes → traversal`. GraphRAG retrieval would be rebuilt.

### GDS (Leiden / PageRank) — ❌ BLOCKER
`gds.pageRank` / `gds.leiden` → *syntax error*. AGE has **zero** graph-algorithm library. Degree centrality is hand-rollable; community detection + PageRank (Aura's memory consolidation engine) need an external tool (e.g. Apache MADlib on relational edge tables) or app-side code — not native.

### Backup (pg_dump round-trip) — ✅ works, with a silent-data-loss SHARP EDGE
Custom-format `pg_dump`/`pg_restore` of the AGE graph **survived** (6/6 nodes restored) — better than the feared AGE-catalog gotcha. **BUT** the pgvector table created under AGE's default `search_path` (`ag_catalog, "$user", public`) landed in **`ag_catalog`** and was **silently dropped from the dump** (`vector` extension restored, table gone). Fix proven: `public.chunk_vec2` (explicitly schema-qualified) round-trips intact (graph 6 + vec 1). **App tables MUST be qualified to `public`, never rely on AGE's search_path, or vector data vanishes from backups.**

### Latency — ⚠ not validly testable here
On the 23-node-scale graph, AGE warm 2-hop is sub-ms; first call per query-shape is 170–341ms (cold plan). No valid scale signal — the §5 p95-within-1.5× criterion needs a representative dataset in the real PoC.

### MCP path (`rioriost/age_mcp_server`) — ⚠ feasible but unmaintained
MIT, Python 3.13, psycopg, read + write (`--allow-write`), exposes query/schema/create-graph/nodes/relationships — but its README is marked **"Obsoleted"** (4 stars). The AGE LLM↔graph path is *feasible*, but there's no maintained off-the-shelf server → Aura would fork/build its own (as it already vendors agent-memory).

## §5 success-criteria scorecard
| # | Criterion | Result |
|---|---|---|
| 1 | Query portability ≥8/10 | ❌ FAIL — graph-explorer reads port with rework; agent-memory's deep set does not (9 gaps) |
| 2 | p95 within 1.5× Neo4j | ⚠ UNVALIDATED (tiny graph) |
| 3 | GraphRAG Recall@5/nDCG@10 within 5% | ⚠ PARTIAL — hybrid feasible (qualitatively sane); full harness not run |
| 4 | GDS path ≤2 days or blocker | ❌ **BLOCKER** — no GDS at all |
| 5 | LLM↔graph (MCP) read+write | ⚠ PARTIAL — feasible but server "Obsoleted" → fork/build |
| 6 | single pg_dump round-trip | ✅ PASS (with the public-schema caveat) |

**Decision gate (from the report):** "proceed only if 2,3,6 pass AND 1,4,5 at most moderate; any blocker in 3/4/5 → stop." Criterion 4 is a hard blocker and 1 fails → **STOP.**

## Verdict — STAY with Neo4j

Apache AGE **can** host a graph + vectors in one Postgres, and Aura's read-only graph explorer is portable with moderate rework. But the **heart of the pipeline — the agent-memory subgraph — is a reimplementation, not a migration**: it leans on temporal types, spatial types, in-graph vector search, `MERGE` upsert semantics, conditional writes, and GDS algorithms that AGE simply does not have. Migrating means rewriting ~80 queries, replacing Bolt/MCP/param-map seams, re-architecting GraphRAG onto a two-step pgvector join, sourcing a PageRank/Leiden substitute, and accepting capability regressions (temporal valid-time, geospatial, native GDS). For a Neo4j Community instance that is free at Aura's scale and already wired, the cost/benefit is firmly negative.

**AGE is worth revisiting only if Aura makes a deliberate strategic decision to collapse onto Postgres-only** — at which point this spike's findings define the rebuild scope. Until then: no migration.

## Repro
```powershell
docker build -f .planning/spikes/067-apache-age-pipeline/Dockerfile.age-pgvector -t age-pgvector-spike:local .planning/spikes/067-apache-age-pipeline
docker run -d --name age-spike -e POSTGRES_PASSWORD=spike -e POSTGRES_DB=aura_age -p 15432:5432 age-pgvector-spike:local
docker exec age-spike psql -U postgres -d aura_age -c "CREATE EXTENSION age; CREATE EXTENSION vector;"
# copy + run 01..04 (PowerShell, to avoid MSYS path mangling):
docker cp .planning/spikes/067-apache-age-pipeline/01_graphview_port.sql age-spike:/tmp/01.sql; docker exec age-spike psql -U postgres -d aura_age -f /tmp/01.sql
docker cp .planning/spikes/067-apache-age-pipeline/02_agent_memory_features.sql age-spike:/tmp/02.sql; docker exec age-spike psql -U postgres -d aura_age -f /tmp/02.sql
& .planning/spikes/067-apache-age-pipeline/03_gen_hybrid_sql.ps1   # needs aura-llama-embed up
docker cp .planning/spikes/067-apache-age-pipeline/03_hybrid.sql age-spike:/tmp/03.sql; docker exec age-spike psql -U postgres -d aura_age -f /tmp/03.sql
docker cp .planning/spikes/067-apache-age-pipeline/04_gds_and_latency.sql age-spike:/tmp/04.sql; docker exec age-spike psql -U postgres -d aura_age -f /tmp/04.sql
docker rm -f age-spike   # cleanup
```
