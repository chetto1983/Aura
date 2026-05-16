# Phase 7C — Industry Framework Audit (2026-05-16)

**Date:** 2026-05-16
**Scope:** Audit of 5 framework reali per come implementano freshness/versioning nei loro indici. Aura context: 5 projection targets — wiki FTS5, Qdrant wiki, `compact_memory_documents` + compact FTS5, Qdrant compact mirror, embedding cache. Constraints: Go binary + SQLite + Qdrant sidecar + locked embeddinggemma-300m@256d, single-machine, no external graph DB.

---

## 1. Qdrant — Payload Indexes + Collection Aliases

### 1.1 Payload schema for freshness signals

Qdrant stores metadata as JSON "payload" attached to each point. Recommended primitives: **datetime payload indexes** (v1.8.0+) and **tenant indexes** for partitioning.

```json
PUT /collections/{collection_name}/index
{
    "field_name": "timestamp_field",
    "field_schema": "datetime"
}
```

Source: qdrant.tech/documentation/concepts/indexing/

For multi-tenancy:
```json
PUT /collections/{collection_name}/index
{
    "field_name": "tenant_id",
    "field_schema": { "type": "keyword", "is_tenant": true }
}
```

Multitenancy article quote: **"You should only create multiple collections when your data is not homogenous or if users' vectors are created by different embedding models."** — This is the escape valve for embedding-model swap: *new model = new collection*.

### 1.2 Collection-level metadata via REST

`GET /collections/{name}` returns: `status, optimizer_status, indexed_vectors_count, points_count, segments_count, config, payload_schema`. Qdrant also supports collection-level metadata as KV pairs.

### 1.3 Aliases — atomic swap for full rebuild

```json
POST /collections/aliases
{
    "actions": [
        { "delete_alias": { "alias_name": "production_collection" } },
        { "create_alias": {
            "collection_name": "example_collection",
            "alias_name":      "production_collection"
        } }
    ]
}
```

Doc guarantees: **atomic** swap. "no concurrent requests will be affected during the switch." Building block: Aura writes to `wiki_v2`, runs `index_build_id`-stamped rebuild, then swaps the alias `wiki` → `wiki_v2`. Go client already supports this via `qdrant.UpdateCollectionAliases`.

### 1.4 Snapshots — point-in-time versioning

`POST /collections/{collection_name}/snapshots` returns `name, creation_time, size, checksum`. `creation_time` + `checksum` together provide a usable "version" tuple persistable to SQLite.

### 1.5 Applicability to Aura

- **Adopt**: alias swap for full Qdrant rebuild (wiki + compact mirror). 1d effort.
- **Adopt**: payload `index_build_id` (keyword) + `updated_at` (datetime, indexed) on every point. 0.5d.
- **Adopt**: snapshot `creation_time` + `checksum` recorded into SQLite `projection_state` table per rebuild. 0.5d.
- **Skip**: collection-level KV metadata sync — Aura is single-node, SQLite is a better store.

---

## 2. Vespa — Attribute Fields + Live Deployment

### 2.1 Schema (`.sd`) attribute declaration

```
field timestamp type long {
    indexing: attribute | summary
}
```

`indexing: attribute` makes the field available for "structured search, ranking, sorting, grouping, aggregation". For high-throughput partial update, Vespa adds `attribute: fast-search`.

### 2.2 Partial updates — memory-speed writes on attributes

```json
{
    "update": "id:namespace:doctype::1",
    "fields": {
        "firstName": { "assign": "John" },
        "lastName":  { "assign": "Smith" }
    }
}
```

Doc quote: **"For highest possible write throughput for field updates, use attributes to write at memory speed."** Architectural lesson for Aura: a *separate column* in SQLite for `dirty_at` / `last_seen_at` rather than a full row rewrite per touch.

### 2.3 Conditional updates — test-and-set versioning

```json
{
    "update": "id:mynamespace:music::bob/BestOf",
    "condition": "music.sales==999",
    "fields": { "sales": { "increment": 1 } }
}
```

Returns HTTP 412 if condition fails. Optimistic concurrency without an explicit version column. Aura can do the same in SQLite via `UPDATE … WHERE version = ?`.

### 2.4 No built-in versioning — caveat

Vespa does NOT have a dedicated version field type. Users implement versioning manually with `long`/`int` attribute fields. Freshness contract is therefore *application-level* — exactly the pattern Aura should adopt for `projection_state.index_build_id`.

