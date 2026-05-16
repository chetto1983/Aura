# Phase 7 — Current RAG/Retrieval Stack Audit

> Date: 2026-05-16
> Scope: read-only audit of Aura's retrieval/RAG layer to scope Phase 7
> ("Rebuild RAG On Typed Memory Layers"). Trigger: the 2026-05-15 live
> debug session found `compact_memory_documents` rows holding raw tool
> errors, tool-schema dumps, AGENT.md dumps, stdout snippets, and
> assistant/tool loop noise.
>
> Method: file:line citations only. Every claim is anchored. Where a
> capability is documented in PRD §7 but absent in code, the row says
> so explicitly.

---

## A. Memory Layers Present Today (code-level inventory)

### A.1 SQLite — primary durable store (`DB_PATH`, default `./aura.db`)

| Table / virtual table | Schema (migration) | Writers | Readers |
|---|---|---|---|
| `settings` | `internal/db/migrations/migrations.go:36-41` (`createCurrentSchema`) | runtime settings overlay | runtime settings overlay |
| `api_tokens` | `internal/db/migrations/migrations.go:43-50` | `internal/api/auth/store.go` (modified) | dashboard auth |
| `allowed_users`, `pending_users` | `internal/db/migrations/migrations.go:53-66` | bootstrap / Telegram | auth gate |
| `scheduled_tasks` | `internal/db/migrations/migrations.go:68-90` | `internal/cron/dispatch.go` | scheduler |
| `conversations` (turn archive) | `internal/db/migrations/migrations.go:92-110`. Unique key `(chat_id, turn_index)` | `internal/conversation/archive.go:107-129` (`ArchiveStore.Append`), called via `internal/conversation/archive_turns.go:36-80` | `internal/conversation/archive.go:132-184` (`ListByChat` / `ListAll`); `internal/storage/memoryindex/rebuild.go:59-74` (`Rebuild` archive branch); dashboard |
| `proposed_updates` | `internal/db/migrations/migrations.go:112-125` | summarizer (`internal/conversation/summarizer`) | `internal/storage/memoryindex/rebuild.go:76-91` (proposals branch) |
| `wiki_issues` | `internal/db/migrations/migrations.go:127-139` | wiki lint | dashboard |
| `embedding_cache` (sha-keyed) | `internal/db/migrations/migrations.go:141-147` | `internal/storage/search/embed_cache.go:267-275` (`store`) | `internal/storage/search/embed_cache.go:244-265` (`cached`) |
| `wiki_documents` (FTS5) | `internal/db/migrations/migrations.go:149-150` | `internal/storage/search/sqlite.go` (`indexSQLiteDocuments`) | wiki search SQLite path |
| `compact_memory_documents` | `internal/db/migrations/migrations.go:152-175` (also recreated at v4: `migrations.go:988-1021`) | `internal/storage/memoryindex/store.go:134-217` (`Upsert`), `:219-279` (`ReplaceKind`) | `internal/storage/memoryindex/store.go:311-339` (`Search`) |
| `compact_memory_fts` (FTS5) | `internal/db/migrations/migrations.go:177-178`; `store.go:29-32` (CREATE) | mirrored on every `Upsert` / `ReplaceKind` / `RebuildFTS` | `store.go:484-505` (`ftsSearch`) |
| `principals` / `channel_accounts` / `actors` / `capability_grants` / `authz_decisions` | `migrations.go:180-266` (v7 `addIdentityCapabilityGrants`) | identity layer | authz |
| `swarm_runs` / `swarm_tasks` | `migrations.go:268-305` | swarm manager | swarm dashboard |
| `runs` | `migrations.go:307-340` (v6 also at `:654-712`) | run event foundation | dashboard / replay |
| `run_events` | `migrations.go:342-366` | run event foundation | dashboard / replay |
| `run_outbox` | `migrations.go:368-386` | run event foundation | outbox dispatcher |
| `run_idempotency_keys` | `migrations.go:388-398` | run event foundation | idempotency check |
| `audit_events` | `migrations.go:400-415` | governance / authz audit | dashboard |
| `tool_index_state` | `migrations.go:636-651` (v5 `addToolIndexState`) | tool reconciler | tool reconciler |
| `tool_attempts` | `migrations.go:1024-1066` (v10 `addToolAttempts`) + view `tool_warnings` | `internal/agent/executor.go:160-208` (`recordObs`, called inside `executeOneTool`) | briefer / `/api/tool-warnings` |
| `secrets` | `migrations.go:1069-1081` (v9) | settings | runtime |

