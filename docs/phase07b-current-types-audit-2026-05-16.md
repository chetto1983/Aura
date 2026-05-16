# Phase 7B Audit — Current Typed-Collection State

**Date:** 2026-05-16
**Scope:** Read-only audit of the typed concepts that exist today across `internal/storage/memoryindex`, `internal/storage/sources`, `internal/wiki`, `internal/conversation`, and `internal/storage/runs`. Inputs for Phase 7B planning (Typed Collection Registry + structured retrieval hits + chunk→parent expansion + projection freshness registry).
**Reader contract:** every claim has a `file:line` citation. "Exists" vs "documented but not implemented" vs "absent" are called out explicitly.

---

## A. Layer concepts (Kind enum values)

### A.1 `memoryindex.Document.Kind` — the canonical layer discriminator

Three constants are declared in [`internal/storage/memoryindex/store.go:16-20`](../internal/storage/memoryindex/store.go):

```go
KindSource   = "source"
KindArchive  = "archive"
KindProposal = "proposal"
```

Confirmed sites:

- **Declaration:** `internal/storage/memoryindex/store.go:16-20`.
- **Filter input:** `Filter.Kinds []string` at `internal/storage/memoryindex/store.go:52-56`; consumed in `filterWhere` at `internal/storage/memoryindex/store.go:507-527`.
- **`KindSource` writes:** assigned at `internal/storage/memoryindex/rebuild.go:155` (`Kind: KindSource`) inside `sourcePageDocuments`; replaced wholesale via `store.ReplaceKind(ctx, KindSource, docs)` at `internal/storage/memoryindex/rebuild.go:54`.
- **`KindArchive` writes:** assigned at `internal/storage/memoryindex/rebuild.go:228` (`Kind: KindArchive`) inside `ArchiveDocument`; replaced via `store.ReplaceKind(ctx, KindArchive, docs)` at `internal/storage/memoryindex/rebuild.go:75`; live-append via `IndexingTurnAppender.Append` at `internal/storage/memoryindex/rebuild.go:313-333`.
- **`KindProposal` writes:** assigned at `internal/storage/memoryindex/rebuild.go:255` (`Kind: KindProposal`) inside `ProposalDocument`; replaced via `store.ReplaceKind(ctx, KindProposal, docs)` at `internal/storage/memoryindex/rebuild.go:92`.
- **Reads:** the LLM-facing `search_memory` tool dispatches on these three values at `internal/agent/tools/registry/memory_search.go:164-172`; archive/proposal recency-bucket split at `memory_search.go:354-361`.

### A.2 Wiki is **NOT** stored in `compact_memory_documents`

Wiki pages are NOT one of the `Kind` values above. The wiki lives on its own:

- **Wiki backend:** `internal/storage/search/sqlite.go` (FTS5 table `wiki_documents`, schema at `internal/db/migrations/migrations.go:150-151`) + Qdrant collection from `internal/storage/search/qdrant.go:65-93`.
- **Result type:** `search.Result.Kind` at `internal/storage/search/search.go:88-101` — a STRING (not the `memoryindex.Kind*` constants). Values observed:
  - `"wiki_page"` — set at index time in `loadWikiDocuments` payload `"kind"` field, `internal/storage/search/search.go:293`.
  - `"graph_node"` — emitted by `buildGraphDocuments`; surfaced in `resultLabel` at `internal/storage/search/search.go:508-518`.
  - `"graph_index"` — handled in `memory_search.go:245-250`.
- **Tool boundary fuses both backends** in `search_memory` (`memory_search.go:158-177`): `searchWiki` (calls the `search.Searcher`) appended with `searchCompact` (calls `memoryindex.Store`). Two separate searches whose results are merged by the LLM tool layer, not by a single `memoryindex` query.

### A.3 "User memory" — does NOT exist as a distinct layer

PRD §7 names "user memory" as one of the five typed collections. Today it is **conflated with archive** (and partially with `proposed_updates`):

