# Graph-native document retrieval — implementation plan

> **For agentic workers:** each task is self-contained. Read the §Grounding section
> before your task; it holds the measurements the design rests on and the mistakes
> already made. Do NOT re-derive them, and do NOT contradict them without a new
> measurement of your own.

**Goal:** answer a question about a document by the cheapest route that can actually
answer it — compute over a spreadsheet, a passage from prose — with the graph choosing
the candidate set before any vector search runs.

**Corpus of record:** `D:/tmp/baseline-corpus` — 17 files, 56 MB, curated for this.
6 xlsx (incl. `Clienti.xlsx`, ground truth: 699 clients in TORINO), 7 pdf (incl. a 30 MB
manual and five Normattiva decrees whose filenames differ only by number and date), pptx,
epub, docx, html. Every number in this plan must be measured against it.

---

## Grounding — read this first

### The rule, corrected

An earlier reading of the retrieval study took one measurement about **aggregates over
spreadsheets** and generalised it into "chunking is dead". That was wrong, and the study
already said so: its own target architecture (`spikes/RETRIEVAL-POSTGRES-SYNTHESIS.md`
§6) reads `prosa → chunk+overlap → embed`.

What the study actually measured:

| finding | number | what it means |
|---|---|---|
| answerability | "importo medio delle fatture non pagate" = 23851.00 € is **in no chunk** | a derived aggregate is COMPUTED, never retrieved |
| routing, 21 files | card 40% vs chunk-flatten 35% R@1 | representation alone is **not** the lever |
| routing, 465 files | flatten 0.917 vs card 0.903 MRR | same conclusion at scale |
| equal-weight RRF | dense 8/8 → hybrid 5/8; routing 0.751 → 0.532 | naive fusion **hurts** when one leg dominates |
| embedder | MiniLM 2/8 → EmbeddingGemma 8/8 @5 | the embedder WAS the lever |

And the study's own caveat: *"tsvector basta / brute-force le card / apri l'originale
sempre"* are single-operator premises that **break at 1000 files and 700-page PDFs**.

So the rule is not one granularity. It is:

| file kind | route | why |
|---|---|---|
| **spreadsheet / tabular** | card → open → compute | the aggregate is in no cell |
| **prose** (pdf, docx, epub, html, pptx notes) | card to route → **passage to answer** | the passage IS the answer |

The card stays on both routes: it is what says WHICH file. What changes is what happens
next.

### The three ArcadeDB levers we are not using

Read the source, not the site — `docs.arcadedb.com` returns near-empty pages to a fetch.
Use `gh api repos/ArcadeData/arcadedb-docs/contents/src/main/asciidoc/<path>.adoc --jq .content | base64 -d`.
Reference: `reference/extended-functions/vector.adoc`, `concepts/vector-search.adoc`.

**1. `vector.neighbors` takes a RID filter.** This is the whole design.

```sql
vector.neighbors('Section[embedding]', :q, 10,
  { filter: (SELECT @rid FROM Section WHERE ...) })
```

> *"Restricts the search to the provided set of records. Useful to combine a vector
> search with a logical filter: first select the candidate RIDs, then pass them as
> `filter`."*

The candidate set can come from a **graph traversal**. That is the answer to SIGMOD 2026
(arXiv 2603.23710), which found that filtered vector search is decided by system-level
overheads and page accesses, not distance computations — *"published ANN benchmarks do
not predict `WHERE tenant_id = ?` performance"*.

**2. Weighted fusion exists server-side**, which is the remedy the study asked for and we
never built:

- `vector.multiscore(scores, 'WEIGHTED', weights)` — also `'MAX'` (ColBERT style), `'AVG'`, `'MIN'`
- `vector.hybridscore(s1, s2, alpha)` — `alpha*s1 + (1-alpha)*s2`
- `vector.rrfscore(ranks…, { k })` — k is set ONLY via the trailing options map; a bare trailing number is a rank
- `vector.normalizescores(scores)` — min-max to [0,1], which is how you make an unbounded BM25 score comparable to a cosine in [-1,1]

