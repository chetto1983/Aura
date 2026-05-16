# Phase 7B — Web Research: Collection Registry, Citation Handles, Freshness, Parent Expansion

Date: 2026-05-16
Audience: Aura masterplan Phase 7B (PRD §7)
Scope: typed collections + metadata registry + citation handles + projection freshness + parent-chunk expansion
Method: WebFetch / WebSearch of primary framework docs (2024-2026). Marketing fluff flagged inline.

---

## 1. LlamaIndex

**A. Collection model.** `VectorStoreIndex` is built from a list of `Node` objects (chunks) parsed out of `Document` containers. There is no single "collection" primitive in core LlamaIndex — the unit of separation is "one index per logical corpus," and the actual collection/namespace concept is delegated to the underlying vector store (Qdrant `collection_name`, Pinecone `namespace`, etc.) ([VectorStoreIndex docs](https://developers.llamaindex.ai/python/framework/module_guides/indexing/vector_store_index/), [Documents & Nodes docs](https://developers.llamaindex.ai/python/framework/module_guides/loading/documents_and_nodes/)).

**B. Metadata registry shape.** Freeform `metadata: Dict[str, Any]` on both `Document` and `Node`. Every `Node` derived from a parsed `Document` **inherits the parent's metadata automatically** (e.g. `file_name`) — that auto-propagation is the only schema discipline LlamaIndex enforces. There is no typed schema declared up-front; the developer is expected to keep keys consistent. Pro: zero-ceremony. Con: drift across pipelines is the #1 production complaint and Qdrant docs explicitly call this out ("payload schema is inconsistent across data pipelines") ([Qdrant payload best practices search](https://qdrant.tech/documentation/manage-data/payload/)).

**C. Citation handles.** Every `Node` carries `node_id` (UUID), `ref_doc_id` (the source `Document`), and a `relationships` dict. The retriever returns `NodeWithScore` containing both the node and its score — the consumer can walk `relationships[NodeRelationship.SOURCE]` to get back to the original document for citation rendering ([NodeRelationship API ref](https://docs.llamaindex.ai/en/v0.10.19/api/llama_index.core.schema.NodeRelationship.html), [schema.py](https://github.com/run-llama/llama_index/blob/main/llama-index-core/llama_index/core/schema.py)).

**D. Freshness model.** Two mechanisms: (1) `IngestionPipeline` with a `DocstoreStrategy` (UPSERTS / DUPLICATES_ONLY) that hashes documents and skips unchanged ones; (2) `refresh_ref_docs()` on the index which re-runs ingestion only for changed `Document.doc_id`. There is **no global "this projection is stale" signal** — freshness is tracked per-doc by content hash, not per-collection ([Documents & Nodes docs](https://developers.llamaindex.ai/python/framework/module_guides/loading/documents_and_nodes/)).

**E. Parent-chunk expansion.** First-class. The `NodeRelationship` enum has `{SOURCE, PREVIOUS, NEXT, PARENT, CHILD}`. The "small-to-big" / `RecursiveRetriever` pattern indexes small `IndexNode`s that hold an `index_id` pointer to a larger `TextNode`; retrieval matches the small chunk, then follows the pointer to fetch the parent for synthesis. Reported MRR 0.72 vs 0.56 baseline on their own example ([Recursive Retriever example](https://developers.llamaindex.ai/python/examples/retrievers/recursive_retriever_nodes/)). This is the pattern Aura's PRD §7 is implicitly describing.

**F. Hybrid retrieval with metadata.** Metadata filters are pushed down to the vector store as `MetadataFilters` (pre-rank — the vector store enforces the predicate during ANN search, not after). Hybrid fusion is via `QueryFusionRetriever` with RRF.

---

## 2. LangChain

**A. Collection model.** No native collection abstraction — the `VectorStore` base class is intentionally thin and delegates "collection" semantics to the backend. Pinecone uses `namespace`, Qdrant uses `collection_name`, Chroma uses `collection`, Postgres `PGVector` uses one table per collection ([LangChain VectorStore API](https://python.langchain.com/api_reference/core/vectorstores/langchain_core.vectorstores.base.VectorStore.html), [Cloud SQL LangChain blog](https://cloud.google.com/blog/products/databases/vectorstore-in-the-cloud-sql-for-postgresql-langchain-package/)). This is portability theatre — in practice you write backend-specific code.

**B. Metadata registry shape.** `Document` has two fields: `page_content: str` and `metadata: Dict[str, Any]`. **Freeform JSON, no schema.** The community has filed [issue #17459](https://github.com/langchain-ai/langchain/issues/17459) and similar repeatedly asking for typed metadata; the official answer remains "convention only."

**C. Citation handles.** `Document.metadata["source"]` is the de-facto convention for citation, but it is **not enforced**. The `Retriever` interface returns `List[Document]` with no stable handle beyond whatever the loader put in `metadata`. For real citation grounding the community has migrated to LangGraph's "structured tool output" pattern where the LLM is forced to return `{answer, citations: [doc_id...]}` via a Pydantic schema.

**D. Freshness model.** None at the framework level. Either rebuild the index or use `indexing.index()` (LangChain's content-aware indexer that hashes by `source` + `content` and upserts) — same idea as LlamaIndex's `IngestionPipeline`. No collection-wide "stale" bit.

**E. Parent-chunk expansion.** `ParentDocumentRetriever`: stores small chunks in the vector store with a `doc_id` payload pointing at a parent doc in a separate `InMemoryStore` / `LocalFileStore`. Retrieval = vector match on small chunks → group by `doc_id` → return parents. It works but the parent store is a separate KV — not a relationship graph like LlamaIndex.

**F. Hybrid retrieval with metadata.** `EnsembleRetriever` + RRF on top of multiple retrievers. Metadata filtering is backend-specific (`search_kwargs={"filter": {...}}`), generally pre-rank.

**Verdict.** LangChain's metadata story is the negative example. Useful as the "what not to do" baseline.

---

## 3. Haystack (deepset)

**A. Collection model.** The unit is `DocumentStore` (one store per logical corpus). The store is **not a pipeline component** — it's an injected dependency with a four-method protocol: `count_documents / filter_documents / write_documents / delete_documents` ([DocumentStore docs](https://docs.haystack.deepset.ai/docs/document-store)).

**B. Metadata registry shape.** `Document` has `content`, `meta` (freeform dict), `id` (auto from SHA-256 of content if not provided), `score`, plus typed fields like `embedding`, `sparse_embedding`, `dataframe`, `blob`. So Haystack is **freeform meta but with a typed envelope** — closer to a hybrid than pure LangChain. `DuplicatePolicy` enum (OVERWRITE/SKIP/FAIL) is the ingest contract.

**C. Citation handles.** The auto-derived `id` from content hash is the stable handle. Citations are surfaced via the pipeline's `AnswerBuilder` which packs `documents: List[Document]` alongside the generated answer — caller can render `meta["url"]` / `meta["source"]`.

**D. Freshness model.** Content-hash IDs + `DuplicatePolicy.OVERWRITE` give automatic dedup on re-ingest. No collection-wide stale signal; freshness is implicit ("if the hash changed it's a new doc").

**E. Parent-chunk expansion.** Not built-in as a first-class relationship. The community pattern is `meta["parent_id"]` + a custom retriever post-step that fetches parents via `filter_documents({"id": {"$in": parent_ids}})`. Workable but not blessed.

**F. Hybrid retrieval with metadata.** Rich filter DSL: `{field, operator, value}` with operators `==, !=, >, >=, <, <=, in, not in` plus `AND/OR/NOT` logic nodes ([Metadata filtering docs](https://docs.haystack.deepset.ai/docs/metadata-filtering)). Filtering is **pre-rank inside the store** for backends that support it (Elasticsearch, OpenSearch, Qdrant), post-rank for `InMemoryDocumentStore`. The docs explicitly warn this varies by backend.

---

## 4. R2R (SciPhi)

**A. Collection model.** R2R promotes "Collections" to a first-class concept: a container that **groups documents AND defines an ACL** (owner / members / non-members). Every user gets a default collection on signup ([R2R README](https://github.com/SciPhi-AI/R2R), [Collections docs](https://r2r-docs.sciphi.ai/documentation/collections) — note the latter 404'd at fetch time, info from search snippets).

**B. Metadata registry shape.** Document-level metadata is freeform JSON, but the collection itself is **the typed envelope** (collection has owner, members, name, description, KG settings). This is the only framework in the survey where the collection is itself a typed first-class object with its own CRUD.

**C. Citation handles.** `client.retrieval.rag(query=...)` returns answer text **with inline citation markers tied back to `document_id` and `chunk_id`**. Citations are not a convention layered on metadata — they're a guaranteed part of the response contract.

**D. Freshness model.** Document-level `created_at` / `updated_at`. KG is auto-rebuilt on ingest. Documentation does not surface a "projection stale" bit per se — the assumption is the agentic loop re-queries on demand. **Marketing fluff alert:** "Deep Research API" sounds like agentic orchestration on top of the same retrieval primitives — not a separate freshness mechanism.

**E. Parent-chunk expansion.** Chunks store `document_id` pointers; retrieval can return either chunk-level or document-level hits. Not a true graph of relationships — flat parent pointer.

**F. Hybrid retrieval with metadata.** Native hybrid (semantic + keyword) fused with RRF. Filters are pushed down via a Postgres backend (R2R is Postgres-first, not Qdrant-first).

---

## 5. Letta (formerly MemGPT)

**A. Collection model.** Four named memory layers, each with a different access modality ([MemGPT concepts](https://docs.letta.com/concepts/memgpt/), [Memory blocks guide](https://docs.letta.com/guides/agents/memory-blocks/), [Memory management](https://docs.letta.com/advanced/memory-management/)):

| Layer | Access | Storage | Purpose |
|---|---|---|---|
| Core memory (blocks) | always in-context | structured blocks with `label/description/value/limit` | persona, human, working facts |
| Archival memory | tool-call `archival_memory_search` | vector DB | long-running explicit knowledge |
| Recall memory | tool-call (date / semantic) | conversation log table | prior turns |
| FIFO message queue | implicit | message buffer | sliding window |

**B. Metadata registry shape.** **Typed by layer, not by free metadata.** Each core memory block has a `label`, `description`, `value`, `limit`, `read_only` — these are the registry entries. The agent learns to use a block from its `description` field. This is the most opinionated typed-registry design in the survey.

**C. Citation handles.** Block label = stable handle. Archival passages have IDs returned by `archival_memory_search`. There's no rich citation grammar — Letta is an agent runtime, not a RAG framework, so it cites by injecting the retrieved passage into context.

**D. Freshness model.** Core blocks are *always fresh* by definition (they're in context). Archival is append-only with semantic search — no TTL primitive. The newer "MemFS" feature (mentioned in their API platform page) introduces git-tracked memory, which is the closest thing to a per-block versioning / freshness signal.

**E. Parent-chunk expansion.** Not the model — Letta's archival passages are self-contained. No small-to-big retrieval.

**F. Hybrid retrieval with metadata.** Archival is vector-only. Recall has a date-index path. No RRF.

---

## 6. Mem0

**A. Collection model.** Four lifetime tiers, not collections ([Memory Types docs](https://docs.mem0.ai/core-concepts/memory-types)):

| Tier | Lifespan | Trigger |
|---|---|---|
| Conversation | one response | implicit |
| Session | minutes-hours | `run_id` |
| User | weeks to forever | `user_id` |
| Organizational | global | configured |

**B. Metadata registry shape.** Three semantic categories — **factual** (preferences, account details), **episodic** (interaction summaries), **semantic** (concept relationships). Categorization is automatic via an LLM classifier, then stored across three backends: vector store for semantic, graph store for relationships, KV store for direct lookups.

**C. Citation handles.** Memory ID + the category. Mem0 emits a "why this was remembered" provenance string per fact — useful pattern.

**D. Freshness model.** TTL is **derived from the lifetime tier** (session_id → expires on session end; user_id → persistent). This is the most explicit TTL model in the survey. **Caveat from their own docs:** "Avoid storing secrets or unredacted PII in user or org memories — Mem0 is retrievable by design."

**E. Parent-chunk expansion.** Not applicable — Mem0 stores atomic facts, not chunked documents.

**F. Hybrid retrieval with metadata.** Three-backend fusion: vector + graph + KV. The fusion mechanism is not documented in detail (marketing-fluffy in places).

---

## 7. Vespa.ai

**A. Collection model.** Schema-bound document types declared in `.sd` files. One schema = one document type = one "collection" in our terms ([Vespa schemas](https://docs.vespa.ai/en/schemas.html)).

**B. Metadata registry shape.** **Fully typed, declared up-front.** Each field has a type (`string`, `int`, `tensor<float>(x[768])`, `array<string>`, `position`) and an indexing strategy (`index`, `attribute`, `summary`, `attribute | index`). Synthetic fields support `input myField | embed | attribute | index` — embedding generation declared in schema.

**C. Citation handles.** Vespa document ID is `id:namespace:doctype::userspecified` — namespace + type + user key baked into the handle. The grouping framework can return parent docs with child counts. Strongest stable-ID story in the survey.

**D. Freshness model.** **Best-in-class.** Partial updates target individual fields without reindex; attribute fields update in milliseconds without touching the document store ([Partial Updates docs](https://docs.vespa.ai/en/partial-updates.html)). A `lastUpdated` attribute is the canonical freshness signal and can be combined into rank profiles to penalize stale docs.

**E. Parent-chunk expansion.** Modeled via parent-child references in schema (`field parent_id type reference<parent_doctype>`). Joins at query time. Heavy machinery — overkill unless you're at Vespa's scale.

**F. Hybrid retrieval with metadata.** Rank profiles combine BM25, vector closeness, attribute signals in a single expression. Filtering is pre-rank in the matching phase via YQL.

---

## 8. Weaviate

**A. Collection model.** "Collection" (formerly "Class") is the typed unit. Multi-tenancy is a first-class collection setting: one collection holds many tenants, isolated at query time ([Collections docs](https://docs.weaviate.io/weaviate/manage-data/collections)).

**B. Metadata registry shape.** **Typed schema declared up-front** — properties have `dataType` (text, int, number, boolean, date, geoCoordinates, phoneNumber, cross-reference). Each property can be `indexFilterable` / `indexSearchable`. Vectorizer config lives on the collection.

**C. Citation handles.** UUID per object + optional named cross-references between objects (`Article -> hasAuthor -> Author`). Cross-refs are the citation graph.

**D. Freshness model.** `lastUpdateTimeUnix` is auto-maintained. Collection-level "aliases" let you swap a stale collection for a freshly-rebuilt one atomically — a great primitive for rebuilds.

**E. Parent-chunk expansion.** Via cross-references — `chunk` class has `partOf -> document`. Query-time follow.

**F. Hybrid retrieval with metadata.** Native hybrid (BM25 + vector) with `alpha` weighting; metadata filters are pre-rank via the inverted index.

---

## 9. Qdrant (best-practice review for Aura)

**A. Collection model.** Collection = vector space + payload schema. Aura already uses this.

**B. Metadata registry shape.** Payload is freeform JSON, **but** Qdrant docs explicitly recommend declaring a payload schema and indexing every filtered field with the correct typed index ([Payload docs](https://qdrant.tech/documentation/manage-data/payload/)). Indexable types: `keyword, integer, float, bool, geo, datetime, uuid` (UUID 16-byte vs string 36-byte — meaningful at scale).

**C. Citation handles.** Point ID (uint64 or UUID). Payload should carry `source_id` and `parent_id` for citation rendering.

**D. Freshness model.** No native freshness — convention is to add `updated_at` (datetime payload, RFC3339) and index it. Use `is_tenant=true` for tenant fields to optimize storage.

**E. Parent-chunk expansion.** Convention: payload field `parent_id` indexed as `uuid`, second `scroll` call to fetch parents.

**F. Hybrid retrieval with metadata.** Filters are **pre-rank** during HNSW traversal (cardinality-driven plan). Hybrid via Qdrant's Query API with named vectors + sparse.

**Aura gaps vs Qdrant best practice:**
- We almost certainly don't have `is_tenant=true` on tenant fields.
- Payload schema consistency across writers is the documented #1 failure mode and matches what we've seen.
- UUID vs string for IDs — worth auditing.

---

## G. Cross-Framework Shortlist for Aura Phase 7B

### ADOPT 1 — Letta-style typed collection registry, Weaviate/Vespa-style typed properties per kind

The `Document.Kind` enum already exists in Aura. Promote it to a **typed registry** structure, one entry per kind, declared in Go (not JSON), shape:

```go
type CollectionDescriptor struct {
    Kind            DocumentKind        // wiki | source | archive | proposal | usermem | opsmem
    Label           string              // stable display name
    Description     string              // for agent prompt overlay
    Properties      []PropertyDescriptor // typed
    FreshnessPolicy FreshnessPolicy     // see ADOPT 2
    ParentPointer   string              // "" if root kind, otherwise property name carrying parent ID
    ProjectionsInto []ProjectionTarget  // {FTS, Qdrant, Graph}
}
```

This is Letta's "typed memory layer with description" + Weaviate's "typed properties on the class" + Vespa's "schema as source of truth." Aura already half-does this (`Document.Kind`) but the descriptor is implicit. Phase 7B makes it explicit and code-owned. Tools, retrievers, and the prompt overlay all consume from the same registry.

### ADOPT 2 — Per-projection freshness registry with content-hash + `updated_at`, plus an alias swap for full rebuilds

Combine three patterns:
- LlamaIndex `IngestionPipeline.DocstoreStrategy` (per-doc content hash skip-if-unchanged)
- Vespa partial updates (per-field freshness signal — wiki page can update `tags` without re-embedding)
- Weaviate aliases (atomic collection swap when a global rebuild is needed)

In Aura terms: a `projection_state` table keyed by `(collection_kind, projection_target, source_id)` carrying `(content_hash, projected_at, status)`. A nightly reconciler walks the kinds against ground truth (wiki filesystem, sources directory, archive table) and flags stale rows. The retriever can also surface `staleness_ms` in the hit so the LLM knows when to mistrust.

This generalizes the existing pattern in `internal/tools/internal_state` and applies it uniformly across FTS / Qdrant / graph documents / embedding cache (the four targets called out in PRD §7).

### ADOPT 3 — LlamaIndex small-to-big with explicit `NodeRelationship`, citation handle = `{collection_kind, source_id, chunk_id, parent_id}`

Aura already chunks. What's missing is the *typed relationship*:
- Indexed chunks carry `parent_id` (pointer to wiki page slug / source SHA / archive turn ID).
- Retrieval hit returns a structured `Hit{ collection_kind, source_id, chunk_id, parent_id, score_components{vector, bm25, rrf}, snippet, expand_handle }`.
- An LLM tool `expand_parent(handle)` fetches the parent (wiki page body, source extraction, full archive turn). This is the "follow-up handle" the PRD literally asks for.
- Citation rendering in the assistant reply uses `{collection_kind}:{source_id}` as the stable handle — matches Vespa's `id:namespace:doctype::key` philosophy.

Bonus: this is also what Letta does at the tool level (`archival_memory_search` returns passages with handles, agent decides whether to expand) — the small-to-big pattern is the retrieval-side mirror of Letta's agentic-memory access pattern.

### AVOID — The fashionable trap: a "universal knowledge graph" auto-extracted from every collection (R2R / Mem0 graph-store style)

Multiple frameworks (R2R, Mem0, several "graph memory" startups) sell auto-extracted entity/relationship graphs as the production retrieval substrate. Two reasons this is the wrong move for Aura right now:

1. **It rebuilds the world from LLM-extracted triples** instead of trusting the already-curated artifact graph Aura has (wiki `[[wiki-links]]` + frontmatter `related`/`sources` + source-to-wiki provenance). The user's own memory says "Graph memory IS the project core — wiki markdown + `[[wiki-links]]` IS the graph; NO KuzuDB/Neo4j/Zep." Building an LLM-extracted shadow graph on top duplicates work and creates a second source of truth that drifts from the first.
2. **Citation grounding gets worse, not better.** R2R-style extracted-triple citations point at synthesized entities, not at the markdown line the user actually edited. For an audit / trust use case (which is Aura's whole point), the parent-chunk handle pointing at `wiki/foo.md#section` beats an entity node `Person:Davide` every time.

The wiki-link graph + the typed projection registry covers the same surface as "graph memory" without the extraction risk. Adopt knowledge-graph rhetoric, reject the auto-extraction implementation.

---

## Sources

- LlamaIndex Documents & Nodes — https://developers.llamaindex.ai/python/framework/module_guides/loading/documents_and_nodes/
- LlamaIndex VectorStoreIndex — https://developers.llamaindex.ai/python/framework/module_guides/indexing/vector_store_index/
- LlamaIndex NodeRelationship API — https://docs.llamaindex.ai/en/v0.10.19/api/llama_index.core.schema.NodeRelationship.html
- LlamaIndex schema.py (source) — https://github.com/run-llama/llama_index/blob/main/llama-index-core/llama_index/core/schema.py
- LlamaIndex Recursive Retriever — https://developers.llamaindex.ai/python/examples/retrievers/recursive_retriever_nodes/
- LangChain VectorStore API — https://python.langchain.com/api_reference/core/vectorstores/langchain_core.vectorstores.base.VectorStore.html
- LangChain Document metadata issue #17459 — https://github.com/langchain-ai/langchain/issues/17459
- LangChain Cloud SQL collection-per-table — https://cloud.google.com/blog/products/databases/vectorstore-in-the-cloud-sql-for-postgresql-langchain-package/
- Haystack DocumentStore — https://docs.haystack.deepset.ai/docs/document-store
- Haystack Metadata Filtering — https://docs.haystack.deepset.ai/docs/metadata-filtering
- R2R GitHub — https://github.com/SciPhi-AI/R2R
- R2R Collections (docs page) — https://r2r-docs.sciphi.ai/documentation/collections
- Letta MemGPT concepts — https://docs.letta.com/concepts/memgpt/
- Letta Memory blocks — https://docs.letta.com/guides/agents/memory-blocks/
- Letta Memory management — https://docs.letta.com/advanced/memory-management/
- Letta Archival memory — https://docs.letta.com/guides/ade/archival-memory/
- Mem0 Memory Types — https://docs.mem0.ai/core-concepts/memory-types
- Vespa Schemas — https://docs.vespa.ai/en/schemas.html
- Vespa Partial Updates — https://docs.vespa.ai/en/partial-updates.html
- Weaviate Collections — https://docs.weaviate.io/weaviate/manage-data/collections
- Qdrant Payload — https://qdrant.tech/documentation/manage-data/payload/
- Qdrant Indexing — https://qdrant.tech/documentation/manage-data/indexing/
- Qdrant Multitenancy with LlamaIndex — https://qdrant.tech/documentation/examples/llama-index-multitenancy/