### A.2 Filesystem

| Surface | Path | Writer | Reader |
|---|---|---|---|
| Wiki pages (`.md`+frontmatter) | `<WIKI_PATH>/<slug>.md` | `internal/wiki/store_writes.go:54-69` (`WritePage`), atomic via `writeAtomic` at `:364-381` | `internal/wiki/store.go:186-205` (`ReadPage`); `internal/storage/search/search.go:224-318` (`loadWikiDocuments`) |
| Wiki operational files | `<WIKI_PATH>/index.md`, `log.md`, `graph/graph.json`, `graph/context.md` | `internal/wiki/store.go:379-451` (`updateIndex`, `updateGraph`, `appendLog`); `internal/wiki/graph.go:159-176` (`WriteGraphFiles`) | dashboard, internal graph builder |
| Sources (raw bytes + metadata) | `<WIKI_PATH>/raw/<src_xxx>/` (atomic temp+rename) | `internal/storage/sources/store/store.go:103-149` (`Put`) | `internal/storage/sources/store/store.go:152-173` (`Get`, `getLocked`) |
| OCR markdown | `<src_xxx>/ocr.md` (written by `internal/ocr`, ingested by `memoryindex/rebuild.go`) | `internal/ocr` (not part of audit) | `internal/storage/memoryindex/rebuild.go:187-203` (`readSourceIndexBody`) |
| Skills | `<SKILLS_PATH>/<name>/SKILL.md` | dashboard installer | skill registry (not part of RAG layer) |
| Prompt overlays | `<PROMPT_OVERLAY_PATH>/*.md` | external operator | every turn |

### A.3 Qdrant — vector mirror (sidecar)

| Collection | Embedded shape | Writer | Reader |
|---|---|---|---|
| Wiki vector collection (configured base name) | One point per wiki page **and** per synthetic `graph_node` "card" doc | `internal/storage/search/qdrant.go` (`qdrantRepository.IndexWikiPages` / `ReindexWikiPage`); enqueued by `internal/storage/reindex/worker.go:64-90` (`Submit`) | `internal/storage/search/qdrant.go` (`Search`); used via `search.Searcher` in `internal/agent/tools/registry/memory_search.go:226-267` |
| `<base>_compact` (compact memory mirror) | One point per `compact_memory_documents` row, payload from `compactPayload()` at `internal/storage/search/compact_qdrant.go:247-265` | `compact_qdrant.go:49-95` (`Recreate`, called via `Store.SyncVector` → `memoryindex/store.go:429-441`); `:97-116` (`Upsert`, fired per `Store.Upsert`) | `compact_qdrant.go:136-178` (`Search`), wired into `memoryindex.Store.Search` at `store.go:327-337` |

> Note: there is no `tool_index` collection registered as a RAG layer.
> `tool_index_state` is the diff table used by `internal/toolindex.Reconciler`
> for tool registry → Qdrant tool retrieval — separate from the
> memory layers under audit.

### A.4 In-memory non-durable

| Structure | Type | Built from | Used by |
|---|---|---|---|
| `wiki.GraphIndex` | adjacency map (`outbound`/`inbound`/`meta`) at `internal/wiki/graph_index.go:19-44` | warmed at boot from disk (`internal/wiki/store_graph.go:16-33`); per-page deltas via `WritePage` (`store_writes.go:179-181`) and `DeletePage` (`store_writes.go:336-339`) | exposed via `Store.GraphIndex()` (`store_graph.go:39`). **No reader in `internal/agent/tools/registry/memory_search.go` today** — the index is built but unused by the retrieval tool. See §F. |

---

## B. Where Raw Tool Output Enters `compact_memory`

The full write path is one chain: **`agent.executor` → loop msgs → `archive_turns` → `ArchiveStore.Append` → `IndexingTurnAppender` → `compact_memory_documents`**.

### B.1 Step-by-step trace

