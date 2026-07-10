# graph db eval

Due-diligence verdict on replacing Neo4j as Aura's graph store. **Decision: STAY with Neo4j.** Apache AGE is rejected (9 agent-memory gaps + GDS hard blocker). ArcadeDB is the strongest alternative (Bolt-native, 7/9 gaps cleared, real-data latency/recall parity) and is **NOT rejected** — it is the pre-vetted fallback, gated behind a deeper PoC. This file is the rebuild scope if/when that PoC is greenlit, plus the store-agnostic Leiden + rerank upgrades that ship on today's Neo4j regardless.

## Requirements

These are hard constraints any future graph-store decision MUST honor (from MANIFEST Sessions 17/18, spike rows 067/068/069, and the COMPARISON/LEIDEN analyses). They are not suggestions.

- **STAY with Neo4j now.** The decision is final at single-node scale and on **maturity/risk grounds, NOT performance** (real-data perf is parity). Do not migrate the graph store without a fresh deeper-PoC go/no-go. Both engines are `$0` today; Neo4j Community + free sidecars wins on maturity + zero migration cost.
- **Embeddings are 384d, NOT 768d.** Granite-97m (`aura-llama-embed` sidecar `:8081/v1/embeddings`) emits **384-dimension** cosine vectors. CLAUDE.md's "768d" is stale. Every vector index DDL (Neo4j `CREATE VECTOR INDEX … 384d cosine`, ArcadeDB `LSM_VECTOR {dimensions:384, similarity:'COSINE'}`) MUST pin 384.
- **Apache AGE is rejected — do not reopen as a migration.** Its decision gate fails: GDS (Leiden/PageRank) is a hard blocker and query portability is below target (9 agent-memory feature gaps). AGE is revisitable ONLY if Aura makes a deliberate strategic choice to collapse onto Postgres-only; this spike then defines the rebuild scope.
- **ArcadeDB is the only credible fallback and remains NOT rejected.** Migration is bounded (vector→SQL rewrite, Leiden-invocation rework, schema pre-definition), not a reimplementation. Any go decision is gated behind three unflipped items (below) — keep them as the PoC acceptance gate.
- **Leiden is an external store-agnostic sidecar, on EITHER database.** Neo4j Community has no Leiden (Enterprise GDS only); ArcadeDB has Leiden but Java-API-only (unreachable over the wire). The mandated path is an external `leidenalg`/`graspologic` consolidation job reading over Bolt, writing `community_id` back — scheduled (Slice 6 cron), never per-turn. This unblocks today's Neo4j Community and removes Leiden as an ArcadeDB blocker simultaneously.
- **Reranking is a DB-agnostic llama.cpp sidecar (`aura-rerank`, `:8085`, `/v1/rerank`), not a graph-store feature.** It is identical post-processing on both engines and never a differentiator in the DB decision. Two-stage: hybrid top-N (≤30–50) → cross-encoder rerank → top-K (~5).

## How to Build It

Nothing shipped to the Aura codebase from these spikes — they are due-diligence. The two store-agnostic upgrades (Leiden, rerank) DO carry forward as build items on the current Neo4j stack. The ArcadeDB migration recipe is banked for the deeper PoC.

### Two consumers that any graph store must satisfy (ground-truthed from live `aura-neo4j` 5.26.26 + code)
- **(A)** read-only graph explorer: `internal/knowledge/graphview_intent.go` — 4 compiled queries (`compileSeed`/`compileExpand`/`compileOverview`/`compileSchema`), reached via the generic `knowledge.Client.Read(query, params)` seam.
- **(B)** `aura-agent-memory-mcp` fork: `docker/agent-memory/.../graph/queries.py` — ~80 Cypher templates over the agent-memory Bolt driver, plus `mcp-neo4j-cypher` as the LLM↔graph interface.
Both reach Neo4j over **Bolt** (`7687`). Any replacement must serve both seams or rewrite them.

