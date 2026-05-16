# Phase 7C — Academic / arXiv State of the Art (2024–2026)

**Date:** 2026-05-16
**Target:** Aura masterplan Phase 7C — projection freshness registry. Tracking per-collection (and where useful per-doc) of `embedding_model_id`, `index_build_id`, `last_full_rebuild_at`, `dirty_count`, `freshness_sla` across: FTS5 `wiki_documents`, Qdrant `aura_memory_v1`, Qdrant `aura_memory_v1_compact`, `embedding_cache`, `compact_memory_documents`. Must be (a) observable, (b) actionable (idempotent reindex triggers), (c) safe against embedding-model swap.

**Method:** WebSearch + WebFetch over arXiv abstracts, ACL Anthology, vendor docs (Qdrant, Vespa, Weaviate, Pinecone, LlamaIndex). Compiled by general-purpose research subagent.

**Aura ground truth (do not re-litigate):**
- Go binary + SQLite + Qdrant sidecar
- Embedding locked: embeddinggemma-300m @ 256d MRL
- `tool_index_state.indexed_at` is the ONE persisted freshness column today
- `VectorHealthTracker` in-memory only (lost on restart)
- Phase 7B already adds typed Collection enum + score components + follow-up handles

---

## Q1. Granularity — per-collection, per-doc, per-chunk, or multi-level?

### Comparative table

| System / paper | Granularity | Where staleness lives | Storage overhead | Query overhead |
|---|---|---|---|---|
| Pinecone serverless (2025) | per-namespace LSN | header `x-pinecone-max-indexed-lsn` | 1 monotonic int per namespace | 0 (header on every response) |
| Vespa (docs) | per-document-type | reindex status endpoint (`pending`/`running`/`successful`/`failed`) | tiny (per type) | 0 — reindex is online |
| Weaviate aliases (2025) | per-collection (pointer swap) | alias → collection target | 1 alias row per swap | 0 — alias resolution is free |
| LlamaIndex `refresh_ref_docs` (docs) | per-document (hash) | docstore `doc_id → hash` map | 1 hash per doc | hash compare on ingest only |
| LiveVectorLake (arXiv:2601.05270, 2026) | per-chunk (SHA-256) + per-doc (`valid_from`/`status`) | hot tier + cold tier metadata | SHA + 2 timestamps per chunk | minor — surfaced on each hit |
| VersionRAG (arXiv:2510.08109, 2025) | per-document version chain + change-edges | hierarchical graph nodes | high (multi-version retention) | adds intent-classified routing step |
| FreshStack temporal analysis (arXiv:2603.04532, 2026) | corpus-level snapshot | snapshot timestamp only | n/a | n/a |
| ARM / "Selective Memory" (arXiv:2601.02428, 2026) | per-item decay score | dynamic memory substrate | counter + last-access per item | small re-weight |
| Mem0 (2026, mem0.ai/blog/state-of-ai-agent-memory-2026) | per-memory item | "stale memory" flag — author notes it remains "unresolved" | 1 flag/timestamp per item | small |
| Pinecone *additionally* (docs) | per-write LSN on response | header `x-pinecone-request-lsn` | n/a | 0 |

### Synthesis

Three things converge in the 2025-2026 literature.

First, **single-granularity registries lose either precision or actionability.** LiveVectorLake (arXiv:2601.05270, 2026) explicitly motivates multi-level: it carries `chunk_id` (SHA-256), `doc_id`, `valid_from`, and `status` per chunk *because* a registry that stops at the collection level cannot answer "is *this* hit fresh?". The dual-tier design is the existence proof — hot tier (Milvus/HNSW) is a per-chunk-addressed plane, cold tier (Delta Lake) is a per-doc-version plane, with the freshness state replicated on both.

Second, **per-collection alone is sufficient when the collection is the unit of rebuild** (Weaviate aliases, Pinecone serverless, Vespa per-document-type reindex). All three vendor docs treat the build-version question as a collection-or-larger problem because their rebuild action is a collection-or-larger action. Pinecone's LSN is per-namespace because writes commit per-namespace; Vespa's `pending/running/successful/failed` is per-document-type because that is the reindex unit (docs.vespa.ai/en/operations/reindexing.html).

Third, **per-document hash with no per-collection register is the LlamaIndex pattern** (`DocstoreStrategy.UPSERTS_AND_DELETE`, docs.llamaindex.ai). It deduplicates with `doc_id → document_hash` and re-runs transforms when the hash diverges — but it has no answer for "is the whole index stale because the embedding model changed?" This is exactly the gap Drift-Adapter (arXiv:2509.23471, 2025) and Weaviate's "When Good Models Go Bad" alias swap target.

