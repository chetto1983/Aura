# Changing the embedding model — design

**Date:** 2026-09-23
**Status:** revised the same day after an adversarial audit
(`2026-09-23-embedding-model-change-audit.md`, alongside). Its first version (`06deb3577`) used
one marker per database with a clear, a grace window and a writer guard; the audit showed
that a per-database marker cannot prove the space of individual rows, so this version stamps
every vector instead. Not implemented.
**Replaces:** the three 2026-09-21 documents (provenance/runtime-route design, its review, the
re-embedding design), deleted in `06deb3577`; `git show 06deb3577^:<path>` recovers them.

Written for the implementer and the planner. It assumes no knowledge of the conversations
that produced it. Every claim is a file, a line, or a measurement dated 2026-09-23 on the lab
VM `192.168.101.158` (images `ghcr.io/chetto1983/aura*:edge`, ArcadeDB 26.9.1, cocoindex
1.0.24) against repo HEAD `e8ef129e4`.

## The problem

Vectors from two embedding models do not share a space: the cosine distance between them is
meaningless. Nothing errors when they mix — retrieval still returns its k nearest
neighbours, every healthcheck stays green, and answers quietly get worse.

Since `2b7825cb4` the cockpit selects an embedding route in one click
(`web/src/settings/EmbeddingBackendControl.tsx`: Local / OpenRouter / Manual endpoint). The
save is an ordinary settings save marked "restart required"
(`internal/agui/settings_api.go:36-58`), and `POST /api/admin/restart` restarts only the
daemon (`internal/agui/restart_api.go:1-5`). After that click and a restart:

| writer | embeds with | why |
|---|---|---|
| `cmd/aura` (daemon) | the new route | `config.EmbedRoute()` reads the overlaid settings at boot |
| `cmd/arcadedb-mcp` | the old route until its container restarts | reads `aura.settings` once at boot (`cmd/arcadedb-mcp/boot_settings.go:26-79`) |
| `services/ingest` | always the local sidecar | base URL from compose env only (`services/ingest/app.py:37`, `compose.yaml:969`), model hard-coded `"embeddinggemma"` (`app.py:276`), no Authorization header (`app.py:280`) |

No row records which model produced its vector, so none of this is detectable.

**And the "OpenRouter" option does not reach OpenRouter.** With `AURA_EMBED_CLOUD_BASE_URL`
empty — which is exactly what that option writes (`web/src/settings/embeddingBackendState.ts:14-21`)
— `ResolveEmbedRoute` uses the **chat LLM's** base URL (`internal/config/config_routes.go:31-34`).
On the VM that is `AURA_LLM_BASE_URL=http://host.docker.internal:11434/v1`, an Ollama server,
so choosing OpenRouter sends embeddings to Ollama under an OpenRouter model id; and changing
the chat LLM silently re-routes embeddings. This is a live defect, fixed first (§0).

## Decisions taken by the operator

1. **Re-embed everything on a model change.** No selective or partial passes, no
   optimisation of a rare operation. The change is a one-time decision taken behind a warning
   that states its cost.
2. **Until every vector of a kind is in the new space, that kind is served lexically.** Dense
   retrieval never ranks a query vector against a corpus that is not entirely in its space.
