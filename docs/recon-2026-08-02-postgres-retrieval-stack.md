# Recon: Postgres retrieval stack at scale — measured numbers

Date: 2026-08-02. Web reconnaissance only. Live-appended during research.

Evidence labels: **[PAPER]**, **[VENDOR-BENCH-OWN-PRODUCT]** (vendor benchmarking its own product — treat as advocacy),
**[BLOG-POST]**, **[DOCS]**, **[INDEPENDENT-BENCH]**.

Calibration note for our regime: **thousands of documents, one card row per file, ~768d**. That is 10^3–10^4 rows,
i.e. **three to four orders of magnitude below** every benchmark below. Numbers at 1M/10M/50M/100M are *context for
where the cliff is*, not predictions for us. Explicitly flagged inline.

---

## A) pgvector HNSW — the one benchmark set that is complete and reproducible

### A1. Jonathan Katz, "The 150x pgvector speedup: a year-in-review"
URL: https://jkatz05.com/post/postgres/pgvector-performance-150x-speedup/
Label: **[BLOG-POST]** by a **pgvector maintainer** (Jonathan Katz is a pgvector contributor and AWS PG person).
Not a neutral third party, but it is *pgvector vs older pgvector* — no competitor is being beaten, so the
vendor-bias failure mode mostly does not apply. Uses the ANN-Benchmarks harness datasets. **2024 vintage** —
pgvector 0.8.x has landed since (see A2), so these are a floor, not a ceiling.

Harness: PostgreSQL 16.2. HNSW `m=16, ef_construction=256`. IVFFlat `lists=1000`.
`ef_search` swept over [10, 20, 40, 80, 120, 200, 400, 800]. **Recall is held equal** across rows (99% or 90%),
which is the correct way to compare — good sign for methodology.

**`dbpedia-openai-1000k-angular` — 1M rows @ 1536d, @99% recall, r7gd.16xlarge (64 vCPU / 512 GB RAM):**

| pgvector | Index | QPS | p99 (ms) | Build (s) | Index size (GiB) |
|---|---|---|---|---|---|
| 0.4.1 | IVFFlat | 8 | 150.16 | 474 | 7.56 |
| 0.5.0 | HNSW | 243 | 5.74 | **7479** | 7.55 |
| 0.7.0 | HNSW | 253 | 5.51 | **250** | 7.55 |
| 0.7.0 | HNSW + SQ16 (halfvec) | 263 | 5.30 | 146 | 3.78 |
| 0.7.0 | HNSW + BQ (binary) | 236 | 5.40 | **49** | **0.46** |

Same dataset on r7i.16xlarge: 0.4.1 IVFFlat 8 QPS / 153.01 ms p99 / 496 s; 0.7.0 HNSW 255 QPS / 5.40 ms / 388 s /
7.55 GiB; 0.7.0 HNSW-BQ 267 QPS / 4.77 ms / 66 s / 0.46 GiB.

**Other ANN-Benchmarks datasets, same harness (all 1M-ish rows):**

- `sift-128-euclidean` (128d) @99% recall: 0.4.1 IVFFlat 33 QPS / 44.05 ms p99 / 58 s build →
  0.7.0 HNSW **487 QPS / 2.65 ms p99 / 56 s**; HNSW-SQ16 482 QPS / 2.68 ms / 48 s.
- `gist-960-euclidean` (960d — closest to our 768d) @**90%** recall (note: not 99%, the harness could not hit
  99% economically at 960d): 0.4.1 IVFFlat 13 QPS / 128.91 ms p99 / 300 s → 0.7.0 HNSW **229 QPS / 5.18 ms p99 /
  197 s build**.
- `glove-25-angular` (25d) @99%: 0.4.1 IVFFlat 26 QPS / 53.50 ms → 0.7.0 HNSW **522 QPS / 2.50 ms / 48 s**.

Headline deltas 0.4.1→0.7.0: **31.6x QPS**, **28.2x p99**, **150x index build** (that last one is
0.5.0 HNSW 7479 s → 0.7.0 HNSW-BQ 49 s, i.e. it bundles parallel build *and* binary quantization — the
apples-to-apples parallel-build-only number is 7479 s → 250 s = **~30x**).

**Read-through for our regime:** at 1M x 1536d a full HNSW build is 250 s and the index is 7.55 GiB. Linear-ish
scaling down puts **10k rows x 768d at roughly a 1–3 second build and an index in the tens of MB.** Build time
and index size are simply not a design constraint at our scale. Latency at 1M is already 5 ms p99; at 10k rows
the index is a couple of HNSW layers and the query is sub-millisecond. **The interesting question at our scale is
not speed, it is recall and ranking quality** — HNSW at 10k rows with default `ef_search=40` is near-exhaustive.

