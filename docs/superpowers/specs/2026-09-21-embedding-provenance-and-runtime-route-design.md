# Embedding provenance and runtime route — design

Status: proposed, not implemented. Written for the implementer; it assumes no
knowledge of the conversation that produced it.

## Goal

Two operator-visible gaps, one root cause.

1. The cockpit Settings page cannot pick an embedding model. `AURA_EMBED_MODEL` and
   `AURA_EMBED_BASE_URL` render as free-text inputs
   (`web/src/settings/modelSettingsDefs.ts:165-166`) while chat, image, video,
   transcription and speech all get a catalogue-backed picker.
2. `.env` is to stop being the configuration authority; `aura.settings` in Postgres is.

The root cause under both: **three processes write vectors into the same per-tenant
ArcadeDB, each configured separately, and nothing detects when they disagree.** A
width change fails loudly. A model change at the same width is silent, and it
poisons the index while every healthcheck stays green.

This design makes the disagreement detectable first, and only then makes the
configuration runtime-editable. Detection is the smaller change and the one that
makes the other safe.

## What was measured

Measured 2026-09-21 against this repo at HEAD and against the upstream documentation
named below. Every claim here is a file, a line, or a quoted document.

### Three writers, three configurations

| Process | Language | Embedder configuration | Reads `aura.settings`? |
|---|---|---|---|
| `cmd/aura` | Go | `config.EmbedRoute()`, env overlaid by `settings.OverlayEnv` at boot | yes |
| `cmd/arcadedb-mcp` | Go | `AURA_EMBED_BASE_URL` from env at start | **no** — imports no pgx, holds no settings store |
| `services/ingest` | Python | `os.environ["AURA_EMBED_BASE_URL"]` (`app.py:36`), model hard-coded `"embeddinggemma"` (`app.py:182`, `app.py:206`) | **no** |

`cmd/aura-media-index` also calls `settings.OverlayEnv`; it does not embed.

Compose hard-codes the daemon's base URL as `http://aura-llama-embed:8081`
(`compose.yaml:119`) — a Compose DNS name, not loopback. This matters because
`EmbedRoute` substitutes the OpenRouter base only when the configured base **is**
loopback (`internal/config/config_routes.go:19-29`). Setting `AURA_EMBED_MODEL` alone
from the cockpit therefore sends a cloud model name to the local sidecar. The swap
needs both keys, and the base without a trailing `/v1`, because the shared client
appends `/v1/embeddings`.

### Five vector-bearing types, two schema owners

| Type | Index | Quantization | Dimension source | Schema owner |
|---|---|---|---|---|
| `FACT` (edge) | `LSM_VECTOR` COSINE (`memory_vector.go:57`) | `NONE` | Go const `vectorDimensions = 768` (`memory_vector.go:34`) | Go |
| `ConversationTurn` | `LSM_VECTOR` COSINE (`memory_conversation.go:90`) | `NONE` | same const | Go |
| `ReasoningTrace` | `LSM_VECTOR` COSINE (`memory_reasoning.go:42`) | `NONE` | same const | Go |
| `IndexedDocument` | owned by the pipeline | cosine:none | `AURA_EMBED_DIMENSIONS` env | **Python** (`services/ingest/arcade.py`) |
| `Passage` | `LSM_VECTOR` | cosine:none | same | **Python** |

Go's `DocumentIndex` is a reader, not an owner — its own doc comment says it "reads
the CocoIndex-owned document records for every tenant". The two planes agree today
only because one contract is written twice: `schemaVersion()`
(`internal/arcadedb/document_schema.go:132`) and `schema_version()`
(`services/ingest/arcade.py:83`) produce the identical string
`document-v1:standard-analyzer:cosine:none:<dims>` in two languages.

`EnsureMemorySchema` runs from `internal/arcadedb/tenant_clients.go:93`, shared code,
so both Go processes can create the memory schema with 768 compiled in.

### The provenance knobs exist and are never used

`AURA_EMBED_REVISION` and `AURA_EMBED_FINGERPRINT` are loaded into
`config.EmbedConfig` and read in exactly one non-test place:

```go
if profile.Strict() && (strings.TrimSpace(c.Embed.Revision) == "" ||
    !isLowerHexSHA256(c.Embed.Fingerprint)) {
```

`internal/config/config_document_retrieval.go:62-63`. They are checked for existence
and shape, then never written beside a vector, never compared against stored data,
never surfaced. They assert provenance without recording it. `schemaVersion()`
likewise fingerprints analyzer, similarity, quantization and width — **but not the
model**.

Consequence, stated plainly: a model swap at 768 dimensions is invisible to every
guard this codebase currently has.

### What ArcadeDB's documentation says