3. **Embedding requests are batched.** Measured in `434cdd539`: 25.9x on a cloud route (32
   chunks, 9.95 s → 0.38 s, `perplexity/pplx-embed-v1-0.6b`), 1.0x on the local sidecar
   (8.61 s → 8.33 s; llama.cpp at `-np 1` works a batch's inputs in sequence). OpenRouter bills
   per token either way.
4. **Documents are re-embedded by CocoIndex** through `deps`, not by a Go re-embedder that
   would duplicate the Python recipe.
5. **Every stored vector carries the space that produced it** (2026-09-23, reversing the
   2026-09-21 "no per-row fingerprint"). That decision was about not optimising a rare
   operation; this one is about correctness: the audit measured that a failed file keeps its
   old rows (F1) and that stale writes can land after any time-based fence (F2), and only a
   per-row stamp makes either visible. Everything is still re-embedded: after a model change
   every row carries the old space.
6. **A stale memory sidecar restarts itself** when the route in `aura.settings` no longer
   matches the one it booted with.

## What was measured

### On the VM, 2026-09-23 (read-only)

- `aura.settings` embedding rows: only `AURA_EMBED_BASE_URL=http://aura-llama-embed:8081`.
  Chat route rows: `AURA_LLM_PROVIDER=ollama`, `AURA_LLM_BASE_URL=http://host.docker.internal:11434/v1`,
  `AURA_LLM_MODEL=gemma4:31b-cloud` (updated 2026-09-23 08:25).
- Sidecar: `-m embeddinggemma-300M-Q8_0.gguf --embeddings --embd-normalize 2 -ngl 0 -t 4 -np 1 -c 2048 -ub 2048`.
  Its `/v1/models` reports the model path, `size 327060480`, `n_params 307581696`, `n_embd 768`
  and the quantization (audit, [V]).
- Container env: `aura` has `AURA_EMBED_BASE_URL`, `_DIMENSIONS`, `_REVISION`, `_FINGERPRINT`;
  `aura-arcadedb-mcp` none (route from Postgres); `aura-ingest` only `_BASE_URL`, `_DIMENSIONS`.
- One tenant, `mem_448ddbe1_96ea_405d_8219_4a3d52a425c0`:

  | type | records | with vector | source field | source chars |
  |---|---|---|---|---|
  | `FACT` | 0 | 0 | `statement` | 0 |
  | `ConversationTurn` | 17 | 17 | `content` | 4,356 |
  | `ReasoningTrace` | 5 | 5 | `provider_summary` | 6,663 |
  | `IndexedDocument` | 10 | 10 | `card` | 6,086 |
  | `Passage` | 48 | 48 | `text` | 199,644 |

  ≈217k characters, ≈72k tokens at ingest's overshooting 3 chars/token, ≈3 minutes of
  embedding at the local sidecar's ~400 tok/s (280 tok → 584 ms, 2,016 tok → 5.2 s,
  2026-09-21). Embedding only: see §6 on extraction.
- `IngestStatus` read `status=ready, errors=14` while a zero-byte `prompt.txt` failed every
  cycle with `InputStream must have > 0 bytes`. The row stays `ready` across failures, and its
  error counter is cumulative per process.
- An empty second database, `aura_memory`, exists. Out of scope; noted so it is not taken for
  a tenant.

### CocoIndex `deps` — measured, twice

`cocoindex==1.0.24` (`docker/aura-ingest/requirements.txt:13`) accepts `deps` on `coco.fn` and
`coco.fn.as_async`; the public docstring is at `cocoindex/_internal/function.py` ~2060-2081:
*"folded into the function's logic fingerprint… the change propagates to callers according to
`logic_tracking` (transitively under `"full"`)… Snapshotted once at decoration time."*

A throwaway spike in the production `aura-ingest` container, reproducing
`mount_each(process_file, memo) → coco.map(process_chunk) → _embed(memo, batching, deps)`:

| run | `deps` | `process_file` bodies run | `_embed` calls |
|---|---|---|---|
| 1 | A | 2 of 2 | 4 batched calls for 7 inputs |
| 2 | A | 0 | 0 |
| 3 | B | 2 of 2 | 4 batched calls for 7 inputs |
| 4 | B | 0 | 0 |

The audit's spike added what matters for correctness ([V]):
- A→B→A re-runs again: memo entries are keyed by the fingerprint they were recorded under.
- When re-chunking changes the set of `passage_key`s, CocoIndex deletes the rows it no longer
  declares.
- **A file whose processing fails keeps its previous target rows.** `component.rs` (v1.0.24)
  returns the body's error before `submit`, and `coco.map` fails the whole file on its first
  failing item (`api.py:553-566`).
- **Batching spreads a failure.** `as_async(batching=True)` groups calls across files, and
  `_embed_batch` raises on any bad input (`app.py:281-286`), so one input fails every chunk it
  shares a request with. `coco.RetryWithSmallerBatch` (public, `api.py:913`, `batching.py`)
  exists to split the batch instead.

### OpenRouter's embedding catalogue

`GET https://openrouter.ai/api/v1/models?output_modalities=embeddings` (2026-09-23): 37 models,
each with `pricing.prompt` (USD/token) and `context_length`, from **512**
(`liquid/lfm-2.5-embedding-350m:free`) to **131,072** (`nvidia/llama-nemotron-embed-vl-1b-v2:free`).
No entry declares an output dimension and every `supported_parameters` is empty. Some ids are
served by several providers (`qwen/qwen3-embedding-8b`: Nebius, DeepInfra, SiliconFlow fp8).

### Code facts the design rests on

- Writers by type:
  - `FACT` — daemon and MCP;
  - `ConversationTurn`, `ReasoningTrace` — daemon only;
  - `IndexedDocument`, `Passage` — Python only (`services/ingest/app.py:376,470`).
