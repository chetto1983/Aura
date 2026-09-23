# Changing the embedding model — design

**Date:** 2026-09-23
**Status:** approved by the operator 2026-09-23; not implemented.
**Replaces:** the three 2026-09-21 documents (the provenance/runtime-route design, Codex's
adversarial review of it, and the re-embedding design). They were deleted in the same change;
`git log --diff-filter=D -- docs/superpowers/specs/` recovers them. The review's findings are
answered below, point by point, wherever this design depends on them.

Written for the implementer. It assumes no knowledge of the conversations that produced it.
Every claim is a file, a line, or a measurement dated 2026-09-23 on the lab VM
`192.168.101.158` (images `ghcr.io/chetto1983/aura*:edge`, ArcadeDB 26.9.1, cocoindex 1.0.24)
against repo HEAD `e8ef129e4`.

## The problem

Vectors from two embedding models do not share a space: the cosine distance between them is
not small or large, it is meaningless. Nothing errors when they mix — retrieval still returns
its k nearest neighbours, every healthcheck stays green, and answers quietly get worse.

Since `2b7825cb4` the cockpit can select a cloud embedding model in one click
(`web/src/settings/EmbeddingBackendControl.tsx`, Local / OpenRouter / Manual endpoint). The
save is an ordinary settings save marked "restart required" (`internal/agui/settings_api.go:36-58`),
and `POST /api/admin/restart` restarts **only the daemon** (`internal/agui/restart_api.go:1-5`).
Today, after that click and a restart:

| writer | embeds with | why |
|---|---|---|
| `cmd/aura` (daemon) | the new cloud model | `config.EmbedRoute()` reads the overlaid settings at boot |
| `cmd/arcadedb-mcp` | the **old** model until its container restarts | reads `aura.settings` once at boot (`cmd/arcadedb-mcp/boot_settings.go:26-79`) |
| `services/ingest` | **always the local sidecar** | base URL from compose env only (`services/ingest/app.py:37`, `compose.yaml:969`), model hard-coded `"embeddinggemma"` (`app.py:276`), no Authorization header (`app.py:280`) |

No record anywhere says which model produced its vector, so none of this is detectable.

## Decisions taken by the operator

1. **Re-embed everything on a model change.** No per-row staleness tracking, no partial or
   selective passes. Changing the embedder is a rare, deliberate, one-time decision, taken
   behind a warning that states the cost.
2. **From the confirmation until the pass completes, retrieval is lexical only.** Dense
   retrieval never serves a query vector against a corpus that is not entirely in its space.