Read from the asciidoc source of `how-to/data-modeling/vector-embeddings`
(`docs.arcadedb.com` is JS-rendered and returns near-empty pages to a fetcher; the
source is `ArcadeData/arcadedb-docs`, `src/main/asciidoc/<path>.adoc`).

- `dimensions` is fixed in `CREATE INDEX ... METADATA`. The page documents no way to
  alter it. `REBUILD INDEX` and `COMPACT INDEX` rebuild the HNSW graph from the
  vectors present; neither changes the width.
- Therefore a width change is drop-and-re-embed, while a model change at equal width
  needs no DDL at all — which is exactly why it is silent.
- The page documents multi-modal storage: several vector properties on one type, each
  with its own index and dimensions. That is the documented shape for a side-by-side
  migration (write the new space into a second property, cut over, drop the old), and
  it is available if a width change is ever needed.
- Aura uses `quantization: NONE`; the page recommends `INT8` above 10K vectors and
  says `NONE` is fine below. Out of scope here, recorded so it is not lost.

### What LibreChat does

Read from `danny-avila/LibreChat` and `danny-avila/rag_api`.

**Model pickers.** Every endpoint in `librechat.example.yaml` takes the same shape:

```yaml
models:
  default: ['mistral-tiny', 'mistral-small', 'mistral-medium']
  fetch: true   # Defaults to false.
```

A curated `default` list is required ("At least one value is required"); `fetch`
pulls the provider's `/models` and defaults to false. They also record where fetch
cannot work: for Anthropic-compatible endpoints, "model auto-fetch uses the OpenAI
`/models` convention and is not used for this provider." The picker never depends on
a live call succeeding.

**The separate service's embedder.** `rag_api` is its own container, configured by env
from compose: `RAG_API_URL`, `RAG_OPENAI_BASEURL`, `RAG_OPENAI_API_KEY`,
`EMBEDDINGS_PROVIDER`, `EMBEDDINGS_MODEL`. There is no publication endpoint, no
authority process and no shared configuration client. The credential is duplicated
into the sidecar deliberately.

**Disagreement between the two.** Not prevented. The `rag_api` README says of
`EMBEDDINGS_DIMENSIONS`: "**do not change this on an existing collection** — all
vectors in a `pgvector` column must share the same dimensionality." The string
`dimension` appears in `app/config.py`, the README and the tests, and in neither
`extended_pg_vector.py` nor `async_pg_vector.py`. No vector records which model
produced it. A width change fails at insert because pgvector rejects it; a same-width
model change does not fail at all.

**What transfers and what does not.** The picker shape transfers as-is. The
env-per-service configuration does **not** settle Aura's question, because LibreChat
has no runtime-editable embedder: their config is a file an operator edits before
recreating containers. Aura's requirement is a cockpit that changes it at runtime,
which is precisely the case their model does not cover. That requirement — not
architectural preference — is the whole justification for anything beyond env.

## Decisions approved by the operator

1. Detection before configuration. Make a disagreement visible first; make the
   configuration runtime-editable second.
2. Env stays the floor for every service, as in LibreChat, so a boot without the
   daemon works. Runtime publication carries only what env cannot: a change that takes
   effect without recreating containers.
3. The picker follows LibreChat: a curated list is mandatory, fetch is an optional
   enrichment.
4. Capabilities needing a credential are proxied by the daemon rather than configured
   per service; plain configuration is published. Only the embedding route is in scope.

## Architecture

### The embedding identity

One string, computed in one place, naming the vector space a set of floats belongs to:

```
embed-v1:<model>:<dimensions>:<artifact>
```

- local sidecar: `<model>` is `local`, `<artifact>` is `AURA_EMBED_FINGERPRINT` (the
  SHA-256 of the deployed GGUF, already required under a strict profile).
- cloud route: `<model>` is the OpenRouter model id, `<artifact>` is the route host.

It is derived from the resolved route, never configured directly. It follows the
existing `schemaVersion()` convention so the codebase keeps one habit, not two.

The identity deliberately does **not** include pooling or the asymmetric prefixes
(`chunk.EMBED_DOC_PREFIX`). Those are properties of the model the identity already
names; adding them creates a second thing that can disagree with the file, which is a
failure this codebase has already paid for once.

### The provenance marker

One record per tenant database, in a new type, holding the identity, when it was set
and which process set it. Not a property on each vector: the five vector types have
two schema owners in two languages, a per-record property would have to be added in
both, and it would not answer a question the marker cannot.

Written by whichever writer first finds it absent. Read by every writer before its
first vector write in a session.

### Detection and degradation

On reading the marker a writer takes one of three paths:

- **absent** — write it. A fresh database, or the first boot after this ships.
- **equal** — proceed.
- **different** — do not write vectors, and do not answer dense queries from a space
  the corpus is not in. Degrade to the lexical leg that already exists
  (`searchFactsFallback`, `internal/arcadedb/memory_vector.go:250`) with a new reason
  constant alongside `reasonEmbeddingFailed` and `reasonEmbeddingInvalid`
  (`memory_vector.go:214-215`). Surface it in the doctor and in health, by name.

Reads degrade rather than fail because a mismatched writer's *query* vectors are in
the new space too: serving them against the old corpus returns plausible nonsense,
which is worse than returning the lexical result and saying so.

### Repair

The repair machinery exists and is reused, not rewritten. `clearFactEmbeddingsStatement`
(`memory_vector.go:398`) already nulls stored vectors for a re-embed pass, and the
backfill sweeps already fill `embedding IS NULL`. Repair is: clear, update the marker,
let the existing sweeps run.

This must be an explicit operator action. It is destructive and costs a full corpus
re-embed; nothing should trigger it as a side effect of saving a settings row.

### Route publication

The daemon resolves the route and the identity and exposes both on an internal
endpoint. `cmd/arcadedb-mcp` and `services/ingest` read it at boot with bounded retry
and fall back to their current env when it is unreachable — the env floor of decision
2, which also keeps the existing boot order working.

The endpoint carries configuration only. The OpenRouter credential is not published:
`OPENROUTER_API_KEY` is `Secret: true` in the settings allowlist and `OverlayEnv`
deliberately never lets a secret row reach process env. A service needing a
credentialed cloud embedder uses the daemon as the endpoint rather than receiving the
key. Whether the daemon can carry that proxy load is an open measurement (below) and
is not promised by this design.

### Cockpit

`AURA_EMBED_MODEL` becomes a picker in `BACKEND_SETTINGS`, built from the existing
`ModelPicker` + `useMediaModelCatalog` seam, with a curated default list and an
optional fetch leg. `AURA_EMBED_BASE_URL` stays an input, because a custom endpoint is
a real case a picker cannot enumerate.

Saving a model that changes the identity is not an ordinary save. The UI states that
the stored corpus was embedded with a different model, that dense retrieval will
degrade to lexical until a re-embed runs, and requires an explicit acknowledgement.

## Testing and acceptance

Unit: identity derivation across local, cloud and custom-base routes, including the
non-loopback Compose base that currently defeats a model-only swap; marker
absent/equal/different; the degradation reason reaching the fallback.

Integration (`arcadedb_integration`): two writers with different identities against one
tenant database — the second must refuse and degrade, never write. Repair path: clear,
re-mark, backfill, dense retrieval returns.

Cockpit (vitest): the picker renders from the curated list with fetch failing; the
identity-change acknowledgement blocks the save until confirmed.

Acceptance is E2E on the live stack, per the repo's definition of done: change the
model in the cockpit on a corpus that has vectors, observe the degradation by name
rather than by silence, run the repair, observe dense retrieval return.

## What this design does not prove

Stated because a number without its perimeter is a supposition wearing a hat.

- **Whether OpenRouter publishes an embedding modality the existing catalogue lister
  can filter**, the way it does for `transcription` and `speech`. Not measured. The
  curated-list-first picker is chosen precisely so this is an enhancement, not a
  dependency; measure it when wiring the fetch leg.
- **Any latency or throughput figure for a cloud embedding route on this stack.** None
  was measured for this design, and figures circulating in older notes and in
  `.env.example` were not re-measured. Do not carry them into the implementation.
- **Whether the daemon can serve as an embedding proxy within its Compose budget**
  (`mem_limit: 768m`, `cpus: 1.0`). Unmeasured; the proxying decision is approved in
  principle and gated on that measurement.
- **Whether the three writers are currently in agreement in any live deployment.** This
  design adds the means to answer that; it does not assert the answer.

## Defects found while measuring, not fixed here

- `.env.example:339` proposes `AURA_MEMORY_EMBED_BASE_URL=https://openrouter.ai/api/v1`.
  That client is the same `internal/embeddings.Client`, which appends
  `/v1/embeddings`; the value as written yields `/api/v1/v1/embeddings`. The base must
  carry no `/v1`.
- `.env.example:302-303` still states the reasoning classifier "embeds 27 anchors
  before EVERY turn, one call at a time". The anchor bank is built once per process
  behind a `built` flag (`internal/agent/prompt/reasoning_classifier.go:165`).
- The `internal/settings` package comment claims the Settings page "picks the embed
  dimension"; `AllowedKeys` contains no `AURA_EMBED_DIMENSIONS`.

## Out of scope

Quantization (`NONE` to `INT8`). Width changes and the multi-modal migration shape.
Publishing configuration other than the embedding route. Third-party inference
sidecars, which are configured by Compose flags and cannot consume a client.