1. **Tool result content materialized** —
   `internal/agent/executor.go:91-93` builds each tool's user-facing body:
   ```go
   wrapped := WrapUntrustedToolResult(call.Name, raw)
   outcomes[i] = toolOutcome{id: call.ID, content: limitToolContent(wrapped, e.maxChars)}
   ```
   And `executor.go:123` writes it into the LLM message history:
   ```go
   e.state.AddToolResultMessage(o.id, o.content)
   ```
   This includes errors: `executor.go:200-203` formats both error and successful output through `tools.FormatToolError(err)` / the raw `out` and they all flow to `AddToolResultMessage`.

2. **Loop messages become `LoopMessages`** — at end-of-turn the channel
   driver (Telegram / chat hub) collects `state.Messages()` and hands the
   slice to `ArchiveConversationTurns`. This is the shape
   `internal/conversation/archive_turns.go:14-23` (`ArchiveTurnInput`):
   ```go
   LoopMessages []llm.Message  // assistant + tool + assistant + tool …
   ```

3. **Each loop message is appended as a `Turn`** —
   `internal/conversation/archive_turns.go:55-79`:
   ```go
   for i, msg := range input.LoopMessages {
       turn := Turn{
           ChatID:     input.ChatID,
           UserID:     input.UserID,
           TurnIndex:  nextIdx,
           Role:       msg.Role,        // "assistant" | "tool"
           Content:    msg.Content,     // <-- raw tool output, untrimmed
           ToolCallID: msg.ToolCallID,
       }
       ...
       appendTurn(turn)
   }
   ```
   Note: `Role` can be `"tool"` and `Content` is the verbatim
   `WrapUntrustedToolResult`-wrapped output. **No content filter, no
   role gate.**

4. **`ArchiveStore.Append` writes the row** —
   `internal/conversation/archive.go:107-129`. SQL is a straight INSERT
   into `conversations`.

5. **`IndexingTurnAppender.Append` mirrors into compact memory** —
   `internal/storage/memoryindex/rebuild.go:308-324`:
   ```go
   func (a *IndexingTurnAppender) Append(ctx context.Context, turn conversation.Turn) error {
       ...
       if err := a.next.Append(ctx, turn); err != nil {
           return err
       }
       if a.index != nil {
           if turn.ID == 0 { turn = a.persistedTurn(ctx, turn) }
           if doc, ok := ArchiveDocument(turn); ok {
               _ = a.index.Upsert(ctx, doc)        // <-- writes compact_memory_documents
           }
       }
       return nil
   }
   ```
   Every persisted `Turn` — including `role="tool"` rows — becomes a
   `compact_memory_documents` row with `kind="archive"`.

6. **`ArchiveDocument` does compaction but not filtering** —
   `internal/storage/memoryindex/rebuild.go:205-232`:
   ```go
   func ArchiveDocument(turn conversation.Turn) (Document, bool) {
       body := compactForIndex(turn.Content, archiveSnippetLimit)  // 1600 char cap
       if body == "" { return Document{}, false }
       ...
       Tags: []string{turn.Role},  // preserves "tool" / "assistant" / "user"
       ...
   }
   ```
   `compactForIndex` (`rebuild.go:343-349`) only flattens whitespace and
   truncates to 1600 chars — it does NOT inspect role or content
   semantics.

### B.2 The exact line where filtering is missing (Phase 7A trigger)

Two candidate insertion points, both currently no-ops:

- **`internal/conversation/archive_turns.go:55-79`** — the loop that
  builds `Turn` from `LoopMessages` has no `if msg.Role == "tool"
  && shouldFilter(msg)` branch. Tool errors and tool schema dumps are
  archived unconditionally.

- **`internal/storage/memoryindex/rebuild.go:308-324`** — `IndexingTurnAppender.Append`
  forwards every persisted `Turn` (including `role="tool"`) to
  `ArchiveDocument` → `index.Upsert`. There is no kind/role gate, no
  content-shape sniff, no "is this a tool-error envelope" check.

The minimum-surface fix lives at `rebuild.go:308-324`, because filtering
there preserves the `conversations` table (debugging/replay needs the
raw record) while keeping the *retrievable* `compact_memory_documents`
layer clean.

### B.3 Concrete noise types observed (per the 2026-05-15 session)

All five categories described in the task brief enter through the path
above:

- **Raw tool errors** — produced by `executor.go:203`
  (`return tools.FormatToolError(err), …`). `FormatToolError` returns a
  string like `"Error: <redacted>"`; the assistant role's prior tool_call
  stub stays in the loop too. Both archive as separate rows.
