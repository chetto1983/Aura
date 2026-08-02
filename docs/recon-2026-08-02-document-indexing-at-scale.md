# Web recon — document indexing & routing at scale (2026-08-02)

Context: system has ALREADY decided route → open → compute. Chunk RAG cannot answer
aggregates; ETL-to-SQL beats open-400-xlsx by ~2500-5000x; per-file "card" ≈ flattened
chunks in BM25 (MRR 0.903 vs 0.917); embedder strength IS the lever (MiniLM 2/8 →
EmbeddingGemma 8/8 recall@5); naive equal-weight RRF HURT (dense 8/8 → hybrid 5/8).

Source labels: **PAPER** / **VENDOR-BENCH-OWN-PRODUCT** / **INDEP-BENCH** / **BLOG** / **DOCS**.

---

## Q1 — Which values to keep in a per-file card

### Pneuma (arXiv 2504.09207, Apr 2025) — **PAPER**
https://arxiv.org/html/2504.09207v1

End-to-end LLM system for tabular data representation + retrieval. Directly on target:
it is *exactly* the "one card per table" problem.

Representation = two parts merged into one embedded/indexed document:
- **Schema narration**: LLM describes *each column*, given the full schema as context.
  Expands cryptic headers ("AB" → "the number of at-bats for a batter", "BABIP" →
  "Batting Average on Balls In Play").
- **Row sample**: **randomly samples r rows, default r=5**. Each sampled row is
  serialised as `column_name: value` pairs — NOT bare values. Authors' reason: "76"
  alone is ambiguous (age, quantity, price).

**Ablation — representation matters, and which one wins is dataset-dependent**
(k=1, n=5, α=0.5):

| Dataset | SchemaNarrations | SampleRows | DBReader |
|---|---|---|---|
| ChEMBL | **81.00%** | 67.90% | 65.10% |
| FeTaQA | 19.04% | 56.14% | **59.51%** |

→ Schema narration dominates on a well-named scientific DB; row samples dominate on
web tables with poor headers. Neither alone is safe — Pneuma ships both.

**Hybrid fusion** (Adventure Works, content benchmark):
- BM25 only: 74.40%
- Vector only: 67.70%
- Hybrid: **81.40%**
Fusion is **weighted score combination, not RRF**: `s(d) = α·s_lexical + (1−α)·s_semantic`,
default α=0.5. Then an **LLM Judge filters** the union of top n·k from each leg.
(Note: here the two legs are close — 74.4 vs 67.7 — which is the regime where equal
weighting is safe. See Q4.)

Scale + cost:
- Datasets: ChEMBL 78 tables, Adventure Works 88, Chicago Open Data 802, BIRD 597
  (17 GB), **FeTaQA 10,330 tables**.
- Summarizer throughput: **8 columns/second** per process.
- FeTaQA summarization: ~15 h naive → **2 h** with dynamic batch-size selection.
  Adventure Works 288 s → 63 s (−78%).
- Index size FeTaQA: **23.9 MB vs LlamaIndex 26 GB (1,089× smaller)**; query serving
  **29.6× faster** at 10,330 tables.
- BIRD content hit-rate: **63.51% (Pneuma) vs 5.58% (full-text search)**.

Takeaway for us: the card should carry **LLM-written column descriptions** (we have
none) and **whole sampled rows with header context** (we have deduped bare values,
alphabetically capped — the worst of both).

### PIPER (arXiv 2605.18199) — **PAPER**
https://arxiv.org/html/2605.18199

Content-based table search via **statistical profile + LLM-generated pseudoqueries**,
embedded for dense retrieval.

Profile contents — **no cell values at all**:
- per column: datatype, **number of distinct values**, missing-value info, value coverage
- numeric columns additionally: min, max, mean, median
- Explicit: "The resulting profile does not aim to preserve cell-level detail; rather,
  it captures the main statistical properties." No frequency selection, no entropy
  filtering, no cardinality threshold. Implemented with `datamart profiler`.

Pseudoqueries: LLM told to produce realistic dataset-search requests that (i) look like
real searches, (ii) **cover the dataset broadly rather than a few attributes**,
(iii) mention inferable relationships between variables.

Results:

| Method | OTT-QA R@10 | FetaQA R@10 |
|---|---|---|
| BM25 | **0.967** | 0.082 |
| TF-IDF | 0.963 | 0.083 |
| Dense metadata | 0.820 | 0.436 |
| Dense table embedding | 0.963 | 0.741 |
| Dense row-level | 0.951 | 0.711 |
| pT + QGpT | 0.915 | 0.586 |
| PIPER | 0.729 | **0.784** |

NTCIR-15 tabular subset (111 tables, **73.7K avg rows**, 25.5 avg cols, 10 queries):

