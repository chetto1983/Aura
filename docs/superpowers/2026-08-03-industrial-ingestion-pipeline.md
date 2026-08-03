# How the industry actually builds an ingestion pipeline — and where Aura's is missing

**Date:** 2026-08-03 · **Status:** evidence-gathered, decision PROPOSED, not approved.
A PRD amendment is required before any code (PRD-first principle).

Every claim below is read from a **primary source** — the projects' own documentation and
source, not blog posts — and cited inline. Where a number is ours, it says so and names
the measurement.

---

## 1. The converged shape

Four independent reference implementations — Microsoft GraphRAG, IBM Docling, CocoIndex,
and the ELT tooling around dbt/Airbyte — converge on the same six stages. That convergence
is the finding; none of them cite each other for it.

```
0  IDENTIFY   content-addressed id, stable across runs
1  CONVERT    format  ->  structured document          (swappable provider)
2  CHUNK      granularity by content kind              (tokenizer-aware)
3  EXTRACT    entities / relations / claims            (~75% of the bill)
4  AUGMENT    communities, summaries
5  EMBED      text units, entity descriptions, reports (three targets, not one)
```

Cross-cutting, and this is where the amateur pipelines fail: **a cache in front of every
LLM call**, **a delta path that is a first-class parallel pipeline**, **provider factories
at every stage**, and an **orchestrator** because this is a DAG with partial failure.

---

## 2. Stage 0 — identity is content-addressed. This is the load-bearing decision.

GraphRAG, `docs/index/inputs.md`, on the `id` column of every input document:

> *"ID of the document. This is generated using **a hash of the text content** to ensure
> **stability across runs**."*

CocoIndex, `common_resources/id_generation.mdx`, states the failure mode outright:

> *"In an incremental pipeline, using random IDs (like `uuid.uuid4()`) means every
> reprocessing run generates different IDs for the same data — causing unnecessary churn
> in your targets (deleting old rows, inserting identical ones with new IDs). CocoIndex's
> ID utilities produce **stable** IDs: the same inputs produce the same IDs across runs,
> so unchanged data keeps its identity and **targets only see real changes**."*

It also distinguishes the two cases explicitly: `generate_id(dep)` — same input, same ID —
versus `IdGenerator.next_id(dep)`, which yields distinct-but-reproducible IDs for chunks
that may have identical content and still need separate identity.

**This is stage 0 for a reason: every later stage's incrementality is derived from it.**
You cannot bolt it on afterwards, because "has this changed?" has no answer without it.

## 3. Stage 1 — conversion is a swappable provider, and Microsoft's own choice is MarkItDown

GraphRAG ships input readers for *"text, CSV, JSON, JSONL, Parquet, and **MarkItDown**"*
(`docs/index/architecture.md`), behind an `InputReader` provider registered in a factory.
Every subsystem is a factory — model, input reader, cache, logger, storage, vector store,
pipeline. The stated purpose is that you can replace any of them without forking.

Docling takes the other route: a typed `DoclingDocument` with layout models, and it names
the fork in the road (`docs/concepts/chunking.md`):

> *"1. exporting the `DoclingDocument` to Markdown (or similar format) and then performing
> user-defined chunking as a post-processing step, or 2. using native Docling chunkers,
> i.e. operating directly on the `DoclingDocument`."*

Markdown-then-chunk is the cheap path and loses the structure the converter recovered.
Chunk-on-the-structured-document is the expensive path and keeps it. **markitdown only
offers the first**, which is the real cost of adopting it — not its image handling.

## 4. Stage 2 — the chunker is tokenizer-aware, and metadata is prepended to every chunk

Docling's `HybridChunker` is hierarchical chunking plus *"tokenization-aware refinements"*:
it starts from document structure, then **splits only when oversized** and **merges only
when undersized**, against *"the user-provided tokenizer (typically to be aligned to the
embedding model tokenizer)"*. Not a fixed character window.