### 2.5 Zero-downtime schema migration

`vespa deploy .`. Vespa safely changes the running system without impacting queries, writes, or data. Destructive changes rejected pre-deployment.

### 2.6 Applicability to Aura

- **Adopt (concept)**: attribute-vs-index distinction → in SQLite, keep freshness signals in a *thin column*, never rewrite the whole projection row. 0.5d.
- **Adopt (concept)**: freshness as a *rank-time signal*, not a filter. Stale wiki page → don't drop from results, *decay its score*. 1d.
- **Adopt (concept)**: conditional update / optimistic concurrency. 0.5d.
- **Skip**: Vespa as runtime. Out of scope.

---

## 3. Weaviate — Auto-Maintained Timestamps + Aliases

### 3.1 Auto-maintained per-object timestamps

`_additional` GraphQL property exposes per-object: `id, vector, creationTimeUnix, lastUpdateTimeUnix, distance, score`.

```graphql
{
  Get {
    Article ( nearText: { concepts: ["fashion"] } ) {
      title
      _additional { id distance }
    }
  }
}
```

Auto-maintained — Weaviate stamps them on every write. Opt-in inverted-index entry:
```json
"invertedIndexConfig": { "indexTimestamps": false }
```

Default `false`; flip to `true` to make filter-able. The store does the bookkeeping for you — you just opt into the index when you want range queries.

### 3.2 Collection aliases — atomic swap

```python
client.alias.update(
    alias_name="ArticlesAlias",
    new_target_collection="ArticlesV2"
)
```

Doc quote: **"This operation is atomic and provides instant switching between collections."** Same shape as Qdrant aliases.

### 3.3 Applicability to Aura

- **Adopt (concept)**: auto-stamped `created_at` / `updated_at` on every projection row. Aura's `compact_memory_documents` should mirror this — SQLite `DEFAULT (unixepoch())` + `AFTER UPDATE` trigger. 0.5d.
- **Adopt (response shape)**: surface `last_updated` on every retrieval hit. 0.5d.
- **Skip**: Weaviate-style GraphQL.

---

## 4. LlamaIndex — DocstoreStrategy + Hash-Based Skip

### 4.1 DocstoreStrategy enum

```python
class DocstoreStrategy(str, Enum):
    UPSERTS              = "upserts"
    DUPLICATES_ONLY      = "duplicates_only"
    UPSERTS_AND_DELETE   = "upserts_and_delete"
```

Docstrings:
- **UPSERTS** — "Checks if a document is already in the doc store based on its id. If it is not, or if the hash of the document is updated, it will update the document in the doc store and run the transformations."
- **DUPLICATES_ONLY** — "Checks if the hash of a document is already in the doc store. Only then it will add the document to the doc store and run the transformations."
- **UPSERTS_AND_DELETE** — "Like the upsert strategy but it will also delete non-existing documents from the doc store."

Mechanism: **a `doc_id` → `document_hash` map persisted in the docstore.** Hash mismatch ⇒ re-embed + re-write; hash match ⇒ skip.

### 4.2 What's missing from LlamaIndex

No explicit `embedding_model_id` tracking — the docstore is *model-agnostic*. Anti-pattern Aura must avoid — Phase 7C explicitly tracks `embedding_model_id` per row.

### 4.3 Applicability to Aura

- **Adopt**: content-hash skip is exactly the `dirty_count` mechanism — store hash on `compact_memory_documents`, set `dirty=1` when hash changes, decrement `dirty_count` on re-embed. 1d.
- **Adopt**: 3-state strategy enum (`UPSERTS` / `DUPLICATES_ONLY` / `UPSERTS_AND_DELETE`) maps cleanly to Aura's partial-update modes. 0.5d.
- **Reject**: model-agnostic docstore. Aura must explicitly track `embedding_model_id` per row.

---

## 5. Letta — Versioned Memory Blocks + MemFS

### 5.1 Block ORM — version column, no `updated_at`

From `letta/orm/block.py`:
```python
version: Mapped[int] = mapped_column(
    Integer, nullable=False, default=1, server_default="1"
)
```

Two key choices:
1. **Optimistic concurrency via `version`**, configured via SQLAlchemy `__mapper_args__ = {"version_id_col": version}`.
2. **No `updated_at` column on the block itself** — history is delegated to a separate `block_history` table linked via `current_history_entry_id`. The block row holds *current state*; history is append-only sibling.