**3. `vector.fuse` fuses ANY source yielding `(@rid, $score)`** — dense neighbours, sparse
neighbours, full-text `SEARCH_INDEX`, *or a plain `SELECT … ORDER BY … LIMIT N`*. So a
graph traversal, ordered, can be a fusion leg.

**Operational:** declare the embedding `EXTERNAL true` so the bytes live in a paired
external bucket and the record carries an 8-byte pointer, loaded lazily only by queries
that need it.

### Traps already paid for

- `out.name` on an edge returns **NULL**; endpoints need `outV()` / `inV()`
  (`internal/arcadedb/memory.go:127`).
- `defaultDatabases=db[]` — the EMPTY brackets are load-bearing; the bracketed-credential
  form locks root out of the database it just created.
- ArcadeDB rejects `before` and `after` as bind-parameter names.
- The embedder must be local. A cloud embedder cost 76s on the cold anchor build.
- The shipped GGUF must carry `dense_2`/`dense_3`; unsloth's Q8_0 omits them and returns
  backbone-only vectors at the right width with no error.
- Migration numbers come from `ls internal/db/migrations/ | tail -1` **at write time**,
  never from this document.

---

## Phase 0 — the mechanical card (IN FLIGHT, separate agent)

`internal/documents/filecard/` with a builder per format. Not this plan's work, but every
later phase depends on it: the card is the routing layer for BOTH routes, and today a file
nobody opened has no card and is invisible.

**Acceptance already set for it:** recall@1 measured before/after over Italian queries on
the baseline corpus, with the five Normattiva PDFs called out explicitly — their filenames
carry almost no discriminating signal, so if PDF cards degrade to name+size+pages those
five are unroutable and that must be written down, not hidden.

---

## Phase 1 — the prose graph in ArcadeDB

**Why ArcadeDB and not Postgres:** it is already the memory engine, so a remembered fact
and a manual section land in the SAME graph and the entity "sensore di pressione" can have
edges to both. A second engine buys nothing; the study's D10 says exactly this.

**Postgres keeps the catalog** — the card, the weighted `tsvector` + GIN, the lifecycle,
and the RLS we just closed. Do not move it.

### 1.1 Schema
- [ ] `Document` and `Section` vertex types, `HAS_SECTION` edge, in a Cypher/SQL migration
      under the ArcadeDB schema path (find how `internal/arcadedb/memory_vector.go`
      declares its types and follow it — `CREATE PROPERTY … ARRAY_OF_FLOATS`,
      `CREATE INDEX … LSM_VECTOR METADATA {dimensions, similarity}`).
- [ ] Embedding property declared `EXTERNAL true`.
- [ ] Dimensions 768, cosine — same contract as memory. Do NOT introduce a second width.
- [ ] The Postgres `documents.id` is the join key; a Section knows its document id.

### 1.2 Sectioning
- [ ] Split prose into sections at STRUCTURE where the format gives it (PDF outline,
      docx headings, epub spine, html headings) and fall back to a size window with
      overlap only where it does not.
- [ ] A section carries: text, the document id, an ordinal, and the heading path.
      The heading path is what makes a passage citable ("Manual → §4.2 Calibration").
- [ ] Cap what you READ, not only what you keep. State the cap. The 30 MB manual is the test.
- [ ] **Tabular files get NO sections.** They route to open-and-compute. Assert it.

### 1.3 Embedding
- [ ] Sections embed through the SAME shared embedder as memory, with the document-side
      task prefix (`title: none | text: `). Query side uses `task: search result | query: `.
      Measured 2026-08-02: 86.7% → 93.3% recall@1, 0 regressions.
- [ ] Embedding happens off the ingest hot path, like memory's backfill, and there must be
      a caller — `EmbedMissingFacts` had none for months and no fact ever got a vector.

### 1.4 Retrieval
- [ ] `document_passage` tool (name it as you like, but it is NOT `document_search`, which
      returns FILES and must keep doing so).
- [ ] The query shape: card/graph selects candidate section RIDs →
      `vector.neighbors('Section[embedding]', :q, k, { filter: <those RIDs> })`.