| Model | MAP | P@10 | R@10 | nDCG@10 |
|---|---|---|---|---|
| TAPAS-base | 0.035 | 0.110 | 0.104 | 0.128 |
| BM25 | 0.192 | 0.250 | 0.347 | 0.336 |
| SPLADE | 0.234 | 0.280 | 0.373 | 0.386 |
| ColBERTv2 | 0.242 | 0.280 | 0.389 | 0.389 |
| Dense-BGE | 0.364 | 0.360 | 0.468 | 0.510 |
| PIPER | **0.560** | **0.480** | **0.647** | **0.676** |

Caveats: only 10 queries on NTCIR-15 — very thin. Pseudoquery generation is **not
uniformly beneficial**: FetaQA R@10 0.783 → 0.784 (noise), but NTCIR-15 MAP
0.161 → 0.560. Authors admit "mild query drift" in controlled settings. **No cost,
latency, or throughput numbers anywhere in the paper.** Uses gpt5-mini +
text-embedding-3-small.

Note the BM25-vs-dense split here is *enormous and inverted between datasets*
(OTT-QA BM25 0.967 / FetaQA BM25 0.082). That is our Q4 problem in someone else's data.

### TARGET benchmark (arXiv 2505.11545, May 2025) — **PAPER**
https://arxiv.org/abs/2505.11545 · https://openreview.net/forum?id=gGGvnjFUfL

The benchmark for exactly our problem: **TAble Retrieval for GEnerative Tasks**.
Evaluates retrievers in isolation AND their effect on downstream QA / fact
verification / text-to-SQL.

Findings that bear directly on our card design:
- **"Dense embedding-based retrievers far outperform a BM25 baseline, which is less
  effective than it is for retrieval over unstructured text."** ← This is the
  independent confirmation that our BM25-card-vs-chunk tie (MRR 0.903 vs 0.917) was
  measuring the wrong leg. Lexical is the weak leg on tables specifically.
- **"Embeddings of table headers AND rows generally yield the best performance"** —
  headers alone lose; rows alone lose.
- Retrievers are **sensitive to missing table titles**, and **generating descriptive
  table titles in place of non-descriptive ones enhances retrieval**. (Our xlsx files
  have filenames and sheet names; an LLM-written title is a cheap, measured win.)
- Best dense baseline in the paper: `stella_en_400M_v5`.

### Table Serialization Kitchen (Daniel Gomm, 2025) — **BLOG** (with TARGET-based experiments)
https://www.daniel-gomm.com/blog/2025/Table-Serialization-Kitchen/

Python package that sweeps the serialization design space and evaluates on TARGET
(FeTaQA, OTTQA, Spider, BIRD). Variables swept: with/without metadata; Markdown vs
JSON; **1–30 sampled rows (random)**.

Measured:
- **Metadata (title/context) inclusion: +0.42 average recall@3 on FeTaQA.** Largest
  single effect measured in the study.
- Row count (OTTQA): more rows help with **diminishing returns; most models show little
  improvement past ~10 rows**. Long-context embedders (gte-large-en-v1.5) keep
  improving; **short-context embedders (jina-v2-base-en) get WORSE with more rows** —
  dilution/truncation.
- Headline conclusion: **"There is no single 'best' parameter combination that
  generalizes across all models."** Serialization must be tuned per embedding model.

Caveat: blog reports trends + the one +0.42 figure; no full per-config score table.
EmbeddingGemma's context is 2048 tokens — that puts us in the *short-context* camp,
where MORE content actively hurts. This is a real constraint on card size.

### Postgres' own answer to "which values to keep" — **DOCS**
https://www.postgresql.org/docs/current/planner-stats.html
https://www.cybertec-postgresql.com/en/postgresql-analyze-and-optimizer-statistics/

The "which N values summarise a column" problem is *solved and shipped* in every query
optimizer, and the answer is **not alphabetical**. `ANALYZE` stores in `pg_stats`:
- `most_common_vals` + `most_common_freqs` — **top-N by FREQUENCY** (MCV list)
- `histogram_bounds` — equi-depth quantiles covering the *non-MCV* remainder
- `n_distinct`, `null_frac`
- **Default N = 100** per column (`default_statistics_target`, per-column override via
  `ALTER TABLE … SET STATISTICS`). Sample size ≈ **300 × statistics_target rows**.

The design is instructive: MCV catches the skewed/categorical head, the histogram
catches the spread of the tail, and the two are **disjoint** (MCVs are excluded from the
histogram). Our 80-alphabetical cap has neither property.

### Data catalogs — what they actually index — **DOCS**
https://docs.datahub.com/docs/how/search · https://github.com/amundsen-io/amundsen ·
https://docs.open-metadata.org/v1.12.x/api-reference/data-assets/search-indexes

Honest answer: **weaker than the papers, and not a good model for us.**
- All three (DataHub, Amundsen, OpenMetadata) are Elasticsearch/OpenSearch text-match
  over *metadata*: entity name, description, tags, owners, dataset properties as
  key=value. Fields carry a `Searchable` annotation in DataHub's metamodel.
- **Amundsen's ranking lever is usage, not content**: "page-rank style search based on
  usage patterns — highly queried tables show up earlier." Column stats are a *display*
  feature, not a retrieval feature.