- The `summarizer` pipeline extracts user facts/preferences from conversations (`internal/conversation/summarizer/types.go:41-48` — `Candidate.Category` is `"person|project|preference|fact|todo"`) and writes them to `proposed_updates` (`internal/conversation/summarizer/proposals.go:160-178`).
- Approved proposals become wiki pages (the `ProposalKind` is mostly a wiki action: `ActionNew`/`ActionPatch`, `summarizer/types.go:7-9`).
- There is no `KindUserMemory` constant anywhere; there is no separate "user memory" table; there is no separate retrieval path. User memory = (a) raw conversation turns under `KindArchive`, plus (b) pending facts queued in `proposed_updates` indexed under `KindProposal`, plus (c) eventually-promoted wiki pages.

### A.4 "Operational memory" — also does NOT exist as a distinct layer

PRD §7 names "operational memory" too. Today it surfaces only as:

- `tool_attempts` table (`migrations.go:1052-1095`) — typed via `tool_kind`, `class`, `outcome` — but NOT indexed in `compact_memory_documents`.
- `runs` / `run_events` (`migrations.go:308-368`) — typed via `Run.Channel`, `Event.Type` — also NOT indexed.
- `audit_events` (`migrations.go:429-444`) — typed via `target_type` + `type` — also NOT indexed.
- `swarm_runs` / `swarm_tasks` (`migrations.go:269-306`) — typed via `Task.Role`/`Task.Status` — also NOT indexed.

None of these participate in `search_memory` today.

### A.5 Conclusion — A.1 holds, with caveats

The audit conclusion that the layer enum lives in `Document.Kind`/`Result.Kind` is **partially correct**:

- `memoryindex.Document.Kind` is canonical for the three values `source|archive|proposal`.
- `search.Result.Kind` is a parallel, free-form string used by the wiki path with values `wiki_page|graph_node|graph_index` that NEVER appear in `compact_memory_documents` and NEVER are written through the same registry.
- "wiki", "user memory", and "operational memory" are not first-class `Kind` values today. They're either implicit (wiki via a separate index) or absent (user memory, operational memory).

---

## B. Metadata per layer

### B.1 Wiki frontmatter — what's indexed vs what's read on demand

Schema fields declared in [`internal/wiki/schema.go:17-38`](../internal/wiki/schema.go):

| Field            | Type            | Schema source                  | Indexed in FTS `wiki_documents`?                       | Indexed in Qdrant payload?                                                              | Returned from `search.Result`?                                                      |
|------------------|-----------------|--------------------------------|--------------------------------------------------------|-----------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------|
| `title`          | string          | `schema.go:19`                 | yes — column `title`, `migrations.go:151`              | yes — payload key `title`, `search/search.go:291`                                       | yes — `Result.Title`, `search.go:90`                                                |
| `tags`           | []string        | `schema.go:20`                 | indirect — joined into `content`, `search.go:277`      | yes — payload key `tags` (CSV), `search/search.go:297`                                  | yes — `Result.Tags`, `search.go:96`                                                 |
| `category`       | string          | `schema.go:21`                 | indirect — joined into `content`                       | yes — payload key `category`, `search/search.go:296`                                    | yes — `Result.Category`, `search.go:95`                                             |
| `related`        | []string        | `schema.go:22`                 | indirect                                               | yes — payload key `related` (CSV), `search/search.go:298`                               | yes — `Result.Related`, `search.go:97`                                              |
| `sources`        | []string        | `schema.go:23`                 | indirect                                               | yes — payload key `sources` (CSV), `search/search.go:299`                               | yes — `Result.Sources`, `search.go:98`                                              |
| `schema_version` | int             | `schema.go:24`                 | NO                                                     | NO                                                                                      | NO                                                                                  |
| `prompt_version` | string          | `schema.go:25`                 | NO                                                     | NO                                                                                      | NO                                                                                  |
| `created_at`     | string ISO-8601 | `schema.go:26`                 | NO                                                     | NO                                                                                      | NO                                                                                  |
| `updated_at`     | string ISO-8601 | `schema.go:27`                 | yes — payload `updated_at`, `search/search.go:301-303` | yes                                                                                     | yes — `Result.UpdatedAt`, `search.go:94`                                            |
| `unversioned`    | bool            | `schema.go:33`                 | NO                                                     | NO                                                                                      | NO                                                                                  |
| `body`           | string          | `schema.go:37`                 | yes — column `content` carries `title+"\n"+body`       | yes — `Document.Content` embedded                                                       | yes — `Result.Content` (full body)                                                  |
| (file path)      | derived         | `search/search.go:294`         | yes — payload `filepath`                               | yes                                                                                     | yes — `Result.FilePath`, `search.go:95`                                             |
| (file size)      | derived         | `search/search.go:285-289`     | yes — payload `size`                                   | yes                                                                                     | yes — `Result.SizeBytes`, `search.go:100`                                           |

