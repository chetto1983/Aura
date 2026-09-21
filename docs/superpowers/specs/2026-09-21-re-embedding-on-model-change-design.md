# Re-embedding When the Embedding Model Changes — Design

**Date:** 2026-09-21
**Status:** design, approved in outline; not implemented

## The problem

Selecting a cloud embedding model became possible in `2b7825cb4`, and the cockpit will soon
offer Locale / OpenRouter / Endpoint manuale as one control. The moment an operator uses it,
every vector already stored was produced by a *different* model.

Vectors from two models do not share a space. Cosine distance between them is not small, or
large — it is meaningless. Nothing errors: retrieval keeps returning its `k` nearest
neighbours, the healthchecks stay green, and the answers quietly get worse. This is the same
failure shape as the misroute `2b7825cb4` fixed, one layer up.

So: when the embedding model changes, the corpus must be re-embedded, and the operator must
be warned before it starts.

## Decisions taken by the operator (2026-09-21)

1. **Scope: documents (Postgres) AND memory (ArcadeDB).** Both hold vectors; both go stale.
2. **Trigger: the operator confirms a warning.** Changing the model does not silently start
   spending compute or money.
3. **During the transition, search keeps working over everything**, with the operator told
   it is in progress.

Decision 3 was taken with its cost stated: a search that fuses two vector spaces returns
noise without raising an error. It is recorded here as a decision, not as a property of the
design, and the mitigation below exists because of it.

## What already exists — measured, do not rebuild

**Provenance is already stored.** `document_embeddings` carries `embedding_model`,
`embedding_version`, `embedding_dim` and a composed `embedding_fingerprint`
(`concat_ws(':', model, version, dim)`), since migration `0093`. Detecting stale rows is a
query, not a schema change.

**The config already has a fingerprint.** `config.EmbedConfig` has `Revision` and
`Fingerprint`, used today in exactly one place — a strict-profile validation at
`internal/config/config_document_retrieval.go:62`. Nothing consumes it as a change signal.

**CocoIndex already re-processes incrementally, and has the exact hook for this.** The
installed package's `coco.fn` / `coco.fn.as_async` takes:

- `version: int | None` — "the version is used as the logic fingerprint instead of the AST",
  an **int**, so a hex fingerprint cannot be passed here;
- `deps: Any` — "Additional value(s) the function logic depends on but that aren't visible
  in its body … the value is canonicalized through the memoization key pipeline and folded
  into the function's logic fingerprint."

`deps` describes this problem exactly. `services/ingest/app.py`'s `_embed` is
`@coco.fn.as_async(memo=True, batching=True, max_batch_size=32)`; the model it uses arrives
from the environment, so it is invisible to the AST and the memo does **not** invalidate
when the model changes. Folding the active route's fingerprint into `deps` makes it
invalidate, and CocoIndex then re-embeds the affected chunks incrementally, on its own.

**Consequence: the document half needs no re-embedding job.** It needs the fingerprint
wired into `deps` and a way to start a pass. Building a bespoke re-embedder for documents
would be re-implementing the engine we already depend on.

## Design

### 1. One fingerprint that names the route, not the file

Today `AURA_EMBED_FINGERPRINT` is a SHA-256 of the local GGUF (measured on the deployment:
`b5ce9d77…`). A cloud model has no file, so the fingerprint must be derived from whatever
`EmbedRoute()` resolves: base URL, model id and dimensions. One function, in
`internal/config`, returning a stable string for the active route. The local case keeps
hashing the artifact; the cloud case hashes the route triple.

This value is what `services/ingest` passes to `deps`, and what new rows record in
`embedding_fingerprint`.

### 2. Documents: let CocoIndex do it

`_embed` gains `deps=<active fingerprint>`. On a model change the memo misses, the pipeline
re-embeds, and `document_embeddings` converges. The existing batching, token budgeting and
oversize-head handling are untouched.

Cost is known, not guessed: measured 2026-09-21, the local sidecar runs at ~400 tok/s
(280 tok → 584 ms; 2016 tok → 5.2 s), and a 62-page PDF took ~80 s for 29 chunks. The same
32 texts on `perplexity/pplx-embed-v1-0.6b` took 0.38 s against 9.95 s sequential — 25.9×,
at identical token cost. So a full re-embed is minutes-to-hours locally and minutes on a
cloud route, and the warning must say which.

### 3. Memory: a re-embed pass, because there is no engine to delegate to

ArcadeDB holds one database per identity with a native vector index, and nothing there
tracks lineage. This half is real work:

- read facts whose stored fingerprint differs from the active one,
- re-embed them through the same `internal/embeddings` client the daemon uses,
- write the vector back, bitemporally consistent with the fact's existing validity.

It is bounded by `AURA_MEMORY_*` limits already in `aura.settings`, and it must be
restartable: a pass interrupted halfway leaves a mixed corpus, which is the state decision 3
already accepts.

**Measured 2026-09-21: they do not.** The `Fact` edge carries an `embedding` property
(`internal/arcadedb/memory.go:301`), but the schema it declares is `statement`, `predicate`,
`valid_from`, `valid_to`, `created_at`, `expired_at`, `fact_key`, `sources`
(`memory.go:39-52`) — no model, no fingerprint, no dimension. So "which facts are stale" is
today an **unanswerable question**, and a schema addition recording the fingerprint at write
time must land before the memory half can begin.

That ordering has a consequence worth stating: facts written before that change carry no
fingerprint, so they are indistinguishable from current ones. The pass must treat an absent
fingerprint as stale — the only safe reading, and one that re-embeds the whole existing
corpus exactly once.

### 4. The warning

Shown when the operator changes the embedding model, before anything is written. It states:
how many vectors are stale (documents and memory, counted separately), that retrieval
quality degrades until the pass completes, and — when the target is a cloud route — that the
pass bills tokens. Confirming starts the pass; declining leaves the model unchanged.

### 5. The mitigation decision 3 requires

Because search keeps running over a mixed corpus, every result carries the fingerprint it
was embedded with, and the UI shows that a pass is in progress. This does not make the
scores comparable — nothing can — but it makes the mixture **visible** rather than silent,
which is the difference between a degraded answer and an unexplained one.

## What this design does NOT do

- It does not make cross-model scores comparable. Nothing does.
- It does not re-embed automatically on a model change: the operator confirms first.
- It does not address dimension changes beyond what `TruncateMRL` already enforces (exact
  passes, narrower errors, wider truncates). A narrower model is an error, not a re-embed.
- It does not touch STT/TTS/image/video, which hold no vectors.

## Open questions to close before implementation

1. ~~Do ArcadeDB memory facts record the embedding model?~~ **Closed 2026-09-21: no.** The
   schema addition is a prerequisite, and an absent fingerprint counts as stale.
2. Where does the pass run — the daemon, `services/ingest`, or the memory sidecar? The
   sidecar owns the ArcadeDB credentials and now reads `aura.settings` at boot, which makes
   it the candidate, but it has no job runner today.
3. How is progress reported to the cockpit? There is an existing `/healthz` seam and a
   scheduler; neither is obviously right for a long pass.