- Go writes vectors through the tenant client's `embedder` (`internal/arcadedb/client.go:187-189`).
  Two paths hand over vectors that were **not** just embedded:
  - the copy-by-statement lookup (`memory_batch_store.go:35-74`, written at
    `memory_batch_state.go:117-118`);
  - `writeVectors`, which updates rows by `@rid` (`memory_vector.go:456-500`).
- Batch writes embed before taking the per-identity lock and retry on conflict
  (`memory_batch.go:334-357`, `write_retry.go`). A vector can therefore be written long after
  it was computed.
- `FACT` has a null-vector fill (`EmbedMissingFacts`, `memory_vector.go:357-394`) run by the
  `memory_embed_backfill` sweep:
  - every 5 min, each run capped at 5 min, 20 rounds × 32 per tenant
    (`cmd/aura/serve_memory_backfill.go:51-78`, `memory_backfill.go:31-38`,
    `internal/cron/handlers/memory_embed_backfill.go:18`);
  - one bad input fails the whole `Embed` call (`internal/embeddings/client.go:100-103`), and
    the fill re-selects the same set each round.
  - Turns without a vector are re-embedded one by one by the one-minute conversation reconciler
    (`internal/runner/runner_memory_projection.go:259-302`; `memory_conversation.go:167-170`).
  - Traces have no fill.
- Dense relevance floors were measured on EmbeddingGemma only:
  - memory: `DenseMaxDistance 0.72`, `MinRelevance 0.28`, from a 102-fact memory on 2026-09-02
    (`internal/arcadedb/client.go:64-77`);
  - documents: `RelevanceFloor 0.32` (`internal/arcadedb/document_schema.go:55-79`).
- Document retrieval has no lexical-only path by a measured decision
  (`internal/arcadedb/document_retrieval.go:20-24`: Go-side fusion 0.300 vs engine 0.850
  recall@1). A failed query embedding returns zero documents (`internal/documents/retrieval.go:296-305`).
  Full-text indexes exist on `Passage[text]`, `IndexedDocument[card]` and
  `IndexedDocument[file_name_words]` (`services/ingest/arcade.py:274,315,323`).
- `settings.OverlayEnv` only ever calls `Setenv`, never unsets (`internal/settings/settings.go:360-373`).
- The ingest supervisor restarts a child when its `ProcessSpec` fingerprint changes, checked
  every 15 s. The fingerprint already includes a secret. The supervisor reads Postgres but not
  `aura.settings` (`internal/ingestsupervisor/supervisor.go:57-97,135-224`,
  `cmd/aura-ingest-supervisor/main.go:32-57`).
- `media.derive` is not memoized (`services/ingest/media.py:64-72`), and `_card` memoizes on a
  temporary path (`app.py:147,422`). Any re-run of `process_file` therefore re-extracts,
  including billed vision and speech-to-text calls.
- Go's and Python's document prefixes are equal (`internal/embeddings/tasks.go:11`,
  `services/ingest/chunk.py:54`); only a comment asserts it.

## Design

### §0. The OpenRouter option reaches OpenRouter (live defect, lands first)

`ResolveEmbedRoute` resolves a set model with an empty cloud base to OpenRouter's own base
(`llm.DefaultBaseURL`, `internal/llm/config.go:29`, without its `/v1`), never to the chat LLM's.
A regression test pins it: an Ollama chat base plus an embedding model resolves to OpenRouter.
This lands as its own commit before anything below, because it is wrong in production now.

### §1. The space identity

One Go function, `config.EmbedSpace`, names the vector space a route produces. Every Go
process computes it from its own resolved route. Python receives it from its supervisor and
never derives it.

- **Canonical form.** A JSON object with fixed key order:
  `{"v":1,"recipe":<embeddings.RecipeVersion>,"dims":<n>,"route":"local|openrouter|endpoint","model":…,"base":…,"artifact":…}`.
  - `base` is set only for a manual endpoint, where the base URL is what selects the model.
    OpenRouter's host never enters the identity.
  - The stored stamp is `es1-` plus the first 16 hex digits of its SHA-256; the cockpit shows
    the readable form. Hashing removes the colon ambiguity of model ids like `:free` (audit F14).
- **`recipe`** is a constant beside the prefixes in `internal/embeddings/tasks.go`, incremented
  whenever something turns the same text into a different stored vector without changing a
  model name: the query and document prefixes (Go and Python), `--embd-normalize`,
  `TruncateMRL`.