- OpenMetadata's SearchIndex entity indexes field `name`, `displayName`, `dataType`.

→ No catalog puts a curated set of *cell values* into the search document. They rely on
humans writing descriptions plus usage signals — neither of which a fresh tenant upload
has. **Do not copy the catalog model.** The Pneuma/TARGET model (LLM column descriptions
+ sampled rows with headers + a generated title) is the evidence-backed one.

**Q1 verdict.** Evidence supports, in order of measured effect:
1. an LLM-generated **title/description** for the file (+0.42 recall@3, TARGET/TSK);
2. **LLM column narrations** (Pneuma: 81.0% vs 67.9% on well-named schemas);
3. **~5–10 whole sampled rows serialized as `header: value`** (Pneuma default r=5; TSK
   plateau ~10 rows; and *more hurts* on short-context embedders like ours);
4. if keeping bare values at all, select **by frequency (MCV) plus a min/max/quantile
   sketch**, per column, not globally ASCII-sorted.
No paper found that endorses a global alphabetical cap. No paper found that selects
values by entropy for *retrieval* (entropy appears only in cardinality-estimation
literature). Frequency-based (MCV) is the well-established selection criterion, but its
evidence base is query optimization, not retrieval — flagged as transfer, not proof.

---

## Q2 — Landing arbitrary spreadsheets into queryable tables

### Auto-Tables (VLDB 2023; VLDB Journal 2025) — **PAPER**
https://arxiv.org/abs/2307.14565 · https://dl.acm.org/doi/10.14778/3611479.3611534
https://link.springer.com/article/10.1007/s00778-025-00921-z (journal version, 2025)
Benchmark is public: https://github.com/LiPengCS/Auto-Tables-Benchmark

Synthesizes multi-step transformation pipelines to turn non-relational tables
(pivoted, stacked, multi-header, wide) into relational form **with no user examples**.
- Benchmark **ATBench: 244 real test cases** from user spreadsheets, online forums,
  Jupyter notebooks, web tables.
- **~75% of cases solved in top-3; >70% at interactive speed, zero user input.**
- Baselines needing examples (Foofah, FlashRelate, SQLSynthesizer, Scythe) were *given
  100 ground-truth example cells* and still lost.
Microsoft Research. This is the strongest evidence that no-schema relationalization is
tractable — and also that **~25% will not relationalize automatically.** Plan a fallback.

### The structural reality — **PAPER** (quoted via 2025 survey/search snippet)
"**Less than 3% of tables in spreadsheet corpora have a pre-defined data model**" —
most are built for human viewing/reporting, with custom layouts. (Recurring statistic
in the spreadsheet-understanding literature; see TableSense
https://arxiv.org/pdf/2106.13500 for the table-detection formulation — CNN-based
spreadsheet table-boundary detection, Microsoft.)
→ Header detection + multi-table-per-sheet segmentation is a *first-class* pipeline
stage, not an edge case. Heuristics reported: all-string rows, bold/background
formatting, gap/empty-row separators assigning a Table ID per detected block.

Related 2025 work: RELATIONALCODER (ACL 2025)
https://aclanthology.org/2025.acl-long.89.pdf — rethinking complex tables by encoding
them as relational schemas. LlamaSheets (LlamaIndex, 2025) — hierarchical header
extraction + adaptive table segmentation, productised [**BLOG/VENDOR**].

### DuckDB-over-files as the ETL alternative — **BLOG / VENDOR**
https://motherduck.com/learn/no-etl-query-raw-files/ · https://motherduck.com/blog/pg-duckdb-release/

Honest labelling: the numbers below are **blog posts and MotherDuck (vendor) material**,
not peer-reviewed, and the comparisons are not equal-effort.
- DuckDB `read_csv_auto` / `read_xlsx` **auto-infer column names, types and dialect by
  sampling** — this is the no-schema-per-file path, built in.
- Claimed: Postgres ~25 s vs DuckDB ~8 s on 50M-row aggregation [BLOG, Medium].
- Claimed: 20 GB web logs, DuckDB ~15 s on a laptop vs Postgres ~3–5 min on a tuned
  server [BLOG].
- Claimed: "ETL into Postgres 3+ hours vs querying Parquet directly with DuckDB
  15 minutes" [BLOG].
- `pg_duckdb` 1.0 exists — DuckDB execution engine inside Postgres [VENDOR].
⚠️ All of these compare *analytical scan* workloads. They do NOT contradict our own
2500–5000x ETL-vs-open-400-xlsx measurement, which is about **avoiding re-parsing
per query**. DuckDB-over-Parquet is a third option that also avoids re-parsing (columnar,
already typed) — that IS a real design alternative and no one has published a fair
head-to-head against Postgres-with-ETL for our shape of workload.

### What breaks at thousands of tables in one Postgres — **DOCS** (authoritative)
https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/PostgreSQL.HighObjectCount.html