3. **Embedding requests are batched.** Measured in `434cdd539`: 25.9x faster on a cloud route
   (32 chunks, 9.95 s → 0.38 s, `perplexity/pplx-embed-v1-0.6b`), 1.0x on the local sidecar
   (8.61 s → 8.33 s, because llama.cpp at `-np 1` works a batch's inputs one after another).
   The bill is identical either way: OpenRouter charges per token.
4. **Documents are re-embedded by CocoIndex**, through `deps` on the embedding function —
   not by a Go re-embedder that would duplicate the Python recipe.
5. **An unverifiable corpus is never self-certified.** A database whose stored vectors cannot
   prove their space (every database that exists before this ships) is treated as a
   mismatch until the operator runs the pass. The pass is always operator-started.
6. **A stale memory sidecar restarts itself.** When `arcadedb-mcp` finds the route in
   `aura.settings` differs from the one it booted with, it drains and exits;
   `restart: unless-stopped` (`compose.yaml:728`) boots it on the new route.

## What was measured

### On the VM, 2026-09-23 (read-only)

- `aura.settings` holds exactly one embedding row: `AURA_EMBED_BASE_URL=http://aura-llama-embed:8081`.
  No model is set, so every stored vector on this VM came from the local sidecar.
- Sidecar command: `-m embeddinggemma-300M-Q8_0.gguf --embeddings --embd-normalize 2 -ngl 0 -t 4 -np 1 -c 2048 -ub 2048`.
- Container env: `aura` has `AURA_EMBED_BASE_URL`, `_DIMENSIONS`, `_REVISION`, `_FINGERPRINT`;
  `aura-arcadedb-mcp` has **no** `AURA_EMBED_*` (it reads the route from Postgres);
  `aura-ingest` has only `AURA_EMBED_BASE_URL` and `AURA_EMBED_DIMENSIONS`.
- One tenant database, `mem_448ddbe1_96ea_405d_8219_4a3d52a425c0`:

  | type | records | with vector | source field | source chars |
  |---|---|---|---|---|
  | `FACT` | 0 | 0 | `statement` | 0 |
  | `ConversationTurn` | 17 | 17 | `content` | 4,356 |
  | `ReasoningTrace` | 5 | 5 | `provider_summary` | 6,663 |
  | `IndexedDocument` | 10 | 10 | `card` | 6,086 |
  | `Passage` | 48 | 48 | `text` | 199,644 |

  About 217k characters in total: ~72k tokens by ingest's deliberately overshooting
  3-chars/token fallback, i.e. roughly three minutes at the local sidecar's measured
  ~400 tok/s (280 tok → 584 ms, 2,016 tok → 5.2 s, measured 2026-09-21).
- A second database, `aura_memory`, exists and holds no types at all. It is a leftover, not
  a tenant; out of scope here and reported so it is not mistaken for one.
- No type records a model. The only per-database singleton rows are `IngestStatus`
  (Python-owned, `services/ingest/arcade.py:180-244`) and `MemoryBatchReceipt`; neither
  names a model.

### CocoIndex `deps` propagates through a memoized caller — measured

The installed package (`cocoindex==1.0.24`, `docker/aura-ingest/requirements.txt:13`) has
`deps` on `coco.fn` and `coco.fn.as_async` (`cocoindex/_internal/function.py:629`). Its
docstring: *"folded into the function's logic fingerprint; when the canonical form changes,
memoized results are invalidated and the change propagates to callers according to
`logic_tracking` (transitively under `"full"`)… Snapshotted once at decoration time."*

A throwaway spike inside the production `aura-ingest` container reproduced ingest's chain —
`app_main → mount_each(process_file, memo=True) → coco.map(process_chunk) →
_embed(memo, batching, deps=…)` — with a temporary `COCOINDEX_DB`, and ran it four times:

| run | `deps` | `process_file` bodies run | `_embed` calls |
|---|---|---|---|
| 1 | A | 2 of 2 | 4 batched calls for 7 inputs |
| 2 | A | 0 | 0 |
| 3 | B | **2 of 2** | 4 batched calls for 7 inputs |
| 4 | B | 0 | 0 |

So a new `deps` value on `_embed` re-runs every unchanged, memoized file and re-embeds every
chunk and card, batched. It also re-runs when the value changes **back**: memo entries are
keyed by the fingerprint they were recorded under. `update_blocking(full_reprocess=True)`
exists too; `deps` is preferred because it fires on exactly the change that matters and on
nothing else.

### OpenRouter publishes price and input limit per embedding model

`GET https://openrouter.ai/api/v1/models?output_modalities=embeddings` returned 37 models on
2026-09-23, each with `pricing.prompt` (USD per token) and `context_length`. Limits range
from **512** (`liquid/lfm-2.5-embedding-350m:free`) through 8,192 (`google/gemini-embedding-2`)
to 32,768. Ingest's chunk ceiling is a constant 2,048 EmbeddingGemma tokens
(`services/ingest/chunk.py:47`), counted by the local sidecar's `/tokenize`
(`chunk.py:90-113`), which does not exist on a cloud route. The Go client already handles
this correctly: it reads the route's published limit and, on a hosted route, cuts on UTF-8
bytes because no token is shorter than one byte (`internal/embeddings/fit.go:43-99`).

### Code facts the design rests on

- Every Go vector write and dense read goes through the tenant `*arcadedb.Client`'s
  `embedder` (`internal/arcadedb/client.go:187-189,412-417`; call sites in
  `memory_vector.go:70,114,248,429`, `memory_conversation.go:170`, `memory_reasoning.go:196`).
  Each already degrades softly: a write without a vector, a read on the lexical leg
  (`memory_vector.go:209-221,245-254`; `memory_reasoning.go:331-343` falls back with no reason).
- Writers by type: `FACT` — daemon and MCP; `ConversationTurn` and `ReasoningTrace` — daemon
  only; `IndexedDocument` and `Passage` — Python only (`services/ingest/app.py:376,470`).
- `FACT` backfill exists: `EmbedMissingFacts` (`memory_vector.go:357-394`), the clear
  statement (`:398-399`), and the scheduled per-tenant sweep `memory_embed_backfill`, every
  5 minutes, capped at 5 minutes a run (`cmd/aura/serve_memory_backfill.go:51-78`,
  `cmd/aura/serve_provisioning.go:56`, `internal/cron/handlers/memory_embed_backfill.go:18`).
  `ConversationTurn` and `ReasoningTrace` have no `embedding IS NULL` fill.
- A new `FACT` copies the vector of any stored `FACT` with the same statement
  (`memory_batch_store.go:35-58`). Harmless once every stored vector is in the current space;
  it is why the pass clears before it fills.
- Document retrieval has **no lexical-only path, by a measured decision**:
  `internal/arcadedb/document_retrieval.go:20-24` (Go-side fusion measured 0.300 recall@1
  against the engine's 0.850) and `internal/documents/retrieval.go:296-305` (no query vector →
  zero documents). The full-text indexes it would need already exist: `Passage[text]`,
  `IndexedDocument[card]`, `IndexedDocument[file_name_words]` (`services/ingest/arcade.py:274,315,323`).
- The ingest supervisor restarts a child whenever its `ProcessSpec` fingerprint changes,
  checked every 15 s (`internal/ingestsupervisor/supervisor.go:57-97,135-224`); the
  fingerprint already includes a secret (`SecretKey`). It reads Postgres (`AURA_DB_URL`) but
  not `aura.settings` (`cmd/aura-ingest-supervisor/main.go:32-57`).
- `arcadedb-mcp` already reads the route and the sealed `OPENROUTER_API_KEY` from Postgres
  (`boot_settings.go:68-100`, `13baccf36`) and shuts down gracefully on SIGTERM
  (`cmd/arcadedb-mcp/main.go:139-160`).
- Go's and Python's document prefixes are equal today (`internal/embeddings/tasks.go:11`,
  `services/ingest/chunk.py:54`) but nothing asserts it across the two languages.

## Design

### 1. The space identity

One Go function, `config.EmbedSpace`, names the vector space a route produces. Every process
computes it from its own resolved route. Python never derives it: it receives the string
from its supervisor and treats it as opaque.

```
embed-v1:r<recipe>:<dims>:local:<AURA_EMBED_FINGERPRINT>
embed-v1:r<recipe>:<dims>:openrouter:<model id>
embed-v1:r<recipe>:<dims>:endpoint:<cloud base URL>:<model id>
```

- `recipe` is a constant, `embeddings.RecipeVersion`, placed beside the prefixes in
  `internal/embeddings/tasks.go`. It is incremented whenever anything that transforms text
  into a stored vector changes without changing a model name: the query/document prefixes
  (Go and Python), `--embd-normalize`, `TruncateMRL`. This answers the review's finding that a
  route string names a request, not the transform.
- OpenRouter's hostname is not part of the identity (a proxy or DNS alias for the same model
  must not demand a rebuild). A manual endpoint keeps its base URL, because there the base URL
  is what selects the model.
