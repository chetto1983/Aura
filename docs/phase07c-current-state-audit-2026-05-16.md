# Phase 7C — Current Typed-State Audit

**Date:** 2026-05-16
**Scope:** Read-only audit of state Aura HAS today around freshness/projection tracking, before planning Phase 7C. Inputs for design of projection freshness registry. Every claim has `file_path:line_number`.

---

## A. State persistito già esistente

### A.1 `tool_index_state` table — the ONLY existing freshness primitive

- **Schema**: [`internal/db/migrations/migrations.go:665-681`](../internal/db/migrations/migrations.go#L665-L681) — `tool_name (PK), content_hash, point_id, embed_model, indexed_at`
- **Writers**: `toolindex.SQLiteStateStore.Upsert()` at [`internal/agent/tools/index/state.go:51-68`](../internal/agent/tools/index/state.go#L51-L68)
- **Readers**: `toolindex.SQLiteStateStore.LoadAll()` at [`internal/agent/tools/index/state.go:26-46`](../internal/agent/tools/index/state.go#L26-L46)
- **Purpose**: Authoritative "what is currently indexed" set; reconciler reads it, computes upsert/delete buckets, writes atomically
- **Phase 7C signal**: this IS the template for the registry — `(content_hash, embed_model, indexed_at)` is exactly the 3-field freshness tuple. Promote and generalize.

### A.2 `embedding_cache` table

- **Schema**: [`internal/db/migrations/migrations.go:142-148`](../internal/db/migrations/migrations.go#L142-L148) — `content_sha (PK), model (PK), embedding (BLOB), created_at`
- **Writers**: `search.EmbedCache.Store()` (package `internal/storage/search`)
- **Readers**: cache hit path at [`cmd/aura/app.go:229-236`](../cmd/aura/app.go#L229-L236)
- **Purpose**: SHA-keyed embedding cache; hits skip Mistral round-trip. Hits/misses tracked in `EmbedCacheHealth` ([`internal/api/types.go:72-78`](../internal/api/types.go#L72-L78))

### A.3 `compact_memory_documents.updated_at` semantics

- **Schema**: [`internal/db/migrations/migrations.go:153-177`](../internal/db/migrations/migrations.go#L153-L177)
- **Columns**: id, kind, title, body, handle, source_id, page, chat_id, conversation_id, proposal_id, status, entities_json, tags_json, **updated_at**
- **Writers**:
  - `memoryindex.Store.ReplaceKind()` called from `Rebuild()` at `internal/storage/memoryindex/rebuild.go:54, 75, 92`
  - `memoryindex.Store.Append()` for source ingest
- **Readers**: `memoryindex.Store.Search()` filters by `updated_at` for recency decay in `memory_search` tool ([`memory_search.go:185-190`](../internal/agent/tools/registry/memory_search.go#L185-L190))
- **Semantics**: `updated_at` is set to wall-clock time when a document is indexed, used for age-based recency weighting (wiki half-life 180d, archive 30d)

### A.4 FTS5 virtual tables

- **`wiki_documents`** (`migrations.go:150-151`): `CREATE VIRTUAL TABLE wiki_documents USING fts5(id, content, metadata, title)`. No metadata.json sidecar (FTS5 self-contained). Writers: `search.RebuildQdrantWikiDocuments()` (called from `app.go:247`). Readers: `search.Repository.Search()` for hybrid wiki search.
- **`compact_memory_fts`** (`migrations.go:178-179`): `CREATE VIRTUAL TABLE compact_memory_fts USING fts5(id UNINDEXED, kind, title, body, handle, source_id, status, entities, tags)`. Writers: `memoryindex.Store.RebuildFTS()` at `rebuild.go:99`. Readers: `memoryindex.Store.SearchFTS()`.

### A.5 Qdrant collection payload fields (per point)

- **Wiki documents** ([`internal/storage/search/qdrant.go:139-144`](../internal/storage/search/qdrant.go#L139-L144)): `doc_id`, `content`, plus any metadata map
- **Compact memory** ([`internal/storage/search/compact_qdrant.go:247-264`](../internal/storage/search/compact_qdrant.go#L247-L264)): `doc_id, kind, title, content, body, handle, source_id, page, chat_id, conversation_id, proposal_id, status, entities, tags, updated_at`
- **Phase 7C gap**: NO `index_build_id`, NO `embedding_model_id` in payload today. Mem0 pattern would add `content_hash` here too.

---

## B. In-memory health trackers

### B.1 `VectorHealthTracker`

- **Location**: [`internal/storage/memoryindex/vector_health.go:20-99`](../internal/storage/memoryindex/vector_health.go#L20-L99)
- **Struct fields** (lines 9-18): `Enabled, Running, Collection, VectorSize, LastStartedAt (*time.Time), LastFinishedAt (*time.Time), LastDocsIndexed, LastError`
- **Lifecycle**: Constructed at `app.go:220`. Enabled when compact Qdrant wired (line 377). Started/Succeeded/Failed methods called by vector sync goroutine (`app.go:649, 656, 652`)
- **Accessibility**: `Snapshot()` method (line 92) returns immutable copy. Surfaced via `HealthRollup.CompactMemory` ([`api/types.go:55`](../internal/api/types.go#L55))
- **Phase 7C gap**: lost on restart. Phase 7C registry must persist this.

### B.2 `QdrantRebuildReport`

- **Location**: [`internal/storage/search/qdrant.go:35-42`](../internal/storage/search/qdrant.go#L35-L42)
- **Struct fields**: `Collection, DocsIndexed, PagesIndexed (sentinel PagesIndexedUnknown = -1 for warm-cache hits), VectorSize`
- **Generation**: Returned by `RebuildQdrantWikiDocuments()` and `CompactMemoryQdrantIndex.Recreate()`
- **Death**: Passed to `VectorHealthTracker.Succeeded()` (`app.go:656`), then forgotten after logging

### B.3 `reindex.Health`

- **Location**: [`internal/storage/reindex/types.go:43-50`](../internal/storage/reindex/types.go#L43-L50)
- **Struct fields**: `QueueDepth, Dropped, DroppedAfterStop, LastSuccess (time.Time), LastError (string)`
- **Exposed**: Via `api.ReindexHealthResponse` ([`api/types.go:19-40`](../internal/api/types.go#L19-L40)) at GET /health

---

## C. Write-path inventory (invalidation cascades)

### C.1 `wiki.Store.WritePage()`

- **Referenced from**: `internal/agent/tools/registry/wiki.go`, `internal/api/wiki_write.go`
- **Invalidates**:
  - wiki_documents FTS (if wiki_maintenance task runs next)
  - Qdrant wiki collection (via reindex worker submit: `app.go:262`)
  - Does NOT directly invalidate compact_memory_documents (archives point to turns, not wiki pages)
- **Submitter**: `wikiStore.SetReindexSubmitter(rw)` wires `reindex.Worker`; `Submit(Job{Slug, OpUpsert})` enqueued ([`reindex/types.go:31-34`](../internal/storage/reindex/types.go#L31-L34))

### C.2 `memoryindex.Store.ReplaceKind()`

- **Caller chain**: `Rebuild()` ← called from:
  - `app.go:628` boot-time rebuild with Sources/Archive/Proposals
  - `cron_maintenance` nightly task (per `cron/types.go:5-6`)
- **Invalidates**:
  - compact_memory_documents rows of that kind (delete + insert)
  - compact_memory_fts (rebuilt by `Store.RebuildFTS()` at `rebuild.go:99`)
  - Qdrant compact mirror (via `Store.SyncVector()`, `app.go:650`)

### C.3 `ArchiveStore.Append()`

- **Location**: [`internal/conversation/archive.go:107-129`](../internal/conversation/archive.go#L107-L129)
- **Caller**: Buffered appender wraps it (`app.go:618`). Each turn appended triggers:
  - Archive rows inserted to conversations table
  - Memory indexing via `IndexingArchiveRepository` wrapper (`app.go:613`) which calls memoryindex async
- **Invalidates**: Compact archive kind in memory index (next Rebuild)

### C.4 `source.Store.Put()`

- **Location**: [`internal/storage/sources/store/store.go:99-149`](../internal/storage/sources/store/store.go#L99-L149)
- **Calls**: Writes `raw/<id>/original.<ext>` + `source.json` atomically
- **Invalidates**:
  - compact_memory_documents source kind (next ingest pipeline rebuild)
  - Qdrant compact mirror (via ingest -> memory rebuild cascade)
- **Retry path**: Ingest pipeline ([`sources/ingest/pipeline.go`](../internal/storage/sources/ingest/pipeline.go)) handles OCR/extraction failures

### C.5 `summarizer.Proposals` write

- **Location**: [`internal/conversation/summarizer/applier.go`](../internal/conversation/summarizer/applier.go)
- **Writes**: `proposed_updates` table via `applier.ApplyProposal()`
- **Invalidates**:
  - compact_memory_documents proposal kind (next Rebuild)
  - Qdrant compact mirror (next SyncVector)
  - Does NOT trigger wiki page write unless proposal is accepted

### C.6 `reconciler.Reconcile()` (tool registry)

- **Location**: [`internal/agent/tools/index/reconciler.go`](../internal/agent/tools/index/reconciler.go), boot path at `app.go:555-577`
- **Target**: tool_index_state table + Qdrant tool collection
- **Invalidation**: On skills.json/mcp.json change, reconciler calls `Notify(toolindex.ReasonMCPConfig)` (`app.go:587`) to re-diff and upsert/delete changed tools

---

## D. Reindex jobs already existing

### D.1 `internal/storage/reindex` package

- **Job type**: `Op` enum (OpUpsert, OpDelete) with slug-only body ([`reindex/types.go:31-34`](../internal/storage/reindex/types.go#L31-L34))
- **Watermark-based**: **NO** — currently drop-newest coalescing only ([`worker.go:72-89`](../internal/storage/reindex/worker.go#L72-L89)). Idempotent per slug because disk is source of truth.
- **Worker**: Single drain goroutine ([`worker.go:123-150`](../internal/storage/reindex/worker.go#L123-L150)) consumed from buffered channel, re-reads page from disk on process
- **Entrypoint**: `NewWorker()` ([`worker.go:48-67`](../internal/storage/reindex/worker.go#L48-L67)) with health tracking

### D.2 `internal/storage/memoryindex/rebuild.go`

- **Entrypoint**: `Rebuild(ctx, store, RebuildInput)` called from:
  - `app.go:628` at boot (sources, archive, proposals)
  - cron wiki_maintenance task
  - NO explicit `BuildVectorIndex` or per-collection rebuild — all three kinds rebuilt together
- **Idempotent**: `ReplaceKind()` atomically deletes old + inserts new rows by kind
- **Vector sync**: Optional skip via `SkipVector` flag when compact Qdrant unavailable (`app.go:632`)

### D.3 Boot-time reconciler

- **Reason**: `toolindex.ReasonBoot` ([`toolindex/reconciler.go:23`](../internal/agent/tools/index/reconciler.go#L23))
- **Execution**: `reconciler.Reconcile(context.Background(), ReasonBoot)` (`app.go:570`)
- **Subsequent runs**: `reconciler.Run()` started as background goroutine (`app.go:580`); debounced by `toolindex.Config.Debounce` (default 500ms)

### D.4 Cron jobs

- **wiki_maintenance task**: Defined in [`cron/types.go:32`](../internal/cron/types.go#L32). Schedule: daily nightly pass
- **What it does**: Calls wiki store maintenance directly (no dedicated rebuild endpoint yet)

### D.5 Markitdown/source ingest retry path

- **On OCR/extraction success**: Calls memoryindex rebuild to index extracted source documents
- **Failure handling**: Retry logic in pipeline (not explicit watermark; disk state is source of truth)

---

## E. Retrieval-time response shape

### E.1 `memoryindex.Document` struct

- **Location**: [`store.go:34-53`](../internal/storage/memoryindex/store.go#L34-L53)
- **Fields**: ID, Kind, Title, Body, Handle, SourceID, Page, ChatID, ConversationID, ProposalID, Status, Entities[], Tags[], UpdatedAt, **Score, ScoreExact, ScoreFTS, ScoreVector** (Phase 7B US-L03)
- **Phase 7C gap**: no `IndexBuildID`, no `EmbeddingModelID`, no `IndexedAt` separate from UpdatedAt

### E.2 `search.Result` struct

- **Location**: [`search/search.go`](../internal/storage/search/search.go)
- **Fields**: Kind, Slug, Title, Content, Score, ScoreExact, ScoreFTS, ScoreVector, UpdatedAt, FilePath

### E.3 `memory_search` tool output

- **Location**: [`memory_search.go:143-200+`](../internal/agent/tools/registry/memory_search.go)
- **Response**: Markdown plain text (no JSON envelope, per lines 17-29 comment)
- **Scoring**: Relevance × Recency decay: `exp(-age_days / halfLifeDays)` (lines 185-190)
- **Score format**: `[exact=… fts=… vector=…]` when any component non-zero (Phase 7B US-L04)
- **Formatted by**: `formatMemoryResults()` (line 201+)

### E.4 Dashboard `/api/health` endpoint

- **Location**: [`api/health.go`](../internal/api/health.go), [`api/types.go:42-59`](../internal/api/types.go#L42-L59)
- **Structure**: `HealthRollup` aggregates per-subsystem state
- **Freshness exposure**:
  - `CompactMemory: VectorHealth` (running status, last finished time, doc count)
  - `Reindex: ReindexHealthResponse` (queue depth, last success, last error)
  - **No per-collection registry or dirty_count today**

---

## F. Asset Phase 7B: US-L01 Typed Collection enum

### F.1 `internal/storage/memoryindex/collections.go`

- **6 Collection constants** ([`lines 6-33`](../internal/storage/memoryindex/collections.go#L6-L33)):
  1. `CollectionWiki` = "wiki" (line 9)
  2. `CollectionSource` = "source" (line 13)
  3. `CollectionArchive` = "archive" (line 17)
  4. `CollectionProposal` = "proposal" (line 21)
  5. `CollectionUserMemory` = "user_memory" (line 27, scaffolded)
  6. `CollectionOperational` = "operational" (line 32, scaffolded)

- **CollectionDescriptor struct** ([`lines 48-55`](../internal/storage/memoryindex/collections.go#L48-L55)): `Kind, Label, Description, DefaultMode (retrieval strategy), ParentCollection, StorageBackend`

- **Registry map** ([`lines 58-101`](../internal/storage/memoryindex/collections.go#L58-L101)):
  - Wiki: `StorageBackend="wiki_documents"`
  - Source: `StorageBackend="compact_memory_documents"`
  - Archive: `StorageBackend="compact_memory_documents"`
  - Proposal: `StorageBackend="proposed_updates"`
  - UserMemory: `StorageBackend=""` (empty, Phase 7C/7D)
  - Operational: `StorageBackend=""` (empty, Phase 7C/7D)

- **Lookup() helper** ([`lines 105-108`](../internal/storage/memoryindex/collections.go#L105-L108)): Returns (CollectionDescriptor, bool)

### F.2 Wiring status

- **WIRED (3/6)**: Source, Archive, Proposal → compact_memory_documents + compact_memory_fts
- **SCAFFOLDED (3/6)**: Wiki (lives in separate `wiki_documents` FTS5 + Qdrant), UserMemory (no writer), Operational (no writer)

### F.3 The 5 projection targets for Phase 7C

Combining F.1 + the storage backend inventory:
1. **`wiki_documents`** (FTS5) — wiki collection
2. **`aura_memory_v1`** (Qdrant wiki) — wiki collection
3. **`compact_memory_documents`** + `compact_memory_fts` (SQLite) — source + archive + proposal
4. **`aura_memory_v1_compact`** (Qdrant) — source + archive + proposal mirror
5. **`embedding_cache`** (SQLite) — orthogonal: embedding result cache

---

## G. Migration baseline

### G.1 Latest migration

- **File**: [`internal/db/migrations/migrations.go:29`](../internal/db/migrations/migrations.go#L29)
- **Current version**: v11 (lines 18-30 show registered migrations 1-11)
- **Last table added**: `chat_questions` (v11, [lines 1098-1131](../internal/db/migrations/migrations.go#L1098-L1131))

### G.2 Registry-like table pattern (template for Phase 7C)

**Candidate template**: `tool_index_state` (v5, [`lines 665-681`](../internal/db/migrations/migrations.go#L665-L681))
- Shape: `(id PK, *_version/hash, *_at timestamp, status, *_metadata_json)`
- Index pattern: `(content_hash)` for diff queries
- Atomic upsert semantics via `ON CONFLICT DO UPDATE`

### G.3 Migration entrypoint for Phase 7C

- Add new migration v12 in registered slice (line 29) with `addProjectionFreshnessRegistry` function
- Table schema: `(projection_id PK, kind, embedding_model_id, embedding_dim, index_build_id, schema_version, last_full_rebuild_at, last_incremental_at, pending_invalidations, status, health_reason, version, updated_at)`
- Index pattern: `(projection_id, last_full_rebuild_at)` for watermark-based reindex selection

---

## H. NOTES & ORPHANED CODE

1. **No existing index_build_id stamping**: Retrieval paths do NOT currently surface which index build produced a result.

2. **Warm-cache hit semantics**: Qdrant warm-cache at [`compact_qdrant.go:70`](../internal/storage/search/compact_qdrant.go#L70) skips embedding entirely if collection exists + has points, but does NOT validate vector dimension drift (T-01-24 accepted risk per code comment). Phase 7C watermark scheme should detect this.

3. **CompactMemoryQdrantIndex vs QdrantRepository split**: Wiki uses Repository (warm-cache, reindex worker), compact uses CompactMemoryQdrantIndex (no warm-cache, full rebuild each SyncVector). Different invalidation patterns.

4. **Embedding cache hits not wired to health**: `EmbedCacheHealth` tracks hits/misses ([`api/types.go:75-78`](../internal/api/types.go#L75-L78)) but NOT surfaced in `app.go` — cache is wrapped transparently at line 233, no callback hook.

5. **Reindex worker drop-newest coalescing loses watermark**: Current job queue (`Job{slug, Op}`) cannot track "all changes after T" — only latest per slug survives. Full-rebuild-watermark approach in Phase 7C needed for idempotent trigger.

6. **Archive ingest path dual-indexed**: Turns written to conversations table, THEN wrapped by `IndexingArchiveRepository` which async updates compact_memory_documents. If wrapper fails, archive row exists but not indexed — cache coherency gap.

7. **No per-collection dirty tracking today**: `KindSource/Archive/Proposal` writes blindly invalidate the entire kind via `ReplaceKind()`. Phase 7C `pending_invalidations` can enable targeted reindex of just changed records.