Corollary worth stating plainly: **at ~10k rows a flat/exact scan is competitive.** 10k x 768d x 4 bytes = ~30 MB,
which a single core scans in low tens of milliseconds. An index buys you latency you may not need and costs you
recall you cannot easily audit.

### A2. pgvector 0.8.x — what changed after the Katz post

URLs: https://www.postgresql.org/about/news/pgvector-080-released-2952 · https://github.com/pgvector/pgvector
· https://docs.pgedge.com/pgvector/v0-8-0/iterative-index-scans/ — Label: **[DOCS]**. Released **2024-10-30**.

The headline 0.8.0 feature is **iterative index scans**, and it is the one that matters most for a multi-tenant
design. Pre-0.8.0, an ANN index scan returned its `ef_search` candidates and *then* the `WHERE tenant_id = ?`
filter was applied — so a selective filter could "overfilter" and return **fewer rows than the `LIMIT` asked for**,
silently. 0.8.0 adds `hnsw.iterative_scan` / `ivfflat.iterative_scan`, which keep walking the index until enough
rows survive the filter or a cap is hit (`hnsw.max_scan_tuples`, `ivfflat.max_probes`).

**This is directly load-bearing for us.** A per-tenant filter over a shared table is exactly the overfiltering
case. Two mitigations, and we should pick deliberately: (a) turn on `hnsw.iterative_scan = relaxed_order`, or
(b) partition/scope so the vector index is not shared across tenants. 0.8.0 also improved the planner's cost
estimate for ANN indexes so PG can correctly prefer a B-tree when the filter is very selective — which at
**thousands of rows per tenant is very likely the right plan anyway**.