- `local` uses the GGUF's SHA-256 already required under a strict profile
  (`internal/config/config_document_retrieval.go:62-67`). Compose passes
  `AURA_EMBED_FINGERPRINT` to `arcadedb-mcp` and `aura-ingest` as it already does to `aura`
  (`compose.yaml:253-254`). With no fingerprint (non-strict dev profiles) the value is
  `local:unpinned`.
- `AURA_EMBED_DIMENSIONS` is in the string so a width mismatch is a named state rather than an
  insert error. Width changes themselves stay out of scope (the setting is deliberately not
  editable, `internal/settings/settings.go:3-6`).

### 2. The marker

A document type `EmbeddingSpace` in each tenant database, one row under a UNIQUE key.

| property | meaning |
|---|---|
| `key` | always `corpus`; UNIQUE, so two creators race safely |
| `state` | `ready` or `reembedding` |
| `space` | the space every stored vector is in (`ready`) |
| `target` | the space the pass is moving the corpus to (`reembedding`) |
| `started_at`, `cleared_at` | pass progress; `cleared_at` is null until the Go vectors are cleared |
| `updated_at`, `updated_by` | audit |

Only Go writes it. Its DDL is part of the memory schema (`EnsureMemorySchema`,
`internal/arcadedb/memory.go:64-94`), and `EnsureEmbeddingSpace` runs right after it in
`TenantClients.For` (`internal/arcadedb/tenant_clients.go:93-101`).