### Leiden consolidation sidecar (BUILD NOW — net upgrade to current Neo4j)
Mirrors Microsoft GraphRAG (`graspologic.partition.hierarchical_leiden`). Store-agnostic because the `neo4j` Python driver connects to **both** Neo4j and ArcadeDB over Bolt.
```python
import igraph as ig, leidenalg as la
# 1. pull entity edges over Bolt:
#    MATCH (a:Entity)-[r:RELATED_TO]-(b:Entity) RETURN a.id, b.id, r.confidence
edges, weights = fetch_entity_edges()           # [(a_id,b_id)], [w]
g = ig.Graph.TupleList(edges, directed=False)
part = la.find_partition(g, la.RBConfigurationVertexPartition,
                         weights=weights, resolution_parameter=1.0, seed=42)
# 2. write back: UNWIND $rows MATCH (e:Entity{id:row.id}) SET e.community_id=row.c
write_back([(v["name"], part.membership[i]) for i, v in enumerate(g.vs)])
```
- Libraries (all pip, CPU, no Enterprise): `leidenalg` + `python-igraph` (canonical, vtraag); `graspologic` `hierarchical_leiden` (nested topics, GraphRAG-proven); `cdlib` (wraps both).
- Wiring: scheduled job via Aura's Slice 6 cron, or a thin sidecar — periodic/background, NOT per-turn. Deterministic with fixed `seed=42`; `resolution_parameter` tunes granularity. The `ConsolidationRun` label already exists in the live graph.
- Reject the alternatives: Neo4j Enterprise GDS = paid; ArcadeDB Java sidecar = couples to a JVM component; `networkx` Louvain = lower quality + slower than C++ Leiden.

### Rerank sidecar (BUILD NOW — net upgrade, precision stage for hybrid retrieval)
Same image as `aura-llama-embed` (`ghcr.io/ggml-org/llama.cpp:server`), CPU, `-t 4` (mini-PC budget), loopback `:8085`, fail-soft (degrade to RRF order if down).
```
llama-server --hf-repo Voodisss/Qwen3-Reranker-0.6B-GGUF-llama_cpp \
             --hf-file Qwen3-Reranker-0.6B-Q4_K_M.gguf \
             --reranking --pooling rank --embedding \
             -c 4096 -t 4 --host 0.0.0.0 --port 8085 --alias aura-rerank
```
- Endpoint `POST /v1/rerank`, body `{"model":"aura-rerank","query":"…","documents":["chunk1",…]}` → `{"results":[{"index":0,"relevance_score":0.95},…]}`, scores 0–1, **sort descending client-side**.
- Go client mirrors `documents.EmbeddingClient` → `RerankClient.Rerank(query, docs) []scored`.
- Two-stage pattern: (1) hybrid BM25 + vector (`db.index.vector.queryNodes`) + 1-hop graph → top-N (~30–50); (2) rerank → top-K (~5) for LLM context. Ship **RRF first** (spike 056 production shape = embedding-primary + guarded RRF, zero model), add the reranker when precision@5 needs the lift.
- Model: **Qwen3-Reranker-0.6B** primary (32k ctx, 100+ langs incl. Italian, instruction-aware, Apache-2.0; beats bge-reranker-v2-m3 multilingual: MTEB-R 65.80 vs 57.03). Alternative: `bge-reranker-v2-m3` (~568M, classic cross-encoder).

### ArcadeDB migration recipe (BANKED — only on a deeper-PoC go decision)
Image `arcadedata/arcadedb:latest` (26.7.1-SNAPSHOT, JVM/OrientDB lineage, Apache-2.0). Bolt plugin enabled via `JAVA_OPTS`:
```
-Darcadedb.server.rootPassword=… \
-Darcadedb.server.plugins=Bolt:com.arcadedb.bolt.BoltProtocolPlugin \
-Darcadedb.server.defaultDatabases=aura[admin:admin] \
-Darcadedb.bolt.defaultDatabase=aura
```
Bolt `7687`, HTTP/SQL `2480`. The official Neo4j `cypher-shell` (from `neo4j:5.26.26-community`) connects over Bolt → proves `mcp-neo4j-cypher` + agent-memory Bolt driver interop.

What runs **unmodified over Bolt** (graphview 4/4 + 7/9 agent-memory deep features): `datetime()`, `duration({days:N})`, `point({latitude,longitude})`, `MERGE … ON CREATE/ON MATCH SET`, `FOREACH`, `CALL(a,b){}` + `CALL{WITH}` subqueries, `apoc.convert.toJson`, `apoc.map.removeKeys`, multi-label `:Entity:OBJECT:VEHICLE`, `db.labels()`, `elementId()`, `EXISTS{MATCH…}`, var-length `*1..3`. (See `sources/068-arcadedb-pipeline/01_graphview.cypher` + `02_agent_memory.cypher`.)

