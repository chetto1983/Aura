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

1. **Scope: every durable vector store.** Not only documents and memory facts -- also
   conversation turns, reasoning traces and the ETL passage cards. All five are enumerated
   in the design; all of them go stale together.
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

### 3. Every vector store, enumerated

"Memory" was too small a word for this. Enumerated on 2026-09-21, the durable vector
stores are **five**:

| store | where | owner |
|---|---|---|
| `aura.document_embeddings` | Postgres, one fingerprint per row | CocoIndex (§2) |
| `Fact.embedding` | ArcadeDB edge | memory writes |
| `ConversationTurn.embedding` | ArcadeDB vertex, `LSM_VECTOR` index | conversation capture |
| `ReasoningTrace.embedding` | ArcadeDB vertex, `LSM_VECTOR` index | reasoning capture |
| `Passage.embedding` | ArcadeDB vertex, the ETL cards | document ingest |

Two more places embed but store nothing durable, and therefore need no pass:

- the reasoning-tier classifier's anchors are rebuilt per process behind a `singleflight`
  and its own comment calls the unconditional publish "the whole invalidation story", so
  they already follow whatever model is current;
- `internal/semindex` holds its index in memory and states that a failed add is not
  persisted, so a restart re-derives it.

### 4. The ArcadeDB side: re-embed all of it, and do not track what went stale

None of those four ArcadeDB types records which model produced its vector. The `Fact` edge
carries `embedding` (`internal/arcadedb/memory.go:301`) but its schema declares only
`statement`, `predicate`, `valid_from`, `valid_to`, `created_at`, `expired_at`, `fact_key`,
`sources` (`memory.go:39-52`); the other three are the same shape. The obvious reading is
that a per-row fingerprint must be added so a pass can select what went stale.

**Do not add it.** Changing the embedding model is a rare, deliberate act — that is the
whole reason it is behind a confirmed warning rather than a save button. Optimising a rare
operation buys nothing and costs a schema change on four types, a migration ordering
constraint, selective queries, and partial-pass states to reason about forever after. The
warning already makes the cost explicit and the operator already agreed to pay it.

So: **on a model change, re-embed everything in ArcadeDB.** One scalar decides it — the
fingerprint the corpus was last embedded with, stored once for the database, not once per
row. Compare it to the active route's fingerprint; if they differ, walk each type, re-embed
its text through the same `internal/embeddings` client the daemon uses, write the vector
back, and store the new fingerprint only when the whole pass completes. Writing it at the
end is what makes an interrupted pass simply run again — the cheapest correct recovery, and
no resume state to maintain.

No ArcadeDB schema change, no migration, no prerequisite. The whole ArcadeDB half is a walk
and a comparison.

Sizing is a stated cost, not a risk to design around. `Fact` was measured at 102 rows during
the `f4830e0b7` calibration — about 3k tokens, ~8 s at the measured ~400 tok/s locally.
`ConversationTurn` and `Passage` grow without bound and were NOT counted (the per-tenant
ArcadeDB credential is derived by HMAC and the sidecar's own credentials do not open that
database from outside). They could be minutes or hours locally, and ~26x less on a cloud
route. That number belongs in the warning, computed at the time — not in a design that tries
to avoid it.

The asymmetry with documents is not a different philosophy: `deps` makes CocoIndex redo
every chunk the embedder touched. Same semantics, delegated to an engine that already
implements it incrementally and for free.

### 5. The warning

Shown when the operator changes the embedding model, before anything is written. It states:
how many vectors are stale (documents and memory, counted separately), that retrieval
quality degrades until the pass completes, and — when the target is a cloud route — that the
pass bills tokens. Confirming starts the pass; declining leaves the model unchanged.

### 6. The mitigation decision 3 requires

Because search keeps running over a mixed corpus, a **document** result carries the
fingerprint it was embedded with — `document_embeddings` already stores one per row — and
the UI shows that a pass is in progress. This does not make the scores comparable, nothing
can, but it makes the mixture **visible** rather than silent, which is the difference
between a degraded answer and an unexplained one.

The ArcadeDB side gets no per-row equivalent, because that would mean adding exactly the
per-row fingerprint the design declined. What it gets instead is the pass's own state: while
a re-embed is running, the operator has been told so, and that is the honest signal at the
granularity the design keeps.

## What this design does NOT do

- It does not make cross-model scores comparable. Nothing does.
- It does not re-embed automatically on a model change: the operator confirms first.
- It does not address dimension changes beyond what `TruncateMRL` already enforces (exact
  passes, narrower errors, wider truncates). A narrower model is an error, not a re-embed.
- It does not touch STT/TTS/image/video, which hold no vectors.

## Open questions to close before implementation

1. ~~Do the ArcadeDB types record the embedding model?~~ **Closed: no, and it stays that
   way.** Changing embedder is rare and confirmed, so the pass redoes everything rather
   than earning the right to skip work.
2. How large are `ConversationTurn` and `Passage` in a real deployment? Unmeasured — the
   per-tenant credential is derived and did not open the database from outside. The number
   is needed for the warning's estimate, not for the design.
3. Where does the pass run — the daemon, `services/ingest`, or the memory sidecar? The
   sidecar owns the ArcadeDB credentials and now reads `aura.settings` at boot, which makes
   it the candidate, but it has no job runner today.
4. How is progress reported to the cockpit? There is an existing `/healthz` seam and a
   scheduler; neither is obviously right for a long pass.