AWS's official thresholds table, with named impacts:
- **Relations (tables): approximate threshold "Millions."** So one-table-per-file at
  *thousands* is NOT where Postgres breaks. Named impacts as counts grow: autovacuum
  falling behind; `pg_attribute`/`pg_class`/`pg_depend` catalog growth degrading shared
  buffer efficiency; **major version upgrade / pg_dump-restore downtime scales with
  relation count** (pg_upgrade --link still restores all schema metadata);
  **file-descriptor exhaustion** ("out of file descriptors: Too many open files") driven
  by `max_files_per_process` × connections × tables-per-join; **inode exhaustion**.
- **Partitions: approximate threshold "Hundreds."** Pre-PG18, many partitions under load
  → `LWLock:LockManager` waits. So do NOT model files as partitions of one big table.
- Temporary tables: threshold "Tens of thousands", and they are **not autovacuumed** —
  they hold XIDs and bloat catalogs. Recommended: reuse + `TRUNCATE`, or use CTEs.
- Unlogged tables: threshold "Thousands" — truncated **serially** on crash recovery,
  adding significant startup/PITR downtime. Relevant if we were tempted to make ETL
  landing tables unlogged for speed. Don't, past ~1k.

Community corroboration of the failure mode [**MAILING LIST**, 2015 — older, may have
improved]: https://www.postgresql.org/message-id/20151030134646.GE5726%40alvherre.pgsql
— ~200,000 small tables → `pg_class` >½ million rows, **`pg_attribute` bloated to
>200 GB of mostly-empty pages**, new connections hanging in startup because catalogs no
longer fit in buffer cache. Raising autovacuum workers made it worse.

**Q2 verdict.** One-table-per-file is viable at thousands (AWS says relations scale to
"millions"), but the cost is paid at **upgrade/pg_dump time and in catalog cache
pressure**, not at query time. The 2015 horror story is at 200k tables — two orders of
magnitude past us. **No public measured comparison found** of table-per-file vs generic
EAV vs JSONB for this exact workload — that gap is real; nobody has benchmarked it.

---

## Q3 — Is document-level routing a named, measured pattern?

**Yes — two separate literatures, both with numbers, and neither is "RAG".**

### 1. Selective search / shard selection (IR, mature) — **PAPER**
- Kulkarni & Callan, *Selective Search: Efficient and Effective Search of Large Textual
  Collections*, **ACM TOIS 33(4), 2015** — https://dl.acm.org/doi/10.1145/2738035
- Kim, Callan, Culpepper, Moffat, *Efficient distributed selective search*,
  **Information Retrieval Journal 2016/17** —
  https://www.cs.cmu.edu/~callan/Papers/IRJ16-yubink.pdf ·
  https://people.eng.unimelb.edu.au/ammoffat/abstracts/kccm17irj.pdf
- *Selective Search as a First-Stage Retriever*, **2025** —
  https://link.springer.com/chapter/10.1007/978-3-032-04354-2_2 (recent revival)
- MICO: Selective Search with Mutual Information Co-training —
  https://arxiv.org/pdf/2209.04378

This is precisely route-then-search, formalised 10+ years ago: partition the corpus
topically into **50–1,000 shards**, then a **resource-selection** algorithm
(**ReDDE, Rank-S, Taily**) ranks shards AND estimates the cutoff — how many shards to
actually open. Headline finding: **selective search matches exhaustive search on early
precision while touching a small fraction of the corpus**, and "delivers markedly better
performance characteristics than exhaustive search in all configurations investigated."
Also: ShRkC, shard rank *cutoff prediction*
https://link.springer.com/chapter/10.1007/978-3-319-23826-5_32 — a learned "how many to
open" model, which is exactly the knob a route→open system needs.
⚠️ Ages: 2015-2017 core, 2025 revival. Numbers are lexical-era (no dense retrieval).
The *architecture* transfers; the specific effectiveness numbers should not be quoted
as current.

### 2. TARGET (2025) — the modern, table-specific measurement
Covered under Q1. Key: **dense >> BM25 for table retrieval**, headers+rows is the best
representation, descriptive titles help. This is the closest published benchmark to our
routing task.

### Honest gap
"Retrieve-then-read" (retriever→reader) is of course canonical since DrQA (2017), but
**one-vector-per-whole-file routing followed by opening the file and computing** is not
a cleanly named, separately benchmarked pattern in the RAG literature. Much of what
search surfaces under "document-level embedding" / "coarse-to-fine RAG" is
AI-generated aggregator content (emergentmind.com, chatnexus.io) with **no primary
measurements** — I found one such page recommending "embed the first 300 characters of
each document" as the document representation, with no evaluation behind it. Treat as
worthless. **The rigorous evidence is selective search (old, lexical) and TARGET
(new, tables).**

---

## Q4 — Fusion when the legs are unequal

