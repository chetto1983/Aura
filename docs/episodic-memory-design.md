# Episodic memory — past-conversation search (design)

**Status:** design, ready to review. Not implemented. Authored without a live stack
(no Postgres/ArcadeDB/cocoindex here), so every claim below is either cited from code
in this repo or from CocoIndex's docs — nothing is assumed.

**Goal:** make PAST conversations retrievable. Today Aura's long-term memory holds
*distilled facts* (ArcadeDB entity/fact graph). It cannot answer "what did we decide
about X in that chat three weeks ago" verbatim, with context. This adds the missing
layer.

**Non-goal:** the CURRENT conversation. It stays a linear replayed history (+ L2.4
compaction). Retrieval is ADDITIVE, never a replacement for the live history — the rule
Google ADK never breaks (`preload_memory` / `load_memory` inject; the session event log
is untouched). Breaking it would also kill the cacheable prefix Aura's whole context
design is built on.

---

## 1. Inventory — what already exists (nothing here is new machinery)

| Need | Already in Aura | Where |
|---|---|---|
| Ingestion orchestration, incremental add/modify/delete | **CocoIndex**, "with zero code of ours" | `services/ingest/app.py:1-11` |
| Chunking | `chunk.py` | `services/ingest/chunk.py` |
| Embedding | llama-embed sidecar, `AURA_EMBED_BASE_URL`, `EMBED_DIMENSIONS` (768) | `app.py:34-35` |
| ArcadeDB write | CocoIndex **`neo4j` connector on ArcadeDB's Bolt** | `app.py:32,67-68`, target `neo4j.TableTarget[Passage]` (`app.py:250`) |
| Schema DDL authority | **`arcade.py`** ("that is the schema authority") | `app.py:110`, `services/ingest/arcade.py` |
| Per-identity isolation | DB `mem_<identity_uuid>` — the database IS the tenant | `app.py:50`, `identity.database_for()` |
| Hybrid retrieval | **native ArcadeDB** `vector.fuse` + `vector.neighbors(filter:)` + FULL_TEXT | `internal/arcadedb/document_retrieval.go:160-163` |
| Abstention / lexical fallback | `RetrievalPath`, `Abstained`, `Reason` | `internal/arcadedb/memory_vector.go` (`SearchFactsHybrid`) |
| Context injection seam | `TransientContext` trailing block (digest + recall) | `internal/runner/runner_context.go` (Phase 2) |

