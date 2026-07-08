# Solving Leiden + Rerank for Aura (store-agnostic)

**Date:** 2026-06-20 · **Trigger:** the two open technical gaps from the ArcadeDB eval.
**Key reframing:** both solutions are **independent of the Neo4j-vs-ArcadeDB decision** — they upgrade Aura's *current* Neo4j stack. Neither requires migrating.

**Evidence:** `[doc]` official · `[ver]` verified in spike 068 · `[ref]` reference implementation (Microsoft GraphRAG etc.).

> **⚠ CORRECTION (2026-07-08).** §1 below originally claimed *"Neo4j Community does NOT have Leiden — it's a Neo4j Enterprise GDS algorithm (paid)."* **That is false.** Leiden (and PageRank, Louvain, WCC, …) ship in **GDS *Community* (free)** and run on a free Neo4j Community DB via `CALL gds.leiden.*` over Bolt; Enterprise only lifts the **4-core concurrency cap**, it does not gate the algorithm. The external-`leidenalg` recommendation still stands — but as an *optimization* (store-agnostic, sidesteps the 4-core cap, adds hierarchical communities), **not** because Neo4j Community lacks Leiden. Source: Neo4j GDS docs (Leiden; Introduction → Community vs Enterprise). Inline claims corrected below.

---

## 1. Leiden community detection

### The real problem (bigger than ArcadeDB)
Aura's memory consolidation wants **Leiden** community detection over the entity/memory graph. The invocation story differs per store:
- **Neo4j GDS *Community* (free) HAS Leiden** — `CALL gds.leiden.*` runs in-engine over Bolt on a free Neo4j Community DB; the only Enterprise gate is the **4-core concurrency cap**, not the algorithm. `[doc]`
- **ArcadeDB has Leiden, but Java-API-only** — `new GraphAlgorithms().pageRank(gav, …)` on a `GraphAnalyticalView`; no SQL/HTTP/Cypher invocation exists in the docs, the OLAP blog, or the `arcadedb-usecases/graph-rag` example. Aura (Go over Bolt/HTTP) can't reach it without embedding Java. `[ver]`

→ On **ArcadeDB** Leiden isn't reachable over the wire (Java-only); on **Neo4j** it *is* (`CALL gds.leiden.*` over Bolt, GDS Community, 4-core cap). An **external** consolidation job is still the recommended path — store-agnostic, sidesteps the 4-core cap, yields hierarchical communities, and exactly how the state-of-the-art (Microsoft GraphRAG) does it — but as an *optimization*, not because Neo4j can't.

### Recommended solution: external Python consolidation job
Compute communities outside the DB with a C++-backed Leiden lib, write `community_id` back as a node property. This is **the same pattern Microsoft GraphRAG uses** (`graspologic.partition.hierarchical_leiden`). `[ref]`