### The result we measured is KNOWN, and it has a name: the **weakest-link effect**
- **Balancing the Blend: An Experimental Analysis of Trade-offs in Hybrid Search**,
  arXiv **2508.01405**, submitted **August 2025** — https://arxiv.org/abs/2508.01405
  [**PAPER**]. Three stated findings:
  1. **"Weakest link" phenomenon** — "a low-accuracy path can **pollute the candidate
     pool** with irrelevant documents, causing the hybrid system to **underperform its
     best constituent path**." They call for **path-wise quality assessment before
     fusion**.
  2. A data-driven map of trade-offs: the optimal configuration **depends on resource
     constraints and data characteristics** — i.e. there is no default hybrid setting.
  3. **Tensor-based Re-ranking Fusion (TRF)** identified as a **high-efficacy alternative
     to mainstream fusion methods** (i.e. to RRF).
  This paper is the citation for our result. Our hybrid-hurts observation is a
  *reproduction* of a published 2025 finding, not a local quirk.
- Practitioner statement of the same mechanism [**BLOG**]:
  https://srcecde.me/posts/reciprocal-rank-fusion-rrf/ ·
  https://dev.to/aws-builders/reciprocal-rank-fusion-rrf-how-it-works-and-when-to-skip-it-4obi
  — "plain RRF gives every retriever an **equal vote**. If your dense retriever is
  genuinely good and BM25 is just along for the ride, a weak ranker still gets an equal
  say and can pull the result down."

Root cause, stated precisely and worth internalising: **in RRF the input scores are never
read.** RRF consumes *ranks only*. So a leg that is confidently wrong contributes exactly
as much as a leg that is confidently right, and the fusion cannot tell the difference.
That is why our Gemma-dense 8/8 → hybrid 5/8 collapse is the expected behaviour, not an
anomaly.

### Remedies, in the order the evidence supports
1. **Don't fuse when one leg dominates.** Explicitly endorsed by the "when to skip it"
   material above. Our data (8/8 vs hybrid 5/8; concept-routing MRR 0.751/0.750 → 0.532)
   is a textbook instance.
2. **Weighted RRF** — supported natively in Elasticsearch [**DOCS/VENDOR**]:
   https://www.elastic.co/search-labs/blog/weighted-reciprocal-rank-fusion-rrf ·
   https://www.elastic.co/docs/reference/elasticsearch/rest-apis/reciprocal-rank-fusion
   Per-retriever weight. Elastic themselves call it a **partial** fix — α is a single
   knob that must be **tuned on a labelled set**, and it still discards score magnitude.
3. **Score-based (convex / relative-score) fusion instead of rank fusion** — keeps
   magnitude, so a confident leg can outvote an unconfident one. Pneuma uses exactly this
   (`s = α·lexical + (1−α)·semantic`) rather than RRF.
4. **Dense → rerank, skipping fusion entirely.** Matches our own finding that the LLM
   reranker was 4/4 on everything retrieval surfaced and abstained correctly 4/4 —
   i.e. our ceiling is recall, and a second weak *recall* leg that reorders the strong
   leg's list is strictly harmful. Pneuma's own design agrees in spirit: it fuses, then
   applies an **LLM Judge filter** over the union.

Also seen: score-normalization discussion for RRF [**BLOG**]
https://avchauzov.github.io/blog/2025/hybrid-retrieval-rrf-rank-fusion/ and a 2026
comparison of rank fusion vs **projection fusion** with diversity reranking on COVID-19
literature [**PAPER**] https://arxiv.org/pdf/2604.13728 — relevant if we revisit fusion
later, not needed to act now.

---

## Q5 — Postgres retrieval stack at millions of rows

⚠️ **Provenance warning up front: this area is almost entirely vendors benchmarking
their own products.** I found no neutral, well-tuned, third-party head-to-head. Treat
every number below as a marketing artifact unless labelled otherwise.

### Vectors: pgvector HNSW vs pgvectorscale (DiskANN)
- **Tiger Data / Timescale (pgvectorscale's OWN VENDOR)** —
  https://www.tigerdata.com/blog/pgvector-vs-qdrant
  🚩 **VENDOR-BENCHMARK-ON-OWN-PRODUCT.** Claims: **471 QPS at 99% recall on 50M
  vectors, 11.4× Qdrant's 41 QPS** (May 2025); **p95 latency 28× lower than Pinecone
  s1 at 99% recall**. Method: DiskANN + **Statistical Binary Quantization**, vectors on
  disk. Competitor tuning is not independently verified. Note also pgvectorscale is
  **Timescale License, not OSI open source** — a real adoption constraint.
- **Counter-evidence from pgvectorscale's own issue tracker** [**GITHUB ISSUE** — the
  most useful item here for us]: *"Poor recall/throughput perf vs. pgvector on
  small/low-dimension datasets"*
  https://github.com/timescale/pgvectorscale/issues/116
  → **pgvectorscale can be WORSE than plain pgvector at small scale.** We are at
  thousands of vectors. This is disqualifying for us.