The 3 bounded rewrites required:
1. **Vectors: Cypher → SQL/HTTP.** ArcadeDB has native HNSW but NOT the Neo4j procedure/DDL. Replace the 8+ `db.index.vector.queryNodes` calls with `vectorNeighbors()` over `POST :2480/api/v1/command/aura`:
   ```sql
   CREATE VERTEX TYPE Doc;
   CREATE PROPERTY Doc.embedding ARRAY_OF_FLOATS;
   CREATE INDEX ON Doc (embedding) LSM_VECTOR
     METADATA {dimensions: 384, similarity: 'COSINE', maxConnections: 16, beamWidth: 100};
   SELECT node_id, txt, $distance AS dist
     FROM (SELECT expand(vectorNeighbors('Doc[embedding]', <vec384>, 3)));
   ```
   (Working PoC: `sources/068-arcadedb-pipeline/03_vector.ps1`; real-data 1500-chunk DDL: `sources/069-…/g220_spike.py`.)
2. **Leiden/PageRank invocation rework** — algorithms exist (70+, incl. Leiden which needs Neo4j *Enterprise*) but are bound to the embedded Java `GraphAlgorithms`/`GraphAnalyticalView` OLAP API, NOT `CALL gds.*` over Bolt. Use the external `leidenalg` sidecar above (store-agnostic) rather than ArcadeDB's Java path. **This is the main open risk.**
3. **Schema pre-definition** — ArcadeDB forbids implicit label/type auto-creation over the network. Pre-define vertex/edge types via SQL DDL before any Cypher `CREATE`.

Backup: `BACKUP DATABASE` → one zip, one command (no AGE-style silent-drop footgun).

### Real-data harness (closed the 068 latency/recall gates)
`sources/069-arcadedb-vs-neo4j-realdata/g220_spike.py` — `python:3.12-slim` worker joined to both the throwaway DB net and `aura_default` (for the Granite sidecar). Ingests the 830-page Siemens G220 PDF via PyMuPDF (~900-char / 120-overlap windows, capped 1500 chunks), embeds via `:8081`, indexes into both DBs, runs 8 realistic queries ×15. Resumable (caches chunks+vecs to `/work`).

## What to Avoid

- **Do NOT migrate to Apache AGE.** Hard landmines actually hit (`sources/067-…/02_agent_memory_features.sql`, `01_graphview_port.sql`):
  - AGE allows **ONE label per node** — `:Entity:OBJECT:VEHICLE` is a *syntax error at ":"*. Aura's POLE+O multi-label model breaks.
  - **9 agent-memory blockers with no Cypher substitute:** `datetime()`, `duration({days})`, `point()/point.distance`, `MERGE … ON CREATE/ON MATCH SET` (*syntax error at "ON"*), `FOREACH`, `CALL(a,b){}` scoped subquery, `db.index.vector.queryNodes` (8+ queries), `CREATE VECTOR INDEX`, `apoc.map.removeKeys`, `SHOW INDEXES`. No temporal type, no spatial type — schema-level workarounds only (epoch bigints, SQL `ON CONFLICT` outside `cypher()`, all vector on pgvector).
  - **GDS = zero algorithms.** `gds.pageRank`/`gds.leiden` → syntax error. Hard blocker; the report's decision gate ("any blocker in 3/4/5 → stop") fires.
  - `$param` cannot be inlined into AGE Cypher — requires `PREPARE … cypher(g,$$…$$,$1)` with one agtype blob **and an explicit `AS (col agtype,…)` column list per query**. Aura's inline-param `knowledge.Client.Read` seam and `mcp-neo4j-cypher` (no Bolt) must be replaced.
  - **AGE backup silent-data-loss SHARP EDGE:** a pgvector table created under AGE's default `search_path` lands in `ag_catalog` and is **silently dropped from `pg_dump`** (extension restored, table gone, no error). Fix proven: explicitly qualify app tables to `public` (`public.chunk_vec2` round-trips). Never rely on AGE's search_path.
  - `rioriost/age_mcp_server` (the only off-the-shelf AGE MCP) is marked **"Obsoleted"** (MIT, 4 stars) — would have to fork/build.
- **ArcadeDB gotchas (live-diagnosed):**
  - `defaultDatabases=aura[root:…]` naming the bracket user `root` **shadows** the server-root's `databases:{"*":["admin"]}` wildcard → silently strips schema rights → `SecurityException`/403. Use a **different** bracket user (`aura[admin:admin]`, `g220[dbadmin:dbadmin]`).
  - Implicit label creation is forbidden over the network (`User 'root' is not allowed to update schema`) — pre-define types or `CREATE` fails.
  - Piping `.cypher` to `cypher-shell` stdin adds a **BOM** → use mounted `-f`, not a pipe.
  - `FOREACH … SET` flag is read NULL within the same statement (visibility quirk) — verify, don't assume same-stmt readback.
