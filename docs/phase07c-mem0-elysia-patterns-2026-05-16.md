# Phase 7C — Mem0 V3 + Elysia patterns (2026-05-16)

**Scope:** Pattern reali letti dai 2 codebase di riferimento `D:/tmp/mem0` (Mem0 v3 April 2026) e `D:/tmp/elysia` (Weaviate decision-tree framework). Sintesi delle decisioni di design Phase 7C che ne derivano.

---

## 1. Mem0 V3 — pattern operativi

### 1.1 Storia tracciata in SQLite (`mem0/memory/storage.py:102-126`)

```sql
CREATE TABLE history (
  id           TEXT PRIMARY KEY,
  memory_id    TEXT,
  old_memory   TEXT,
  new_memory   TEXT,
  event        TEXT,             -- 'ADD' | 'UPDATE' | 'DELETE'
  created_at   DATETIME,
  updated_at   DATETIME,
  is_deleted   INTEGER,
  actor_id     TEXT,
  role         TEXT
);
```

Append-only audit log. Ogni write della memoria è un evento storicizzato. **Aura ha già `tool_attempts` con shape simile** — può estendere il pattern a `memory_events`.

### 1.2 Add pipeline V3 ADD-only (`main.py:662-870`)

6 fasi:
1. **Context gathering**: last 10 messages from session
2. **Existing memory retrieval**: top_k=10 per evitare duplicati
3. **LLM extraction single-call** con UUID-to-int mapping (anti-hallucination)
4. **Batch embed** di tutti i testi estratti
5. **MD5 dedup hash** (`mem_hash = md5(text)`) — skip se esiste già
6. **Batch persist** con payload completo

Payload per ogni memoria:
```python
mem_metadata = {
    "data": text,
    "text_lemmatized": lemmatize_for_bm25(text),
    "hash": md5(text),
    "created_at": iso_now(),
    "updated_at": iso_now(),
    "attributed_to": ...,
    "role": ...,
    "actor_id": ...,
}
```

### 1.3 Multi-signal retrieval (`main.py:1343-1438`)

```python
def _search_vector_store(self, query, filters, limit, threshold=0.1):
    # Step 1: Preprocess
    query_lemmatized = lemmatize_for_bm25(query)
    query_entities = extract_entities(query)

    # Step 2: Embed
    embeddings = self.embedding_model.embed(query, "search")

    # Step 3: Semantic (over-fetch 4x)
    internal_limit = max(limit * 4, 60)
    semantic_results = self.vector_store.search(...)

    # Step 4: Keyword (BM25)
    keyword_results = self.vector_store.keyword_search(...)

    # Step 5: BM25 normalization (sigmoid)
    bm25_scores = {...}

    # Step 6: Entity boosts (max +0.5)
    entity_boosts = self._compute_entity_boosts(...)

    # Step 7-8: Fusion + rank
    scored_results = score_and_rank(
        semantic_results=candidates,
        bm25_scores=bm25_scores,
        entity_boosts=entity_boosts,
        threshold=threshold,
        top_k=limit,
    )
```

**Già coperto da Aura US-L03/L04** (score components attraverso RRF). Bonus mancante: **entity-linking boost**.

### 1.4 Hash come freshness check via payload

`main.py:786-803`:
```python
existing_hashes = set()
for mem in existing_results:
    h = mem.payload.get("hash") if hasattr(mem, "payload") and mem.payload else None
    if h:
        existing_hashes.add(h)

records = []
seen_hashes = set()
for mem in extracted_memories:
    text = mem.get("text")
    mem_hash = hashlib.md5(text.encode()).hexdigest()
    if mem_hash in existing_hashes or mem_hash in seen_hashes:
        logger.debug(f"Skipping duplicate memory (hash match): {text[:50]}")
        continue
    seen_hashes.add(mem_hash)
    ...
```