Read on demand only (require a second hop via `wiki.Store.ReadPage`): `schema_version`, `prompt_version`, `created_at`, `unversioned`. The remaining frontmatter fields are present in the search result.

### B.2 Source metadata — page tracked, span/chunk absent

The on-disk `source.json` shape ([`internal/storage/sources/store/source.go:93-107`](../internal/storage/sources/store/source.go)):

```go
type Source struct {
    ID, Kind, Filename, MimeType, SHA256 string
    SizeBytes int64
    CreatedAt time.Time
    Status    Status
    OCRModel  string
    PageCount int
    Extract   *ExtractionMeta    // ExtractorName, ExtractorVersion, TextBytes, PageCount, SheetCount, RowCount, Warnings (source.go:81-89)
    WikiPages []string           // back-pointer to wiki pages that cite this source
    Error     string
}
```

How this maps to `compact_memory_documents` rows for sources (`memoryindex/rebuild.go:130-164`):

| Concept            | Tracked?   | Where                                                                      | Notes                                                                                                                                            |
|--------------------|------------|----------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------|
| **page number**    | YES        | `Document.Page int`, `memoryindex/store.go:46`; populated `rebuild.go:156` | Extracted by `splitSourcePages` regex `^## Page (N)$` on `ocr.md`, `memoryindex/rebuild.go:27,171-190`.                                          |
| **chunk index**    | NO         | absent                                                                     | Each page becomes ONE row; there is no sub-page chunking. `sourceSnippetLimit = 2400` truncates per page (`rebuild.go:23`).                      |
| **byte span**      | NO         | absent                                                                     | The `Body` field stores the snippet text only; no offset back into the original.                                                                 |
| **parent source**  | YES        | `Document.SourceID`, `memoryindex/store.go:41`; populated `rebuild.go:156` | The page-document carries `SourceID = src.ID` so a hit can be joined back to `source.json`. ID is also embedded in the row ID: `source:<id>#page=N` (`rebuild.go:147-149`). |
| **MIME / format**  | indirect   | `Tags` includes `string(src.Kind)`, `rebuild.go:159`                       | The structured `Kind`/`MimeType` is NOT promoted to a dedicated column; only the loose `tags_json` carries it.                                   |
| **status**         | YES        | `Document.Status string`, `memoryindex/store.go:47`; populated `rebuild.go:158` | Mirrors `source.Status` (`stored|extracting|ocr_complete|extract_complete|ingested|failed`, `source.go:72-79`).                                  |
| **created_at**     | YES        | `Document.UpdatedAt = src.CreatedAt`, `rebuild.go:160`                     | Note: the memoryindex field is named `UpdatedAt` but stores the source's `CreatedAt`. There is no real "source last reindexed" timestamp.        |
| **page_count**     | NO         | absent from `compact_memory_documents`                                     | Present on `source.json` (`source.go:103`) but not promoted into the indexed document.                                                           |
| **extractor info** | NO         | absent from `compact_memory_documents`                                     | `Extract.ExtractorName`/`ExtractorVersion` live on disk only.                                                                                    |
| **wiki backlinks** | NO         | absent from `compact_memory_documents`                                     | `source.WikiPages []string` lives on disk only; the LLM can't filter "find sources that have already been wiki-ized".                            |

### B.3 Archive metadata — chat_id and turn_index queryable, role/tool not separately filterable