Also in 0.8.0: faster HNSW scans, faster HNSW inserts, faster on-disk builds, array→`sparsevec` casts.
Latest in the 0.8.x line is **0.8.1** (https://api.pgxn.org/src/vector/vector-0.8.1/CHANGELOG.md).

**There is no pgvector 0.9.** Verified against the upstream changelog
(https://github.com/pgvector/pgvector/blob/master/CHANGELOG.md — **[DOCS]**). The line is 0.8.x and the
**latest is 0.8.6, released 2026-07-29** — four days before this recon.

**⚠️ Version-pinning finding, and it is the sharpest practical item in this report.** The 2026 point releases are
almost entirely **HNSW vacuum and memory-safety bug fixes**:

| Version | Date | Fix |
|---|---|---|
| 0.8.1 | 2025-09-04 | PostgreSQL 18 support; faster binary quantization |
| 0.8.2 | 2026-02-25 | **Buffer overflow in parallel HNSW index build** |
| 0.8.3 | 2026-06-17 | **Potential index CORRUPTION during HNSW vacuuming** |
| 0.8.4 | 2026-06-30 | HNSW vacuuming errors and memory usage |
| 0.8.5 | 2026-07-08 | IVFFlat build memory on small tables |
| 0.8.6 | 2026-07-29 | Buffer overflow in IVFFlat build on 32-bit; array cast fix |

**Pin ≥ 0.8.4, and prefer 0.8.6.** A pgvector between 0.8.0 and 0.8.2 can corrupt an HNSW index during vacuum —
i.e. the exact autovacuum path that runs unattended in production. This also reframes §E2: HNSW vacuum
interaction was not merely a bloat annoyance, it was an active correctness bug being fixed as recently as
six weeks ago. The extension is mature in its API and **still stabilizing in its storage layer**.

### A3. pgvectorscale / StreamingDiskANN — claimed multiples and who measured them

URL: https://www.tigerdata.com/blog/pgvector-vs-qdrant (and the Medium mirror)
Label: **[VENDOR-BENCH-OWN-PRODUCT]** — ⚠️ **Timescale/Tiger Data benchmarking pgvectorscale, which Timescale
wrote, against Qdrant, a competitor.** Read as advocacy.

Claimed, at **50M vectors @ 768d, 99% recall**:
- Throughput: **471.57 QPS (pgvector+pgvectorscale) vs 41.47 QPS (Qdrant) = 11.4x**.
- Latency, where Qdrant *wins* and Timescale says so: p50 30.75 vs 31.07 ms (Qdrant 1% better),
  **p95 36.73 vs 60.42 ms (Qdrant 39% better)**, **p99 38.71 vs 74.60 ms (Qdrant 48% better)**.

**Fairness read:** the fact that they publish the p95/p99 losses is a point in their favour — this is not a
sanitized vendor chart. But note the shape of the claim: they win on *aggregate throughput* and lose on
*tail latency*. Those are different products. A RAG system serving one user's query cares about p95, not QPS.
The 11.4x is the number that gets quoted and it is the least relevant one for interactive retrieval.

Related and even more explicitly promotional: https://www.tigerdata.com/blog/pgvector-is-now-as-fast-as-pinecone-at-75-less-cost
— **[VENDOR-BENCH-OWN-PRODUCT]**, "28x faster than Pinecone" claims. Same caveat, stronger.

The genuine, non-contested engineering claim: **HNSW must fit in RAM to perform; StreamingDiskANN does not.**
Timescale's own sizing: **50M x 768d HNSW ≈ 150 GB+ RAM.** StreamingDiskANN keeps most of the index on SSD and
adds **SBQ (Statistical Binary Quantization)**, claimed more accurate than plain BQ at the same compression.

**No fair independent benchmark of pgvectorscale vs tuned pgvector 0.8.x exists that I could find.** The
comparisons in circulation are Timescale's own, and several of them predate pgvector 0.7/0.8 parallel builds and
quantization — i.e. they beat a pgvector that no longer exists. Treat the multiples as unverified.

Licensing — this matters and the search summaries get it wrong: pgvectorscale's own README says
"Postgres OSS licensed" (https://github.com/timescale/pgvectorscale), but **Timescale historically ships
dual-licensed code and the Timescale License (TSL) is not OSI-approved**. The repo LICENSE file
(https://github.com/timescale/pgvectorscale/blob/main/LICENSE) is the only authority — **verify it directly
before adopting; do not trust a blog's "OSS" adjective.** Company rebranded Timescale → **Tiger Data (June 2025)**,
repositioning from time-series to "fastest PostgreSQL". **No evidence of abandonment** — releases and issues
are active.

**Read-through for our regime: pgvectorscale is irrelevant to us.** Its entire value proposition is
"your HNSW index no longer fits in RAM." At thousands of rows x 768d our index is tens of megabytes. Adopting it
would buy nothing and cost us a non-OSI license question, a Rust build dependency, and a managed-Postgres
availability problem (see §C).

---

## B) Full-text

### B1. ⚠️ The most important finding in this whole recon: the tsvector baseline is usually rigged

URL: https://blog.vectorchord.ai/postgresql-full-text-search-fast-when-done-right-debunking-the-slow-myth
Label: **[VENDOR-BENCH-OWN-PRODUCT]**, but *inverted* — VectorChord sells a BM25 extension and this post argues
**native Postgres FTS is fast**, i.e. it argues against its own commercial interest. That makes it unusually
credible. It is an explicit rebuttal of Neon's "Performance Benchmark: pg_search on Neon" post, on the grounds
that the "standard Postgres" baseline was **"unintentionally handicapped."**

Setup: **10M log rows**, AWS EC2 `i7ie.xlarge` (4 vCPU, local NVMe), PostgreSQL 16 in Docker,
`shared_buffers=8GB`, `maintenance_work_mem=8GB`.

| Configuration | Query latency |
|---|---|
| Naive native FTS (as benchmarked by vendors) | **41,301 ms** |
| Native FTS, done correctly | **877 ms** |

**~50x, from two changes and nothing else:**
1. **Materialize the tsvector into a stored column** instead of calling `to_tsvector()` in the `WHERE` clause.
   Computing `to_tsvector()` per row at query time makes the GIN index unusable and turns the query into a
   full-table parse. This is the single biggest FTS mistake and it is *the* thing vendor benchmarks exploit.
2. **`WITH (fastupdate = off)`** on the GIN index.

**Consequence: every "Postgres FTS is 265x slower" number below must be read against this.** If the vendor
baseline computed `to_tsvector()` at query time, the honest multiple is not 265x, it is 265/50 ≈ **5x** — and
possibly less.

### B2. The GIN cliffs, named

Label: **[DOCS]** + corroborating blog posts
(https://danielabaron.me/blog/speed-up-pg-fts-with-persistent-ts-vectors/ ·
http://pcode.hu/posts/optimizing-postgresql-full-text-search/).

1. **GIN pending list / `fastupdate`.** GIN's `fastupdate=on` (the **default**) buffers new entries in an
   unsorted pending list because a single tsvector INSERT can mean hundreds of key insertions. Every *search*
   must then linearly scan that pending list. Under sustained writes the list grows and query latency degrades
   *progressively and invisibly*. Fixes: `fastupdate=off` (slower writes, compact index, no pending scan),
   aggressive autovacuum on the table, or periodic `gin_clean_pending_list()`.
   **For us:** our write pattern is ingest-then-read, low write rate. `fastupdate=off` is the correct default
   and costs us nothing.
2. **`ts_rank` is not indexable.** The GIN index answers *matching* (`@@`), never *ranking*. `ts_rank` /
   `ts_rank_cd` are executed as a filter/sort over **every row that matched**, so cost scales linearly with
   match-set size, not with `LIMIT`. A broad query that matches 200k rows pays for 200k rank computations to
   return 10. This is the structural reason pg_search/BM25 extensions win on *ranked* queries specifically.
   **For us:** with thousands of docs the entire match set is small, so this cliff is far away.
3. **`ts_rank` is not BM25** — see B4.
4. **Phrase search (`<->`, `phraseto_tsquery`)** requires positional data, so it cannot be answered from the
   index alone; it needs a recheck against the tsvector, which is materially more expensive than plain `@@`.

### B3. ParadeDB pg_search (Tantivy-in-Postgres) — the numbers, and who ran them

URLs: https://www.paradedb.com/blog/introducing-search · https://www.paradedb.com/blog/elasticsearch-vs-postgres
· https://www.paradedb.com/blog/benchmarker-iteration
Label: **⚠️ [VENDOR-BENCH-OWN-PRODUCT]** — ParadeDB benchmarking ParadeDB against native Postgres and
Elasticsearch. Reported figures:

| Metric | Postgres tsvector+GIN | ParadeDB pg_search | Claimed multiple |
|---|---|---|---|
| Index build (~1M rows) | 282.93 s | 44.72 s | 6.3x |
| Index build, **100M rows** | 58 min 24 s | 19 min 42 s | 3.0x |
| Top-10 ranked query | **1.66 s** | **6.28 ms** | **265x** |
| Ranked query, 1M rows | — | — | "20x" |
| Indexes needed for FTS+fuzzy | 11 (GIN + pg_trgm) | 1 BM25 index | — |

**Fairness verdict: the 265x is not credible as stated.** A 1.66 s top-10 on a table where the tsvector is
materialized and `fastupdate=off` is set does not happen — B1 shows a *10M-row* correctly-configured query at
877 ms. The 1.66 s figure is consistent with a query-time `to_tsvector()` and/or unbounded `ts_rank` over a
large match set. I could not find a ParadeDB benchmark that states the baseline used a **stored, indexed**
tsvector column. **Assume the real multiple is single-digit to low-double-digit for ranked queries, and near
1x for pure `@@` matching.**

The **index-build** numbers are more believable (build time is harder to rig) and the **"11 indexes vs 1"**
operational point is a real, non-numeric advantage.

The 100M-row column is **four orders of magnitude above our regime** — pure context.

### B4. `ts_rank` is not BM25 — what that actually costs

Label: **[DOCS]** + **[BLOG-POST]**. Postgres `ts_rank`/`ts_rank_cd` weight by term frequency and by the
`setweight()` A/B/C/D labels. It has **no IDF term** (a word's rarity across the corpus does not raise its score)
and **no proper document-length normalization** (BM25's `b`/`avgdl`). Practical consequences: common words
contribute as much as rare ones, so a query's discriminating term does not dominate; and long documents are not
penalized consistently. `ts_rank` also has a `normalization` bitmask that can divide by document length, but it
is a blunt instrument, not BM25's tuned `b` parameter.

**Quantified cost: I found no study that measures NDCG@10 or MRR for `ts_rank` vs BM25 on the same Postgres
corpus.** This is a real evidence gap — the "ts_rank is worse" claim is universally asserted and, as far as I can
find, never measured by a neutral party. The closest quantitative anchor is VectorChord-BM25's own NDCG@10
comparison against **Elasticsearch** (not against ts_rank).

### B5. BM25 alternatives inside Postgres — the 2025/2026 field

- **VectorChord-bm25 (`vchord_bm25`)**, TensorChord. https://github.com/tensorchord/VectorChord-bm25 ·
  https://docs.vectorchord.ai/vectorchord/benchmark/elasticsearch.html — Label:
  **⚠️ [VENDOR-BENCH-OWN-PRODUCT]**. Implements **Block-WeakAnd (BlockMax WAND)** as a real Postgres index +
  operator, with bitpacked ID compression and an ES-aligned tokenizer. Claim: **~3x the QPS of Elasticsearch**
  at comparable **NDCG@10**, on the `bm25-benchmarks` datasets, retrieving top-1000.
  Reporting NDCG@10 alongside QPS is good practice — quality is held roughly equal rather than ignored.
  Also packaged by pgEdge (https://docs.pgedge.com/vchord-bm25/), which is mild third-party validation.
- **`pg_textsearch`**, Tiger Data (new, 2025/2026).
  https://www.tigerdata.com/blog/introducing-pg_textsearch-true-bm25-ranking-hybrid-retrieval-postgres —
  Label: **[VENDOR-BENCH-OWN-PRODUCT]/[BLOG-POST]**. Explicitly framed as "from ts_rank to BM25". A third
  independent BM25-in-Postgres implementation appearing in this window is itself the signal: **`ts_rank`'s
  inadequacy is now consensus among Postgres vendors**, even if nobody has published the NDCG delta (B4).
- **`rum` index** (PostgresPro): stores positional/rank info *inside* the index so ranking and phrase search do
  not require a heap recheck — the native answer to cliff B2.2. Mature but niche; slower to build and larger
  than GIN, and **not available on most managed Postgres**.
- **`pg_bm25`** — the *former* name of ParadeDB's extension. Renamed to `pg_search`. Old blog posts referencing
  `pg_bm25` are stale; there is no separate project.

## C) Extension coexistence and managed-Postgres availability

### C1. pg_search now *depends on* pgvector — that is the coexistence story

URL: https://github.com/paradedb/paradedb/blob/main/pg_search/README.md — Label: **[DOCS]**.
**As of pg_search 0.25.0, pg_search requires pgvector's `vector` type** — pgvector is a hard prerequisite and
must be installed first. So "pgvector + pg_search conflict" is the wrong question: they are now *coupled*.
The real cost is **version-pinning**: a pgvector upgrade can now break your FTS extension, and you must
upgrade the pair together. pg_search also **requires superuser** to install.

**ParadeDB does not require their distro.** `pg_search` is installable as a standalone extension into a
self-managed Postgres (https://docs.paradedb.com/deploy/self-hosted/extension); the ParadeDB Docker image is a
convenience, not a requirement. Good — no distro lock-in.

### C2. Managed Postgres availability matrix (as of this recon, 2026-08)

| | pgvector | pg_search (ParadeDB) | pgvectorscale | vchord_bm25 |
|---|---|---|---|---|
| **Self-hosted / Docker** | yes | yes (superuser) | yes | yes |
| **AWS RDS / Aurora** | **yes** (0.8.0 supported since Nov 2024) | **NO** — RDS allows only a pre-approved extension list | no | no |
| **Neon** | yes | **WITHDRAWN — unavailable for new projects since 2026-03-19**; existing projects grandfathered | no | no |
| **Google Cloud SQL** | yes | not listed / no evidence | no | no |
| **Supabase** | yes | not listed / no evidence | no | no |
| **pgEdge** | yes | — | — | **yes** (packaged) |

Sources: https://aws.amazon.com/about-aws/whats-new/2024/11/amazon-rds-for-postgresql-pgvector-080/ **[DOCS]** ·
https://neon.com/docs/extensions/pg_search **[DOCS]** · https://docs.pgedge.com/vchord-bm25/ **[DOCS]**.

**⚠️ The Neon withdrawal is the loudest signal in section C.** A managed provider that had shipped pg_search
stopped offering it to new projects in March 2026. Whatever the reason (operational burden, stability,
licensing, resource isolation), a provider removing an extension it already supported is negative evidence
about running it in production, and it should temper the ParadeDB adoption case independently of any benchmark.

**Net for us: pgvector is universally available; everything else pins us to self-hosted.** Since we already run
our own containers this is survivable, but it removes the "just move to RDS later" exit.

---

## D) Mixed-language corpora — Italian + English in one tenant

### D1. What Postgres officially recommends

URL: https://www.postgresql.org/docs/current/datatype-textsearch.html and the FTS chapter — Label: **[DOCS]**.
The documented answer is the **per-row language column**: store a `regconfig` column, and build the tsvector as
`to_tsvector(lang_col, body)`, maintained by a trigger or a generated column. Postgres ships Snowball stemmers
for English, Italian, French, Spanish, German and ~20 more. **CJK is not supported** without an extension.

**Critical constraint that people miss:** with a per-row `regconfig`, the *query* must also be parsed with the
right config — `to_tsquery('italian', ...)` will not match a row indexed as `english` for any word the two
stemmers reduce differently. So the per-row approach only works if you **either** know the query language
**or** run the query against every config and union. That second option is what makes this a sharp edge rather
than a solved problem.

Note: `to_tsvector(lang_col, body)` where `lang_col` is a column is **not IMMUTABLE**, so it **cannot be used
directly in a plain generated column or a simple expression index** — you need the `regconfig`-typed column plus
a trigger-maintained stored `tsvector` column. This is a real implementation trap.

### D2. The realistic option set, with the trade each one actually makes

1. **`simple` config** — no stemming, no stopword removal, just lowercase+tokenize. Language-agnostic by
   construction, so mixed IT/EN in one column "works". **Cost:** `documenti` will not match `documento`,
   `running` will not match `run`. For a morphologically rich language like **Italian this is a much bigger loss
   than for English** — Italian inflects nouns, adjectives and verbs heavily.
2. **Per-row language column + language detection at ingest** — correct stemming, but needs a detector, and
   breaks on documents that are genuinely bilingual (an Italian contract quoting English clauses).
3. **Two tsvector columns, one per language, unioned at query time** —
   `tsv_en @@ q_en OR tsv_it @@ q_it`. Doubles index size and write cost, and needs `ts_rank` combined across
   two columns, but it is the only option that does not require knowing the document's language *or* the
   query's. This is the pragmatic answer most production systems converge on for a two-language corpus.
4. **`unaccent` + `pg_trgm` fallback** — trigram similarity is language-agnostic and handles typos and
   inflection *approximately* (`documento`/`documenti` share most trigrams). Widely used as the safety net
   under a `simple` config. Costs a second, large index (GIN/GiST on trigrams) and does not rank well alone.
5. **ParadeDB / Tantivy tokenizers** — Tantivy ships language-specific stemmers plus dedicated
   **CJK / `lindera` tokenizers**, and pg_search lets you configure the tokenizer per field. This is a genuine
   capability advantage over native Postgres for CJK specifically. For IT+EN it does not solve the underlying
   *which stemmer* question — it just relocates it.

### D3. Is it solved? No — and the evidence gap is glaring

**I could not find a single published measurement of the retrieval-quality cost of `simple` vs a correct
stemmer, in Postgres, on a mixed-language corpus.** No NDCG, no recall@k, no MRR. What exists is uniformly
opinion and folklore (e.g. https://dev.to/_swanand/postgres-text-search-simple-adequate-2c8j argues `simple` is
"adequate" — **[BLOG-POST]**, no numbers).

State it plainly to the team: **this is a known sharp edge with no public quantitative guidance.** Anyone who
tells you the number is guessing. If the IT/EN stemming decision is load-bearing for us, **the only reliable
path is to measure it on our own corpus** — build a small labelled query set and compare `simple` vs
`italian` vs `english` vs the two-column union directly. That is a day of work and it is the only way to get a
real number.

**Mitigating factor for our design:** we are pairing FTS with a **dense 768d vector leg**. Multilingual
embeddings absorb morphology natively — `documenti`/`documento` are near-identical vectors regardless of
stemmer. The vector leg is a *structural* hedge against the stemmer question, which lowers the stakes of
choosing `simple`. That is an argument, not a measurement, and should be labelled as such.

---

## E) What breaks at scale, operationally

### E1. `maintenance_work_mem` — the cliff, not a slope

Sources: https://github.com/pgvector/pgvector/issues/969 · https://github.com/pgvector/pgvector/issues/822
**[DOCS]/upstream issues** · https://neon.com/blog/pgvector-30x-faster-index-build-for-your-vector-embeddings
**[VENDOR-BENCH-OWN-PRODUCT]** (Neon) · https://www.crunchydata.com/blog/hnsw-indexes-with-postgres-and-pgvector
**[BLOG-POST]**.

The HNSW build is **in-memory if the whole graph fits in `maintenance_work_mem`, and disk-based if it does not** —
and the two regimes are not close:
- Default `maintenance_work_mem` is **64 MB**. Any real vector table blows past that instantly and silently
  falls into the disk build, which is **10–50x slower**.
- In-memory build throughput is roughly **10 MiB/s**; once the index exceeds RAM, throughput drops
  **>10x**. Upstream issue #822 is literally titled "HNSW index creation is stuck on dozens millions entries."
- Practical rule: **size `maintenance_work_mem` above the expected index size before building**, and check
  the server log — pgvector logs which build path it took.

**For our regime this cliff does not exist.** Tens of MB of index versus a `maintenance_work_mem` we can trivially
set to 1–2 GB. Worth setting explicitly anyway, because the default 64 MB is small enough to bite even a modest
table and the failure is *slowness*, not an error.

### E2. Bloat: HNSW indexes do not compact except on REINDEX

**pgvector indexes do not compact on UPDATE/DELETE — only `REINDEX` reclaims.** Vacuum marks tuples dead but the
HNSW graph keeps the nodes. Under a churn-heavy workload (re-embedding documents, replacing versions) the index
grows monotonically and **recall drifts** as the graph fills with tombstoned neighbours. There is no
auto-maintenance for this. The operational answer is a **scheduled `REINDEX CONCURRENTLY`**.

A second, nastier property, stated in the tuning literature: **HNSW can return wrong top-K results with no error
and no plan anomaly.** Approximate means approximate — there is no signal in `EXPLAIN` that recall was bad. If
correctness matters you must measure recall against an exact scan out-of-band; you will never be told.

**For us this is the operationally relevant item in section E**, and it applies at *any* scale including ours:
if we re-embed documents on update, we need either a periodic REINDEX or a table small enough that exact search
sidesteps the whole question.

### E3. `ef_search` tuning — the actual guidance

Sources: https://github.com/pgvector/pgvector/blob/master/README.md **[DOCS]** ·
https://aws.amazon.com/blogs/database/running-pgvector-in-production-on-amazon-aurora-postgresql/ **[DOCS]** ·
https://www.paradedb.com/learn/postgresql/tuning-pgvector **[BLOG-POST]** (competitor writing about pgvector —
mild negative bias possible).

- Build-time: `m` (default 16) = graph connectivity; `ef_construction` (default 64). Consensus baseline is
  **`m=16, ef_construction=64`**; `m=32`/`ef_construction=128` buys "slightly" better recall for roughly
  **double the build time and memory**. Katz used `ef_construction=256` for his 99%-recall runs.
- Query-time: **`hnsw.ef_search` is the only knob that matters and it needs no rebuild** — set it per session or
  per workload. Higher = better recall, more latency, linearly-ish.
- **Raising `ef_search` is the first lever for filtered queries** where the filter is eating candidates —
  though since 0.8.0, `hnsw.iterative_scan` is the more principled fix (§A2).

### E4. When do people say "leave Postgres"?

Label: **[BLOG-POST]** consensus, not measurement —
https://open-techstack.com/blog/pgvector-vs-qdrant-2026/ ·
https://www.instaclustr.com/education/vector-database/pgvector-performance-benchmark-results-and-5-ways-to-boost-performance/
· ⚠️ https://www.pinecone.io/blog/pinecone-vs-pgvector/ (**[VENDOR-BENCH-OWN-PRODUCT]** — Pinecone arguing
against pgvector; maximally biased, cited only to show the argument exists).

The recurring threshold in the non-vendor writing: **HNSW in Postgres is comfortable below ~5M vectors**, and
**100M+ is where dedicated stores are said to earn their keep**. Between those, it depends on RAM budget and
whether you can hold the index resident. Note this is folklore convergence, not a measured breakpoint.

The reasons cited to *stay* in Postgres are the ones that apply to us: ranking needs SQL joins against
permissions/tenancy/state; you want **row and vector updated in one transaction**; you do not want a second
production system to operate. **At thousands of documents per tenant, "should we use a dedicated vector store"
is not a live question** — we are ~4 orders of magnitude below the threshold anyone argues about.

---

## F) Combining a BM25 leg and a vector leg in one query

### F1. The fast shape: two CTEs + RRF, no extension needed

Sources: https://www.paradedb.com/blog/hybrid-search-in-postgresql-the-missing-manual **[BLOG-POST/vendor]** ·
https://www.tigerdata.com/docs/build/examples/hybrid-search **[DOCS/vendor]** ·
https://www.tigerdata.com/blog/elasticsearchs-hybrid-search-now-in-postgres-bm25-vector-rrf
**[VENDOR-BENCH-OWN-PRODUCT]**.

The converged shape, and it is the same in every source:

1. CTE A: the lexical leg, `ORDER BY <rank> LIMIT n`, with `ROW_NUMBER()` to capture rank.
2. CTE B: the dense leg, `ORDER BY embedding <=> $q LIMIT n`, likewise ranked.
3. CTE C: `FULL OUTER JOIN` on id, score `1/(k + rank)` per leg with **k = 60**, sum.
4. Final `SELECT … ORDER BY rrf_score DESC LIMIT 10`.

**~20–30 lines of SQL, no extension required for the fusion itself**, wrappable in a SQL function.

**Why RRF and not score normalization:** BM25 scores are unbounded log-probability-ish ratios; cosine similarity
is bounded [-1, 1]. There is no principled way to put them on one scale, and every attempt is corpus-dependent
and fragile. **RRF discards the scores entirely and fuses ranks**, which makes it scale-invariant and is why it
is the default everywhere. `k=60` is the standard constant from the original RRF paper and essentially nobody
tunes it.

### F2. What it costs versus running the legs separately

**No published measurement of one-query vs two-round-trip hybrid retrieval could be found.** This is a genuine
gap. What can be said from the query shape rather than from data:

- **Both legs run regardless** — CTE A and CTE B each execute their own index scan. The fusion adds a join over
  `2n` rows (n per leg, typically 50–100), which is negligible.
- **The single-query form's win is one round trip and one snapshot** — both legs see the same MVCC snapshot,
  which the two-call form cannot guarantee. For a system where documents are being ingested concurrently this
  is a correctness property, not just latency.
- **The single-query form's cost is planner opacity.** Two CTEs with `LIMIT` are optimization fences in
  practice; you get less freedom to push the tenant filter down into both legs, and you must remember to apply
  the tenant predicate **inside each CTE**, not outside the fusion — applying it outside re-creates the
  overfiltering bug of §A2 at the SQL level, and it is an easy mistake to make.
- **Latency is dominated by neither leg.** The strongest recurring claim in the practitioner writing is that
  "most of the production weight is in the embedding pipeline and the eval rig, not the retriever." At our
  scale that is almost certainly true: **the embedding call for the query will cost more than both index scans
  combined.**

**Recommendation implied by the evidence: use the single-query CTE+RRF form.** It is not measurably slower, it
gives snapshot consistency, and it keeps the ranking logic in one auditable place.

---

## G) Neutral third-party benchmarks — what actually exists

- **ANN-Benchmarks** (https://ann-benchmarks.com) — the standard neutral harness, and the source of the
  datasets Katz uses (`sift-128`, `gist-960`, `glove-25`, `dbpedia-openai-1000k`). It benchmarks *libraries and
  some databases*, and pgvector results circulate through it. This is the most trustworthy family of numbers in
  this report.
- **VectorDBBench** (Zilliz) — ⚠️ **maintained by Zilliz, who sell Milvus.** It is open source and widely used,
  which is genuinely valuable, but a harness authored by a vendor whose product is in the ranking is not a
  neutral harness. Results from it that favour Milvus should be discounted.
- **`bm25-benchmarks`** — the dataset family VectorChord-bm25 reports NDCG@10 against. Neutral datasets,
  vendor-run measurements.
- **SIGMOD 2026 [PAPER]**, "An In-Depth Study of Filter-Agnostic Vector Search on a PostgreSQL Database System",
  Lu, Caminal, Chatzakis et al. https://arxiv.org/abs/2603.23710 (DOI 10.1145/3802011). The only genuine
  peer-reviewed source found. Its finding is directly relevant to multi-tenant retrieval: for **filtered**
  vector search, the winner is decided by **system-level overheads — page accesses and data retrieval — not by
  distance-computation counts**, and graph-based filtered methods (NaviX, ACORN) "can incur prohibitive numbers
  of filter checks … often canceling out their theoretical benefits in real-world database environments"
  versus clustering-based indexes like ScaNN. Translation for us: **published ANN benchmarks do not predict
  filtered-search performance in a real database**, so no number in section A should be trusted for a
  `WHERE tenant_id = ?` workload.

### The honest gaps — say these out loud

1. **No fair independent benchmark of pgvectorscale vs tuned pgvector 0.8.x.** All extant comparisons are
   Timescale's own, several against a pre-0.7 pgvector that no longer exists.
2. **No independent benchmark of pg_search vs a *correctly configured* tsvector baseline.** Every published
   comparison appears to use a handicapped baseline (§B1).
3. **No measurement of `ts_rank` vs BM25 retrieval quality (NDCG/MRR) on the same Postgres corpus.** Universally
   asserted, never measured by a neutral party.
4. **No measurement of the `simple`-vs-stemmer quality cost on a mixed-language corpus.** (§D3)
5. **No measurement of single-query hybrid vs separate legs.** (§F2)
6. **Almost nothing published at 10^3–10^4 rows** — our regime. Benchmarks start at 1M because below that
   everything is fast and nobody publishes it. That absence is itself informative.

