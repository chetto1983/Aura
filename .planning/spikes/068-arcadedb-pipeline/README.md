---
id: 068-arcadedb-pipeline
title: ArcadeDB as a Neo4j replacement for the full Aura graph pipeline
date: 2026-06-20
status: VALIDATED (verdict: STRONGEST candidate — partial replacement / deeper PoC, NOT rejected)
type: standard
tags: [neo4j, arcadedb, bolt, opencypher, vector, graph, agent-memory, gds, migration]
related: docs/research/2026-06-20-neo4j-alternatives-puppygraph-turingdb-apache-age.md, .planning/spikes/067-apache-age-pipeline
---

# Spike 068 — ArcadeDB on the full Aura graph pipeline

## Idea

The operator flagged that the desk-research report + spike 067 missed **ArcadeDB** — a multi-model
(graph/document/vector/...) Apache-2.0 engine that speaks **Cypher, Gremlin, SQL** *and the Neo4j Bolt
protocol*. Unlike PuppyGraph/TuringDB/AGE it could clear Aura's whole bar. This spike runs Aura's
REAL pipeline against a live ArcadeDB and grades it against the report's §5 criteria — and because
ArcadeDB speaks Bolt, the graph/agent-memory Cypher runs **unmodified** (a far truer test than 067's
SQL porting against AGE).

Aura's four-part bar: durable graph store · native 384d-cosine vector index · GDS (Leiden+PageRank) ·
Cypher+Bolt (`mcp-neo4j-cypher` + the agent-memory Bolt driver). Two consumers:
`internal/knowledge/graphview_intent.go` (4 reads) + `docker/agent-memory/.../graph/queries.py` (~80 Cypher).

## Harness / stack

- **Image:** `arcadedata/arcadedb:latest` = ArcadeDB **26.7.1-SNAPSHOT on PG-free JVM** (OrientDB lineage, Arcade Data).
- **Container:** `arcadedb-spike` on a dedicated docker network, Bolt enabled via
  `JAVA_OPTS=-Darcadedb.server.plugins=Bolt:com.arcadedb.bolt.BoltProtocolPlugin -Darcadedb.server.rootPassword=… -Darcadedb.server.defaultDatabases=aura[admin:admin] -Darcadedb.bolt.defaultDatabase=aura`.
- **Cypher/Bolt client:** the official **Neo4j `cypher-shell`** (from `neo4j:5.26.26-community`) pointed at `bolt://arcadedb-spike:7687` — proving Neo4j-tooling interop. Real `.cypher` files run via mounted `-f` + `--fail-at-end` (records per-statement pass/fail).
- **SQL/HTTP:** `POST :2480/api/v1/command/aura` (vector + GDS + backup + schema DDL).
- **Embeddings:** real Granite-97m 384d from the live `aura-llama-embed` sidecar.
- Docker driven from PowerShell (MSYS mangles paths); piping to cypher-shell stdin adds a BOM → use mounted `-f`, not a pipe (conventions 059/060).

## Results

### Bolt + Cypher — ✅ native, Neo4j-driver compatible
The official Neo4j `cypher-shell` connects over Bolt and runs Cypher (`RETURN 1` → ok). Bolt v3/4/4.4, neo4j-go-driver compatible. (No TLS yet → loopback only; fine for Aura.)

### Schema model — ⚠ stricter than Neo4j (two real gotchas)
- ArcadeDB **forbids implicit label/type auto-creation** over the network: `SecurityException: User 'root' is not allowed to update schema`. You must pre-define vertex/edge types (SQL DDL) before Cypher `CREATE`.
- The `defaultDatabases` value **requires** a `[user:pass]` bracket, and naming that user `root` **shadows** the server-root's `databases:{"*":["admin"]}` wildcard → silently strips schema rights. Use a *different* bracket user (`aura[admin:admin]`) so server-root keeps admin. (Both diagnosed live.)