**Library choices (all pip, no Enterprise, CPU, Apache/GPL):**
- `leidenalg` + `python-igraph` — the canonical Leiden (vtraag, the algorithm's author). Fast C++ core. `[doc]`
- `graspologic` `hierarchical_leiden` — hierarchical communities, GraphRAG-proven (good when you want nested topics). `[ref]`
- `cdlib` — wraps both behind one API if you want to swap quality functions.

**Shape (store-agnostic — reads over Bolt, works on Neo4j *and* ArcadeDB):**
```python
import igraph as ig, leidenalg as la
# 1. pull the entity edges over Bolt (neo4j driver works against BOTH Neo4j and ArcadeDB)
#    MATCH (a:Entity)-[r:RELATED_TO]-(b:Entity) RETURN a.id, b.id, r.confidence
edges, weights = fetch_entity_edges()           # [(a_id,b_id)], [w]
g = ig.Graph.TupleList(edges, directed=False)
part = la.find_partition(g, la.RBConfigurationVertexPartition,
                         weights=weights, resolution_parameter=1.0, seed=42)
# 2. write community ids back: UNWIND $rows MATCH (e:Entity{id:row.id}) SET e.community_id=row.c
write_back([(v["name"], part.membership[i]) for i,v in enumerate(g.vs)])
```

**Aura wiring:**
- Run it as a **scheduled job** (Aura already has the cron/scheduler from Slice 6) or a thin sidecar — *not* per-turn; consolidation is periodic/background.
- Deterministic with a fixed `seed`; `resolution_parameter` tunes community granularity.
- Mirrors the existing memory-consolidation idea (`ConsolidationRun` label already exists in the live graph).

**Why not the alternatives:** in-engine `CALL gds.leiden.*` works on Neo4j (GDS Community, free) but is 4-core-capped and writes back inside the query transaction; ArcadeDB Java sidecar = couples Aura to ArcadeDB + a JVM component; `networkx` Louvain = pure-python but lower quality + slower than C++ Leiden. The external `leidenalg`/`graspologic` job is the cheapest, highest-quality, store-agnostic path.

---

## 2. Reranking for GraphRAG retrieval

### The problem
Aura's retrieval is hybrid (BM25 + vector + graph). First-stage retrieval (cosine / RRF) optimizes recall; a **cross-encoder reranker** is the precision second stage that reorders the top-N candidates — especially valuable for **Italian** queries where bi-encoder cosine is noisier.

### Recommended solution: a llama.cpp reranker sidecar
llama.cpp natively serves rerankers. `[doc/ver-from-docs]`
- **Model (primary): `Qwen3-Reranker-0.6B`** — 0.6B, **32k context**, **100+ languages** (incl. Italian), **instruction-aware** (pass a task instruction to steer ranking), Apache-2.0. Scores from normalized yes/no token logits. Per its model card it **beats bge-reranker-v2-m3** on multilingual reranking: MTEB-R **65.80 vs 57.03**, MMTEB-R **66.36 vs 58.36**, MLDR **67.28 vs 59.51**. `[ref]`
  - GGUF: use the **correctly-converted** `Voodisss/Qwen3-Reranker-0.6B-GGUF-llama_cpp` (community conversions miss `cls.output.weight` → garbage ~4.5e-23 scores).
  - **Quant: Q4_K_M (396 MB)** = sweet spot (~0.3% loss) — or Q8_0 (639 MB). **DO NOT use Q2_K** — the card reports it is *"unusable (−28.7% NDCG@10)"*; small rerankers are the most quant-sensitive.
- **Alternative:** `bge-reranker-v2-m3` (~568M, multilingual, Apache-2.0, Q8_0 ~600 MB) — the established RAG default; use if you want a non-instruction, pooling=rank classic cross-encoder.
- **Serve** (same image as `aura-llama-embed`, `ghcr.io/ggml-org/llama.cpp:server`):
  ```
  llama-server --hf-repo Voodisss/Qwen3-Reranker-0.6B-GGUF-llama_cpp \
               --hf-file Qwen3-Reranker-0.6B-Q4_K_M.gguf \
               --reranking --pooling rank --embedding \
               -c 4096 -t 4 --host 0.0.0.0 --port 8085 --alias aura-rerank
  ```
  (`--reranking` = `--embedding` + `--pooling rank`.)
- **Endpoint:** `POST /v1/rerank`
  ```json
  { "model":"aura-rerank", "query":"<user query>", "documents":["chunk1","chunk2", ...] }
  → { "results":[ {"index":0,"relevance_score":0.95}, {"index":1,"relevance_score":0.12} ] }
  ```
  Scores 0–1; **sort descending client-side**.

**Two-stage retrieval pattern:**
1. First stage (hybrid): BM25 + vector (`db.index.vector.queryNodes` on Neo4j / `vectorNeighbors()` on ArcadeDB) + 1-hop graph → **top-N (~30–50)** candidates.
2. **Rerank:** send (query, N chunk texts) to `/v1/rerank` → reorder → **top-K (~5)** for the LLM context.

**RRF as the zero-model baseline:** Aura already studied **Reciprocal Rank Fusion** (spike 056 — production shape = embedding-primary, guarded RRF). RRF fuses BM25+vector ranks with no model; the cross-encoder rerank is the *precision upgrade on top* of (or instead of) RRF for the final top-K. Ship RRF first (free), add the reranker sidecar when precision@5 needs the lift.

**Aura wiring:**
- New compose sidecar `aura-rerank` (loopback `:8085`), CPU, `-t 4` (mini-PC budget), fail-soft (retrieval degrades to RRF order if the sidecar is down).
- Small Go client mirroring `documents.EmbeddingClient` → `RerankClient.Rerank(query, docs) []scored`.
- Caveat: rerank is one cross-encoder pass over N docs → keep **N ≤ 30–50** and **measure p95**; it adds latency proportional to N×query-len.

---

## Bottom line
- **Leiden:** external `leidenalg`/`graspologic` consolidation job over Bolt — store-agnostic, free, GraphRAG-proven. Neo4j GDS Community already provides `CALL gds.leiden.*` in-engine (free, 4-core cap), so the external job is an *optimization* (sidesteps the cap, adds hierarchical communities), not a gap-filler; on ArcadeDB it's the only path (Java-only in-engine).
- **Rerank:** a `bge-reranker-v2-m3` llama.cpp sidecar (`/v1/rerank`) for two-stage retrieval, with RRF as the free fusion baseline. Pure CPU, fits the existing sidecar pattern.

Neither depends on switching databases — both are net upgrades to the current Neo4j pipeline.