- **Tool schema dumps** — when the LLM calls `tool_search`, the result
  body contains tool definitions. `executor.go:570-577` absorbs the
  result into the per-turn pool but does NOT short-circuit archival;
  the raw schema list is still in the `tool` message and is archived.
- **AGENT.md dumps** — when the LLM calls `file` with `action="read"`
  on `AGENT.md` (or a SKILL.md), the file body returns through the
  tool-result path and is archived verbatim.
- **stdout snippets** — `execute_code` / `execute_shell` results flow
  the same way; `governance.Apply` (`agent/loop.go:318`) shrinks them
  for the LLM but the unshrunk version still sits in
  `state.Messages()` until `AddToolResultMessage` — and the archived
  copy is taken BEFORE microcompaction (microcompaction is applied
  per LLM round, not at archive time).
- **Assistant/tool loop noise** — every assistant turn that contains
  reasoning prose alongside `tool_calls` is archived; the
  `assistant`-role row holds the prose (`archive_turns.go:64-71`
  serializes `msg.ToolCalls` into a sibling JSON column but `Content`
  still holds the inline narration).

> Implication: a single user turn with N tool calls produces **at
> least 1 + 2N rows** in `conversations` and the same number of
> `compact_memory_documents` archive rows (one user, N assistant
> w/ tool_calls, N tool results). All of them are searchable.

---

## C. How `search_memory` Ranks Today

### C.1 Per-query call sequence

`internal/agent/tools/registry/memory_search.go:139-197` (`Execute`):

1. Parse args (`query`, `scope`, `limit`, `chat_id`) → `memoryScopes` at
   `:476-500`.
2. Apply per-call timeout (default `5 * time.Second`, `:46`).
3. Fan out per scope:
   - `searchWiki` (`:226-267`) → calls the `search.Searcher` interface
     (Qdrant + SQLite FTS hybrid lives inside that backend; see C.2).
   - `searchCompact` (`:269-300`) → one call into `memoryindex.Store.Search`
     for all compact kinds in the scope set.
4. Apply per-result `relevanceTimesRecencyWithHalfLife` (`:332-349`):
   - Wiki/source/graph kinds use 180-day half-life (`:52`).
   - Archive/proposal kinds use 30-day half-life (`:53`).
   - Score = relevance × `0.5^(age_days / halfLife)`.
5. Stable-sort by score desc, truncate to `limit`, format as plain
   markdown (`:384-452`).

The "all" scope issues **one** compact search with merged kinds —
`memory_search_test.go:338-351` enforces this with
`TestSearchMemoryToolUsesOneCompactSearchForAllScope`.

### C.2 Fusion layers — RRF exists at TWO levels today

#### C.2.a Compact memory (SQLite FTS + Qdrant)
`internal/storage/memoryindex/store.go:311-339` (`Store.Search`):
```go
exact, err := s.exactSearch(ctx, query, filter, limit)   // lower(id|handle|title) =
fts,   err := s.ftsSearch(ctx, query, filter, limit)     // compact_memory_fts MATCH
var vector []Document
if s.vector != nil {
    vectorResults, vecErr := s.vector.Search(ctx, query, filter)  // Qdrant compact
    ...
}
return mergeDocumentsRRF(exact, fts, vector, limit), nil
```
RRF fusion at `store.go:577-622`. Constants: `k=60`, weights
exact=1.0, fts=0.6, vector=0.8 (`store.go:561-566`).

Vector-search failure is a logged warning (`store.go:331-333`); the
fusion proceeds with `exact+fts` only.

#### C.2.b Wiki (Qdrant cosine + SQLite FTS)
Hybrid handled inside `internal/storage/search/qdrant.go` (not read in
this audit) and merged via
`internal/storage/search/search.go:343-386` (`mergeHybridResults`) with
the same RRF constants (`k=60`, exact=1.0, fts=0.6, vector=0.8,
`search.go:326-332`). Result is `[]search.Result` returned to
`searchWiki` at `memory_search.go:233`.

### C.3 Layer labels preserved in the result struct?

YES.

- Compact path: `memoryindex.Document.Kind` (`store.go:34-50`) — values
  `KindSource="source"`, `KindArchive="archive"`, `KindProposal="proposal"`
  (`store.go:17-20`).
- Wiki path: `search.Result.Kind` (`search.go:88-101`) — values
  `wiki_page` or `graph_node` (`search.go:443-457`).