- **Independent-ish practitioner analysis** [**BLOG**, Christophe Pettus / The Build]
  https://thebuild.com/blog/on-pgvectorscale-and-hybrid-search-without-an-elasticsearch-sidecar/
  The clearest decision rule found: *"If your vector index fits comfortably in
  shared_buffers plus filesystem cache and your throughput is fine, **stay on pgvector
  HNSW**. It is structurally faster than DiskANN when the whole graph is RAM-resident —
  DiskANN always pays at least one disk read at the rerank step."* pgvectorscale only
  becomes interesting when the index does not fit in RAM.
- Scale threshold repeatedly cited [**BLOG**]: plain pgvector HNSW "starts to slow down
  noticeably above **5–10M vectors**"; **50M × 768d ≈ 150 GB+ RAM** for the HNSW graph.

**For us this is settled and the answer is boring: one vector per file × thousands of
files = ~10⁴ vectors ≈ tens of MB. pgvector HNSW is enormously over-provisioned for
that. None of the scale literature applies unless we go back to chunking.**

### Full text: tsvector+GIN vs ParadeDB pg_search (Tantivy)
The real, non-controversial technical point (and it is a *correctness* point, not a
speed one): **Postgres `ts_rank` is not BM25.** `ts_rank` scores **every matching row**
before TopK; Tantivy uses **block-max-WAND** with per-block score upper bounds and skips
blocks that cannot reach the top-K. That is an algorithmic difference, not tuning.

Numbers — all from parties with an interest:
- 🚩 **ParadeDB on ParadeDB**: https://www.paradedb.com/blog/benchmarker-iteration ·
  https://www.paradedb.com/blog/elasticsearch-vs-postgres — "**20× faster ranking than
  tsvector on a 1M-row table**".
- **Tembo** (a Postgres vendor, not ParadeDB — semi-independent) [**VENDOR, third-party**]
  https://legacy.tembo.io/blog/paradedb-search/ — ParadeDB answered a comparable query in
  **6.28 ms, ~265× faster** than Postgres FTS.
- **Independent blog** https://www.vineeth.fyi/blog/pg-vs-pg-search/ — **GIN index build
  282.93 s vs pg_search 44.72 s**; vanilla PG needed **11 indexes** (GIN + pg_trgm for
  fuzzy) vs **1 BM25 index** for ParadeDB. pg_search's index is a **covering** index, so
  boolean filters + facets + FTS push down into one index scan instead of post-filtering.
- 🚩 **Counter-claim from a competing vendor (VectorChord)**:
  https://blog.vectorchord.ai/postgresql-full-text-search-fast-when-done-right-debunking-the-slow-myth
  — argues Postgres FTS is fast **when done right** and the slowness is a myth. Note
  VectorChord ships `vchord_bm25`, so they too have a horse in this race.
- Availability signal: **pg_search is now on Neon** https://neon.com/blog/pgsearch-on-neon
  — i.e. it is escaping the ParadeDB distro, which was previously a real blocker.

**For us: given TARGET's finding that dense >> BM25 on tables, and our own finding that
the lexical leg is not the routing lever, investing in a Tantivy-grade BM25 would be
optimising the leg that does not decide our outcomes.** ArcadeDB's Lucene FTS is already
a real BM25 implementation, which covers the correctness gap without a Postgres extension.

### Mixed-language (`simple` vs a stemmer)
**Honest answer: no rigorous public measurement found** of the retrieval-quality cost of
`simple` vs a correct stemmer on a mixed-language corpus. This is folklore territory.
What is documented is only the mechanism (per-row `to_tsvector(lang_col, body)`,
multiple tsvector columns, `unaccent`, ICU, pg_trgm fallback, language detection at
ingest). If we need an answer here we will have to measure it ourselves; do not expect
to find it published.

---

## Q6 — EmbeddingGemma-300M in production

### Official spec — **DOCS / VENDOR (Google marketing its own model)**
https://huggingface.co/blog/embeddinggemma · https://ai.google.dev/gemma/docs/embeddinggemma/model_card
https://developers.googleblog.com/en/introducing-embeddinggemma/

- **308M params**, output **768d**, **context 2,048 tokens**, "**under 200 MB of RAM when
  quantized**".
- **Pooling is MEAN.** "A mean pooling layer converts these token embeddings into text
  embeddings." ⚠️ Note for us: our house note says *pooling LAST* — that is correct for
  **Qwen3-Embedding**, and **WRONG for EmbeddingGemma**. Do not carry the setting across.
- MTEB scores at full precision: **61.15 Multilingual v2**, **69.67 English v2**,
  **68.76 Code v1**. Claim: "highest-ranking open multilingual text embedding model
  under 500M on MTEB." ⚠️ **This is Google benchmarking its own model** — but the MTEB
  leaderboard is third-party hosted, which partially mitigates.

### Prompt prefixes — EXACT strings (this is easy to get silently wrong)
| Task | Literal prefix |
|---|---|
| Query (retrieval) | `task: search result \| query: ` |
| **Document (corpus side)** | `title: none \| text: ` |
| Classification | `task: classification \| query: ` |
| Clustering | `task: clustering \| query: ` |
| STS | `task: sentence similarity \| query: ` |
| Code retrieval | `task: code retrieval \| query: ` |