- [ ] Return the passage WITH its heading path and document id, so the agent can cite it
      and can escalate to `document_open` when the passage is not enough.

**Acceptance:** on the baseline corpus, a set of Italian passage questions — including at
least three that can only be answered from inside the five Normattiva decrees and two from
the 30 MB manual — with recall@1 reported. Compare against today's baseline, which is
`document_open` + shell (`pdftotext` + grep). If the graph leg does not beat that, say so.

---

## Phase 2 — weighted fusion, measured

- [ ] Replace any equal-weight RRF on the document path with `vector.multiscore(…,
      'WEIGHTED', …)` or `vector.hybridscore(…, alpha)`.
- [ ] **Measure the weight, do not choose it.** Sweep alpha on the baseline corpus and
      report the curve, not just the winner.
- [ ] Normalize before combining if the legs are on different scales
      (`vector.normalizescores`) — BM25 is unbounded, cosine is [-1,1].
- [ ] The memory path's RRF was added on 2026-08-01 and is STILL UNMEASURED. Measure it in
      the same pass or state that you did not.

**Acceptance:** a table with one row per arm (dense only, lexical only, equal-weight RRF,
weighted at the chosen alpha) and recall@1 + MRR for each. The study's warning is the
hypothesis under test: equal weight should LOSE when one leg dominates.

---

## Phase 3 — the graph play

This is the part that makes it not a stupid RAG. Everything above is table stakes.

- [ ] Extract entities from sections into the SAME entity space memory already uses, so a
      document section and a remembered fact can share an entity vertex.
- [ ] Expansion query: from the routed document, traverse to its entities, then to other
      sections/documents mentioning them, and use that as the `filter` RID set.
- [ ] Make the expansion BOUNDED and explain the bound. An unbounded 2-hop expansion on a
      dense entity ("Torino") is the whole corpus, and then the filter buys nothing.
- [ ] Feed the traversal in as a `vector.fuse` leg where it is a ranking, not just a filter.

**Acceptance:** at least one question that is UNANSWERABLE without expansion — a fact in
document A that only makes sense with a section of document B — answered correctly, with
the traversal shown. And the honest counter-measurement: how much does expansion cost on
questions that did not need it?

---

## Phase 4 — ETL for the tabular route (D2)

Measured in the study, `spikes/document-routing-scale/results_scale.txt`, over 400 invoices:

```
open+compute (per query)   1193.4 ms   re-reads all 400 files EVERY query
ETL build (once, ingest)   1222.7 ms
SQL query (per query)          0.2 ms
results identical: True
```

Not an approximation that is faster — **the same answer**. Nothing tabular exists in
`internal/db/migrations` yet.

- [ ] Land rows into real Postgres tables at ingest, schema inferred per file.
- [ ] **Budget for ~25% of files not landing.** Auto-Tables (Microsoft, VLDB) reaches
      ~75% top-3 on 244 real cases with no examples, and <3% of real spreadsheets have a
      predefined data model. Design the fallback (open-and-compute) as a first-class path,
      not an error.
- [ ] Table-per-file is fine at thousands — AWS's own threshold for relation count is
      "millions" — and the cost lands on `pg_dump`/upgrade, not on queries.
- [ ] Two hard don'ts: do NOT model files as **partitions** (threshold is "hundreds"), and
      do NOT use **unlogged** landing tables past ~1k (serial truncation on recovery).

**Acceptance:** the `Clienti.xlsx` ground truth (699 clients in TORINO) answered by SQL,
identical to the open-and-compute answer, with both timings reported.

---

## Standing rules for every task here

- **Measure or say you did not.** A retrieval change without a number is a guess.
- Disposable databases only. Never `aura`. Create, prove, drop.
- Migration numbers from the directory at write time.
- No file over 600 LOC; refactor on touch.
- A tier that runs nowhere is not a gate: wire it into CI, and make its skip-helper
  `t.Fatal` under `$CI`.
- The card is a proxy, never the truth. The file is the truth.