### Recommendation for Aura

Adopt **two-level, not three-level**:

1. **Per-collection** as the primary registry row — one row per index unit (`wiki_documents_fts5`, `aura_memory_v1`, `aura_memory_v1_compact`, `embedding_cache`, `compact_memory_documents`). Each row carries `embedding_model_id`, `index_build_id`, `last_full_rebuild_at`, `last_incremental_at`, `pending_invalidation_count`. This is the unit at which Aura actually rebuilds (Phase 7A already established this for the compact mirror via `VectorHealthTracker`).
2. **Per-document hash** in a side table keyed by `(collection_id, doc_id) → content_hash + indexed_at`, mirroring the LlamaIndex `docstore` pattern (developers.llamaindex.ai/python/framework/module_guides/indexing/document_management/).

Do **not** add per-chunk freshness rows. Aura's chunks live inside Qdrant payload only; replicating SHA per chunk to SQLite costs storage and write amplification for negligible gain over per-doc — LiveVectorLake's per-chunk hashing makes sense at their scale (Milvus + Delta Lake) and their compliance use case (point-in-time historical queries, arXiv:2601.05270 §3); Aura has neither.

---

## Q2. Schema / data model — minimum industry-level fields

### Comparative table — what production systems expose

| Field | Pinecone | Weaviate | Vespa | Qdrant | LiveVectorLake (2026) | OpenTelemetry GenAI draft (2026) |
|---|---|---|---|---|---|---|
| `index_id` / `collection_name` | yes (header) | yes | yes | yes | yes (`collection`) | proposed |
| `embedding_model_id` / version | **NOT in response** (API version only) | **NOT exposed** (only at config) | **NOT** (config generation) | **NOT** in response | yes (`embedding_model`) | proposed (`gen_ai.embedding.model`) |
| `index_build_id` / generation | n/a | n/a | config generation (internal) | n/a | yes (`build_id`) | proposed |
| `indexed_at` per row | n/a | n/a | n/a (use `documentId`+CDC) | n/a | yes (`valid_from`) | proposed (`gen_ai.retrieval.indexed_at`) |
| LSN / monotonic freshness | yes (`x-pinecone-max-indexed-lsn`) | n/a | transaction log internal | n/a | implicit (Delta version) | n/a |
| Per-hit `staleness_ms` | derivable from LSN | n/a | n/a | n/a | yes (`valid_from` → now) | proposed |
| `status` (active/superseded/deleted) | n/a | n/a | n/a (doc presence only) | n/a | yes | n/a |
| `degraded_read` flag | n/a | n/a | n/a | n/a | implicit | proposed |

### Synthesis

`embedding_model_id` and `index_build_id` are **NOT** universally raccomandati at the **response** level today. They are universally recommended at the **configuration** level — every paper that discusses embedding swap (Drift-Adapter, arXiv:2509.23471 §3; Query Drift Compensation, arXiv:2506.00037 §4; "When Good Models Go Bad", weaviate.io/blog 2025) assumes the catalog knows which model produced which index. None of Pinecone, Weaviate, Qdrant, or Vespa **return** the embedding model name on a search response (verified via vendor docs above). The GenAI semantic-conventions SIG in OpenTelemetry is drafting attributes but there is no stable standard yet ("no OTel standard for RAG semantic conventions as of early 2026", futureagi.com/blog/what-is-rag-observability-2026).

The systems that DO surface model/version per-hit are research systems (LiveVectorLake arXiv:2601.05270 returns `chunk_id`, `valid_from`, `status`; OwlerLite arXiv:2601.17824 returns `scope` + `version`). The production lore says: surface freshness via **side channel** (Pinecone LSN headers; Vespa reindex endpoint; Weaviate alias name), not embedded in payload.

### Concrete schema for Aura