**Verified externally:** CocoIndex's Postgres **source** imports rows from a table,
requires a **primary key**, is **incremental** (only new/updated rows re-run), takes an
`ordinal_column` for change detection, and supports **LISTEN/NOTIFY** change capture.
(<https://cocoindex.io/docs/sources/postgres>). The spike in
`spikes/cocoindex-ingestion/flows/etl_flow.py` uses the same connector as a *target*.

**Consequence: no new component is warranted.** No Go `MessageIndex`, no custom source,
no fusion/rerank layer of ours (ArcadeDB exposes it natively — the `internal/rerank`
deletion is the standing precedent).

## 2. Shape

```
aura.conversation_turns (Postgres)
        │  CocoIndex postgres SOURCE (incremental, ordinal_column, optional LISTEN/NOTIFY)
        ▼
 services/ingest  →  chunk.py  →  embed sidecar
        │  CocoIndex neo4j TARGET on ArcadeDB Bolt
        ▼
 mem_<identity_uuid> : Utterance vertices (embedding + FULL_TEXT)
        │  native hybrid: vector.fuse(vector.neighbors(...), full-text leg)
        ▼
 Go reader  →  TransientContext trailing block  →  model
```

## 3. Schema (DDL in `arcade.py`, mirroring the `Passage` precedent)

A new vertex type beside `Passage`, same physical pattern
(`internal/arcadedb/document_schema.go:185-216` is the field-for-field template):

```
Utterance
  utterance_key        STRING   UNIQUE      -- <conversation_id>:<seq>:<chunk_ordinal>
  conversation_id      STRING
  seq                  LONG
  chunk_ordinal        LONG
  role                 STRING               -- user | assistant  (see §4 filter)
  text                 STRING   FULL_TEXT (StandardAnalyzer)
  normalized_text_sha256 STRING
  embedding            ARRAY_OF_FLOATS  LSM_VECTOR {dimensions:<dims>, similarity:COSINE, quantization:NONE}
  occurred_at          DATETIME             -- conversation_turns.created_at
  schema_version       STRING               -- "utterance-v1:standard-analyzer:cosine:none:<dims>"
  active               BOOLEAN
  INDEX (active, conversation_id) NOTUNIQUE
```

Optional `Conversation` vertex + `HAS_UTTERANCE` edge if title/aggregate lookups are
wanted; not required for retrieval (the flat `conversation_id` property suffices) —
add it only when a consumer needs it.

**Schema-version contract:** `app.py:39-51` documents that a writer/reader stamp
mismatch fails **silently** (rows land, nothing can read them). The `Utterance` stamp
must be derived the same way, in one place, and asserted by a test on both sides.

## 4. Ingestion

- **Source:** the `aura.conversation_turns` table. PK `(conversation_id, seq)` exists
  (`0005_conversations.up.sql:35`). **Open question O1:** the connector requires a PK —
  composite-PK support is not confirmed in the docs I read. If unsupported, project a
  surrogate key (`conversation_id || ':' || seq`) via a view.
- **Ordinal column:** `created_at` (monotonic per insert). `seq` is per-conversation, so
  it is NOT a global ordinal — do not use it.
- **Identity scoping:** the ingest app is already **one app per identity**
  (`AppConfig(name=f"aura-ingest/{identity_id}")`, DB `mem_<identity>`), so the source
  must filter to that identity's conversations (join `aura.conversations.identity_id`).
  Getting this wrong writes one tenant's messages into another's database — it is the
  single highest-severity failure mode here and needs an explicit test.
- **What to index:** `role IN ('user','assistant')` only. **Never** `system` (it is the
  static prompt) and **never** `tool` (tool payloads are already spilled to sidecars and
  are noise as recall targets). Reasoning is **out of scope** (see §8).
- **Chunking:** reuse `chunk.py`. The embedding model caps at **2048 tokens** (per
  CLAUDE.md), so long turns chunk; `chunk_ordinal` keys them.
- **Skip trivia:** drop turns under a small rune floor ("ok", "grazie") before embedding —
  they cost an embedding call and can only add noise. Knob, not a constant.
- **Freshness:** `coco.auto_refresh(reconcile, interval=…)` already wraps the S3 pass
  (`app.py:349`); the message pass rides the same loop. LISTEN/NOTIFY is a later upgrade,
  not needed for v1.
- **Deletion/erasure:** CocoIndex deletes the row outright on source deletion
  (`app.py:117` notes documents never soft-tombstone). A deleted conversation must
  therefore drop its `Utterance` rows — verify this against the conversation delete
  cascade (`runner_delete.go`) and against `memory_forget`.

## 5. Retrieval (native, no Go fusion layer)

Same query shape as `document_retrieval.go:160-163`, retargeted:

```
SELECT expand(`vector.fuse`(
  `vector.neighbors`('Utterance[embedding]', :embedding, :fetch,
     { filter: (SELECT @rid FROM Utterance
                WHERE active = true AND conversation_id <> :current).@rid }),
  (SELECT @rid, $score FROM Utterance WHERE ... full-text leg ...)))
```

- **Exclude the current conversation** (`conversation_id <> :current`): it is already in
  the linear history; retrieving it back would duplicate context and waste budget.
- **Abstention + lexical fallback:** mirror `SearchFactsHybrid` — no embedder or no
  qualifying leg ⇒ abstain with a `Reason`, never a fabricated hit.
- **Return** `{conversation_id, occurred_at, role, text}` + a conversation title lookup so
  a hit can be cited ("il 14 luglio, in <titolo>").

## 6. Injection (Go side — the only Go work)

The Phase 2 seam already merges fenced sections into ONE trailing
`TransientContext` (`runner_context.go`: digest `<memory_context>` + recall
`<memory_recall>`). Add a third fence:

```
## Past conversations (your own history)
<past_conversations>
[2026-07-14 · "budget refactor"] user: …
[2026-07-14 · "budget refactor"] assistant: …
</past_conversations>
```

Fail-soft exactly like the other two (error/timeout/abstention ⇒ omit the section, the
turn proceeds). The provider gains one method — `SearchConversations(ctx, identityID,
query, excludeConvID)` — reached through the memory MCP host client, alongside
`Context`/`Search`.

**Budget note:** `usableTransientContext` treats the whole block as ONE indivisible unit
(`context_tail.go:18-32`) — it is dropped whole when it does not fit. Three fences make
that block materially bigger, so cap the past-conversation section explicitly rather than
letting it push the digest out.

## 7. Config knobs (three-place pattern: `config.go` field + `envutil` load + `config_knobs.go` row)

| Knob | Default | Why |
|---|---|---|
| `AURA_EPISODIC_ENABLED` | `false` | net-new; dark until measured |
| `AURA_EPISODIC_TOP_K` | `5` | hits injected |
| `AURA_EPISODIC_TIMEOUT_MS` | `1500` | mirrors the preload timeout |
| `AURA_EPISODIC_MIN_RUNES` | `40` | trivia floor before embedding |
| `AURA_INGEST_MESSAGES` | `false` | ingestion side kill-switch |

## 8. Explicitly out of scope

- **Reasoning / procedural memory.** Neo4j Labs' third layer stores reasoning traces for
  *task-similarity* recall — a different use case with a different retrieval contract.
  Aura already persists `reasoning` (amendment #91, display-only), so the raw material
  exists, but it lands as its own phase, not bolted onto this one.
- **The current conversation.** Linear history + compaction, unchanged.
- **A `Conversation` vertex / graph traversal over chats.** Only if a consumer appears.

## 9. Test plan

**Offline (runnable anywhere):** the fence renderer, the trivia floor, the
current-conversation exclusion predicate, fail-soft (error/timeout/abstention ⇒ digest
survives), the schema-version stamp agreeing between the Python DDL and the Go reader.

**`arcadedb_integration` (live):** schema creation idempotent; ingest N turns → hybrid
search returns them ranked; the current conversation is excluded; **tenant isolation —
identity A's search never returns identity B's utterances**; deleting a conversation
removes its utterances; abstention on a nonsense query.
⚠ Per CLAUDE.md, the `arcadedb_integration` tier is currently **wired into nothing** —
not CI, not the Makefile, not the coverage scripts, and not even `go vet`-compiled.
Wiring it is a prerequisite for this slice, not an afterthought.

**Live E2E:** ask about something said only in an older conversation; it must be answered
from the injected block without a memory tool call.

## 10. Risks / open questions

- **O1** — composite-PK support in the CocoIndex postgres source (§4).
- **O2** — one app per identity today; does the message pass ride the same `reconcile`
  or a second flow? (Affects whether one CocoIndex app hosts two sources.)
- **O3** — **privacy**: this puts raw conversation text into the memory database.
  Erasure (`memory_forget`, conversation delete, retention policy) must cover
  `Utterance` rows, and that must be tested, not assumed.
- **O4** — **volume/cost**: every indexed turn is an embedding call. Measure the rate on
  a real corpus before enabling; the trivia floor and `role` filter are the levers.
- **O5** — **retrieval quality unmeasured**: whether verbatim-message recall beats the
  existing fact digest for real questions is exactly what the LOCOMO suite exists to
  answer. Measure before flipping `AURA_EPISODIC_ENABLED` on, then amend the PRD with the
  measurement (PRD-first: the amendment records a measurement, it does not predict one).