- **The local artifact is attested at run time.** It is the sidecar's own `/v1/models` answer:
  model file name, `size`, `n_params`, `n_embd`, quantization. A GGUF swapped on disk changes
  the space even when `.env` is stale, which the env fingerprint could not catch (audit F15).
  `AURA_EMBED_FINGERPRINT` stays the strict-profile install gate; it no longer names a space.
- A new Go test asserts `services/ingest/chunk.py`'s `EMBED_DOC_PREFIX` equals
  `embeddings.UntitledDocumentPrefix`.
- **Each family names its own width.** Memory vectors are pinned at 768
  (`arcadedb vectorDimensions`); documents use `AURA_EMBED_DIMENSIONS`. Outside the pinned
  Compose deployment the two can differ, so each family's stamp is computed with its own
  width, never a shared one (review of plan 1).

### §2. Every vector is stamped

- The five types gain `embed_space STRING` with a NOTUNIQUE index.
  The index is `NOTUNIQUE NULL_STRATEGY INDEX`: with ArcadeDB's default, `SKIP`, "queries
  against null values that use an index return no entries" (arcadedb-docs
  `reference/sql/sql-indexes.adoc`), so every unstamped row would be invisible to the gate.
  Measured 2026-09-24 on a local 26.9.1 (`TestMemorySpaceStampsFactsAndFindsTheUnstampedThroughTheIndex`):
  with the index declared this way, `embed_space IS NULL` counts the unstamped fact.
- A writer reads its space **before** the request that produces the vector. A model swapped
  during the request can then only make a vector claim the older space, which the pass
  re-embeds, never the newer one.
- Every write that sets `embedding` sets `embed_space` in the same statement, to the writer's
  own space; every path that removes `embedding` removes `embed_space` too, except the §5
  quarantine, which leaves the stamp to mean "this space refused this record".
  - Go: `createFactEmbeddingClause`, `createFact`, `writeVectors`,
    `ApplyConversationProjection`, `UpsertReasoningTrace`.
  - Python: the `Passage` and `IndexedDocument` rows it declares.
- The copy-by-statement lookup copies only vectors whose `embed_space` equals the writer's
  space, together with their stamp.

A writer never refuses to write: a vector stamped with the space that produced it is always
true information, whichever route the writer is on. There is no marker, no clear, no grace
window, and nothing to fence.

### §3. The dense gate

Retrieval is split into two families: **memory** (`FACT`, `ConversationTurn`,
`ReasoningTrace`) and **documents** (`Passage`, `IndexedDocument`).

- A process in space `S` serves a family densely for a tenant only when no row in that family
  has a vector with `embed_space` ≠ `S`. A missing stamp counts as different.
- The check is one indexed count per type, cached per tenant and family for 30 s.
- Otherwise the family is served lexically with the reason `embedding_space_mismatch`.
  - Memory: through the soft paths that already exist (`memory_vector.go:209-221,245-254`;
    `memory_recall.go:248-251`; `memory_reasoning.go:331-343`, which gains a reason).
  - Documents: through §8.

The two families are gated separately, so a document that will not re-index never turns off
dense memory. Rows without a vector are not counted. A stale writer's vectors carry its old
stamp, so they turn the gate off until the pass has re-embedded them: the result is visible,
never silent.

### §4. Changing the route from the cockpit

- **Protected keys.** The generic settings `PUT`/`DELETE` refuse `AURA_EMBED_MODEL`,
  `AURA_EMBED_BASE_URL` and `AURA_EMBED_CLOUD_BASE_URL` with 409 and point to the endpoints
  below. After §0 the chat route no longer reaches embeddings. The new endpoints use the
  capabilities the route keys already require (`internal/agui/settings_api_authz.go`).
- **`GET /api/settings/embedding-space`** — per tenant and family:
  - rows in the current space, in another space, and without a vector;
  - the documents still in another space, by file name, with their last ingest error.