- **ArcadeDB is NOT a clean drop-in despite Bolt.** The "it speaks Cypher+Bolt so it just works" framing is wrong for vectors (SQL-only `vectorNeighbors`, not the Cypher procedure) and GDS (Java/OLAP, not `CALL gds.*`). Both are real rework.
- **Do NOT trust toy-graph latency.** Spikes 067/068 latency on a 23-node graph (sub-ms warm, 170–341ms cold plan) is **not a valid scale signal** — that's exactly why spike 069 was run on real data.
- **Do NOT use Qwen3-Reranker Q2_K** ("unusable, −28.7% NDCG@10"); small rerankers are the most quant-sensitive. Use **Q4_K_M** (396 MB, ~0.3% loss) or Q8_0 (639 MB). And do NOT use community GGUF conversions that miss `cls.output.weight` (garbage ~4.5e-23 scores) — use `Voodisss/Qwen3-Reranker-0.6B-GGUF-llama_cpp`.
- **Drive Docker from PowerShell, not Git-Bash/MSYS** — MSYS mangles `/tmp/...` paths and the spaced Docker path (conventions 025/059/060).

## Constraints

- **Embeddings: Granite-97m, 384d, cosine.** Sidecar `aura-llama-embed` at `:8081/v1/embeddings`. Float literals on the it-IT host must use invariant-culture formatting (`G9` invariant) or vectors corrupt.
- **Real-data perf (1500 G220 chunks, identical Granite embeddings):** vector search p50 Neo4j 6.8ms / ArcadeDB 6.4ms; p95 Neo4j 14.6ms / ArcadeDB 13.8ms — **~1.0× parity, well within the 1.5× gate.** Retrieval overlap@5 = 4.5/5 mean (~90% identical top-5; divergences are HNSW tie-breaking on near-neighbors, not relevance regressions). Load: Neo4j 2.0s (Bolt UNWIND batches) vs ArcadeDB 8.1s (per-record HTTP inserts — a bulk path would close most of the gap).
- **Version pins:** Neo4j `5.26.26-community` (Bolt `7687`, HTTP `7474`); ArcadeDB `arcadedata/arcadedb:latest` = `26.7.1-SNAPSHOT` (Bolt `7687`, HTTP `2480`); Apache AGE `apache/age:latest` = AGE `1.7.0` on PostgreSQL `18.1` (Debian 13) + `postgresql-18-pgvector` → pgvector `0.8.2`.
- **ArcadeDB ops ceilings:** Bolt is new (26.x SNAPSHOT), **no TLS yet** (loopback only — fine for Aura's single-node use), younger/smaller ecosystem than Neo4j. **Security: CVE-2026-44221 (CVSS 9.0)** cross-DB authz bypass in the multi-tenant area; the tested Bolt DB-attach boundary was permissive (alice opened a session on bob_db; record-level data held). Any go decision MUST be on a **pinned post-CVE release build, NOT a SNAPSHOT.**
- **Licensing:** Neo4j Community = GPLv3, single DB, no Leiden, no online backup, no clustering. ArcadeDB = Apache-2.0, ALL features free (multi-DB, Leiden/GDS, HA, vectors, online backup). Neo4j Enterprise (to lift those limits) ≈ $3k–6k/core/yr → ~$20k–40k/yr (4–8 cores), up to $80k–200k+/yr.
- **Cost decision rule:** at single self-hosted node with current features, **Neo4j Community wins** ($0 + mature + already-wired + zero migration; Aura fills the gaps with external `leidenalg` + offline `neo4j-admin dump` + logical user-scoping). ArcadeDB's `$0`-all-features only wins for a **shipped multi-customer appliance** (DGX-Spark SMB vision) where a per-deployment Neo4j Enterprise license is economically fatal AND a hard Enterprise-only requirement (physical per-tenant isolation / HA / online backup) actually appears.
- **The 3 flippers (deeper-PoC acceptance gate) toward ArcadeDB — all currently UNFLIPPED:** (1) Bolt tenant isolation proven airtight on a pinned release build (deny cross-DB *attach*, not just cross-DB read); (2) a working, performant Leiden/PageRank invocation reachable from Aura's Go runtime; (3) real-dataset p95 + GraphRAG Recall@5/nDCG@10 within target vs the live Neo4j baseline. Item (3) is already closed positively by spike 069; (1) and (2) remain.
- **Rerank sidecar:** `aura-rerank` loopback `:8085`, CPU `-t 4`, fail-soft to RRF; keep rerank candidate count **N ≤ 30–50** (one cross-encoder pass over N docs, latency ∝ N×query-len — measure p95).

## Origin

Synthesized from spikes 067, 068, 069. Source files in `sources/067-apache-age-pipeline/` (Dockerfile.age-pgvector, 01_graphview_port.sql, 02_agent_memory_features.sql, 03_gen_hybrid_sql.ps1, 03_hybrid.sql, 04_gds_and_latency.sql, README.md), `sources/068-arcadedb-pipeline/` (01_graphview.cypher, 02_agent_memory.cypher, 03_vector.ps1, COMPARISON.md, LEIDEN-AND-RERANK.md, README.md), `sources/069-arcadedb-vs-neo4j-realdata/` (g220_spike.py, README.md). Verdicts: 067 VALIDATED → STAY-with-Neo4j (AGE rejected, GDS hard blocker + 9 agent-memory gaps); 068 VALIDATED → ArcadeDB STRONGEST candidate, partial-replace / deeper-PoC, NOT rejected; 069 VALIDATED → real-data vector retrieval PARITY (latency ~1.0×, overlap@5 4.5/5) — closes 068's latency/recall gates, final decision STAY-with-Neo4j on maturity/risk not performance. No code shipped to the Aura codebase from these spikes (due-diligence); the Leiden + rerank sidecars are forward build items.

## Update (2026-07-08/09): ArcadeDB adopt-strategy (071) + TuringDB eval (090–094)

**Verdict unchanged — STAY with Neo4j.** Two more alternatives were scoped/probed; neither flips the decision.

- **071-arcadedb-adopt-strategy** (PLANNED contingency, no containers run — gated by **ADR 0038** triggers: appliance ships AND legal rejects GPLv3, OR hard per-tenant isolation makes Neo4j Enterprise price fatal). Contribution: don't fork-and-rewrite — **adopt existing ArcadeDB pieces.** Strategy = fork `agent-memory` (neo4j-labs), repoint to ArcadeDB **Bolt**, adopt ArcadeDB's **built-in MCP server** (5 tools incl. native `get_schema` — drops the `apoc.meta.*` need), keep POLE+O. Three Neo4j bindings mapped to port: `agent-memory` Python sidecar (~200 Cypher constructs, **1 real APOC** `apoc.map.removeKeys`, ~3 vector sites), `graphview` Go (3 read intents, 2 APOC helpers), + the daemon Bolt URL. `langchain-arcadedb` / `flexible-graphrag` are references, NOT drop-ins. **Only execute when an ADR-0038 trigger fires.**
- **090–094 TuringDB** (live Docker probes, PyPI 1.35 wheel — the June desk report was stale): 090 durability **VALIDATED** (graph+vector survive restart, but user graphs need explicit `load_graph` on boot); 091 Cypher compat **INVALIDATED_AS_DROP_IN** (native basics pass — multi-label, `labels`, native vector search, shortestPath — but drop-in fails: `db.relationshipTypes`, `elementId`, `properties`, datetime/duration/point, APOC, CALL subquery, GDS, `MERGE ON CREATE`, FOREACH); 092 vector+GraphRAG **VALIDATED** (overlap@5=1.00, p50 45ms); 093 access path **PARTIAL** (REST works, no native MCP or Bolt → Aura would own a custom REST-backed MCP bridge); 094 E2E **VALIDATED 10/10** via a `cli-printing-press` REST bridge. Net: stronger than expected on vectors + basic Cypher, but **still not a Neo4j drop-in** (no APOC/GDS/temporal, no native MCP/Bolt) → **stay Neo4j**.

**Constraint carried:** any future non-Neo4j graph store must satisfy the agent-memory stack (POLE+O + APOC-equivalent + GDS/Leiden + native MCP or a bridge). ArcadeDB is the strongest contingency (Apache-2.0, built-in MCP); TuringDB and Apache AGE remain rejected as drop-ins.

Synthesized additionally from spikes: 071, 090, 091, 092, 093, 094. Sources in `sources/071-arcadedb-adopt-strategy/`, `sources/090-…/` … `sources/094-turingdb-memory-doc-e2e/`.