**An absent row is created only when the corpus provably is in the caller's space:**
no `FACT`, `ConversationTurn` or `ReasoningTrace` has a vector, and either no document vector
exists or `IngestStatus` reports `status=ready` with `embed_space` equal to the caller's space
(§6). Otherwise the row stays absent and the database reads as `unknown`. Every database that
exists before this ships is therefore `unknown`: none of its records can prove its space. A
fresh database becomes `ready` on its own within one ingest cycle.

Effective state as seen by a process with space `S`:

| marker | dense reads | vector writes | cockpit |
|---|---|---|---|
| `ready(S)` | yes | yes | ready |
| `reembedding(target=S)` | **no** (lexical) | yes | pass in progress |
| `ready(X≠S)`, `reembedding(target≠S)` or absent (`unknown`) | **no** (lexical) | **no** | re-embed required |

### 3. The guard

The tenant `*arcadedb.Client` carries its process's space and wraps every use of `embedder`:

- a **write** embed proceeds only when the marker's destination (`space` when ready,
  `target` when re-embedding) equals the process's space;
- a **query** embed proceeds only when the marker is `ready(S)`.

A refused embed returns a typed error. The existing soft paths turn it into a write without a
vector or a lexical read, with two new reasons beside `memory_vector.go:213-220`:
`embedding_space_mismatch` and `embedding_reembedding`. The marker is cached per client for
10 s.

The daemon's document retriever embeds its query through `documents.EmbeddingClient`, not the
arcadedb client, so `HostRetriever` asks `DocumentIndex` for the same effective state before
embedding (§8).

### 4. Changing the route from the cockpit

The generic settings `PUT`/`DELETE` refuse `AURA_EMBED_MODEL`, `AURA_EMBED_BASE_URL` and
`AURA_EMBED_CLOUD_BASE_URL` with 409 and point to the endpoints below, so the warning cannot
be bypassed. The new endpoints use the capabilities the route keys already require
(`internal/agui/settings_api_authz.go`).

- `GET /api/settings/embedding-space` — per tenant: effective state, `space`, `target`, own
  space, pending counts per type, and the ingest status.
- `POST /api/settings/embedding-route/preview` — body: the three route values. It returns:
  - the target space;
  - per type, the record and character counts, summed across tenants;
  - estimated tokens: characters ÷ `chunk.CHARS_PER_TOKEN_FALLBACK`, an overshoot by design;
  - estimated duration: ~400 tok/s on the local sidecar; for a cloud route, the one measured
    rate of 4,694 tokens in 0.38 s, labelled as such;
  - estimated cost: tokens × `pricing.prompt` from the OpenRouter catalogue the picker
    already fetches, or "unknown" when the entry has no price, never a guess;
  - the model's published input limit;
  - the fixed statement that retrieval is lexical-only until the pass completes.
- `POST /api/settings/embedding-route` — body: the route plus `confirm_space`, which must
  equal the target the server recomputes, so what runs is exactly what was previewed. In
  order: write the settings rows, move every tenant's marker to `reembedding(target)` with
  `started_at=now`, then fire the existing restart trigger (`cmd/aura/serve_restart.go:17`).
  With the route unchanged this is the "re-embed" action for an `unknown` or mismatched
  corpus.

### 5. The pass

The pass is the existing `memory_embed_backfill` sweep, extended, plus one run at daemon boot.
Per tenant, when the marker is `reembedding(target)` and the daemon's space is `target`:

1. **Grace.** Do nothing until `started_at + grace`, where
   grace = `embeddings.DefaultTimeout` + 2 × the marker cache TTL (80 s with today's
   constants). By then no writer can still be completing a vector it embedded in the old
   space.
2. **Clear**, once: `UPDATE … SET embedding = NULL` on `FACT`, `ConversationTurn` and
   `ReasoningTrace`, then set `cleared_at`. From here on a non-null Go vector can only be in
   `target` (§3), so `embedding IS NULL` is an exact work queue. An interrupted pass resumes
   by filling; it never needs to restart from scratch.