GraphRAG's chunk size is 1200 tokens by default, and it states the tradeoff plainly:
*"Larger chunks result in lower-fidelity output and less meaningful reference texts;
however, using larger chunks can result in much faster processing time."*

And the one everybody discovers the hard way — `prepend_metadata`:

> *"When documents are chunked, they are split evenly according to your configured chunk
> size… This means that front matter at the beginning of the document (such as the
> headline and author) **is not copied to each chunk**. It only exists in the first chunk.
> When we later retrieve those chunks… they may therefore be missing shared information
> about the source document that should always be provided to the model."*

**Aura already diagnosed this exact defect independently.** `spikes/RETRIEVAL-POSTGRES-SYNTHESIS.md`
records the old markitdown sidecar losing the Excel header after the first chunk. Same bug,
same remedy, and GraphRAG made the remedy a config knob.

## 5. Stage 3 — the graph is 75% of the bill, and there is a cheap tier

GraphRAG, `docs/index/methods.md`, states the cost split directly:

> *"We estimate **graph extraction to constitute roughly 75% of indexing cost**."*

So they ship two methods, and the difference is not a parameter — it is a different
algorithm:

| | Standard | FastGraphRAG |
|---|---|---|
| entities | LLM extracts + describes | **noun phrases** via NLTK / spaCy, no description |
| relationships | LLM describes each pair | **co-occurrence** within a text unit, no description |
| summarization | LLM, entities and relationships | not needed |
| claims | optional LLM pass | never |
| chunk size | 1200 tokens | **50–100 tokens** — smaller chunks make a better co-occurrence graph |

Their own guidance on choosing: *"If high fidelity entities and graph exploration are
important to your use case, stay with traditional GraphRAG. If your use case is primarily
aimed at summary questions using global search, FastGraphRAG provides high quality
summarization with much lower language model cost."*

Note the chunk size **changes with the method**. Chunking is not an independent decision;
it is downstream of what the graph is for.

## 6. The cross-cutting half nobody demos

**Cache.** GraphRAG wraps every model interaction: *"When completion requests are made
using the same input set (prompt and tuning parameters), we return a cached result if one
exists. This allows our indexer to be **more resilient to network issues, to act
idempotently**, and to provide a more efficient end-user experience."*

We measured the cost of not having one, today, on one file: **56% of markitdown's vision
calls were redundant** — 72 picture placements behind 32 distinct images, one logo
captioned 36 times. A hash-keyed shim removed all 40 of them
([the probe](2026-08-03-markitdown-mcp-probe.md)).

**Delta.** GraphRAG carries a second, parallel set of workflows purely for updates —
`load_update_documents`, `update_text_units`, `update_entities_relationships`,
`update_communities`, `update_community_reports`, `update_text_embeddings`,
`update_clean_state`. Seven workflows whose only job is not re-doing stage 3.
CocoIndex is built around nothing else: *"Declare what should be in your target —
CocoIndex keeps it in sync forever, **recomputing only the Δ**."*

**Orchestration.** The ELT landscape by adoption is Airflow 46k, Kestra 27k, Airbyte 21k,
Dagster 15k, dbt-core 13k, dlt 5k. They exist because an ingestion pipeline is a DAG with
retries, partial failure and backfill — not a function call.

---

## 7. Where Aura's pipeline actually stands

Honest audit against the six stages.

| stage | Aura today | verdict |
|---|---|---|
| 0 identify | `DocumentID(contentHash, sourceID)` = sha256 — **the right primitive, applied at only one of two layers** | **HALF-DONE, see below** |
| 1 convert | `internal/documents/filecard/` per format + poppler | present, in-process, no provider seam |
| 2 chunk | **nothing** — the chunk plane was deleted with Neo4j | absent by decision, to be rebuilt for prose |
| 3 extract | **nothing** | not started |
| 4 augment | **nothing** | not started |
| 5 embed | memory facts only; documents have a card + tsvector | partial |
| cache | **none** | **MISSING** |
| delta | **none** — any change is a full redo | **MISSING** |

### Stage 0 — we have it, and it is wired at the wrong layer