- **`POST /api/settings/embedding-route/preview`** — body: the three route values. It returns:
  - the target space and its readable form;
  - **a live probe** — a fixed synthetic batch, never corpus text, since nothing has been
    confirmed yet, sent through the target route:
    - output width: narrower than `AURA_EMBED_DIMENSIONS` is refused; wider is allowed with a
      warning that truncation is valid only for Matryoshka-trained models;
    - measured throughput, used for the duration estimate;
  - per type, the rows and characters whose stamp differs from the target, and the estimated
    tokens (characters ÷ `chunk.CHARS_PER_TOKEN_FALLBACK`, an overshoot by design);
  - cost as tokens × `pricing.prompt`, or "unknown" when the catalogue entry has no price;
  - the model's published input limit — models below 2,048 are refused — and on a hosted route
    how many stored passages exceed it in bytes and would be cut (§6);
  - the statement that each family is served lexically until its pass completes, and that dense
    relevance floors are uncalibrated for any model but EmbeddingGemma (§9).
- **`POST /api/settings/embedding-route`** — body: the route plus `confirm_space`, which must
  equal the target the server recomputes. It writes the three settings rows and fires the
  existing restart trigger (`cmd/aura/serve_restart.go:17`). Nothing else: the pass starts
  because rows now carry another space.

### §5. The pass for the three Go types

The `memory_embed_backfill` sweep generalises from "facts without a vector" to two selections
over the daemon's space `S`:

- **stale:** rows of all three Go types whose vector is stamped with a space other than `S`,
  or carries no stamp;
- **missing:** `FACT` and `ReasoningTrace` rows with source text, no vector, and a stamp other
  than `S`. A stamp of `S` without a vector means `S` already refused the record.

- Each type is selected by `@rid` cursor, not by re-selecting the same `LIMIT` set.
- Batches go through `Embed` 32 texts at a time, within the client's token budget
  (`internal/embeddings/fit.go:20-25`).
- Each write sets the vector and the stamp.
- The 20-round cap applies only when no stamp mismatch remains. While mismatches remain, a run
  continues until its 5-minute budget, and the sweep is also kicked once at daemon boot.
- Tenant order rotates between runs, so no tenant starves.
- **Failures.** A failing batch is halved down to single records:
  - a single record the provider rejects as input (400, 413, 422) loses its vector but is
    stamped `S` (quarantined): it is lexical-only, excluded from both selections and from the
    gate count, and counted and shown. A later route change re-selects it, because its stamp is
    no longer the current space;
  - a network error, a 5xx, or a 401, 403 or 429 ends the run, and the next run retries. Those
    three say nothing about the record: quarantining on them would stamp a whole space refused
    after one revoked key or one rate limit (review of plan 1).
- **No credential, no pass.** A cloud route with no key embeds nothing: the pass does not run,
  the family stays lexical, and `aura doctor` and the cockpit name the missing key.
- **The key is read live.** The daemon's embedders take the credential from the running LLM
  profile, not a boot copy, so a key rotated in the cockpit reaches them without a restart.
- **Turns.** The conversation reconciler keeps filling turns that have no vector, now as one
  batched call per projection instead of one request per turn. The pass touches turns only
  through the stale selection, so no turn is embedded twice.

Interruption needs no protocol: the next run selects what is still mismatched. A second route
change in mid-pass (A→B→C) needs none either: B-stamped rows differ from C and are re-embedded
(audit F4).

### §6. Ingest

**Supervisor.** On every reconcile tick it resolves the embedding route from `aura.settings`
through a pure helper in `internal/settings`. The helper reads the rows, distinguishes an
absent row from an empty one, and never touches the environment (audit F10). `arcadedb-mcp`
uses the same helper. Its credential is the daemon's: the sealed `OPENROUTER_API_KEY` when
set, else the environment's.

Every re-read after boot passes the helper a lookup over the environment **as it was before
`OverlayEnv`**. Both processes overlay rows into their own environment at boot and never
unset, so a live `os.LookupEnv` would bring a deleted row's boot value back as the fallback
(review of plan 1).

- `ProcessSpec`, `Environment()` and `fingerprint()` gain `AURA_EMBED_BASE_URL`,
  `AURA_EMBED_MODEL`, `AURA_EMBED_API_KEY` (the sealed `OPENROUTER_API_KEY`: no embed-specific
  key exists, `bd31e157b`), `AURA_EMBED_SPACE`, `AURA_EMBED_INPUT_LIMIT`, and
  `AURA_EMBED_TOKENIZER_URL`, which is always the local sidecar.
- A route change restarts every child within one poll.
- The input limit and the local attestation are read once per route change, never per tick.
- A failed settings read keeps the running children and their specs. At startup it starts
  nothing, the fail-closed behaviour `arcadedb-mcp` already has.

**Python child.** Embedding moves out of `app.py` (617 lines, over the cap) into
`services/ingest/embed.py`, which:

- decorates `_embed` with `deps=AURA_EMBED_SPACE`, required;
- raises `coco.RetryWithSmallerBatch() from err` on a failed request, so one bad input fails
  only its own caller (audit F1);
- on a hosted route sends the model, `Authorization: Bearer` and `dimensions`, truncates and
  renormalises a wider vector as `TruncateMRL` does (`internal/embeddings/client.go:230-256`),
  and cuts each input to the published limit in UTF-8 bytes, the Go client's rule
  (`fit.go:88-99`);
- validates every returned width, which is never checked today (`app.py:287-302`);
- reads `AURA_EMBED_API_KEY` once and removes it from `os.environ`, so Tika, LibreOffice,
  `aura-filecard` and `aura-media-index` do not inherit it.

`chunk.py` counts tokens against `AURA_EMBED_TOKENIZER_URL`, so chunk boundaries do not move
with the embedding model and a model change never re-chunks (audit F9).

**Extraction is memoized by content.** `process_file` splits:
- `_extract(content, file_name, content_type)`, memoized, returns text, card and anchors;
- the rest chunks and embeds.

`_extract` does not call `_embed`, so the `deps` change on `_embed` re-runs chunking and
embedding only, not vision or speech-to-text (audit F7). `media.CONFIG_FINGERPRINT` still
re-extracts when the vision or STT route changes. The deploy that ships this re-extracts and
re-embeds every document once. `deps` and the new functions are new fingerprints, so this is
unavoidable; the release note says so.

**A document that keeps failing keeps its old rows.** That is CocoIndex's contract, measured
above. Those rows carry the old stamp, so the documents family stays lexical and the cockpit
names the file and its error. The operator fixes or removes the file. Nothing drops vectors
silently to force the gate open.

**Build vs reuse, recorded.** The installed cocoindex ships `cocoindex.ops.litellm.LiteLLMEmbedder`:
429/5xx backoff, `RetryWithSmallerBatch`, `api_base`/`api_key`/`dimensions`. It needs the
`litellm` package, which the image does not carry (`docker/aura-ingest/requirements.txt`).
This design extends the existing `_embed_batch` instead, because that path's batching and
input fitting are already measured. Adopting LiteLLM is the alternative if 429 handling proves
insufficient.

### §7. `arcadedb-mcp`

Every 60 s MCP re-resolves the route from `aura.settings` through the §6 helper. When the
resulting space or credential differs from its boot one (the credential compared by hash,
never logged), MCP cancels its root context and exits through the existing graceful shutdown
(`cmd/arcadedb-mcp/main.go:139-160`, 10 s budget: exit 0, or 1 when the budget runs out).
`restart: unless-stopped` (`compose.yaml:728`) boots it on the new route. It cannot loop: it
exits only when the settings resolve to a space or credential other than the one it is running.

Until it exits, its writes carry its old stamp and its reads find the gate closed, so the
window is visible and bounded, never wrong.

### §8. Documents in lexical mode

`HostRetriever.Retrieve` serves documents lexically when the documents gate is closed, and
when the query embedding fails; today the latter returns zero documents (`retrieval.go:301-305`).

- Passages come from `SEARCH_INDEX('Passage[text]', :q)`, ranked by its score.
- Cards come from two separate queries, `IndexedDocument[card]` and
  `IndexedDocument[file_name_words]`, merged by the higher score. One `OR` query is not used:
  its `$score` depends on predicate order (measured: 1.3798 vs 1.2880 for the same document,
  audit F8).
- Results are grouped by `raw_sha256`, so identical files count once. Measured: five identical
  transcripts took the top five places for an unrelated question.
- A document ranks by the best score among its card and its passages, and a floor drops weak
  matches: the documents twin of memory's `LexicalMinScore` (`client.go:77`), calibrated in
  acceptance.
- The status is `RetrievalLexicalOnly`, with the reason `embedding_space_mismatch` or
  `query_embedding_unavailable`.

This deliberately amends `document_retrieval.go:20-24`. That measurement compared two fusions.
It says nothing about a single-leg lexical answer while the dense leg is unavailable. Lexical
results are candidates, and the response says so.

### §9. Relevance floors

The dense floors are measured values for EmbeddingGemma alone. They become a table keyed by
the attested model, holding one entry today. A space with no entry keeps the current values,
and every dense response carries the reason `uncalibrated_floors`. The cockpit and the preview
state the same. Calibrating a new model is the existing measurement procedure (the
2026-09-02 calibration behind `client.go:66-77`) adding a row. The E2E below records the first
datum for the cloud model it uses.