3. **Fill** each type in batches of 32 texts per `Embed` call, within the client's token
   budget (`internal/embeddings/fit.go:20-25`):
   - `FACT` reuses `EmbedMissingFacts`;
   - `ConversationTurn` gets `EmbedMissingTurns`, over `content`;
   - `ReasoningTrace` gets `EmbedMissingTraces`, over `provider_summary` (a trace with no
     summary has no vector today and gets none);
   - all three use the document prefix, as the writers do.
4. **Documents.** Wait until `IngestStatus` reports `embed_space = target`, `status = ready`,
   and `observed_at > started_at` (§6).
5. **Promote.** When no fillable Go record is null and step 4 holds, set `ready(space=target)`.

When the marker is `ready(S)` the sweep does what it does today, and additionally fills turns
and traces that were written without a vector.

`ApplyConversationProjection` embeds one turn per request (`memory_conversation.go:170`).
After the clear, the one-minute conversation reconciler replays turns without a vector
(`internal/runner/runner_memory_projection.go:259-302`) and would re-embed each one singly,
racing the batched fill. The projection is changed to embed every turn that needs a vector in
one batched call.

### 6. Ingest

**Supervisor.** On every reconcile tick it resolves the embedding route from `aura.settings`.
It uses a helper extracted from `cmd/arcadedb-mcp/boot_settings.go` into `internal/settings`,
so the memory sidecar and the supervisor share one mapping and the one sealed credential,
`OPENROUTER_API_KEY` (there is no embed-specific key: `bd31e157b`).

- `ProcessSpec` gains the base URL, model, API key, space and input limit.
- `Environment()` sets them as `AURA_EMBED_BASE_URL`, `AURA_EMBED_MODEL`,
  `AURA_EMBED_API_KEY`, `AURA_EMBED_SPACE` and `AURA_EMBED_INPUT_LIMIT`.
- `fingerprint()` includes them, so a route change restarts every child within one poll.
- The input limit comes from the same catalogue read `embeddings.Client` performs
  (`fit.go:43-74`), exported for reuse.
- A failed settings read keeps the running children and their specs, and is logged. At
  startup it starts nothing, the fail-closed behaviour `arcadedb-mcp` already has.

**Python child.** Embedding moves out of `app.py` (617 lines, over the cap) into
`services/ingest/embed.py`. The new module:

- decorates `_embed` with `deps=EMBED_SPACE`, read from the environment and required;
- on a hosted route (API key set), sends the model, `Authorization: Bearer`, and
  `dimensions`, and truncates and renormalises a wider vector, mirroring `TruncateMRL`
  (`internal/embeddings/client.go:110-150,230-256`);
- validates every returned width, which today is never checked (`app.py:287-302`);
- sizes chunks to `min(2048, AURA_EMBED_INPUT_LIMIT)` tokens, so a small-context cloud model
  gets more, smaller chunks rather than silent truncation. On a hosted route it counts UTF-8
  bytes instead of calling `/tokenize`: Go's rule, and a guaranteed upper bound. Because
  chunking runs inside `process_file`, the same `deps` change re-chunks.

`record_status` writes `embed_space` into `IngestStatus`: a new property in `_status_ddl`,
`arcade.py:180-192`. This is how the pass and the marker initializer learn which space the
document vectors are in without Python ever touching the marker.

Moving `_embed` to a new module changes its logic fingerprint. Adding `deps` changes it too.
The deploy that ships this therefore re-embeds every document once, in the same local space.
On the VM that is ~206k characters, about three minutes.

### 7. `arcadedb-mcp`

When the guard sees a marker whose destination differs from MCP's space, MCP re-resolves the
route from `aura.settings`, at most once every 30 s. If the resolved space differs from the
one it booted with, MCP cancels its root context: the existing graceful shutdown, with a 10 s
budget. It then exits 0 and compose restarts it on the new route. If the space is unchanged
(an `unknown` corpus, or a mismatch no settings row explains), MCP stays up and degraded: it
writes no vectors and serves lexical reads. Health says why.

### 8. Documents in lexical mode

`HostRetriever.Retrieve` gets a lexical mode, used whenever the tenant's state is not
`ready(S)` and whenever the query embedding fails. The second case today returns zero
documents (`retrieval.go:301-305`). Lexical mode:

- runs the existing `SEARCH_INDEX` legs alone: cards on `card` OR `file_name_words`, passages
  on `text`;