`internal/documents/ids.go:59` derives exactly what GraphRAG derives:
`sha256(contentHash + ":" + sourceID)`. Content-addressed, stable across runs. It is then
enforced where it matters least and unenforced where it matters most:

- **Job layer — idempotent.** `0015_document_ingest_jobs.up.sql:35` carries
  `UNIQUE (source_id, document_id, content_hash)` and the query does `ON CONFLICT`.
  Re-ingesting a file does not create a second job.
- **Catalog layer — NOT idempotent.** `CreateDocument` is a bare `INSERT … RETURNING *`
  with a generated `id`, and `search_document_id` lives inside a `metadata` jsonb with no
  unique index over it. `Service.IngestPath` calls `recordCatalogDocument`
  unconditionally, so a repeat ingest of the same bytes writes **another catalog row**.

That is the same defect fixed on 2026-08-03 for the *asset* writer —
`documentForAssetVersion` now looks a document up by `search_document_id` and reuses it,
and its own comment records that not doing so "produced two rows per upload with
document_open resolving the newer, card-less one". **The fix landed on one writer and not
the other**, and the catalog is the layer `document_search` and `document_open` read.

So the first task is not to invent stage 0. It is to finish it: make the catalog insert
an upsert on the same natural key the job table already trusts.

The two genuinely missing cross-cutting pieces — cache and delta — are the ones the
references call foundational, and **both are cheap**. The graph, the expensive one, is the
piece we have been planning to build first.

### The consequence for the plan's ORDER

[The graph-native plan](plans/2026-08-02-graph-native-document-retrieval.md) sequences
Phase 1 prose graph → Phase 2 fusion → Phase 3 graph play → Phase 4 tabular ETL. Measured
against the references, that order pays the 75% first and builds the plumbing never.

**Proposed re-order, and the reason for each move:**

1. **Finish stage 0 at the catalog layer.** The sha256 key already exists and the job
   table already enforces it; the catalog insert does not, so a repeat ingest still writes
   a second row that `document_open` will then resolve. One upsert on the key we already
   trust. Hours, not days, and everything downstream inherits it.
2. **A derivation cache keyed by (input hash, operation, params).** Already demonstrated
   at 56% saving on one file, in ~30 lines.
3. **Phase 4 tabular ETL** — moved UP. It has the largest measured win in the whole study
   (1193.4 ms → 0.2 ms per query, identical answers), it needs no graph, and ~25% of files
   are budgeted not to land, so the fallback path gets exercised early rather than late.
4. **Phase 1 prose sectioning + embed** — with `prepend_metadata` from the start, and a
   tokenizer-aware split/merge rather than a character window.
5. **Phase 2 weighted fusion, measured.** Unchanged.
6. **Phase 3 the graph, LAST**, and with the standard-vs-fast decision made explicitly on
   a cost number rather than by default. Our corpus is technical manuals and decrees:
   co-occurrence over noun phrases may be enough, and it is the difference between paying
   the 75% and not.

**The honest counter-argument**, stated because it is real: the operator's original
instinct was to play the graph, and re-ordering defers the most interesting part. The
defence is that stages 0 and 2 are days of work and make stage 3 re-runnable, whereas
building stage 3 first means every prompt change re-pays 75% of the bill from scratch.

---

## Sources

All read from the projects' own repositories on 2026-08-03.

- microsoft/graphrag — `docs/index/architecture.md`, `default_dataflow.md`, `methods.md`,
  `inputs.md`, and `packages/graphrag/graphrag/index/workflows/`
- docling-project/docling — `docs/concepts/chunking.md`
- cocoindex-io/cocoindex — `README.md`, `docs/.../common_resources/id_generation.mdx`
- GitHub topic ranking for `etl` / `elt` / `data-engineering` / `data-pipelines`
- Aura's own `spikes/RETRIEVAL-POSTGRES-SYNTHESIS.md` and
  [the markitdown probe](2026-08-03-markitdown-mcp-probe.md)