`compact_memory_documents` row shape for archive (`memoryindex/rebuild.go:210-237`):

| Concept             | Stored where                                                                            | Queryable?                                                                                                  |
|---------------------|-----------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------|
| `chat_id`           | `Document.ChatID int64`, `memoryindex/store.go:42`                                      | YES — `Filter.ChatID` filters via `filterWhere`, `store.go:519-522` ("OR d.chat_id = ?" for archive only)   |
| `conversation_id`   | `Document.ConversationID int64`, `memoryindex/store.go:43`                              | NO direct filter; column exists but `Filter` exposes no field                                               |
| `turn_index`        | encoded into `Title = "chat=%d turn=%d"`, `rebuild.go:221`                              | NO — only as text inside the title, not as a column                                                         |
| `role`              | `Tags: []string{turn.Role}`, `rebuild.go:234`                                           | indirect — present in the loose `tags_json` blob; no first-class filter                                     |
| `tool_call_id`      | NOT PROMOTED into the compact index. Lives only in source table `conversations.tool_call_id`, `migrations.go:101` | NO — `Turn.ToolCallID` (`conversation/archive.go:27`) is set on the live `Turn`, but stripped by `ArchiveDocument` |
| `tool_calls` (JSON) | NOT PROMOTED; lives only in `conversations.tool_calls`, `migrations.go:100`             | NO                                                                                                          |
| `created_at`        | `Document.UpdatedAt = turn.CreatedAt`, `rebuild.go:222-225`                             | YES — `idx_compact_memory_kind` index on `(kind, updated_at)`, `migrations.go:170`                          |

Eligibility filter (`internal/storage/memoryindex/rebuild.go:355-364`): only `role IN (user, assistant)` are indexed. Phase 7A explicitly excludes `tool` and `system` turns. This means **archive search cannot find tool messages**.

### B.4 Proposal metadata

From `internal/storage/memoryindex/rebuild.go:239-264` and `internal/db/migrations/migrations.go:113-126`:

- `Document.ProposalID int64` (`store.go:44`) — first-class column.
- `Document.ChatID` populated from `proposal.ChatID`.
- `Document.Status` populated from `proposal.Status` (`pending|approved|rejected`).
- `Tags` packed with `action`, `category`, `status`, plus `RelatedSlugs`.
- `Document.Handle = "proposal:<id>"`.

The richer `Provenance` struct (`summarizer/proposals.go:45-54` — `OriginTool`, `Evidence[]EvidenceRef`, `AgentJobID`, `SwarmRunID`, `SwarmTaskID`) is on the row in `proposed_updates.provenance_json` but is **NOT promoted into `compact_memory_documents`** — so retrieval results can't show "proposal P was triggered by tool T citing source src_xxx#page=4" without a second `Get` round-trip.

---

## C. Citation handles — current shape

### C.1 Per-kind handle strings actually returned

The LLM sees a markdown line per hit formatted by `formatMemoryResults` ([`memory_search.go:384-451`](../internal/agent/tools/registry/memory_search.go)). The `Identifier` token comes from:

- **Wiki:** `"[[" + r.Slug + "]]"` — `memory_search.go:247`. Surfaced exactly as `[[slug]]`. Graph-index variant returns the bare slug (`memory_search.go:249`).
- **Source:** `compactIdentifier` returns `doc.SourceID` when present, i.e. `src_<16hex>` — `memory_search.go:309-312`. Page-targeted handle ALSO emitted via `handle=source:src_xxx#page=N` (`rebuild.go:147-149`, `memory_search.go:413`).
- **Archive:** `doc.Handle` is preferred when set — `memory_search.go:305-307`. Constructed at `rebuild.go:216-220` as either `conversation:<id>` (integer primary key from the `conversations` table) or fallback `conversation:chat:<chatID>#turn=<idx>` for un-persisted turns.
- **Proposal:** `doc.Handle = "proposal:<id>"` — `rebuild.go:257`.

### C.2 Are these stable for downstream linking?