**KEY INSIGHT**: Mem0 mette il `hash` direttamente nel **Qdrant payload**. Anti-dedup e drift signal nella stessa write. Esattamente il LlamaIndex `DocstoreStrategy.UPSERTS` pattern, ma senza tabella docstore separata.

**Per Aura**: aggiungere `content_hash` come colonna su `compact_memory_documents` + nel Qdrant payload — zero nuova tabella docstore.

---

## 2. Elysia (Weaviate) — pattern "metadata-as-collection"

### 2.1 Collection `ELYSIA_METADATA__` (`preprocessing/collection.py:648-777`)

Una collection Weaviate dedicata che traccia per ogni altra collection:

```python
properties = [
    Property(name="name", data_type=TEXT, tokenization=FIELD),
    Property(name="length", data_type=NUMBER),
    Property(name="summary", data_type=TEXT),  # LLM-generated overview
    Property(name="index_properties", data_type=OBJECT, nested_properties=[
        Property(name="isNullIndexed", data_type=BOOL),
        Property(name="isLengthIndexed", data_type=BOOL),
        Property(name="isTimestampIndexed", data_type=BOOL),
    ]),
    Property(name="named_vectors", data_type=OBJECT_ARRAY, nested_properties=[
        Property(name="name", data_type=TEXT),
        Property(name="vectorizer", data_type=TEXT),
        Property(name="model", data_type=TEXT),  # ← embedding_model_id qui!
        Property(name="source_properties", data_type=TEXT_ARRAY),
        Property(name="enabled", data_type=BOOL),
        Property(name="description", data_type=TEXT),
    ]),
    Property(name="vectorizer", data_type=OBJECT, nested_properties=[
        Property(name="vectorizer", data_type=TEXT),
        Property(name="model", data_type=TEXT),
    ]),
    Property(name="fields", data_type=OBJECT_ARRAY, nested_properties=[
        Property(name="name", data_type=TEXT),
        Property(name="type", data_type=TEXT),
        Property(name="description", data_type=TEXT),
        Property(name="range", data_type=NUMBER_ARRAY),
        Property(name="date_range", data_type=DATE_ARRAY),
        Property(name="groups", data_type=OBJECT_ARRAY, ...),  # histogram
        Property(name="date_median", data_type=DATE),
        Property(name="mean", data_type=NUMBER),
    ]),
]
```

### 2.2 Tre observation chiave

1. **Self-describing**: ogni collection ha metadata strutturato che il modello può consultare. NON separato.
2. **Per-field statistics + summaries**: non solo timestamp, ALSO type/range/groups (= histogram). Aiuta il modello a decidere se vale la pena interrogare.
3. **Named vectors + vectorizer.model** sono per-collection — quando l'embedding model cambia, è registrato.

Il modello DSPy lo consulta a query time per decidere quale collection interrogare. **Non è solo health/freshness — è "rich self-description"** che fa anche da freshness registry incidentalmente.

### 2.3 Considerazione per Aura

Il pattern Elysia "metadata as a collection co-locato col data" è seducente ma per Aura significa una collection extra in Qdrant che SQLite può servire meglio (single source of truth per la freshness, fuori dal piano dati). Il pattern Mem0 di mettere hash direttamente nel payload è il "best of both" — niente lookup join, niente storage duplication.

---

## 3. Sintesi dei 3 dossier + Mem0 + Elysia

Le 5 evidenze convergono su 3 decisioni di design per Phase 7C:

### Decisione 1: NON serve una tabella `projection_freshness_doc` separata

Aura ha già `compact_memory_documents.id + updated_at` — basta aggiungere 3 nuove colonne:
- `content_hash TEXT` — Mem0 + LlamaIndex pattern
- `embedding_model_id TEXT` — drift detection
- `index_build_id TEXT` — generation tracking

**Source**: Mem0 v3 mette hash nel payload (anti-dedup + drift signal nella stessa write). LlamaIndex usa docstore `doc_id → hash` map (1 tabella, niente sidecar). Aura segue Mem0 pattern (payload, evitando lookup join).

