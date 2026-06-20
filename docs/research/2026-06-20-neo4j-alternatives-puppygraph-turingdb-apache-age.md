# Neo4j Alternatives Due Diligence — PuppyGraph vs. TuringDB vs. Apache AGE

**Date:** 2026-06-20
**Author:** Research spike (`/gsd-spike` → deep-research)
**Audience:** Leadership / architecture decision
**Question:** Should Aura *replace*, *partially replace*, *test-first*, or *stay with* Neo4j, given three candidate alternatives — **PuppyGraph**, **TuringDB**, and **Apache AGE**?

> **Prompt note.** The originating request was a half-filled template: the placeholder `PuppyGraph; turingdb; Apache AGE` (and a non-existent URL `https://docs.PuppyGraph; turingdb; Apache AGE.com/`) was pasted into every slot. This report interprets it as "evaluate all three as Neo4j alternatives," which is the only coherent reading.

> **Citation discipline.** Each major claim is tagged: **[doc]** = stated in official documentation/repo (cited in Sources); **[vendor]** = vendor marketing/benchmark, treat as a claim to validate; **[absence]** = not found in the official docs reviewed (an absence of evidence, not proof of absence); **[infer]** = analytical inference from Aura's known architecture; **[validate]** = needs hands-on validation before relied upon.

---

## What Aura actually uses Neo4j for (the bar any replacement must clear)

This grounds "technical fit." Aura's graph is **not** a relational-to-graph projection — it is a native, durable, writable knowledge + memory store with several Neo4j-specific dependencies **[infer]**:

1. **Durable writable graph store** — agent memory persists across sessions (the `neo4j-labs/agent-memory` subgraph pattern).
2. **Native vector index** — HNSW, 384-dimension, cosine, embeddings stored as node properties (Granite-97m; GraphRAG hybrid retrieval = BM25 + vector + graph). *(Corrected from CLAUDE.md's stale "768d" via spike 067 — the live sidecar + Neo4j migrations are 384d.)*
3. **GDS algorithm library** — Leiden community detection, PageRank for memory consolidation/ranking.
4. **APOC** procedures.
5. **Cypher dialect + Bolt protocol** — the `mcp-neo4j-cypher` MCP server is the LLM↔graph interface; the agent-memory fork speaks the Neo4j Bolt driver.
6. **Postgres** runs alongside as the primary operational store.

A *full* replacement must therefore provide: durable graph storage **and** a native vector index **and** graph algorithms **and** a compatible query path for the MCP server + agent-memory driver. Keep this four-part bar in mind — it is the lens for every verdict below.

---

## 1. Executive Summary (one page)

**Bottom line: Stay with Neo4j as Aura's graph store. None of the three is a drop-in replacement. Only Apache AGE merits a time-boxed PoC, and only if Postgres-consolidation becomes a strategic goal.**

The three candidates are **not three versions of the same thing** — they occupy three different architectural categories, and only one even competes for Neo4j's job in Aura:

- **PuppyGraph** is a **zero-ETL graph query *engine*, not a graph database** — it has no storage of its own and queries existing relational/lakehouse tables as a graph **[doc]**. Aura already maintains a *native* graph with vector indexes and GDS algorithms; PuppyGraph stores nothing, ships no vector index **[absence]**, and offers no GDS-equivalent algorithm library **[absence]**. Its core value (query your SQL/lakehouse as a graph without ETL) **does not map onto Aura's use case**. It is **complementary at best, and largely orthogonal** to what Aura needs. → **Stay with Neo4j.**

- **TuringDB** is a **very new, in-memory, column-oriented analytical graph engine** under a **Business Source License** (source-available, not OSI open-source), ~158 GitHub stars, **no stated disk-persistence/durability model** **[doc]**. It has no vector index **[absence]**, no GDS-equivalent algorithms **[absence]**, no Bolt protocol, and an openCypher *subset* of unknown coverage. For Aura's durable agent-memory + vector + GDS needs it is **architecturally mismatched and immature**. → **Stay with Neo4j.**

- **Apache AGE** is a **PostgreSQL extension** giving Postgres openCypher graph queries alongside SQL **[doc]**, Apache-2.0 licensed, supporting PG 11–18 **[doc]**. It is the *only* candidate that is both a real store and architecturally interesting for Aura (which already runs Postgres). But it has **no native vector index** **[absence]** (you'd pair pgvector separately **[validate]**), **no GDS-style algorithm library** **[absence]**, slower deep traversals (heap tables + B-tree-per-hop, not index-free adjacency) **[vendor/validate]**, a **Cypher subset wrapped in a `cypher(...)` SQL function** (so existing Neo4j Cypher and the `mcp-neo4j-cypher` server do not port unmodified), and **no Bolt** (so the agent-memory driver doesn't work as-is). → **Test first** for consolidation upside; realistically a **partial** fit, not a full replacement.

**Why not just migrate?** Aura's Neo4j footprint is small, self-hosted, and free (Community Edition covers vector indexes and basic GDS for its scale). The migration cost (rewriting Cypher, replacing Bolt/MCP integration, re-platforming agent-memory, sourcing a vector + algorithms story) is high; the realized benefit is low-to-speculative. **The risk/reward favors staying.**

**Recommended next step:** No migration. If Postgres-consolidation is later prioritized, run the **Apache AGE PoC** in §5 (≈1 week) before committing to anything.

---

## 2. Detailed comparison vs. Neo4j

### 2.0 Reference: Neo4j (current stack)

- **Storage:** native graph engine, on-disk page-cache storage; handles graphs exceeding RAM with predictable performance **[vendor — PuppyGraph's own comparison, uncontroversial]**.
- **Vector index:** available in **Community Edition** (embeddings as `LIST<INTEGER|FLOAT>` properties); cosine & euclidean; dimensions 1–4096 (vector-2.0/3.0); HNSW with tunable `m`/`ef_construction`; optional quantization **[doc]**. This is exactly Aura's 384d-cosine usage.
- **GDS:** library ships Community and Enterprise editions; **Community has the algorithms but caps catalog/graph-projection operations**; clustering & advanced features require an Enterprise license **[doc]**.
- **Query/protocol:** Cypher + Bolt; rich ecosystem (`mcp-neo4j-cypher`, agent-memory, drivers, browser, APOC).
- **License/cost for Aura:** Community Edition (GPLv3), self-hosted, $0. Enterprise (clustering, hot backup, advanced GDS) is commercial — Aura does not currently need it.

---

### 2.1 PuppyGraph vs. Neo4j

| Dimension | PuppyGraph | Implication for Aura |
|---|---|---|
| **Category** | Zero-ETL graph **query engine** over existing data; "the first and only real-time, zero-ETL graph query engine" **[doc/vendor]** | Different category from Neo4j. Not a graph *store*. |
| **Storage** | **None of its own** — maps existing tables (Postgres, MySQL, Iceberg, Delta, Hudi, Snowflake, BigQuery, Redshift, S3, …) to a graph via a JSON schema **[doc]** | Aura's graph is native, not a projection of relational tables. Core value prop doesn't apply. |
| **Query language** | openCypher **and** Gremlin (plus SQL) **[doc]** | Cypher-family ✓, but Neo4j-specific Cypher + GDS/vector procedures won't port. |
| **Vector search** | Not mentioned in docs reviewed **[absence]** | Breaks GraphRAG hybrid retrieval — no vector index. |
| **Graph algorithms** | Path/neighborhood/pattern queries; no GDS-equivalent algorithm library found **[absence]** | No Leiden/PageRank library for memory consolidation. |
| **Architecture** | Vectorized execution, predicate/query pushdown, min/max statistics; auto-sharding across sources (Enterprise) **[doc/vendor]** | Built for analytics over big lakehouse data, not low-latency single-query agent memory. |
| **Performance** | "10-hop neighbor query in 2.26s across billions of edges on a 4-node cluster" **[vendor — validate]** | Vendor benchmark, large-cluster scenario; irrelevant to Aura's single-node mini-PC scale. |
| **Deployment** | Developer Edition: **free, single-node, Docker**; Enterprise: paid (priced on Memory + CPU), AWS AMI, auto-sharding **[doc]** | Free tier exists, but adds a component without solving a problem Aura has. |
| **License** | Proprietary; no open-source designation **[doc/absence]** | Vendor lock-in for anything beyond Developer Edition. |
| **Fit verdict** | **Complementary / specific workloads only** | Would only make sense to query `aura.*` Postgres tables *as a graph* without materializing into Neo4j — a niche Aura doesn't have. |

**Net:** PuppyGraph solves "I have data in SQL/lakehouses and want graph queries without ETL." Aura's problem is the opposite — it *already has* a curated native graph with vectors and algorithms. **Not a replacement; not even a natural complement.**

---

### 2.2 TuringDB vs. Neo4j

| Dimension | TuringDB | Implication for Aura |
|---|---|---|
| **Category** | In-memory, column-oriented graph engine for **analytical / read-intensive** workloads **[doc]** | Optimized for read-heavy analytics, not durable transactional agent memory. |
| **Storage / durability** | **In-memory only**; no disk-persistence/durability mechanism stated **[doc/absence]** | **Disqualifying** for a store of record. Agent memory must survive restarts. |
| **Query language** | openCypher **subset** (exact coverage undocumented in pages reviewed) **[doc]** | Unknown gaps; no compatibility guarantees. |
| **Concurrency / txn** | Immutable DataParts, snapshot isolation, "zero-locking" reads/writes **[doc]** | Good for analytics; says nothing about durability. |
| **Vector search** | Not mentioned **[absence]** | Breaks GraphRAG. |
| **Graph algorithms** | Not mentioned **[absence]** | No GDS equivalent. |
| **Protocol / SDKs** | Python SDK + REST API on `localhost:6666`; Docker/pip/uv/nix **[doc]** | No Bolt → agent-memory + `mcp-neo4j-cypher` don't work. |
| **Maturity** | ~158 stars; v1.34 (Jun 2026); 4.6k commits; CI present; **no production-readiness disclaimers either way** **[doc]** | Tiny ecosystem; early. Aggressive version number ≠ proven maturity. |
| **License** | **Business Source License (BSL)** — source-available, *not* OSI open-source **[doc]** | Future commercial-use restrictions / relicensing risk. |
| **Fit verdict** | **Specific workloads only, and immature** | Mismatched on durability, vectors, algorithms, protocol. |

**Net:** TuringDB targets fast in-memory graph *analytics*. Aura needs a *durable* graph with vectors and algorithms. **Not a replacement; do not adopt.**

---

### 2.3 Apache AGE vs. Neo4j

| Dimension | Apache AGE | Implication for Aura |
|---|---|---|
| **Category** | **PostgreSQL extension** — one engine for SQL + openCypher graph **[doc]** | Real store; *consolidates onto Postgres Aura already runs.* This is the interesting one. |
| **Storage** | Graph data in regular Postgres heap tables; B-tree index lookup **per hop** (not index-free adjacency) **[vendor/validate]** | Deep multi-hop traversals slower than native graph; shallow (2–3 hop) ≈ fine **[validate]**. |
| **Query language** | openCypher, invoked via `SELECT * FROM cypher('graph', $$ ... $$) as (...)` SQL wrapper **[doc]** | Cypher-*family* ✓ but **not** drop-in: syntax wrapper + subset means existing Neo4j Cypher and `mcp-neo4j-cypher` need rework. |
| **Vector search** | Not part of AGE **[absence]**; pgvector can run in the same Postgres but is a *separate* index over relational columns, not over AGE graph nodes **[validate]** | GraphRAG would need re-architecting: embeddings in pgvector tables joined to graph entities, not node-property vectors. |
| **Graph algorithms** | No built-in GDS-style library (PageRank/community detection) found **[absence]** | Leiden/PageRank would need an external lib or hand-rolled SQL/Cypher **[validate]**. |
| **Protocol** | Postgres wire protocol; **no Bolt** **[infer]** | agent-memory (Bolt driver) wouldn't connect; would need a Postgres-native rewrite. |
| **Maturity** | v1.7.0 (Jan 2026), ~4.6k stars, 504 forks, active; supports **PG 11–18** **[doc]** | Healthiest community of the three; ties to Postgres' own maturity. |
| **License** | **Apache 2.0** **[doc]** | Truly open; no Enterprise-license gate for scale features (scaling = Postgres replication). |
| **Ops** | Runs inside Postgres → **one fewer system** to operate, back up, monitor | Operationally attractive: unify on Postgres `pg_dump`/replication. |
| **Fit verdict** | **Partial replacement / test-first** | Viable only with meaningful rewrite; loses GDS, node-vectors, Bolt, MCP integration. |

**Net:** Apache AGE is the only candidate that competes for Neo4j's job *and* offers a real upside (collapse two datastores into one Postgres). But "replace Neo4j with AGE" means **rewriting the Cypher layer, replacing Bolt/MCP, re-architecting GraphRAG vectors onto pgvector, and sourcing graph algorithms** — substantial work for a system that runs fine today. **Test first; partial at best.**

---

## 3. Advantages / Disadvantages table

| Option | Advantages | Disadvantages (for Aura) | Replacement class |
|---|---|---|---|
| **PuppyGraph** | Zero-ETL graph over SQL/lakehouse; Cypher **+ Gremlin**; vectorized + pushdown; free single-node Developer Edition; scales to lakehouse data **[doc]** | No own storage; **no vector index**; **no GDS algorithms**; proprietary; solves a problem Aura doesn't have **[absence/doc]** | **Complementary / niche** — not for Aura |
| **TuringDB** | Very fast in-memory analytical reads; snapshot isolation; zero-locking; git-like versioning; Python SDK + REST **[doc]** | **In-memory only / no durability**; BSL (not OSI-open); tiny ecosystem; no vectors; no algorithms; no Bolt; openCypher subset **[doc/absence]** | **Specific workloads only** — immature |
| **Apache AGE** | Runs in Postgres (one fewer system); Apache-2.0; openCypher; PG 11–18; healthy community; SQL+graph in one engine **[doc]** | Cypher subset + SQL wrapper (not drop-in); **no native node-vector index**; **no GDS library**; no Bolt; slower deep traversals **[doc/absence/vendor]** | **Partial / test-first** |
| **Neo4j (stay)** | Native graph; **vector index in Community**; GDS algorithms; Bolt + Cypher + APOC; `mcp-neo4j-cypher` + agent-memory already wired; $0 at Aura's scale **[doc]** | Separate datastore from Postgres; Enterprise license needed for clustering/hot-backup/advanced GDS (Aura doesn't need these now) **[doc]** | **Incumbent** |

---

## 4. Migration risk assessment

| Risk area | PuppyGraph | TuringDB | Apache AGE |
|---|---|---|---|
| **Query rewrite (Cypher)** | Medium — openCypher, but loses Neo4j vector/GDS procedures | High — openCypher *subset*, undocumented gaps | High — subset + `cypher()` SQL wrapper; every query touched |
| **Protocol/integration** (`mcp-neo4j-cypher`, agent-memory Bolt) | High — no Bolt; MCP server is Neo4j-specific | High — REST/Python only, no Bolt | High — Postgres wire, no Bolt; agent-memory needs rewrite |
| **Vector / GraphRAG** | **Blocker** — no vector index | **Blocker** — no vector index | High — re-architect onto pgvector (separate from graph nodes) |
| **Graph algorithms (Leiden/PageRank)** | **Blocker** — no GDS library | **Blocker** — no GDS library | High — external lib / hand-rolled |
| **Durability** | N/A (no storage) | **Blocker** — in-memory only | Low — Postgres durability |
| **Data migration** | N/A (queries existing data) | Export/import unproven | Medium — graph export → AGE load |
| **Vendor/licensing** | Medium — proprietary beyond free tier | Medium-High — BSL relicensing risk | Low — Apache 2.0 |
| **Ecosystem/maturity** | Medium — proprietary, vendor-led | High — ~158 stars, early | Low-Medium — ~4.6k stars, active |
| **Overall migration risk** | **High (and pointless)** | **Very High** | **High (but the only justifiable bet)** |

**Cross-cutting risks & unknowns:**
- **[validate]** AGE deep-traversal performance vs. Neo4j on Aura's actual graph shape (multi-hop memory walks).
- **[validate]** pgvector + AGE co-existence ergonomics for GraphRAG (join embeddings ↔ graph entities) and recall quality parity.
- **[validate]** Whether a Leiden/PageRank path exists on AGE without unacceptable effort.
- **[validate]** TuringDB durability — if a persistence story exists beyond the docs reviewed, re-evaluate (still won't add vectors/algorithms).
- **[validate]** Exact Apache-graduation status of AGE (repo is under `apache/age`, strongly implying graduation; confirm if it matters for governance).
- **[validate]** All vendor performance numbers (PuppyGraph 10-hop/2.26s, etc.).

---

## 5. Recommended Proof-of-Concept plan

**Only Apache AGE warrants a PoC** (PuppyGraph and TuringDB are screened out on fit/maturity above). Run it **only if** Postgres-consolidation is judged strategically worthwhile; otherwise skip and stay.

**Scope:** ~1 week, time-boxed, throwaway branch, single mini-PC, no production data.

**Setup**
1. Add Apache AGE to an Aura-like Postgres (PG 16/17/18) via Docker; load APOC-free.
2. Export a representative slice of Aura's Neo4j graph (entities, relationships, agent-memory subgraph) and load into AGE.
3. Stand up pgvector in the same Postgres; load the 384d embeddings.

**Experiments & success criteria (measurable)**

| # | Test | Success criterion |
|---|---|---|
| 1 | Port 10 representative Aura Cypher queries (incl. multi-hop memory walks) to AGE `cypher()` syntax | ≥ 8/10 port with ≤ moderate rewrite; document the 2 hardest |
| 2 | Latency on Aura's typical 2–4 hop memory queries | p95 within **1.5×** of current Neo4j on the same hardware |
| 3 | GraphRAG hybrid retrieval rebuilt on AGE-graph + pgvector | Recall@5 / nDCG@10 **within 5%** of current Neo4j GraphRAG on a fixed eval set |
| 4 | Community detection (Leiden or equivalent) + PageRank path | A working path exists with **≤ 2 days** of effort, OR documented as a blocker |
| 5 | LLM↔graph access (replace `mcp-neo4j-cypher`) | A Postgres-native MCP/tool path works for read + write Cypher |
| 6 | Ops: single `pg_dump` backup/restore of graph + relational + vectors | Full round-trip restore verified |

**Decision gate:** Proceed to a real migration plan **only if** tests 2, 3, and 6 pass *and* tests 1, 4, 5 are at most "moderate effort." Any **blocker** in 3, 4, or 5 → stop, stay with Neo4j.

---

## 6. Final recommendation (per option)

| Option | Recommendation | Rationale |
|---|---|---|
| **PuppyGraph** | **Stay with Neo4j** (PuppyGraph = complementary niche only) | Query engine with no storage, no vector index, no GDS algorithms; its zero-ETL-over-SQL value prop doesn't match Aura's native-graph use case. **[doc/absence]** |
| **TuringDB** | **Stay with Neo4j** (do not adopt) | In-memory only / no durability, no vectors, no algorithms, no Bolt, BSL license, tiny ecosystem. Architecturally and maturity-wise unfit for durable agent memory. **[doc/absence]** |
| **Apache AGE** | **Test first** (potential *partial* replacement, not full) | Only candidate that is a real store *and* consolidates onto Aura's existing Postgres. But full replacement requires rewriting Cypher, Bolt/MCP, GraphRAG vectors, and algorithms. Worth a PoC **only if** consolidation is a strategic priority. **[doc/validate]** |
| **Overall** | **STAY with Neo4j** as the graph store; optionally run the §5 AGE PoC | Incumbent already provides storage + vector index (Community) + GDS + Bolt/MCP/agent-memory at $0 for Aura's scale. Migration cost is high; realized benefit is low/speculative. **[infer]** |

**Do not migrate now.** Revisit Apache AGE if/when "collapse Neo4j into Postgres" becomes a deliberate strategic objective — then, and only then, run the time-boxed PoC and let the decision gate decide.

---

## Sources

- PuppyGraph — homepage / product: <https://www.puppygraph.com/>
- PuppyGraph — Neo4j comparison & alternatives: <https://www.puppygraph.com/blog/neo4j-alternatives>
- PuppyGraph — Gremlin overview: <https://www.puppygraph.com/blog/gremlin-graph-database>
- Apache AGE — official site: <https://age.apache.org/>
- Apache AGE — manual (overview): <https://age.apache.org/age-manual/master/intro/overview.html>
- Apache AGE — source repo (version, PG support, license): <https://github.com/apache/age>
- Apache AGE — Snowflake engineering write-up (context): <https://www.snowflake.com/en/blog/engineering/graph-queries-postgres-apache-age/>
- TuringDB — docs: <https://docs.turingdb.ai/>
- TuringDB — source repo (license, maturity, in-memory model): <https://github.com/turing-db/turingdb>
- Neo4j — vector indexes (Community availability, dims, HNSW): <https://neo4j.com/docs/cypher-manual/current/indexes/semantic-indexes/vector-indexes/>
- Neo4j — Community Edition: <https://neo4j.com/product/community-edition/>
- Neo4j — GDS introduction & licensing: <https://neo4j.com/docs/graph-data-science/current/introduction/>
- Neo4j — GDS with cluster (Enterprise requirement): <https://neo4j.com/docs/graph-data-science/current/production-deployment/neo4j-cluster/>

**Validation caveats:** Version numbers, star counts, and release dates reflect the live repos/sites at research time (2026-06-20) and drift. Vendor performance benchmarks and all **[absence]** items (where a feature was simply not found in the pages reviewed) should be confirmed hands-on before any decision is finalized.