- ranks each list by its own full-text score and never weighs cards against passages;
- returns a new status, `RetrievalLexicalOnly`, whose reason names the cause:
  `embedding_reembedding`, `embedding_space_mismatch` or `query_embedding_unavailable`.

This deliberately amends the line at `document_retrieval.go:20-24`. That measurement compared
Go-side fusion against engine fusion. It says nothing against a single-leg lexical answer
while the dense leg is unavailable. Lexical results are candidates, and the response says so:
a full-text top-k always returns something, including for questions the corpus cannot answer
(measured 2026-08-06).

### 9. Visibility

- The cockpit's embedding card shows, per tenant:
  - `ready`;
  - `re-embedding`, with the Go records still pending per type and the ingest status;
  - `re-embed required`, with the button that opens the same preview.
- The route control opens the preview instead of saving directly. The save stays disabled
  until the operator confirms; strings in en and it.
- `aura doctor` reports every tenant whose state is not `ready(S)`, by name.
- Memory tool responses carry their retrieval reason, as `SearchFactsHybrid` already does.
  `SearchReasoningTraces` gains one, because its fallback is currently silent
  (`memory_reasoning.go:331-343`).

## Fix on touch

- `internal/arcadedb/document_cards.go:113-120` says the card leg "outlives an embedding
  failure". It does not: `DocumentCardsScoped` requires a vector (`:156-158`).
- The `internal/agui/settings_api.go` header still says the page picks "the embed dimension";
  `c2a2c3fe8` fixed only the `settings.go` copy of that sentence.
- `SearchConversationTurnsHybrid` (`memory_conversation.go:287-371`) has no production
  caller. Delete it.
- `rankRecallKinds` (`memory_recall.go:279-291`) errors only when both the fact and the turn
  query fail. When one side fails, it returns nothing for that side under a "hybrid" path
  with no reason. It must report the degraded side.
- A Go test asserts `services/ingest/chunk.py`'s `EMBED_DOC_PREFIX` equals
  `embeddings.UntitledDocumentPrefix`. Today only a comment does (`chunk.py:52-53`).

## Files

Line counts measured at `e8ef129e4`; the cap is 600.

| file | today | change |
|---|---|---|
| `internal/config/config_embed_space.go` | new | `EmbedSpace` |
| `internal/embeddings/tasks.go` | 35 | `RecipeVersion` |
| `internal/embeddings/fit.go` | 162 | export the input-limit read |
| `internal/arcadedb/embedding_space.go` | new | marker DDL, read, CAS create, state |
| `internal/arcadedb/embedding_space_guard.go` | new | guard around `embedder`, cache, typed errors |
| `internal/arcadedb/memory_reembed.go` | new | clear + fill for the three Go types |
| `internal/arcadedb/memory_vector.go` | 532 | two reason constants only |
| `internal/arcadedb/memory_backfill.go` | 232 | pass driven by marker state |
| `internal/arcadedb/memory_conversation.go` | 493 | batched projection embed; dead search removed |
| `internal/arcadedb/memory_reasoning.go` | 510 | reason on fallback |
| `internal/arcadedb/memory_recall.go` | 581 | one-sided failure reported; stays under 600 or splits |
| `internal/arcadedb/document_lexical.go` | new | lexical card and passage statements |
| `internal/documents/retrieval.go` | 513 | lexical mode; new status and reasons |
| `internal/settings/embed_route.go` | new | route + credential + space from rows, shared |
| `cmd/arcadedb-mcp/boot_settings.go` | 100 | uses the shared helper |
| `cmd/arcadedb-mcp/space_watch.go` | new | re-resolve and drain-exit |
| `internal/ingestsupervisor/supervisor.go`, `process.go` | 273, 79 | route in `ProcessSpec`, env, fingerprint |
| `cmd/aura-ingest-supervisor/main.go` | 72 | settings store wiring |
| `internal/agui/settings_embedding_space.go` | new | the three endpoints |
| `internal/agui/settings_api.go` | 534 | refuse embed keys; header fixed |
| `services/ingest/embed.py` | new | embedding moved out of `app.py`, hosted route, `deps` |
| `services/ingest/app.py` | 617 | shrinks below 600 |
| `services/ingest/chunk.py` | 282 | limit from env; byte counting on hosted route |
| `services/ingest/arcade.py` | 421 | `embed_space` in `IngestStatus` |
| `compose.yaml` | — | `AURA_EMBED_FINGERPRINT`/`_REVISION` to `arcadedb-mcp` and `aura-ingest` |
| `web/src/settings/EmbeddingBackendControl.tsx` | 177 | preview/confirm instead of save |
| `web/src/settings/EmbeddingSpacePanel.tsx`, `embeddingSpaceApi.ts` | new | state, progress, re-embed |