### 5.2 Passage ORM — typed by source

Three classes:
- `BasePassage`: `id, text, embedding_config, metadata_, tags, embedding`
- `SourcePassage` (uploaded files): adds `file_name`, indexed on `(organization_id, created_at, file_id)`
- `ArchivalPassage` (agent-created): adds `passage_tags`, indexed on `(organization_id, archive_id, created_at)`

`created_at` / `updated_at` come from inherited mixins. **Timestamping is reusable**, not duplicated per table.

### 5.3 MemFS — git-backed memory

`~/.letta/agents/<agent-id>/memory` is a directory of markdown files. Agent "commits and pushes to save changes — giving you a full version history of everything your agent has learned." This is *exactly* Aura's wiki model (markdown + go-git + `[[wiki-links]]`).

### 5.4 Embedding model swap — `embedding_config` per passage

Each passage carries its own `embedding_config`. Letta therefore supports *mixed* embedding configs in one archive. **Opposite** of Aura's locked-model invariant — REJECT.

### 5.5 Applicability to Aura

- **Adopt**: `version` column with optimistic-concurrency-style update (`UPDATE … WHERE version = ?`) on `projection_state` rows. 0.5d.
- **Adopt**: separate `_history` table for blocks that need history. Already approximately how `wiki_issues` / `proposed_updates` work. 0d.
- **Reject**: per-row `embedding_config`. Aura's embeddinggemma is locked.

---

## 6. OpenAI / Anthropic — Vector Store + Files API

### 6.1 OpenAI vector_stores object

```json
{
  "id":          "vs_abc123",
  "object":      "vector_store",
  "created_at":  1699061776
}
```

Full schema: `id, object, created_at, file_counts {in_progress, completed, failed, cancelled, total}, status (expired|in_progress|completed), name, usage_bytes, last_active_at, expires_at, expires_after, metadata`.

**Critical pattern for Aura**: `file_counts` decomposes a single rebuild into a 5-bucket progress signal. Aura's `dirty_count` becomes a tuple: `{ pending_embed, embedded, failed, cancelled, total }`. The `status` enum is a 3-state lifecycle, not a boolean.

### 6.2 Anthropic Files API — file_id contract

```json
{
  "id":         "file_011CNha8iCJcU1wXNR6q4V8w",
  "type":       "file",
  "filename":   "document.pdf",
  ...
  "created_at": "2025-01-01T00:00:00Z"
}
```

No `updated_at` — files are *immutable by id*. Updates are new uploads with new `file_id`. **Content-addressing as the freshness primitive**: identity = content, so staleness ≡ "newer file_id exists for the same logical document". Citation responses surface the `file_id` directly.

### 6.3 Stale-citation semantics

Anthropic docs explicit: **"Files are inaccessible via the API shortly after deletion, but they may persist in active Messages API calls and associated tool uses."** Citations don't proactively invalidate — they fail late, at retrieval.

### 6.4 Applicability to Aura

- **Adopt**: 5-bucket `file_counts` shape for `projection_state.dirty_count` → `{ pending, in_progress, completed, failed, cancelled }`. 0.5d.
- **Adopt**: 3-state status enum on `projection_state` (`in_progress` | `completed` | `expired`). 0.5d.
- **Adopt**: content-addressed identity for embedding-cache rows. Already partially in place. 0.5d.
- **Adopt (concept)**: stale-citation = "fail late on retrieval" rather than proactive sweep. 1d.

---

## DESIGN MATRIX

| Axis | Qdrant | Vespa | Weaviate | LlamaIndex | Letta | OpenAI/Anthropic |
|---|---|---|---|---|---|---|
| **Granularity** | per-coll (alias) + per-point (payload) | per-doc (attribute) | per-object (auto timestamps) | per-doc (hash) | per-block (version) + per-passage | per-file (file_id) + per-store (status) |
| **Staleness signal** | payload field (datetime) + collection KV | attribute field | auto-maintained timestamps; optional inverted index | docstore `doc_id → hash` | DB column `version` + sibling `block_history` | `created_at`, `last_active_at`, `status` enum |
| **Retrieval annotation** | filter via payload | rank profile term | `_additional { lastUpdateTimeUnix }` | not surfaced | `version` returned; passage `created_at` | `file_id` carried into citation; `status` in store |
| **Rebuild atomicity** | **alias swap** (atomic) | live deploy | **alias swap** (atomic) | `UPSERTS_AND_DELETE`; no atomic primitive | None at framework level | None public; file-grained add/delete |
| **Embedding swap** | new collection + alias | new schema deploy | new collection + alias | nuke docstore | per-passage `embedding_config` (mixed-model OK) | per-store; opaque |
| **Partial-update** | upsert payload | `{ "assign": ... }` on attribute | object patch + auto bump | hash-diff skip | optimistic-lock `UPDATE … WHERE version = ?` | re-upload = new file_id |
| **Conditional / TaS** | not directly | `condition: …` → 412 | not directly | hash equality implicit | `version_id_col=version` | not exposed |