| Handle pattern                              | Joinable in SQL?                                                                                       | Clickable in dashboard?                                                                                                  |
|---------------------------------------------|--------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------------------|
| `[[slug]]`                                  | YES — `wiki_documents.id = slug`; on disk `<wiki>/slug.md`.                                            | YES — wiki dashboard `/api/wiki/page?slug=` (per `CLAUDE.md` API spec).                                                  |
| `src_<16hex>`                               | YES — `compact_memory_documents.source_id`; on disk `<wiki>/raw/<id>/source.json`.                     | YES — sources dashboard (`/api/sources`).                                                                                |
| `source:src_xxx#page=N`                     | partially — the row ID is `source:<id>#page=N`, joinable as `compact_memory_documents.id`.             | not exposed as a click target by the source dashboard.                                                                   |
| `conversation:<id>`                         | YES — `<id> = conversations.id` (integer PK).                                                          | YES — conversations dashboard reads `conversations` directly.                                                            |
| `conversation:chat:<chatID>#turn=<idx>`     | partially — requires `(chat_id, turn_index)` lookup against the UNIQUE index in `conversations`.       | not currently consumed by any dashboard route.                                                                           |
| `proposal:<id>`                             | YES — `<id> = proposed_updates.id`.                                                                    | YES — proposals review queue exists in the dashboard.                                                                    |

### C.3 Freshness signal per citation?

**Absent in the citation itself.** The age token `(2d ago)` printed by `formatAge` (`memory_search.go:454-474`) is informational text on the same line, computed from `Document.UpdatedAt`. There is **no structured per-hit `last_indexed_at`** the model can branch on; there's no marker like `stale=true` if the projection lags the source.

---

## D. Score components — single combined score today

### D.1 What's actually exposed

`Document.Score float64` ([`memoryindex/store.go:49`](../internal/storage/memoryindex/store.go)) is a SINGLE combined float; the upstream rank weights are computed inside `mergeDocumentsRRF` (`store.go:577-622`) and **collapsed into one number**:

```go
score(doc) = Σ_g (w_g / (k + rank_in_g + 1))     // store.go:591
```

with constants `rrfK=60`, `rrfWeightExact=1.0`, `rrfWeightFTS=0.6`, `rrfWeightVector=0.8` (`store.go:562-566`). The per-channel contributions are accumulated locally in the `entry.score` field and then thrown away (`store.go:599`). The same applies to the wiki side: `search.mergeHybridResults` (`internal/storage/search/search.go:343-386`) uses the same RRF pattern and emits a single `Result.Score`.

The downstream recency multiplier in `memory_search.go:332-349` then multiplies that single score by a `0.5^(age/halfLife)` factor, again producing a single float.

### D.2 Per-channel components — NOT exposed

There is no `ScoreComponents struct{ Exact, FTS, Vector float64 }`, no JSON envelope per hit, no debug telemetry of which channel found a doc. The LLM and the dashboard see only the final fused number rendered as `score=0.92` (`memory_search.go:418`).

### D.3 Gap for PRD §7 step "structured retrieval hits with score components"

Missing pieces:

1. The fused score discards channel attribution at `store.go:599`. To preserve it, `entry` (or a sibling struct) must carry per-channel contributions through to the returned `Document`.
2. `Document` itself lacks fields for `ExactScore`, `FTSScore`, `VectorScore`. A new struct (or `map[string]float64`) is required.
3. The same change is needed on the wiki side (`search.Result`).
4. The formatter `formatMemoryResults` would need a richer output format (markdown sublist or JSON envelope) — the current "one line per hit" can't fit three more numbers without becoming unreadable.

---

## E. Parent expansion — chunk→source

### E.1 Today's path

A source hit comes back as `memoryindex.Document` with three fields that together identify the parent:

- `SourceID string` — `store.go:41`, populated `rebuild.go:156` from `src.ID`.
- `Handle string` — populated `rebuild.go:147-149` as `source:<id>#page=N` (or just `source:<id>` when page=0).
- `Page int` — `store.go:46`, the integer page number.