Also defined: Reranking, BitextMining, Summarization, PairClassification variants.
Note the **asymmetry**: the document side is NOT `task: … | query:` — it is the
`title: none | text: ` form (or `title: <actual title> | text: `). If a title exists,
pass it. **llama.cpp does not apply these for you** — the caller must prepend them.
**No public measurement found** quantifying the loss from omitting/mismatching prefixes;
that number does not appear to exist. Worth measuring ourselves — it is cheap.

### Matryoshka 768 → 512 / 256 / 128
- Confirmed supported: truncate then **re-normalize** ("MRL allows users to truncate the
  output embedding of size 768 to their desired size and then re-normalize").
- ⚠️ **No per-dimension MTEB table found** in the HF blog, the Google model card, or the
  developers blog. The vendor states the capability and omits the cost. **Honest answer:
  no public evidence found for how much recall truncation costs on EmbeddingGemma
  specifically.** Anyone claiming "256d is free" is extrapolating from the general MRL
  paper, not from this model.
- At our scale (thousands of files, one vector per file) the storage saving from
  truncation is **negligible** — 5,000 files × 768 × 4 B ≈ 15 MB. There is no reason to
  truncate and take an unmeasured recall risk. **Stay at 768.**

### llama.cpp gotchas — **GITHUB ISSUES**
- Issue **#19040** "gemma embedding model accuracy issue"
  https://github.com/ggml-org/llama.cpp/issues/19040 — user reports large cosine distance
  between llama.cpp GGUF output and the HF/transformers reference, on both F32 and Q8_0
  unsloth GGUFs. **Status: `bug-unconfirmed`, no measured numbers, no fix version.**
  Notable detail in the log: the user passed `--pooling cls` and llama.cpp warned
  *"model default pooling_type is [1], but [2] was specified"* — i.e. the model's default
  is MEAN and the user overrode it to CLS. **A meaningful part of this report may be
  operator error, not a llama.cpp bug.** Do not treat as a confirmed defect; DO treat as
  a warning to never override pooling.
- 🔴 **DENSE MODULES IN GGUF — the highest-value finding in this whole section.**
  llama.cpp supports sentence-transformers **dense modules only if they are present in
  the GGUF**, and in `convert_hf_to_gguf.py` **"by default these modules are NOT
  included."** Sources state plainly: *"without the dense modules included, you won't get
  correct embeddings from sentence-transformers models"* — and `google/embeddinggemma-300m`
  is named as exactly such a model.
  https://github.com/ggml-org/llama.cpp/blob/master/convert_hf_to_gguf.py ·
  https://huggingface.co/google/embeddinggemma-300m/discussions/22 ·
  https://huggingface.co/sabafallah/embeddinggemma-300m-sentence-transformers-gguf
  (a prebuilt GGUF that *does* carry all dense modules)
  **This fails silently: no crash, no warning, just degraded vectors.** It is also a
  plausible explanation for issue #19040's cosine mismatch. **Action: verify our GGUF
  carries the dense modules; if not, re-convert with the flag or switch to the
  sentence-transformers GGUF and re-embed.** Our corpus is thousands of files — re-embedding
  is cheap, and if this is wrong then our 8/8 result is a floor, not a ceiling.
  Relevant repos: https://huggingface.co/ggml-org/embeddinggemma-300M-GGUF ·
  https://huggingface.co/ggml-org/embeddinggemma-300m-qat-q8_0-GGUF ·
  https://huggingface.co/unsloth/embeddinggemma-300m-GGUF
- Issue **#21256**: `--rerank` and `--embedding` together break embeddings in llama-server
  router mode. https://github.com/ggml-org/llama.cpp/issues/21256 — relevant if we ever
  colocate reranker + embedder on one llama-server.

### Competitive position — **THIRD-PARTY ROUNDUPS (blog quality)**
https://www.bentoml.com/blog/a-guide-to-open-source-embedding-models (2026) ·
https://www.morphllm.com/ollama-embedding-models (2026)
Granite Embedding Multilingual R2 [**PAPER**] https://arxiv.org/pdf/2605.13521 is a
current small-multilingual competitor worth an A/B if we ever revisit.
**Our own 2/8 → 8/8 measurement is stronger evidence for our corpus than any leaderboard
mean.** MTEB Multilingual v2 = 61.15 tells us it is a credible multilingual model; it
does not tell us anything about Italian business spreadsheets.

---

## What this changes for a system that has already decided route → open → compute

Only the findings that would actually alter a design decision. Everything else above is
confirmation.

**1. Check the GGUF dense modules before anything else.** `convert_hf_to_gguf.py` omits
sentence-transformers dense modules **by default**, and without them EmbeddingGemma
"won't give correct embeddings" — silently. If our GGUF is missing them, every dense
number we have measured is a floor taken with a partially-lobotomised model, and the
cheapest available quality win is a re-convert plus a re-embed of a few thousand
vectors. This is a one-hour check that could invalidate or improve a headline result.

**2. Stop fusing. This is now citable, not just local.** arXiv 2508.01405 names our
exact failure — the *weakest-link effect*, where a weak path pollutes the candidate pool
and the hybrid **underperforms its best constituent path**. The mechanism is that RRF
reads ranks and never reads scores, so a confidently-wrong leg gets an equal vote. Our
8/8 → 5/8 and MRR 0.751 → 0.532 are textbook. Design consequence: **dense → LLM rerank,
no fusion.** If lexical ever comes back, it must be as weighted *score* fusion with α
tuned on a labelled set, never equal-weight RRF.

**3. The card is wrong in a fixable way, and the fix is ordered by measured effect.**
Our 80 deduped values, ASCII-sorted, globally capped, is the one component with no
support anywhere in the literature. Replace with, in priority order:
   - an **LLM-generated descriptive title/summary** per file — largest single measured
     effect found (**+0.42 recall@3**, Table Serialization Kitchen on TARGET; TARGET
     independently reports descriptive titles improve retrieval);
   - **LLM column narrations** (Pneuma: 81.0% vs 67.9% where headers are meaningful);
   - **~5 whole sampled rows serialized as `header: value`**, not bare values (Pneuma
     default r=5; TSK shows a plateau by ~10);
   - if bare values are kept at all, select **per-column by frequency** (the MCV model
     Postgres itself uses, top-N by frequency + a disjoint histogram for the tail), never
     globally alphabetically.

**4. Card size is bounded by the embedder, and ours is a short-context model.**
EmbeddingGemma's context is **2,048 tokens**. TSK measured that short-context embedders
get *worse* as you add rows while long-context ones keep improving. So "make the card
richer" has a hard ceiling for us — richer must mean *better-selected*, not *bigger*.
That reframes the whole card question from "how much can we fit" to "what earns its place".

**5. Our BM25 card-vs-chunk tie was measuring the leg that doesn't matter.** TARGET
states plainly that for *table* retrieval "dense embedding-based retrievers far
outperform a BM25 baseline, which is less effective than it is for retrieval over
unstructured text." Our MRR 0.903 vs 0.917 finding is real but was a comparison inside
the weak leg. **Re-run the card-vs-chunk comparison on the dense leg** — that is where
the routing decision is actually made, and we have not tested it there.

**6. Fix the document-side prefix.** The corpus-side string is `title: none | text: `,
**not** a `task: … | query: ` form, and llama.cpp does not apply it for us. If we have a
filename or sheet title, it belongs in the `title:` slot rather than being discarded.
No published number exists for the cost of getting this wrong — but it is free to fix.

**7. Do not truncate the embeddings.** Google documents MRL 768→512/256/128 and
**publishes no per-dimension quality table**. At one vector per file, thousands of files,
768d costs ~15 MB. There is nothing to buy and an unmeasured recall risk to pay.

**8. Do not adopt pgvectorscale, and do not buy a Tantivy-grade BM25 yet.** Both are
answers to scale problems we do not have: pgvectorscale's own issue tracker reports it
**underperforming plain pgvector at small/low-dimension scale**, and the honest decision
rule is "if the index fits in RAM, stay on pgvector HNSW." At ~10⁴ vectors we are three
orders of magnitude below where any of that literature starts. ArcadeDB's Lucene already
gives real BM25 if we want correct lexical scoring.

**9. One-table-per-file is safe at our scale but has a named tail cost.** AWS puts the
relation threshold at "**millions**", so thousands is fine at query time — but the cost
lands on **pg_dump/pg_restore and major-version upgrade downtime**, plus catalog cache
pressure, plus `max_files_per_process` exhaustion on wide joins. Two things to avoid
outright: **do not model files as partitions** (AWS threshold "hundreds", `LWLock:LockManager`
waits pre-PG18) and **do not make landing tables unlogged** past ~1k (serial truncation
on every crash recovery). There is **no published benchmark** of table-per-file vs EAV vs
JSONB for this workload — if that choice matters to us, nobody has done it for us.

**10. Budget for ~25% of spreadsheets not relationalizing automatically.** Auto-Tables
(Microsoft, VLDB) is the state of the art at no-example relationalization and reaches
**~75% top-3 on 244 real cases**. Combined with the finding that **<3% of real
spreadsheets have a predefined data model**, the ETL path needs an explicit
"could-not-relationalize" branch that falls back to route→open→compute-in-code. That
fallback is not a degradation of the design — it *is* the design, and it is the reason
route→open→compute should stay even after ETL exists.

**11. Steal the shard-cutoff idea from selective search.** Route→open is the same
architecture as selective search's shard ranking, which has a 10-year literature
(Kulkarni & Callan, TOIS 2015; ReDDE / Rank-S / Taily; ShRkC for *cutoff prediction*).
The transferable part is not the effectiveness numbers — those are lexical-era — but the
insight that **"how many to open" should be predicted per query, not fixed at k**.