```sql
-- One row per index unit
CREATE TABLE projection_freshness_collection (
  collection_id          TEXT PRIMARY KEY,   -- e.g. 'aura_memory_v1', 'wiki_documents_fts5'
  kind                   TEXT NOT NULL,      -- 'qdrant' | 'fts5' | 'cache' | 'compact_docs'
  embedding_model_id     TEXT,               -- 'embeddinggemma-300m@256-mrl' (NULL for FTS5)
  embedding_dim          INTEGER,            -- 256 (NULL for FTS5)
  index_build_id         TEXT NOT NULL,      -- ULID; bumped on every full rebuild
  schema_version         INTEGER NOT NULL,
  last_full_rebuild_at   TEXT NOT NULL,      -- RFC3339
  last_incremental_at    TEXT,
  pending_invalidations  INTEGER NOT NULL DEFAULT 0,
  health_state           TEXT NOT NULL,      -- 'fresh' | 'rebuilding' | 'degraded' | 'stale_swap'
  health_reason          TEXT,               -- free text for retrieval-time agent surface
  updated_at             TEXT NOT NULL
);

-- One row per (collection, doc) for hash-based change detection
CREATE TABLE projection_freshness_doc (
  collection_id          TEXT NOT NULL,
  doc_id                 TEXT NOT NULL,      -- wiki slug, compact mem id, source sha, etc.
  content_hash           TEXT NOT NULL,      -- sha256 of source-of-truth bytes
  indexed_at             TEXT NOT NULL,
  index_build_id         TEXT NOT NULL,      -- matches collection row at write time
  PRIMARY KEY (collection_id, doc_id)
);
```

The `(collection_id, doc_id, content_hash, indexed_at, index_build_id)` five-tuple is the **minimum** to: (a) detect drift on individual docs (LlamaIndex hash pattern), (b) detect drift on the whole index (Pinecone-style build_id bump), (c) report `staleness_ms = now - indexed_at` at retrieval time (LiveVectorLake pattern, arXiv:2601.05270), (d) refuse-or-degrade reads when `embedding_model_id` on the live collection diverges from the cached query encoder (Drift-Adapter prerequisite, arXiv:2509.23471 §2.1).

### Recommendation for Aura

- **Persist** the two tables above. The current `tool_index_state.indexed_at` is one row in a degenerate version of this — promote it.
- **Surface** `(collection_id, index_build_id, embedding_model_id, staleness_seconds, health_state)` on every retrieval response that the agent sees. Per OpenTelemetry GenAI draft conventions and OwlerLite (arXiv:2601.17824), this is the minimum that lets an LLM agent decide to retry or to flag a degraded answer.
- **Do not** invent custom names: use `embedding_model_id`, `index_build_id`, `indexed_at`, `staleness_seconds`, `degraded_read` — these match the OpenTelemetry GenAI draft + LiveVectorLake vocabulary so we are interoperable when the SIG ships.

---

## Q3. Staleness triggers — what events mark an index stale?

### Comparative table — taxonomy across sources

| Trigger | Pinecone | Vespa | Weaviate ("good models go bad") | LlamaIndex docstore | LiveVectorLake (2601.05270) | VersionRAG (2510.08109) | Context Drift (atlan.com 2026) |
|---|---|---|---|---|---|---|---|
| Write to source (upsert/update/delete) | LSN bump | reindex trigger | manual | hash change → rerun pipeline | CDC + SHA change | new version node | yes |
| Embedding-model swap | n/a (rebuild required) | reindex new schema | alias swap explicit | manual | dual-tier swap | n/a | yes ("schema drift") |
| Schema / chunking change | n/a | new config generation triggers reindex | alias swap | manual | schema_version bump | implicit | yes |
| Tokenizer / FTS analyzer change | n/a | reindex per type | n/a | n/a | n/a | n/a | yes |
| Decay timeout (time-based) | n/a | n/a | n/a | n/a | n/a (compliance, not decay) | n/a | yes ("staleness threshold") |
| Periodic full rebuild for entropy | n/a | "REINDEX CONCURRENTLY" cooldown advised (medium 2025) | n/a | n/a | periodic reconciliation | n/a | yes |
| External corruption / health-check failure | n/a | failed state | n/a | n/a | "uncommitted" flag in Delta | n/a | yes |

### The 6 triggers Aura should encode

1. **Source-write**: any commit to wiki page, ingestion of a new source, or conversation archived. *Marks affected `doc_id` rows pending; bumps `pending_invalidations`.*
2. **Embedding-model swap**: change of `embedding_model_id` in settings. *Marks every Qdrant collection `stale_swap`; refuses semantic-search writes; triggers Drift-Adapter or full rebuild — see Q6.*
3. **Schema/chunking change**: `schema_version` bump in wiki frontmatter, change to chunk-window size, FTS5 tokenizer change. *Marks affected collections `degraded`; full rebuild required.*
4. **FTS5 trigger inconsistency**: detected by row-count divergence between `wiki_documents` content table and FTS5 index (sqlite.org/fts5.html). *Marks `fts5` collection `degraded`; `INSERT INTO wiki_documents(wiki_documents) VALUES('rebuild')`.*
5. **Decay timeout**: collection has not been touched in N days AND source-of-truth has > X rows newer than `last_full_rebuild_at`. *Soft trigger; emits a maintenance suggestion, not an auto-rebuild.*
6. **Health-check failure**: Qdrant returns 5xx, dimension mismatch, or collection-not-found on routine ping. *Marks `health_state='degraded'`; preserves `index_build_id`; lazy re-create on next write.*