The LLM-visible result includes `handle=source:src_xxx#page=N` (`memory_search.go:413`) and the bare `src_xxx` identifier (`memory_search.go:309-312`). To actually expand the chunk into the parent source the model invokes `read_source` (`internal/agent/tools/registry/source_read.go:21-76`) with `source_id=src_xxx` and `mode=metadata|ocr|excerpt`. That tool does a fresh `store.Get(id)` lookup at `source_read.go:58` against the on-disk `source.json`.

### E.2 What's preserved vs missing

| Capability                                          | State                                                                                                                                          |
|-----------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------|
| chunk-row → `SourceID` pointer                      | EXISTS — `Document.SourceID` is the join key.                                                                                                  |
| chunk-row → page number                             | EXISTS — `Document.Page`.                                                                                                                      |
| chunk-row → in-source byte offset                   | ABSENT — no offset/span column. Snippets are limited to first 2400 chars per page (`rebuild.go:23`).                                           |
| chunk-row → parent metadata in ONE round-trip       | ABSENT — `read_source` is a second tool call. The retrieval result does not include `Filename`, `Kind`, `MimeType`, `PageCount`, `Status`.     |
| chunk-row → sibling pages (same parent)             | requires a separate `memoryindex` query with `source_id` filter (not currently exposed via `Filter`; only `Kinds`/`ChatID` are filterable, `store.go:52-56`). |
| ingest pipeline → wiki backlink (parent → wiki)     | EXISTS but not in the index — `source.WikiPages []string` on disk (`source/source.go:105`).                                                    |

### E.3 Conclusion

The pointer is there (`SourceID`), so the audit conclusion is correct that today's hits "preserve" chunk→parent in the minimal sense of "it's joinable". But there is no enriched parent payload returned inline, and no API for "expand this source-hit to its full parent context" without a second `read_source` round-trip.

---

## F. Projection freshness — registry status

### F.1 Per-collection inventory of "last indexed at" / freshness signals

| Collection / projection                        | Freshness field?                                                                                                              | Notes                                                                                                                          |
|------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------|
| `wiki_documents` (FTS5)                        | ABSENT                                                                                                                        | The FTS table holds `id, content, metadata, title`, no `indexed_at`. (`migrations.go:150-151`)                                 |
| Qdrant wiki collection                         | per-point `updated_at` only (the frontmatter ts, `search/search.go:301-303`)                                                  | The collection itself has no per-collection or per-point `indexed_at`. A warm-cache hit reuses points whose age is unknowable. |
| `compact_memory_documents`                     | `updated_at` is the SOURCE timestamp, not "when indexed"                                                                      | `Document.UpdatedAt = src.CreatedAt` for sources (`rebuild.go:160`); `= turn.CreatedAt` for archive (`rebuild.go:222-225`). There is no "indexed at" column. |
| `compact_memory_fts`                           | none — FTS5 virtual table mirrors `compact_memory_documents`                                                                  |                                                                                                                                |
| Qdrant compact collection                      | per-point `updated_at` mirrors the document field — same caveat as above                                                      | `compactPayload` writes `"updated_at"`, `search/compact_qdrant.go:263`.                                                        |
| `embedding_cache`                              | `created_at` per-row exists (`migrations.go:142-148`)                                                                         | Tracks WHEN the vector was cached, but is not surfaced anywhere.                                                               |
| `tool_index_state` (tool registry projection)  | EXISTS — `indexed_at TEXT NOT NULL` per row (`migrations.go:672`)                                                             | This is the ONE collection with a real freshness column today. Used by `internal/agent/tools/index/state.go`.                  |
| `VectorHealthTracker` (in-memory)              | `LastStartedAt`, `LastFinishedAt`, `LastDocsIndexed` (`memoryindex/vector_health.go:13-17`)                                   | Process-scoped only. Lost on restart. Not per-collection — one tracker for the compact Qdrant rebuild.                         |
| `QdrantRebuildReport` (wiki vector mirror)     | `DocsIndexed`, `PagesIndexed`, `VectorSize`, `Collection` — no timestamp (`search/qdrant.go:35-43`)                           | Produced once per rebuild call; not persisted.                                                                                 |