### §10. Visibility

- **Cockpit, embedding card**, per tenant and family:
  - rows in the current space, in another space, and without a vector;
  - the documents stuck in another space, with their error;
  - records rejected by the model.
  - The route control opens the preview; saving is disabled until the operator confirms.
    Strings in en and it.
- **`aura doctor`** names every tenant and family whose gate is closed.
- **Memory tools** carry their retrieval reason. `SearchReasoningTraces` and its tool
  (`cmd/arcadedb-mcp/tool_memory_recall.go:255`) gain one.

## Fix on touch

- `internal/arcadedb/document_cards.go:113-120` claims the card leg outlives an embedding
  failure. It does not (`:156-158`).
- The `internal/agui/settings_api.go` header says the page picks "the embed dimension".
- `SearchConversationTurnsHybrid` (`memory_conversation.go:287-371`) has no production caller.
  Delete it.
- `rankRecallKinds` (`memory_recall.go:279-291`) hides a one-sided failure under "hybrid".
  Report the degraded side.
- `IngestStatus` counts errors per process, cumulatively, and stays `ready` across failures.
  Nothing in this design relies on it; the cockpit reads the per-file stamps instead.

## Files

Current line counts at `e8ef129e4`; the cap is 600.

| file | today | change |
|---|---|---|
| `internal/config/config_routes.go` | 45 | §0 OpenRouter base |
| `internal/config/config_embed_space.go` | new | `EmbedSpace`, canonical form, hash |
| `internal/embeddings/tasks.go` | 35 | `RecipeVersion` |
| `internal/embeddings/fit.go` | 162 | export the input-limit read |
| `internal/embeddings/attest.go` | new | local `/v1/models` attestation |
| `internal/arcadedb/embedding_space.go` | new | stamp DDL, gate counts, cache |
| `internal/arcadedb/memory_vector.go` | 532 | stamp in writes; reason constant |
| `internal/arcadedb/memory_backfill.go` | 232 | mismatch pass, cursor, rotation, halving |
| `internal/arcadedb/memory_batch_store.go` | 416 | copy only same-space vectors |
| `internal/arcadedb/memory_batch_state.go` | 384 | stamp on `createFact` |
| `internal/arcadedb/memory_conversation.go` | 493 | stamp; batched projection embed; dead search removed |
| `internal/arcadedb/memory_reasoning.go` | 510 | stamp; reason on fallback |
| `internal/arcadedb/memory_recall.go` | 581 | gate; one-sided failure; split if it crosses 600 |
| `internal/arcadedb/document_lexical.go` | new | lexical card/passage statements |
| `internal/arcadedb/document_retrieval.go` | 517 | gate query for documents |
| `internal/documents/retrieval.go` | 513 | lexical mode wiring only |
| `internal/documents/retrieval_lexical.go` | new | grouping, merge, floor, status |
| `internal/documents/retrieval_rank.go` | 342 | lexical ranking entry |
| `internal/arcadedb/client.go` | 420 | floors keyed by model |
| `internal/settings/embed_route.go` | new | route + credential + space from rows, no env |
| `cmd/arcadedb-mcp/boot_settings.go` | 100 | uses the helper |
| `cmd/arcadedb-mcp/space_watch.go` | new | 60 s re-resolve, drain-exit |
| `cmd/arcadedb-mcp/main.go` | 353 | wire the watcher |
| `cmd/arcadedb-mcp/tool_memory_recall.go` | 557 | trace retrieval reason |
| `internal/ingestsupervisor/supervisor.go`, `process.go` | 273, 79 | route in `ProcessSpec`, env, fingerprint |
| `cmd/aura-ingest-supervisor/main.go` | 72 | settings store wiring |
| `internal/agui/settings_embedding_space.go` | new | the three endpoints |
| `internal/agui/settings_api.go` | 534 | refuse embed keys; header |
| `cmd/aura/doctor.go`, `doctor_embed_probe.go` | 184, 126 | family gate report |
| `services/ingest/embed.py` | new | embedding out of `app.py` |
| `services/ingest/app.py` | 617 | `_extract` split; below 600 |
| `services/ingest/chunk.py` | 282 | tokenizer URL |
| `services/ingest/arcade.py` | 421 | `embed_space` DDL and index |
| `compose.yaml` | — | nothing new for identity; ingest env comes from the supervisor |
| `web/src/settings/EmbeddingBackendControl.tsx` | 177 | preview/confirm instead of save |
| `web/src/settings/EmbeddingSpacePanel.tsx`, `embeddingSpaceApi.ts` | new | per-family state |
| `web/src/i18n/*` | — | en + it strings |