ArcadeDB keeps all vector search, fusion and index work. This design adds no Go vector math.

## Testing and acceptance

- **Unit (Go):**
  - `EmbedSpace` across local, OpenRouter, manual endpoint, unpinned, and recipe bump;
  - the guard's truth table (§2) for writes and queries;
  - marker CAS with two concurrent creators;
  - the initializer's proof rule, including the legacy case;
  - the pass state machine across a crash after grace, after clear, and mid-fill;
  - the supervisor's fingerprint changes when, and only when, the route changes;
  - the refused keys on the generic `PUT`;
  - the preview arithmetic;
  - the prefix-parity test.
- **`arcadedb_integration`** (CI-fatal helpers, `internal/arcadedb/testclient_test.go:16-30`):
  - a matrix over {daemon, MCP} writers × the three Go types:
    - a stale writer writes no vector;
    - a target writer does;
    - the pass reaches `ready`;
    - dense retrieval returns afterwards;
  - document lexical mode returns non-empty cards and passages for a known query during a
    mismatch.
- **Python** (`make ingest-test`):
  - hosted payload shape: model, Bearer, `dimensions`, truncation;
  - width validation;
  - byte counting and chunk ceiling from the env limit;
  - `embed_space` recorded;
  - a `deps` change re-running an unchanged file, the spike above turned into a test.
- **vitest:** the save stays disabled until confirmation; preview rendering; an unknown price
  shows as "unknown"; all three panel states.
- **Gates:** 85% coverage per `scripts/coverage_gate.sh` and the delegated
  `arcadedb_integration` report; mutation ≥70% on `embedding_space_guard.go` and
  `memory_reembed.go` (in CI).
- **E2E on the VM `192.168.101.158`**, delivered through the updater, never by hand:
  1. After the upgrade the tenant reads `re-embed required`. `memory_recall` answers lexically
     with `embedding_space_mismatch`. A document question returns lexical results.
  2. Re-embed on the local route. The preview shows 0 / 17 / 5 / 10 / 48 records and about
     217k characters. The marker reaches `ready(local:…)`, and dense (hybrid) retrieval
     returns.
  3. Switch to an OpenRouter embedding model. The preview shows the cost. During the pass,
     reasons read `embedding_reembedding`. `IngestStatus.embed_space` becomes the cloud
     space. The marker reaches `ready(openrouter:…)`. A stored vector differs from its local
     counterpart. `aura-arcadedb-mcp` has restarted on its own.
  4. Switch back to local; same observations.

  Scored >9.8 per the definition of done.

## What this design does not prove

- **A mutable cloud alias.** If OpenRouter serves a different artifact under an unchanged model
  id, the space string does not move and the change is invisible. Nothing in the catalogue
  exposes an artifact hash to bind to.
- **The local fingerprint is trusted from the environment.** The updater converges `.env`
  with the compose pins, and `scripts/install_env.sh:141-160` derives the value once. Nothing
  re-hashes the mounted GGUF at run time.
- **The cloud duration estimate rests on one measurement:** one model, 32 chunks. It is
  shown to the operator as an estimate and labelled with its source.
- **Lexical-mode quality is not measured on this corpus.** Acceptance asserts non-empty,
  relevant results for known queries, not a recall figure.
- **The grace window assumes writers finish within `embeddings.DefaultTimeout`.** A write
  held longer than that across the clear could leave one old vector. The embed timeout
  bounds every request today.

## Out of scope

- Width changes and the side-by-side multi-property migration ArcadeDB documents for them.
- `quantization: NONE` → `INT8` (ArcadeDB recommends it only above 10K vectors).
- The zero vector stored for an empty card (`app.py:470`).
- The empty `aura_memory` database.
- `memory_reembed` / `aura memory reembed --all` (`cmd/arcadedb-mcp/tool_memory_maintenance.go`)
  stays a same-space repair for facts. It is not the model-change path.