### F.2 Summary

The PRD step "projection freshness registry" is **almost entirely absent**:

- Only `tool_index_state.indexed_at` exists as a persisted, per-row "last indexed at" today.
- `VectorHealthTracker` gives a single in-memory snapshot for the compact vector mirror — lost on restart, no per-doc resolution.
- No table records "last full reindex of wiki" or "last reindex of compact memory". `internal/storage/reindex` exists as a job submitter but does not persist freshness state.
- No "staleness budget" config (e.g. "Qdrant warm-cache hit is acceptable if < N hours old").

---

## G. Gap inventory for Phase 7B (PRD-priority ordered)

### G.1 MUST land in 7B (the PRD §7 line items)

**G.1.1 — Typed Collection Registry** (PRD: "collection metadata registry for wiki, sources, user memory, archive, and operational memory")

Today there is no `Collection` enum/registry. Five collections sit in different storage backends with no common discriminator. The closest thing is the three `KindSource/KindArchive/KindProposal` constants in `memoryindex/store.go:16-20` — three of the five PRD-named collections, only partially.

**Files that would change:**
- `internal/storage/memoryindex/store.go` — add `KindWiki`, `KindUserMemory`, `KindOperational`; promote constants into a typed enum (`type Collection string` or similar).
- `internal/storage/memoryindex/rebuild.go` — wire wiki + user-memory + operational sources into the unified collection so `Kind` becomes the single discriminator.
- `internal/storage/search/search.go` — align `Result.Kind` with the new enum (today its values `wiki_page|graph_node|graph_index` are free-form strings).
- `internal/agent/tools/registry/memory_search.go` — drop the dual-path fan-out (`searchWiki` + `searchCompact`) once wiki is in the unified registry; or formalize a Registry that fans out behind one interface.
- `internal/db/migrations/migrations.go` — new migration: backfill wiki + new collections into `compact_memory_documents`, OR introduce a `collections` registry table that lists each typed collection's storage backend + freshness slot.

**G.1.2 — Structured retrieval hits with score components** (PRD requirement)

Today retrieval emits a single fused float; per-channel attribution is lost at `store.go:599` and `search/search.go:374`.

**Files that would change:**
- `internal/storage/memoryindex/store.go` — extend `Document` (or introduce `Hit` envelope) with `ScoreExact`, `ScoreFTS`, `ScoreVector`, `ScoreFused` fields; `mergeDocumentsRRF` must keep per-channel contributions on the returned struct.
- `internal/storage/search/search.go` — same extension for `Result`; `mergeHybridResults` must preserve channel breakdown.
- `internal/agent/tools/registry/memory_search.go` — `memoryResult` already collapses to one `Score float64` (`memory_search.go:217`); needs a richer struct and a format change in `formatMemoryResults` (`memory_search.go:384-451`).

**G.1.3 — Follow-up handles in the result** (PRD: "structured retrieval hits with … follow-up handles")

Today handles are strings inside a markdown line (`memory_search.go:413`). They're stable (§C.2) but not structured — the LLM can't programmatically chain to the right follow-up tool.

**Files that would change:**
- `internal/agent/tools/registry/memory_search.go` — emit a structured per-hit follow-up object (e.g. `{tool: "read_source", args: {source_id: "src_xxx", mode: "ocr"}}`). The current `formatMemoryResults` writes free-form markdown only.
- Alternative: a separate `expand_hit` tool that takes a handle and returns the right typed expansion. Touches `internal/agent/tools/registry/registry.go` to register it.

**G.1.4 — Preserve chunk-to-parent source expansion** (PRD: "preserve …")

The pointer is in `Document.SourceID` already (§E). What's missing is the inline parent payload + a way to ask "give me the full source for this hit" without a separate `read_source` call.

**Files that would change:**
- `internal/agent/tools/registry/memory_search.go` — when emitting a source hit, attach a slimmed-down parent metadata block (filename, kind, status, page_count, wiki_pages back-references).
- `internal/storage/memoryindex/rebuild.go` — `sourcePageDocuments` builds page-level rows; parent metadata is dropped at line 150-163. Decide whether to denormalize parent fields into the page rows, or join at read time.
- `internal/storage/memoryindex/store.go` — `Filter` currently has no `SourceID` field (`store.go:52-56`); add it so "list all chunks for source X" is a one-query operation.