### Recommendation for Aura

Encode triggers 1-4 as **eager** events (write-time invalidate). Trigger 5 as **scheduled** (already implicit in Aura's tool reconciler from Wave 2.10.b, commit `2367f502`). Trigger 6 as **lazy** (on next read, detect and degrade).

---

## Q4. Reconciliation pattern — lazy vs eager vs scheduled vs hybrid

### Recommendation for Aura

Hybrid, with explicit assignment of each trigger to a pattern:

| Trigger | Pattern | Why |
|---|---|---|
| Source write | **Eager** (LlamaIndex-style hash + upsert) | Cheap per-doc; serializes with Git commit; no read-side spike |
| Embedding-model swap | **Hybrid alias swap** (Weaviate/Qdrant pattern) | Build new collection in background, atomic rename via Qdrant aliases |
| Schema / chunking / FTS analyzer change | **Scheduled rebuild** with `health_state='degraded'` warning surfaced *immediately* | Operator-initiated; reads degrade but still serve |
| FTS5 inconsistency detection | **Lazy** on next FTS-touching write | Cheap to detect (row counts); only rebuilds when needed |
| Decay | **Scheduled** (cron sweep, low priority) | Already a background concern in Aura tool reconciler |
| Health-check failure | **Lazy** (mark on next read, degrade) | Preserves availability; matches Qdrant/Pinecone eventual-consistency model |

This pattern minimizes blast radius because:
1. Eager paths run *inside an existing async write* (Git commit, source ingest, conversation archive) — no new latency in the agent loop.
2. The alias-swap atomicity is provided by Qdrant itself, not by Aura code (api.qdrant.tech/api-reference/aliases/update-aliases).
3. The scheduled path runs inside Aura's existing tool reconciler (Wave 2.10.b) with the same `fsnotify` + periodic cadence — no new goroutine, no new lifecycle.
4. The lazy path adds at most one SQLite row-count query on the cold-path of an inconsistency detection.

---

## Q5. Retrieval-time annotation — what to attach per-hit

### The pragmatic 2026 minimum

```json
{
  "chunk_id": "...",
  "similarity_score": 0.83,
  "doc_id": "...",
  "indexed_at": "2026-05-14T08:21:33Z",
  "staleness_seconds": 178200,
  "embedding_model_id": "embeddinggemma-300m@256-mrl",
  "index_build_id": "01HXY...",
  "degraded_read": false,
  "retriever_strategy": "qdrant_semantic"
}
```

### What lets the agent "decide non fidarsi"

Three signals, in priority order:
1. `degraded_read: true` — the index is known-bad (e.g. embedding model swap in flight, FTS5 inconsistency detected). Agent should explicitly disclose in its reply.
2. `embedding_model_id` mismatch between query encoder and hit (this is **the** Drift-Adapter signal, arXiv:2509.23471 §2.1, and the core danger of an undeclared swap).
3. `staleness_seconds` exceeds a domain-specific threshold (e.g. 7-day half-life per arXiv:2509.19376 §3).

### Recommendation for Aura

- Attach the **9-field annotation block** above to every hit returned by `search_memory`, `wiki search`, and the compact-memory retriever.
- The agent system prompt must explicitly note the `degraded_read` flag's semantics — "if degraded_read=true on more than half your hits, tell the user the answer is best-effort because the index is rebuilding".
- The `embedding_model_id` *must* be the resolved string (e.g. `embeddinggemma-300m@256-mrl-checkpoint-v3`), not just `embeddinggemma`.

---

## Q6. Embedding-model swap — the catastrophic case

### Specific recommendation for Aura

Aura's reality is locked to **embeddinggemma-300m at 256d MRL**, so swap is a future-proofing concern, not a current one. The Phase 7C contract should:

1. **Treat the alias swap as the primary path.** Use Qdrant's `UpdateAliases` (api.qdrant.tech/api-reference/aliases/update-aliases) as the rebuild primitive.

2. **Reserve Drift-Adapter (arXiv:2509.23471) as the fallback** for the *one* scenario where alias swap is impractical: an embeddinggemma minor-version upgrade on a cold mini-PC where a 100 GPU-hr full rebuild is infeasible.

3. **Do NOT plan for Query Drift Compensation** (arXiv:2506.00037) — it assumes continual-learning lineage which embeddinggemma releases do not advertise.

4. **Do NOT plan for dual-index parallel-serve** — 2× RAM on a 16-core mini-PC under the CPU budget is the wrong trade.

5. **Refuse semantic-search writes when `embedding_model_id` in settings != the live collection's `embedding_model_id`** until the alias swap completes. This is the `health_state='stale_swap'` row of the registry.

---

## ADOPT / AVOID for Aura Phase 7C

### ADOPT

1. **Two-level freshness registry (per-collection + per-doc hash)** — LlamaIndex `DocstoreStrategy.UPSERTS_AND_DELETE` + LiveVectorLake CDC+SHA pattern (arXiv:2601.05270 §3.2).
2. **9-field per-hit retrieval annotation** — `chunk_id, similarity_score, doc_id, indexed_at, staleness_seconds, embedding_model_id, index_build_id, degraded_read, retriever_strategy`. OpenTelemetry GenAI draft + OwlerLite (arXiv:2601.17824).
3. **Hybrid reconciliation: eager source-write + scheduled decay + lazy health-check + alias swap for model swap.**
4. **Qdrant alias swap as primary model-swap path; Drift-Adapter Orthogonal Procrustes as fallback** (arXiv:2509.23471, EMNLP 2025).
5. **Six explicit staleness triggers, encoded as DB events.**

### AVOID

1. **Per-chunk freshness rows** — only LiveVectorLake does this; motivated by compliance Aura does not have.
2. **Query Drift Compensation as standalone strategy** — assumes continual-learning lineage; embeddinggemma upgrades don't advertise it.
3. **Dual-index parallel-serve** — 2× RAM on mini-PC is wrong trade.

---

## Citation index (year + permanent identifier)

**arXiv papers**
- arXiv:2506.00037 — Query Drift Compensation in Continual Learning of Retrieval Embedding Models, CoLLAs 2025
- arXiv:2509.23471 — Drift-Adapter: Near Zero-Downtime Embedding Model Upgrades in Vector Databases, EMNLP 2025
- arXiv:2509.19376 — Solving Freshness in RAG: A Simple Recency Prior, 2025
- arXiv:2510.08109 — VersionRAG: Version-Aware Retrieval-Augmented Generation for Evolving Documents, 2025
- arXiv:2505.16133 — HASH-RAG: Deep Hashing with Retriever, 2025
- arXiv:2511.09803 — Retrieval as a Decision: Training-Free Adaptive Gating, 2025
- arXiv:2601.05270 — LiveVectorLake: Real-Time Versioned KB for Streaming Vector Updates, 2026
- arXiv:2603.04532 — Still Fresh? Evaluating Temporal Drift in Retrieval Benchmarks, 2026
- arXiv:2601.02428 — Dynamic RAG with Selective Memory and Remembrance (ARM), 2026
- arXiv:2602.03442 — A-RAG: Agentic RAG via Hierarchical Retrieval Interfaces, 2026
- arXiv:2601.17824 — OwlerLite: Scope- and Freshness-Aware Web Retrieval for LLM Assistants, 2026
- arXiv:2603.07670 — Memory for Autonomous LLM Agents: Mechanisms, Evaluation, Frontiers, 2026
- arXiv:2504.13128 — FreshStack: Realistic Benchmarks for Technical Documents, NeurIPS 2025

**Vendor / standard docs**
- Pinecone — Check data freshness: docs.pinecone.io/guides/index-data/check-data-freshness
- Weaviate — Collection aliases: docs.weaviate.io/weaviate/manage-collections/collection-aliases
- Weaviate — "When Good Models Go Bad" blog: weaviate.io/blog/when-good-models-go-bad
- Vespa — Reindexing: docs.vespa.ai/en/operations/reindexing.html
- Vespa — Partial updates: docs.vespa.ai/en/writing/partial-updates.html
- Qdrant — Update collection aliases: api.qdrant.tech/api-reference/aliases/update-aliases
- LlamaIndex — Document Management: developers.llamaindex.ai/python/framework/module_guides/indexing/document_management
- SQLite — FTS5 extension: sqlite.org/fts5.html