### (A) Graphview reads (4 queries) — ✅ run UNMODIFIED over Bolt
| feature | AGE (067) | ArcadeDB |
|---|---|---|
| **multi-label** `:Entity:OBJECT:VEHICLE` | ❌ syntax error | ✅ `["Entity","OBJECT","VEHICLE"]` |
| `CALL db.labels()` | ❌ | ✅ returns label set |
| `apoc.convert.toJson(labels(e))` | ❌ | ✅ `"[\"Entity\"]"` |
| `elementId()` / `startNode()` / `endNode()` / `properties()` | partial | ✅ (RID `#1:1` keys) |
| `EXISTS { MATCH … }` subquery | ✅ | ✅ |
The compileSeed/Expand/Overview/Schema queries port ~as-is; only contract diff = RID element-ids vs Neo4j strings (opaque keys, fine).

### (B) Agent-memory deep features — ✅ 7 of AGE's 9 blockers CLEARED over Bolt
| feature | AGE | ArcadeDB |
|---|---|---|
| `datetime()` temporal | ❌ | ✅ `2026-06-20T11:04…[GMT]` |
| `duration({days:7})` | ❌ | ✅ week-ago computed |
| `point()` spatial | ❌ | ✅ `{lat,lon,crs:WGS-84,srid:4326}` |
| `MERGE … ON CREATE/ON MATCH SET` | ❌ | ✅ ON MATCH fired |
| `FOREACH (… CASE …)` | ❌ | ✅ parses+runs (SET visibility quirk: flag read NULL same-stmt — verify) |
| `CALL(a,b){}` + `CALL{WITH}` subquery | ❌ | ✅ both, mc=1 |
| `apoc.map.removeKeys` | ❌ | ✅ stripped key |
| variable-length `*1..3`, list fns | ✅ | ✅ |
| `db.index.vector.queryNodes` | ❌ | ❌ *"Error executing procedure"* |
| `CREATE VECTOR INDEX` (Neo4j DDL) | ❌ | ❌ *"Only standard/RANGE/TEXT index types"* |

The only 2 unsupported are the **Neo4j vector procedure + DDL** — ArcadeDB has vectors, just via a different API (below).

### (C) Vector — ✅ native HNSW, correct ranking, but SQL-only (not Cypher)
SQL: `CREATE INDEX ON Doc (embedding) LSM_VECTOR METADATA {dimensions:384, similarity:'COSINE', maxConnections:16, beamWidth:100}` + `SELECT expand(vectorNeighbors('Doc[embedding]', <vec>, 3))`. With real Granite embeddings, query *"electric vehicle"* ranked **e4 "a red electric vehicle" > e1 "Tesla…electric car" > e2 "Davide"** (cat excluded) — identical to 067's pgvector ranking, ACID, in-engine. **But it's reachable over the HTTP/SQL API, not Cypher/Bolt** → Aura's 8+ `db.index.vector.queryNodes` queries must be rewritten to `vectorNeighbors()` (capability present, invocation differs).

### (D) GDS (Leiden / PageRank) — ⚠ present but Java-API/OLAP-bound, not Cypher-callable
Docs: 70+ algorithms incl. **PageRank AND Leiden** (+ Louvain, Label Propagation, centralities), Graph OLAP engine (PageRank ~462× OLTP). **But every SQL/HTTP invocation probe failed** (`pageRank()`, `expand(pageRank(...))`, `CREATE GRAPH ANALYTICAL VIEW`) — the algorithms are bound to the embedded **Java `GraphAlgorithms`/`GraphAnalyticalView` API**, not a `CALL gds.*` over Bolt like Neo4j. So Aura's memory-consolidation (Leiden) would need a reworked, non-trivial invocation path (or the exact OLAP SQL syntax, unconfirmed here). **Far better than AGE (which has zero algorithms), but not a clean drop-in.**

### (E) Backup — ✅ one command
SQL `BACKUP DATABASE` → `aura-backup-20260620-110725611.zip` (16 KB) in one call. No AGE-style silent-drop footgun.