### G.2 Can defer to 7C/7D

**G.2.1 — Projection freshness registry** (PRD step, but the heaviest lift)

The work is broad: every collection needs a persisted `last_indexed_at`, ideally per-row. Today only `tool_index_state.indexed_at` exists. A real registry needs a new table + migration + a reindex-job hook that updates it + dashboard/health-endpoint surfacing. This can come after the registry+score-components+follow-up-handles trio in 7B because none of those depend on freshness; the freshness data is observable extra-information.

**G.2.2 — User memory and operational memory as first-class collections**

User-memory and operational-memory exist today only implicitly (§A.3, §A.4). Formalizing them requires:
- Promoting `proposed_updates` (or a sibling table) into a real user-memory store with its own retention/decay rules separate from "I haven't decided whether to wiki this yet".
- Indexing `tool_attempts` + `run_events` + `audit_events` into `compact_memory_documents` (or a sibling typed collection) so the LLM can ask "did my last attempt at tool X fail, and how".

These are significant feature work, not just typing work. Defer.

**G.2.3 — Span / chunk offsets for sources**

Today sources are page-level only (§B.2). True chunking with byte offsets is a useful improvement but orthogonal to the typed-registry PRD step. Defer.

**G.2.4 — Promoting wiki frontmatter `schema_version`, `prompt_version`, `created_at`, `unversioned` into the index**

These four wiki fields are read-on-demand only (§B.1). Useful for staleness/migration tooling but not strictly required for typed retrieval. Defer.

---

## Critical files for implementation

The Phase 7B planner will touch primarily these paths (absolute):

1. `d:/Aura/internal/storage/memoryindex/store.go` — the Kind constants, `Document` struct, `Filter` struct, RRF merge (`mergeDocumentsRRF`). Single biggest surface for 7B.
2. `d:/Aura/internal/storage/memoryindex/rebuild.go` — per-kind document builders (`sourcePageDocuments`, `ArchiveDocument`, `ProposalDocument`). New collections plug in here.
3. `d:/Aura/internal/storage/search/search.go` — `Result` struct, `mergeHybridResults` (twin of `mergeDocumentsRRF`), wiki payload builder in `loadWikiDocuments`.
4. `d:/Aura/internal/storage/search/sqlite.go` and `d:/Aura/internal/storage/search/qdrant.go` — wiki backends; consume the new `Result` shape.
5. `d:/Aura/internal/storage/search/compact_qdrant.go` — the compact-memory Qdrant mirror; payload helpers `compactPayload` / `compactDocumentFromPayload`.
6. `d:/Aura/internal/agent/tools/registry/memory_search.go` — the LLM-facing tool; `memoryResult`, `formatMemoryResults`, scope→kind mapping. The structured-hit and follow-up-handle work lands here.
7. `d:/Aura/internal/agent/tools/registry/source_read.go` — the parent-expansion target; may grow a "fetch by handle" mode.
8. `d:/Aura/internal/db/migrations/migrations.go` — `compact_memory_documents` schema, `wiki_documents` FTS5 schema. New migration for collection registry / freshness table.
9. `d:/Aura/internal/storage/memoryindex/vector_health.go` — in-memory freshness tracker; the freshness-registry step (deferred) replaces this with a persisted store.
10. `d:/Aura/internal/conversation/summarizer/proposals.go` — when "user memory" becomes a first-class kind (deferred to 7C/7D), this is where proposals get rerouted.
11. `d:/Aura/internal/wiki/schema.go` and `d:/Aura/internal/wiki/store.go` — wiki frontmatter source-of-truth; relevant if 7B chooses to indexed more frontmatter fields.
12. `d:/Aura/internal/storage/sources/store/source.go` — `Source` struct and `Status` enum; relevant if 7B promotes parent metadata into source-page rows.