- Surfaced into the LLM-facing markdown line as
  `"- [%s] %s"` (`memory_search.go:399`), e.g. `[archive]`, `[source]`,
  `[wiki]`, `[graph_node]`.

### C.4 Score components NOT returned

`memoryResult` (`memory_search.go:209-224`) carries one final `Score`
float. The per-backend contributions (exact rank, FTS BM25,
cosine cosine, recency factor) are collapsed before the LLM sees the
hit. PRD §7 (`prd.md:1412`) asks for "structured retrieval hits with
score components and follow-up handles" — not implemented.

---

## D. Citation Handle Shapes

### D.1 Wiki
`memory_search.go:247-249`:
```go
identifier := "[[" + r.Slug + "]]"
if kind == "graph_node" {
    identifier = r.Slug      // raw slug, not [[slug]]
}
```
The format line at `memory_search.go:399-414` emits `[[slug]]` as the
identifier and additionally surfaces `file=wiki/<filepath>`,
`category=`, `tags=`, `related=[…]` when populated. Slug-level handle
is stable; no anchor-within-page handle (no `#section`, no line range).

### D.2 Source
`memoryindex/rebuild.go:138-156` shows the source doc shape on indexing.
Identifier comes back via `memory_search.go:302-313` (`compactIdentifier`):
```go
if doc.Kind == memoryindex.KindSource && doc.SourceID != "" {
    return doc.SourceID    // e.g. "src_a1b2c3d4e5f6a7b8"
}
```
Plus a separate `handle` (`memory_search.go:412-414`):
```go
handle=source:src_a1b2c3d4e5f6a7b8#page=2
```
So Phase 7 gate (`prd.md:1421` — "source hits cite source/page/span")
has **source + page** (`Document.Page` from `rebuild.go:142-145`) but
**no span**. Span = byte/line range within the page is absent in code:
`splitSourcePages` (`rebuild.go:166-185`) only captures whole pages
keyed off `^## Page N` headings in `ocr.md`.

### D.3 Archive
`memoryindex/rebuild.go:205-232`:
- ID: `archive:<turn.ID>` (or `archive:<chatID>:<turnIndex>` fallback)
- Handle: `conversation:<turn.ID>` (or
  `conversation:chat:<chatID>#turn=<turnIndex>`)
- Title: `chat=<chatID> turn=<turnIndex>`

`turn.ID` is the `conversations.id` autoincrement primary key, so
`conversation:42` is joinable back to `conversations.id=42` and
through that to `(chat_id, turn_index)` — a real foreign-key path.
The dashboard's archive viewer reads via `ArchiveStore.Get` keyed off
`id` (`internal/conversation/archive.go:277-293`).

### D.4 Proposal
`memoryindex/rebuild.go:234-259`:
- ID & handle: `proposal:<proposal.ID>` (FK to
  `proposed_updates.id`).

### D.5 Wiki graph_node "card"
`memory_search.go:248-250` returns the raw slug (no `[[…]]` wrapping).
The cards are built by `buildGraphDocuments` (referenced at
`internal/storage/search/search.go:316`, in `graph_documents.go`,
not read this audit). They live alongside wiki pages in the same
Qdrant collection.

---

## E. Projection Freshness

### E.1 Wiki page edit → reindex

`internal/wiki/store_writes.go:170-172`:
```go
if s.reindexSubmitter != nil {
    _ = s.reindexSubmitter.Submit(reindex.Job{Slug: slug, Op: reindex.OpUpsert})
}
```
Same fires on delete at `store_writes.go:326-328` (with `OpDelete`),
on backlink edits at `store_writes.go:282-284`. Worker drains in
`internal/storage/reindex/worker.go:48-90`.