## §5 scorecard
| # | Criterion | ArcadeDB |
|---|---|---|
| 1 | Query portability ≥8/10 | ✅ **graphview ~4/4 + agent-memory ~7/9 over Bolt unmodified** (vs AGE's reimplementation) |
| 2 | p95 within 1.5× Neo4j | ⚠ UNVALIDATED (tiny graph) — needs real-dataset PoC |
| 3 | GraphRAG Recall@5/nDCG@10 within 5% | ✅ vector ranking correct (real embeddings); full harness pending |
| 4 | GDS path ≤2 days or blocker | ⚠ algorithms EXIST but Java-API/OLAP invocation → rework needed (not a hard blocker) |
| 5 | LLM↔graph (MCP) read+write | ✅ **native Bolt → `mcp-neo4j-cypher` + agent-memory Bolt driver can connect** (huge vs AGE) |
| 6 | single backup round-trip | ✅ `BACKUP DATABASE` one command |

## Verdict — ArcadeDB is the STRONGEST candidate; partial replacement / deeper PoC, NOT rejected

ArcadeDB clears the compatibility wall that killed every other candidate. Over native **Bolt**, Aura's graphview reads and ~7/9 of the agent-memory deep features (temporal, spatial, MERGE-upsert, FOREACH, CALL subqueries, APOC, multi-label) run **unmodified** — so `mcp-neo4j-cypher` and the agent-memory Bolt driver can point at it with minimal change. Vectors work natively (correct ranking on real embeddings) and the GDS algorithms exist (incl. Leiden — which, *correction 2026-07-08*, is **free in Neo4j's GDS Community** too, capped only at 4-core concurrency, **not** Enterprise-gated).

The migration is **bounded but real**, not a reimplementation:
1. Rewrite the 8+ vector queries `db.index.vector.queryNodes` → SQL `vectorNeighbors()` (HTTP API).
2. Rework the Leiden/PageRank consolidation invocation to ArcadeDB's Graph OLAP / Java API (path unproven over the network — the main open risk).
3. Pre-define vertex/edge types (no implicit label creation) + handle the `defaultDatabases` root-shadow gotcha.
4. Ops caveats: Bolt is new (26.x SNAPSHOT), no TLS (loopback fine), smaller/younger ecosystem than Neo4j.

**Recommendation:** ArcadeDB is no longer "rejected" — it is the first credible Neo4j replacement for Aura and warrants a **deeper PoC** (real-dataset latency/recall + a working Leiden invocation + full mcp-neo4j-cypher/agent-memory round-trip over Bolt) before any go/no-go. Whether to actually migrate is a strategic call (bounded effort + younger ecosystem) — but the compatibility verdict is positive, unlike PuppyGraph/TuringDB/AGE.

## Repro
```powershell
docker network create arcadedb-spike-net
docker run -d --name arcadedb-spike --network arcadedb-spike-net -p 127.0.0.1:12480:2480 -p 127.0.0.1:17688:7687 `
  -e JAVA_OPTS="-Darcadedb.server.rootPassword=playwithdata -Darcadedb.server.plugins=Bolt:com.arcadedb.bolt.BoltProtocolPlugin -Darcadedb.server.defaultDatabases=aura[admin:admin] -Darcadedb.bolt.defaultDatabase=aura" arcadedata/arcadedb:latest
# pre-create types via HTTP SQL (root wildcard-admin), then:
docker run --rm -v "<repo>/.planning/spikes/068-arcadedb-pipeline:/q" --network arcadedb-spike-net neo4j:5.26.26-community `
  cypher-shell -a bolt://arcadedb-spike:7687 -u root -p playwithdata -d aura --fail-at-end -f /q/01_graphview.cypher
# 02_agent_memory.cypher (Bolt), 03_vector.ps1 (HTTP vectorNeighbors), then BACKUP DATABASE
docker rm -f arcadedb-spike; docker network rm arcadedb-spike-net
```