`internal/arcadedb/memory.go` (597) is **not** touched: the stamp DDL goes beside each type's
vector DDL, not into `EnsureMemorySchema`'s base statements. ArcadeDB keeps all vector search,
fusion and index work; nothing here adds Go vector math.

## Testing and acceptance

- **Unit (Go):**
  - `EmbedSpace`: canonical form and hash; local, OpenRouter, endpoint; recipe bump; ids with
    `:free`/`:batch`;
  - §0: an Ollama chat base resolves embeddings to OpenRouter;
  - attestation parsing;
  - gate counts and cache;
  - the pass: cursor, halving, 4xx vs 5xx, rotation, A→B→C;
  - copy-by-statement filtered by stamp;
  - route helper: absent vs empty row;
  - MCP watcher: exits on a change and only then;
  - supervisor: the fingerprint moves with the route and only with it;
  - the refused keys;
  - preview arithmetic and refusal rules;
  - prefix parity.
- **`arcadedb_integration`** (CI-fatal helpers, `internal/arcadedb/testclient_test.go:16-30`):
  - {daemon, MCP} × the three Go types:
    - a stale-route writer stamps its old space and closes the gate;
    - the pass re-embeds its rows;
    - the gate opens and dense retrieval returns;
  - a failing record is quarantined without blocking the rest;
  - documents: with a row in another space, lexical mode returns grouped, floored results for
    known IT and EN queries, and abstains on an out-of-corpus question.
- **Python** (`make ingest-test`):
  - hosted payload: model, Bearer, `dimensions`, truncation, byte cut;
  - width validation;
  - key removed from the environment;
  - `embed_space` declared;
  - `RetryWithSmallerBatch`: one bad input fails only its file;
  - the `deps` spike as a test: a change re-runs, a repeat does not, and a change back re-runs;
  - `_extract` memo survives a `deps` change;
  - a failing file keeps its old stamped rows.
- **vitest:** save disabled until confirmation; preview refusal states; unknown price; the
  per-family panel with stuck documents.
- **Gates:** 85% coverage per `scripts/coverage_gate.sh` and the delegated
  `arcadedb_integration` report; mutation ≥70% on `embedding_space.go` and the pass (CI).
- **E2E on the VM `192.168.101.158`**, delivered through the updater, never by hand. Scored
  >9.8 per the definition of done.
  1. After the upgrade, every existing row is unstamped, so both families are lexical with
     `embedding_space_mismatch`. The pass and CocoIndex re-embed on the local route unattended,
     and both gates open.
  2. Choose an OpenRouter embedding model:
     - the preview shows width, throughput, cost and limit;
     - confirming makes both families lexical;
     - requests go to `openrouter.ai`, not Ollama;
     - stamps converge to the cloud space and both gates open;
     - `aura-arcadedb-mcp` has restarted on its own;
     - an out-of-corpus question is checked for abstention and recorded as the model's first
       calibration datum.
  3. Break one document on purpose (an unreachable vision route for an image). The documents
     gate stays closed, the cockpit names the file, and memory stays dense.
  4. Switch back to local: same observations.

## What this design does not prove

- **A mutable cloud alias.** A provider change behind an unchanged OpenRouter id does not move
  the space. For `qwen/qwen3-embedding-8b`, several providers, one of them fp8, serve one id.
  Provider pinning is out of scope; the stamp names the model, not the provider.
- **Relevance floors are uncalibrated for any model but EmbeddingGemma.** Dense retrieval
  after a switch may abstain too often or too little until a calibration row exists; every
  response says so.
- **Local attestation is metadata, not a hash:** file name, size, parameter count, width and
  quantization. Two different GGUFs identical in all five would share a space.
- **Lexical-mode quality is not measured as recall.** Acceptance asserts grouped, floored,
  relevant results for known queries and abstention for one unanswerable question.
- **A document that never re-indexes keeps the documents family lexical indefinitely.** This is
  deliberate, and visible by file name.

## Out of scope

- Width changes and the side-by-side multi-property migration ArcadeDB documents for them.
- `quantization: NONE` → `INT8`.
- The zero vector stored for an empty card (`app.py:470`).
- The empty `aura_memory` database.
- `memory_reembed` / `aura memory reembed --all` remains a same-space repair for facts.