**Drop-newest queue** (`reindex/types.go:31-34` — "Body is intentionally
not carried — the worker re-reads the page from disk when processing,
so drop-newest is safe"). `reindex/worker.go:80-89`: on full queue the
job is dropped and counted in `droppedTotal`. The drop is logged but
NOT visible to the LLM or the retrieval layer — there's no `stale`
flag on a returned wiki result.

### E.2 Embedding-model change → re-embed?

**Not implemented.** The cache key is `(content_sha, model)` with
`model = EmbedCacheNamespace(baseURL, model)` (`embed_cache.go:26-36`).
A model change means cache misses, but the existing Qdrant collection
keeps its old vectors:
- `compact_qdrant.go:62-95` (`Recreate`) does delete-then-create-then-upsert
  but warm-cache short-circuits when points already exist (`:67-75`),
  and the bypass comment at `:62-66` explicitly admits:
  > "vector-size drift from EMBEDDING_MODEL swaps is not detected here
  > because CollectionInfo does not expose the stored vector size.
  > Search queries will fail loudly with a Qdrant dimension error if the
  > operator changes models without rebuilding the collection."

So an operator-driven model swap silently leaves the Qdrant collection
mismatched until a hard rebuild.

### E.3 Freshness registry — exists?

**No.** A `Grep` for `freshness|stale_after|reindex_watermark|projection_freshness`
across `internal/` returned **zero matches**. The closest operational
visibility:
- `reindex.Health` (`reindex/types.go:43-50`) — exposes `LastSuccess`,
  `LastError`, `QueueDepth`, `Dropped`, `DroppedAfterStop` — wired to
  `/api/health` only.
- `EmbedCache.Stats()` (`embed_cache.go:117-120`) — hits/misses.

Neither surface ships back with a retrieval result as a `freshness`
field, and neither covers FTS5 staleness, graph staleness, or
compact-memory staleness. PRD §7 (`prd.md:1405`, `:1413`,
`:1425-1426`) — not implemented.

---

## F. Graph Index — What Does `RebuildGraph()` Actually Store?

Aura today has **two** graph representations and they are subtly
different:

### F.1 Materialized graph (disk, JSON)

`internal/wiki/store.go:368-370` exposes `RebuildGraph(ctx)` →
`updateGraph` at `:435-451`, which calls `BuildGraphFromReader` /
`WriteGraphFiles`.

The on-disk shape is `Graph` (`internal/wiki/graph.go:30-35`):
```go
type Graph struct {
    Nodes      []GraphNode      // {ID, Title, Category}
    Edges      []GraphEdge      // {Source, Target, Type}
    BrokenRefs []GraphBrokenRef // {Source, Target, Type}
    Orphans    []string
}
```

Edge type is one of two literals (`graph.go:101-106`):
- `"wikilink"` — derived from `ExtractWikiLinks(page.Body)`
- `"related"` — derived from `page.Related` frontmatter

**Edges are typed (2 types) but NOT weighted, NOT source-attributed,
NOT cited.** No edge confidence, no source_id annotation. Each `(Source,
Target)` collapses duplicate refs of the same type via the `seenEdges`
set at `graph.go:77-100`.

Files written: `<WIKI_PATH>/graph/graph.json` + `graph/context.md`
(`graph.go:159-176`). These are NOT indexed into Qdrant or
`compact_memory_documents` directly — but synthetic `graph_node`
"card" documents ARE built from the same pages and shipped into the
wiki Qdrant collection via `buildGraphDocuments` invoked at
`internal/storage/search/search.go:316`.

### F.2 In-memory `GraphIndex`

`internal/wiki/graph_index.go:19-44`:
```go
type GraphIndex struct {
    mu       sync.RWMutex
    outbound map[string]map[string]bool  // slug -> set of slugs
    inbound  map[string]map[string]bool
    meta     map[string]NodeMeta         // {Slug, Title, Category, Tags}
}
```

This is plain `[[wiki-link]] ∪ Related` adjacency, undirected by
convention (the comment at `:14-18` says so). **Edges are not typed at
all here**, are unweighted, and have no source attribution. The index
exposes BFS via `Neighbors(slug, depth)` at `:181-221`, plus
`OutNeighbors`, `InNeighbors`, `Degree`, `HasNode`, `Meta`.

**Unused by retrieval.** No call site in
`internal/agent/tools/registry/memory_search.go` references
`store.GraphIndex()` or `Neighbors()`. Confirmed by `Grep` for
`GraphIndex(` and `Neighbors(` across the tools registry — only the
wiki store's own warm-up and tests reference them.

### F.3 Summary against PRD §7

PRD §7 (`prd.md:1408`) asks for "GraphIndex with typed weighted edges,
source edges, degree, and bounded neighborhood/path queries".

Today:
- typed edges: PARTIAL (materialized: 2 types `wikilink`/`related`; in-mem: none)
- weighted edges: ABSENT
- source edges: ABSENT (no edge carries `source_id` / no edge from a
  page to a `src_xxx`)
- degree: PRESENT (`graph_index.go:260-267`)
- bounded neighborhood: PRESENT (`Neighbors(depth)`)
- path queries: ABSENT
- community detection: ABSENT (PRD §7 `:1409`)
- community reports: ABSENT (PRD §7 `:1410-1411`)

---

## G. Gap Inventory for Phase 7 (ranked by impact)

Each row maps a PRD §7 (`prd.md:1394-1430`) requirement to one of:
- **CODE-EXISTS** — implemented today
- **PARTIAL** — partial implementation, gap noted
- **DOC-ONLY** — referenced in code comments but not implemented
- **ABSENT** — no code anywhere

| # | PRD §7 line | Requirement | Status | Evidence |
|---|---|---|---|---|
| 1 | `:1419-1420` (gate) | user facts not in wiki / tool failures not in wiki | **ABSENT** | `memoryindex/rebuild.go:308-324` archives every Turn regardless of role/content. Phase 7A's actual trigger. |
| 2 | `:1400` | define memory layer IDs and citation handles | **PARTIAL** | Kinds exist (`memoryindex/store.go:17-20`) and handles are constructed per-kind; but no central registry; archive handles lack span/anchor; source handles lack span. |
| 3 | `:1405` | projection freshness registry (FTS, Qdrant, graph, embedding caches) | **ABSENT** | No freshness table or struct anywhere. Closest: `reindex.Health` and `EmbedCache.Stats` (per-layer, in-process, not on the retrieval response). |
| 4 | `:1421` | source hits cite source/page/span | **PARTIAL** | source + page yes (`memoryindex/rebuild.go:138-156`); span NO (`splitSourcePages` only splits by `## Page N`, no intra-page byte/line offsets). |
| 5 | `:1408` | GraphIndex typed weighted edges, source edges, path queries | **PARTIAL** | Adjacency + bounded Neighbors exist; typed materialized graph exists; weights ABSENT; source edges ABSENT; path queries ABSENT. See §F.3. |
| 6 | `:1412` | structured retrieval hits with score components | **ABSENT** | `memoryResult.Score` (`memory_search.go:209-224`) is a collapsed scalar; per-backend rank not surfaced. |
| 7 | `:1413` | projection freshness + degraded-read warnings on hits | **ABSENT** | Vector-search failure becomes a `slog.Warn` (`memoryindex/store.go:331-333`) — never reaches the LLM-facing markdown. Warnings printed today are limited to `"compact memory search failed"` / `"wiki search timed out"` strings (`memory_search.go:269-283`). |
| 8 | `:1407` | per-slug wiki upsert/delete + separate force full-rebuild | **PARTIAL** | Per-slug upsert + delete exist (`reindex.Job{Slug, Op}` + `reindex/worker.go`). Force full-rebuild exists as `memoryindex.Rebuild` / `Store.RebuildFTS` / `SyncVector` but is invoked from boot/dashboard only — no `reindex.Job{Op: OpFullRebuild}` shape. |
| 9 | `:1409-1411` | community detection + community reports as projections | **ABSENT** | No Louvain/Leiden/CD algorithm anywhere; `BuildGraph` (`internal/wiki/graph.go:37-141`) produces nodes/edges/orphans only. |
| 10 | `:1403` | hybrid FTS/vector retrieval with RRF fusion | **CODE-EXISTS** | Two-tier RRF: `memoryindex/store.go:577-622` and `internal/storage/search/search.go:343-398`. Both use `k=60`. |
| 11 | `:1404` | chunk-to-parent source expansion | **PARTIAL** | Source pages are independent docs (`memoryindex/rebuild.go:125-159`); a parent `source:<src_id>` exists alongside `source:<src_id>#page=N`; no expansion call from a page back to the full source body in retrieval. |
| 12 | `:1414` | retrieval errors as recoverable learning events | **ABSENT** | Search errors return plain strings to the LLM (`memory_search.go:269-283`) and never feed `tool_attempts` (`migrations.go:1024-1066`). |
| 13 | `:1415` | golden RAG evals (facts, sources, lessons, stale, deletes, renames, model change) | **ABSENT** | `internal/agent/tools/registry/memory_search_test.go` covers correctness + recency-decay but no "stale projection", "renamed slug", "deleted page", "model change" golden fixture. |
| 14 | `:1406` | durable, idempotent, op-aware, watermark-based reindex jobs | **PARTIAL** | `reindex.Job{Slug, Op}` is op-aware. **Watermark-based: ABSENT** — the queue is in-RAM only; a crash mid-drain loses queued jobs (`reindex/worker.go:48-67`). |
| 15 | `:1402` | split recall by task intent (not one polymorphic mode) | **PARTIAL** | `search_memory` has a `scope` enum (`memory_search.go:111-137`): `all|wiki|sources|archive|proposals`. This is "scope by kind", not "intent" (e.g. "find a fact", "find an example", "find what user said") — semantic intent routing ABSENT. |

### G.1 Most painful first (impact order)

1. **#1 — Compact archive contains tool noise.** The 2026-05-15 trigger. Every `search_memory` query competes with `[archive]` rows that hold raw error text and AGENT.md dumps. Filter at `memoryindex/rebuild.go:308-324`.
2. **#3 — No freshness registry.** When the model embedding changes, the wiki Qdrant collection silently mismatches dimensions until first query (`compact_qdrant.go:62-66`). Phase 7 gate (`prd.md:1425-1426`) cannot pass without this.
3. **#5 — Graph is plain `[[link]]` adjacency.** Wiki graph carries 2 edge types and zero weights; in-memory index is built and never queried by retrieval. Phase 7 GraphRAG (`prd.md:1408-1411`) needs typed + weighted + source-attributed edges and a retrieval call site.
4. **#7 — Degraded reads invisible.** Vector backend errors are dropped to logs (`memoryindex/store.go:331-333`); the LLM gets FTS-only results without knowing why. Phase 7 gate (`prd.md:1425`) requires explicit degraded-read warnings.
5. **#6 — Score components collapsed.** Single scalar precludes the model from understanding why a hit ranked where it did; blocks `:1412` and indirectly `:1414`.
6. **#9 — No community detection / reports.** Phase 7 GraphRAG global-sensemaking gate (`prd.md:1427-1428`) cannot pass.
7. **#4 — Source spans absent.** Per-page citation is the floor; per-span (byte/line range) is needed for "where in the page" citations.
8. **#12 — Retrieval errors are not learning events.** `tool_attempts` would carry them today if recorded; the search_memory tool doesn't write observation rows from its internal sub-search failures.
9. **#14 — Reindex queue is RAM-only.** A boot loses any queued reindex job. Phase 7 requires durable, watermark-based jobs (`:1406`).
10. **#13 — Golden RAG evals missing.** Required for Phase 7 gate (`:1415`).
11. **#11 — No chunk-to-parent expansion.** Source page hits can't fetch the full source body inline.
12. **#15 — Intent-based recall not implemented.** Scope is kind, not intent.
13. **#2 — Citation handles partially registered.** Existing handles work, but a central registry doc/struct is absent.
14. **#8 — Force full-rebuild not exposed as a reindex op.** Available as direct call, not via the queue.

---

## H. Cross-References (for the Phase-K Ralph planner)

**Files to touch for Phase 7A (compact archive filter):**
- `internal/storage/memoryindex/rebuild.go:308-324` — insert role/content gate before `index.Upsert`.
- `internal/conversation/archive_turns.go:55-79` — decide if `role="tool"` should reach `conversations` at all (or only the post-filter assistant message that summarizes the loop). Keep `conversations` lossless for replay; filter at the `IndexingTurnAppender` boundary, not at the `ArchiveStore.Append` boundary.

**Files to touch for Phase 7 RAG rebuild (broader):**
- `internal/storage/memoryindex/store.go:34-50` — extend `Document` with `Provenance`, `Score components`, `FreshnessTag`.
- `internal/agent/tools/registry/memory_search.go:209-224` — extend `memoryResult` to carry per-backend scores + freshness.
- `internal/wiki/graph_index.go:19-44` — typed weighted edges + source nodes.
- `internal/storage/reindex/types.go:9-34` — extend `Op` (`OpFullRebuild`) and add watermark/durable queue.
- `internal/db/migrations/migrations.go` — new `projection_freshness` table.

**Tests that lock the current behavior:**
- `internal/agent/tools/registry/memory_search_test.go` — all 11 tests at `:18-547`.
- `internal/storage/memoryindex/store_test.go` (not read here, exists per file listing).
- `internal/wiki/graph_index_test.go` (per file listing).

---

End of audit.