### Decisione 2: UNA SQLite table `projection_state` è sufficiente per le 5 collection

(non serve N-variable; la lista è chiusa: wiki_documents, qdrant wiki, compact_memory_documents+fts, compact qdrant mirror, embedding_cache — 5 entry totali).

Schema minimo (OpenAI vector_stores + Letta + Weaviate fusi):
```sql
CREATE TABLE projection_state (
  projection_id        TEXT PRIMARY KEY,
  embedding_model_id   TEXT,                              -- '' for FTS
  embedding_dim        INTEGER,                           -- 256 for embeddinggemma
  index_build_id       TEXT NOT NULL,                     -- ULID
  schema_version       INTEGER NOT NULL DEFAULT 1,
  last_full_rebuild_at INTEGER NOT NULL,
  last_incremental_at  INTEGER,
  pending_count        INTEGER NOT NULL DEFAULT 0,
  completed_count      INTEGER NOT NULL DEFAULT 0,
  failed_count         INTEGER NOT NULL DEFAULT 0,
  status               TEXT NOT NULL DEFAULT 'fresh',     -- 'fresh' | 'rebuilding' | 'degraded' | 'stale_swap'
  health_reason        TEXT NOT NULL DEFAULT '',
  version              INTEGER NOT NULL DEFAULT 1,         -- Letta optimistic concurrency
  updated_at           INTEGER NOT NULL DEFAULT (unixepoch())
);
```

### Decisione 3: Retrieval-time annotation deve cadere su 2 livelli

- **Per-collection** (registry row → agent vede `degraded_read=true` su un'intera collection)
- **Per-hit** (payload `content_hash, embedding_model_id, index_build_id, updated_at` direttamente dal Qdrant payload — Mem0 pattern)

---

## 4. Pattern AVOID (validati cross-source)

- **Per-chunk freshness rows** — solo LiveVectorLake li fa, motivato da compliance Aura non ha.
- **Per-row embedding_config mixed-model** — Letta lo fa, contro l'invariante embeddinggemma locked di Aura.
- **Auto-expiry TTL** — Mem0 v2 lo aveva; v3 ADD-only senza decay automatico, e per Aura memory è long-lived.
- **Universal knowledge graph auto-extracted** — Aura memory locked: `[[wiki-links]]` IS the graph; NO KuzuDB/Neo4j/Zep.

---

## 5. Decisione di scope Phase 7C (4 stories)

| Story | LOC stimato | Cosa |
|---|---|---|
| **US-M01** | ~120 + tests | `projection_state` table + migration v12 + `freshness.Store` Go API con optimistic concurrency (versione Letta-style) |
| **US-M02** | ~80 + tests | Aggiungi `content_hash`, `embedding_model_id`, `index_build_id` colonne a `compact_memory_documents` (mig v13). LlamaIndex hash pattern + Mem0 payload-hash. |
| **US-M03** | ~150 + tests | Eager write-time invalidate hooks (`IndexingTurnAppender.Append`, `Rebuild`, source ingest, wiki write). Bumpa `pending_count`. |
| **US-M04** | ~100 + tests | Retrieval-time annotation: surface `index_build_id` + `embedding_model_id` + `staleness_seconds` + `degraded_read` su ogni hit `search_memory`. Anthropic file_id pattern + Mem0 payload return. |

Effort totale: ~3-4 giorni. Tutte le 4 story sono indipendenti, atomiche, commit-per-slice.

**Deferred a Phase 7C-bis (US-M05+ se serve)**:
- Qdrant alias swap (`UpdateCollectionAliases`) — testato solo quando ci sarà embedding swap reale
- Decay timeout scheduled sweep — basta `pending_count` per ora
- Drift-Adapter per embedding swap — futuro
- Per-field statistics Elysia-style (range, groups, histograms) — non è freshness, è discovery