---

## ADOPT FOR AURA — Shortlist

### A. SQLite `projection_state` table with composite freshness tuple — **0.5d**

OpenAI `file_counts` + Weaviate auto-timestamps + Letta `version`.

```sql
CREATE TABLE projection_state (
  projection_id        TEXT PRIMARY KEY,
  embedding_model_id   TEXT NOT NULL,
  index_build_id       TEXT NOT NULL,
  last_full_rebuild_at INTEGER NOT NULL,
  pending_count        INTEGER NOT NULL DEFAULT 0,
  in_progress_count    INTEGER NOT NULL DEFAULT 0,
  completed_count      INTEGER NOT NULL DEFAULT 0,
  failed_count         INTEGER NOT NULL DEFAULT 0,
  cancelled_count      INTEGER NOT NULL DEFAULT 0,
  status               TEXT NOT NULL,
  freshness_sla_secs   INTEGER NOT NULL,
  version              INTEGER NOT NULL DEFAULT 1,
  updated_at           INTEGER NOT NULL DEFAULT (unixepoch())
);
```

### B. Qdrant alias swap for atomic Qdrant rebuilds — **1d**

Wiki and compact mirror collections become alias-fronted. Rebuild writes to `wiki_v{build_id}`, snapshots, then swaps alias.

### C. Per-row content-hash skip for incremental updates — **1d**

LlamaIndex `DocstoreStrategy.UPSERTS` verbatim.

`compact_memory_documents` gets `content_hash` column (SHA256 of text + `embedding_model_id`). On rebuild scan: if `content_hash` unchanged, skip embed; if changed, re-embed and bump `version`.

### D. Surface `index_build_id` + `last_updated` on every retrieval hit — **0.5d**

Weaviate `_additional { lastUpdateTimeUnix }` + Anthropic citation `file_id`.

### E. Optimistic-concurrency update on `projection_state` — **0.5d**

Letta `__mapper_args__ = {"version_id_col": version}` + Vespa `condition: …`.

```sql
UPDATE projection_state SET … , version = version + 1
 WHERE projection_id = ? AND version = ?;
```

If 0 rows affected ⇒ concurrent rebuild detected, abort.

---

## Total effort estimate

A + B + C + D + E ≈ **3.5 days** of focused work. All five are independent and can land as separate user stories.

## Patterns explicitly REJECTED

- **External graph DB / KuzuDB / Neo4j / Zep** — memory-locked.
- **Per-row `embedding_config` (Letta-style mixed model)** — clashes with locked embeddinggemma invariant.
- **Auto-expiry / `expires_after` (OpenAI)** — Aura memory is long-lived.
- **Vespa as runtime** — out of scope; only its *concepts* portable.

---

## Sources

- Qdrant: qdrant.tech/documentation/concepts/collections, /indexing, articles/multitenancy, api.qdrant.tech/api-reference/snapshots/create-snapshot
- Vespa: docs.vespa.ai/en/schemas.html, /partial-updates.html, /reference/document-json-format.html, /reference/schema-reference.html, /application-packages.html
- Weaviate: docs.weaviate.io/weaviate/api/graphql/additional-properties, /manage-collections/collection-aliases, /config-refs/collections
- LlamaIndex: developers.llamaindex.ai/python/framework/module_guides/loading/ingestion_pipeline; github.com/run-llama/llama_index/blob/main/llama-index-core/llama_index/core/ingestion/pipeline.py
- Letta: docs.letta.com/guides/agents/memory-blocks, /letta-code/memory; github.com/letta-ai/letta/blob/main/letta/orm/block.py, /passage.py
- OpenAI: developers.openai.com/api/reference/resources/vector_stores
- Anthropic: docs.anthropic.com/en/docs/build-with-claude/files
