# Phase 7B — Academic / arXiv State of the Art (2024–2026)

**Target:** Aura masterplan Phase 7B — typed collection metadata registry, structured retrieval
hits with score components, hybrid FTS/vector + RRF, parent-chunk expansion, projection
freshness.

**Compiled:** 2026-05-16
**Method:** WebSearch + WebFetch over arXiv abstracts, ACL Anthology, Microsoft Research,
HuggingFace papers, and curated 2024–2026 reading lists. All academic claims carry an arXiv
ID (or DOI). Year required.

**Aura ground truth (do not re-litigate):**
- Go binary + SQLite + Qdrant sidecar.
- RRF already implemented at two levels (`internal/memoryindex/store.go:577-622`,
  `internal/storage/search/search.go:343-398`) with `k=60`.
- `Document.Kind` enum already exists; wiki frontmatter (`schema_version`, `prompt_version`)
  already exists; `conversations` archive already exists.
- Phase 7A is fixing tool-noise pollution. Phase 7B adds the typed collection registry on top.

> **Convention used below:** "Paper recommends" = explicit author claim with §/§§ reference
> in source text. "Production lore" = industry blog / vendor doc, not peer-reviewed. The two
> are tagged so the reader can apply the trust hierarchy.

---

## A. Hybrid retrieval (BM25 + vector) — fusion mechanics

### A.1 — RRF is a strong, parameter-light default; weighted fusion can beat it when tuned

- **Cormack, Clarke & Büttcher (2009)** — *Reciprocal Rank Fusion outperforms Condorcet and
  individual rank learning methods.* SIGIR 2009.
  ([DOI 10.1145/1571941.1572114](https://doi.org/10.1145/1571941.1572114))
  Canonical RRF paper. Formula `score(d) = Σ 1/(k + rank_i(d))` with `k=60` empirically
  derived on TREC. Still the cited reference in 2024–2026 work.
  → **Aura:** `k=60` is the right default; the literature has not displaced it. Keep it.

- **Bruch, Gai & Ingber (2024)** — *An Analysis of Fusion Functions for Hybrid Retrieval.*
  ACM TOIS, also [arXiv:2210.11934](https://arxiv.org/abs/2210.11934) (extended).
  Paper recommends: RRF is **sensitive to rank-tail noise**; **convex combination
  (weighted-sum)** fusion outperforms RRF in both in-domain and out-of-domain settings
  *when scores are normalized*. RRF wins when normalization is unreliable.
  → **Aura:** RRF stays the production default (heterogeneous score scales across
  FTS/vector/exact), but expose **score components** so a future weighted-fusion
  experiment is cheap.

- **From BM25 to Corrective RAG: Benchmarking Retrieval Strategies for Text-and-Table
  Documents** (2025). [arXiv:2504.01733](https://arxiv.org/abs/2504.01733).
  Paper claim: simple weighted hybrid `α=0.5 * BM25 + 0.5 * dense` reaches Recall@5 = 0.726
  on a finance benchmark, **beating RRF** on that corpus. Hybrid + neural reranker reaches
  0.816 (+17.4% over RRF-alone), and cross-encoder reranking lifts MRR@3 by 39.7%.
  Caveat: BM25 alone beats dense on finance — domain matters.
  → **Aura:** the score-components contract should make reranker insertion a one-collection
  change, not a system rewrite.

- **Balancing the Blend: An Experimental Analysis of Trade-offs in Hybrid Search** (2025).
  [arXiv:2508.01405](https://arxiv.org/abs/2508.01405).
  Evaluates 4 retrieval paradigms × 11 hybrid combinations × 3 fusion strategies (RRF,
  Weighted Sum, Tensor RF). Confirms: no single fusion wins on every corpus; the right
  knob is **observability over the components**, not picking one fusion forever.

### A.2 — BGE-M3 (dense + sparse + multi-vector in one model)

- **Chen, Xiao, Zhang, Luo, Lian & Liu (2024)** — *M3-Embedding: Multi-Linguality,
  Multi-Functionality, Multi-Granularity Text Embeddings Through Self-Knowledge
  Distillation.* [arXiv:2402.03216](https://arxiv.org/abs/2402.03216).
  Single model produces dense vector + sparse term weights + multi-vector (ColBERT-style)
  output simultaneously, supports 100+ languages and 8,192-token inputs. Self-knowledge
  distillation across the three heads is the training trick. Paper does **not** prescribe
  fusion weights — leaves it to the integrator.
  → **Aura:** Aura uses `embeddinggemma-300m` (locked, per MEMORY). M3 is a reference for
  the *shape* of typed-output embeddings, not a replacement. If Aura ever needs a hybrid
  multilingual upgrade path, M3 is the named candidate.

### A.3 — Late-interaction (ColBERTv2) — strong but operationally heavy

- **Santhanam, Khattab, Saad-Falcon, Potts & Zaharia (2022/NAACL'22)** — *ColBERTv2:
  Effective and Efficient Retrieval via Lightweight Late Interaction.*
  [arXiv:2112.01488](https://arxiv.org/abs/2112.01488).
  Residual compression + denoised supervision → 6–10× smaller index, SOTA on BEIR.
  Canonical late-interaction reference (kept as 2022 but still cited as SOTA baseline in
  2024–2026 work).

- **Santhanam et al. (2022)** — *PLAID: An Efficient Engine for Late Interaction Retrieval.*
  [arXiv:2205.09707](https://arxiv.org/abs/2205.09707).
  7× GPU / 45× CPU latency reduction over vanilla ColBERTv2 via centroid-only first stage.
  Still the citation production teams use when justifying late-interaction.

- **Jha, Wang, Günther et al. (2024)** — *Jina-ColBERT-v2: A General-Purpose Multilingual
  Late Interaction Retriever.* [arXiv:2408.16672](https://arxiv.org/abs/2408.16672).
  Multilingual extension; demonstrates late-interaction generalizes across languages with
  ~6× index compression.
  → **Aura:** Late-interaction would add a third storage tier (per-token vectors). Not for
  Phase 7B. Worth flagging in the registry design so a future "colbert" collection slots
  in without schema churn.

### A.4 — Learned sparse retrieval (SPLADE family) — competitive with BM25 latency

- **Formal, Lassance, Piwowarski & Clinchant (2021/2024)** — *SPLADE++ / Efficient SPLADE*,
  consolidated in ACM TOIS 2024 *Towards Effective and Efficient Sparse Neural IR*
  ([DOI 10.1145/3634912](https://doi.org/10.1145/3634912); precursor
  [arXiv:2109.10086](https://arxiv.org/abs/2109.10086)). Distillation + hard negatives +
  query/doc encoder split brings SPLADE inference latency on par with BM25.

- **Doshi, Aksitov, et al. (2024)** — *Mistral-SPLADE: LLMs for Better Learned Sparse
  Retrieval.* [arXiv:2408.11119](https://arxiv.org/abs/2408.11119). LLM-initialized
  SPLADE; demonstrates the technique is still gaining ground in 2024–2026.

  → **Aura:** SQLite FTS5 already covers the "sparse retrieval" slot. SPLADE is the
  upgrade path *if* lexical recall ever becomes the bottleneck. Phase 7B should treat FTS
  as one *collection type*, not as the only sparse implementation.

---

## B. Typed memory / multi-collection retrieval — agent memory layering

### B.1 — MemGPT: OS-style hierarchical memory

- **Packer, Wooders, Lin, Fang, Patil, Stoica & Gonzalez (2023, refreshed 2024)** —
  *MemGPT: Towards LLMs as Operating Systems.*
  [arXiv:2310.08560](https://arxiv.org/abs/2310.08560).
  Three tiers: **main context** (in-prompt working memory), **recall storage** (full message
  log, searchable), **archival storage** (vector-indexed long-term). The LLM **calls
  functions** to page data between tiers (rule-based agent control, not learned).
  → **Aura:** Aura's `conversations` archive ≈ MemGPT recall; `wiki/` ≈ archival. The
  *function-call* paging model maps directly to Aura's tool-call architecture. Phase 7B's
  typed registry is the missing piece that lets the agent target a specific tier.

### B.2 — Letta (formerly MemGPT) — production critique

- Letta docs (production lore, not peer-reviewed): same architecture as MemGPT, productized.
  Key design principle exposed in Letta docs: **the agent never sees all tiers at once** —
  the system prompt advertises tier names + summary cards, and the agent issues a
  retrieval call against a named tier. This is rule-based routing by the agent, not by a
  classifier.

### B.3 — A-MEM: Zettelkasten-style linked typed memory

- **Xu, Liang, Mei, Gao, Tan & Zhang (2025)** — *A-Mem: Agentic Memory for LLM Agents.*
  [arXiv:2502.12110](https://arxiv.org/abs/2502.12110).
  Memory nodes carry **typed attributes** (contextual description, keywords, tags) and
  **explicit links** to related notes. New writes can **trigger updates** to existing
  notes' attributes. Critique of graph-DB-only systems: "fixed operations and structures."
  → **Aura:** Aura's `[[wiki-links]]` + frontmatter `tags`/`related` is already this shape.
  A-MEM validates that wiki-as-graph is a current research-grade design, not legacy.

### B.4 — Mem0: explicit production memory layer

- **Chhikara, Khant, Aryan, Singh & Yadav (2025)** — *Mem0: Building Production-Ready AI
  Agents with Scalable Long-Term Memory.*
  [arXiv:2504.19413](https://arxiv.org/abs/2504.19413).
  Two variants: base (dynamic extract/consolidate/retrieve) and graph-augmented.
  Evaluated on **LOCOMO** (300-turn dialogues, ~9K tokens each), 4 question types:
  single-hop, multi-hop, temporal, open-domain. Reports **91% lower p95 latency** and
  **>90% token savings** vs. full-context, with **+26% LLM-as-judge** over the
  full-context baseline.
  → **Aura:** LOCOMO is the right benchmark to target if Phase 7B claims memory wins.

### B.5 — Cost-sensitive store routing

- **"Did You Check the Right Pocket? Cost-Sensitive Store Routing for Memory-Augmented
  Agents"** (2026). [arXiv:2603.15658](https://arxiv.org/abs/2603.15658).
  Explicit taxonomy: episodic vs. semantic, with a **cost-sensitive router that fires
  *before* retrieval** to avoid querying expensive stores when a cheap store would
  suffice. Frames the problem as "memory access," not just "retrieval."
  → **Aura:** Mirrors Phase 7B's typed-collection-registry mission. The paper's
  formalization (each store carries cost, expected hit rate, and a routing prior) is a
  direct blueprint for what Aura's registry should *store* per collection.

### B.6 — MemOS / Memory-OS

- **Zhou et al. (2025)** — *Memory OS of AI Agent.*
  [arXiv:2506.06326](https://arxiv.org/abs/2506.06326). Three-layer model
  (long/mid/short-term) with explicit paging policies; positions memory management as a
  systems-level concern. Useful as a meta-framing reference.

### B.7 — Self-RAG: when to retrieve vs. emit

- **Asai, Wu, Wang, Sil & Hajishirzi (2023, ICLR 2024)** — *Self-RAG: Learning to Retrieve,
  Generate, and Critique through Self-Reflection.*
  [arXiv:2310.11511](https://arxiv.org/abs/2310.11511).
  Special **reflection tokens** (`[Retrieve]`, `[ISREL]`, `[ISSUP]`, `[ISUSE]`) let the
  model gate retrieval per-segment and critique its own output. *Adaptive* retrieval
  frequency — not every turn calls the index.
  → **Aura:** Self-RAG's controller is *trained-in*, which Aura can't replicate without
  fine-tuning. The **operational lesson** is portable: expose `score_components` and
  follow-up handles so the agent can decide "I have enough, stop retrieving" — Aura can
  implement this as a tool-loop heuristic without retraining.

### B.8 — Taxonomies / surveys (orient before building)

- **"Memory for Autonomous LLM Agents: Mechanisms, Evaluation, and Emerging Frontiers"**
  (2026). [arXiv:2603.07670](https://arxiv.org/abs/2603.07670). 3-D taxonomy
  (representation × management × access) unifying MemGPT / A-MEM / Mem0 / Letta.
- **"Agentic Retrieval-Augmented Generation: A Survey on Agentic RAG"** (2025).
  [arXiv:2501.09136](https://arxiv.org/abs/2501.09136).
- **"Anatomy of Agentic Memory: Taxonomy and Empirical Analysis"** (2026).
  [arXiv:2602.19320](https://arxiv.org/abs/2602.19320).

---

## C. GraphRAG and community-aware retrieval

### C.1 — Microsoft GraphRAG (the seed)

- **Edge, Trinh, Cheng, Bradley, Chao, Mody, Truitt, Metropolitansky, Ness & Larson (2024,
  rev. 2025)** — *From Local to Global: A Graph RAG Approach to Query-Focused
  Summarization.* [arXiv:2404.16130](https://arxiv.org/abs/2404.16130).
  Two-stage indexing: LLM extracts an entity-relation graph, **Leiden** community detection
  partitions it, LLM pre-generates **community summaries** at multiple hierarchical
  levels. At query time: **local queries** (entity-anchored, BFS-like) vs. **global queries**
  (map-reduce over community summaries). Best on **global "sensemaking"** questions over
  ~1M-token corpora, where vanilla RAG can't reach.
  No explicit anti-patterns in the abstract, but follow-ups (below) supply them.

### C.2 — LightRAG: incremental-friendly alternative

- **Guo, Xia, Yu, Ao & Huang (2024)** — *LightRAG: Simple and Fast Retrieval-Augmented
  Generation.* [arXiv:2410.05779](https://arxiv.org/abs/2410.05779).
  **Dual-level retrieval** (low-level entity neighborhoods vs. high-level relation
  themes). Critically: avoids recomputing community structure on every update; new
  entities/relations merge incrementally into the existing graph. Cited in 2025 work as
  the production-friendlier GraphRAG variant.

### C.3 — GraphRAG performance is contested

- **"How Significant Are the Real Performance Gains? An Unbiased Evaluation Framework for
  GraphRAG"** (2025). [arXiv:2506.06331](https://arxiv.org/abs/2506.06331).
  Reproduces GraphRAG/LightRAG/etc. under controlled conditions. Finding: many
  win-rate claims (e.g. LightRAG 66.7% vs. NaiveRAG on Agriculture) **do not hold under
  unbiased eval** — NaiveRAG sometimes wins. The paper does **not** say GraphRAG is bad;
  it says claimed gaps are smaller than headline numbers suggest.
  → **Aura:** "Build a knowledge graph because it's modern" is not a sufficient
  justification. The actual win conditions (global sensemaking over million-token corpora)
  must be the use case Aura is solving for. Aura's wiki = ~hundreds of pages, not the
  million-token regime GraphRAG targets.

### C.4 — When to use graphs at all (the survey answer)

- **"When to use Graphs in RAG: A Comprehensive Analysis for Graph
  Retrieval-Augmented Generation"** (2025).
  [arXiv:2506.05690](https://arxiv.org/abs/2506.05690).
  Distills decision criteria: multi-hop reasoning required? Cross-document entity
  resolution? Global sensemaking? If none → graph is overhead.
- **"Graph Retrieval-Augmented Generation: A Survey"** (2024).
  [arXiv:2408.08921](https://arxiv.org/abs/2408.08921).
  Reference taxonomy.

### C.5 — Practical graph construction

- **"Towards Practical GraphRAG: Efficient Knowledge Graph Construction and Hybrid
  Retrieval at Scale"** (2025). [arXiv:2507.03226](https://arxiv.org/abs/2507.03226).
  Quantifies the cost: repeated LLM calls for entity-relation extraction are the dominant
  bottleneck.

→ **Aura net call:** Aura's `[[wiki-links]]` already encode the graph cheaply (writers
maintain it, no extraction LLM pass). Phase 7B should **expose the graph** as a
collection type (entity neighborhoods, link expansions) — not rebuild GraphRAG from
scratch. This aligns with MEMORY note "Graph memory IS the project core — fix writers +
retrievers; NO KuzuDB/Neo4j/Zep."

---

## D. Projection freshness — index versioning, stale embeddings, streaming reindex

### D.1 — Embedding drift is real and silent

- **"Still Fresh? Evaluating Temporal Drift in Retrieval Benchmarks"** (2026).
  [arXiv:2603.04532](https://arxiv.org/abs/2603.04532).
  Studies **FreshStack**: API deprecations, code reorgs, doc moves silently invalidate
  retrieval benchmarks over time. Drift hits *quality*, not *errors* — nothing crashes.

- **"Query Drift Compensation: Enabling Compatibility in Continual Learning of Retrieval
  Embedding Models"** (2025). [arXiv:2506.00037](https://arxiv.org/abs/2506.00037).
  When the embedding model updates, the **old corpus index is suboptimal**, and **full
  re-embedding is expensive**. Proposes query-side compensation so old vectors remain
  usable for one model generation. Paper recommends explicit **version tags on every
  retrieval** so quality regressions can be diagnosed.

### D.2 — Streaming / versioned vector indices

- **"LiveVectorLake: A Real-Time Versioned Knowledge Base Architecture for Streaming
  Vector Updates and Temporal Retrieval"** (2026).
  [arXiv:2601.05270](https://arxiv.org/abs/2601.05270).
  Content-addressed hashing + dual-tier storage + ACID transactions for vector indices.
  Designed so a write commits with a version stamp, retrievers can pin a version, and
  background rebuilds don't block reads.

- **"A Dynamic Retrieval-Augmented Generation System with Selective Memory and
  Remembrance"** (2026). [arXiv:2601.02428](https://arxiv.org/abs/2601.02428).
  Selective memory + forgetting policies; relevant to projection freshness budgets.

### D.3 — Production lore (named as such)

- *Version Your Vectors* (safjan.com, 2025) and *Embedding Models in Production: Selection,
  Versioning, and the Index Drift Problem* (TianPan, 2026). **Production lore, not
  peer-reviewed.** Common takeaways: tag every retrieved chunk with `embedding_model_id`
  + `index_build_id`; alert on cosine-similarity-distribution shift, not just hit-rate.

→ **Aura:** Phase 7B's **projection freshness registry** must store, per collection:
`embedding_model_id`, `embedding_dim`, `index_build_id`, `last_full_rebuild_at`,
`dirty_count`. The literature (2506.00037, 2601.05270) explicitly motivates this design.

---

## E. Parent-chunk expansion / small-to-big retrieval

### E.1 — RAPTOR: recursive abstractive trees

- **Sarthi, Abdullah, Tuli, Khanna, Goldie & Manning (2024, ICLR'24)** — *RAPTOR:
  Recursive Abstractive Processing for Tree-Organized Retrieval.*
  [arXiv:2401.18059](https://arxiv.org/abs/2401.18059).
  Build a tree: leaf = original chunks, parents = LLM-generated cluster summaries,
  recursive. At retrieval, traverse the tree at multiple abstraction levels. **+20%
  absolute** on QuALITY paired with GPT-4. Paper explicitly warns: "most existing methods
  retrieve only short contiguous chunks ... limiting holistic understanding."
  → **Aura:** RAPTOR's recipe (summary-as-parent) is a build-time investment Aura already
  partly has — wiki pages *are* the human-authored summary layer. Phase 7B's parent-chunk
  expansion can mean **return the chunk hit + its wiki page** rather than a recursive
  summarization pass.

### E.2 — Hierarchical chunking benchmarks

- **"HiChunk: Evaluating and Enhancing Retrieval-Augmented Generation with Hierarchical
  Chunking"** (2025). [arXiv:2509.11552](https://arxiv.org/abs/2509.11552).
  Releases **HiCBench**, an explicit benchmark for chunking strategies. Pairs hierarchical
  chunking with an **Auto-Merge retrieval** algorithm (return the smallest sub-tree that
  covers the matched leaves). Significantly improves chunk-quality, retrieval, and
  end-task scores vs. flat baselines.

- **"Enhancing Retrieval Augmented Generation with Hierarchical Text Segmentation
  Chunking"** (2025). [arXiv:2507.09935](https://arxiv.org/abs/2507.09935).
  Tests on NarrativeQA, QuALITY, QASPER. Same direction: hierarchical beats flat.

- **"H-RAG at SemEval-2026 Task 8: Hierarchical Parent–Child Retrieval for Multi-Turn RAG
  Conversations"** (2026). [arXiv:2605.00631](https://arxiv.org/abs/2605.00631).
  Concrete reference implementation of small-to-big in a multi-turn setting.

- **"Mix-of-Granularity: Optimize the Chunking Granularity for Retrieval-Augmented
  Generation"** (2024). [arXiv:2406.00456](https://arxiv.org/abs/2406.00456).
  A router selects granularity per query; mixed-granularity beats fixed.

### E.3 — Contextual chunking (parent context injected into child)

- Anthropic *Contextual Retrieval* (Sept 2024). Production lore. Prepend a short
  LLM-generated context blurb to every chunk so each chunk carries parent semantics into
  the vector. Cited as the cheapest small-to-big approximation.

→ **Aura:** Wiki page = natural parent. `Document.Kind` already encodes type. Phase 7B
should add a **`parent_doc_id` / `parent_collection`** handle on every chunk hit so the
caller (agent or follow-up tool call) can fetch the full page. This is the cheap version
of E.1; no recursive summarization needed.

---

## F. Citation faithfulness

### F.1 — RAGAS (the de facto reference framework)

- **Es, James, Espinosa-Anke & Schockaert (2024)** — *RAGAS: Automated Evaluation of
  Retrieval Augmented Generation.* EACL 2024 demo. ACL Anthology:
  [2024.eacl-demo.16](https://aclanthology.org/2024.eacl-demo.16/). Predecessor arXiv:
  [arXiv:2309.15217](https://arxiv.org/abs/2309.15217).
  Reference-free LLM-as-judge metrics: **context precision, context recall, faithfulness,
  answer relevance**. Now the industry baseline; cited as the evaluation harness in most
  2025 RAG papers.

### F.2 — Faithfulness ≠ correctness (the load-bearing distinction)

- **"Correctness is not Faithfulness in RAG Attributions"** (2024).
  [arXiv:2412.18004](https://arxiv.org/abs/2412.18004).
  Empirically demonstrates: a citation can be **correct** (the cited doc supports the
  claim) yet **unfaithful** (the LLM did not actually use that doc to derive the claim).
  Up to **57% of citations** in tested systems were post-rationalized.
  → **Aura:** Citation strings in agent replies are evidence-of-existence, not
  evidence-of-derivation. Verification still has to compare *artifact bytes* vs. claim —
  matches Aura's existing CLAUDE.md rule "tests verify quality, never tool-call counts."

### F.3 — Faithfulness leaderboards

- **"Benchmarking LLM Faithfulness in RAG with Evolving Leaderboards"** (2025).
  [arXiv:2505.04847](https://arxiv.org/abs/2505.04847). FaithJudge: LLM-as-judge with a
  pool of human-annotated hallucination examples. Standing leaderboard.

### F.4 — Citation/attribution methods

- **RAFT — "Retrieval Augmented Fine-Tuning"** (Zhang et al., 2024, Berkeley).
  [arXiv:2403.10131](https://arxiv.org/abs/2403.10131).
  Training recipe: oracle + **distractor** documents, model trained to **cite verbatim
  the supporting span**. Improves robustness when the retriever returns junk.
  → **Aura:** Aura can't fine-tune, but the *output contract* (cite verbatim spans, not
  paraphrased summaries) is a portable prompt-engineering rule.

- **"CiteGuard: Faithful Citation Attribution for LLMs via Retrieval-Augmented
  Validation"** (2025). [arXiv:2510.17853](https://arxiv.org/abs/2510.17853).
  Post-hoc verifier: after generation, re-retrieve and check each cited span.
- **"Enhancing Factual Accuracy and Citation Generation in LLMs via Multi-Stage
  Self-Verification"** (2025). [arXiv:2509.05741](https://arxiv.org/abs/2509.05741).

---

## G. CROSS-PAPER SHORTLIST — top 5 actionable findings for Aura Phase 7B

> **Section G is the most actionable. The other sections are evidence.**

### G.1 — Expose retrieval scores as **decomposed components**, not a single fused number

- **Summary:** RRF is robust but lossy; weighted-sum sometimes wins; reranker sometimes
  wins; the optimum is corpus-dependent. The literature converges on the conclusion that
  **observability over components beats picking one fusion forever**.
- **Citations:** *An Analysis of Fusion Functions for Hybrid Retrieval* — Bruch et al.
  ([arXiv:2210.11934](https://arxiv.org/abs/2210.11934)); *Balancing the Blend*
  ([arXiv:2508.01405](https://arxiv.org/abs/2508.01405)); *From BM25 to Corrective RAG*
  ([arXiv:2504.01733](https://arxiv.org/abs/2504.01733)).
- **Concrete next step for Aura:** add to `MemoryResult` (or the equivalent struct
  returned by `internal/memoryindex` / `internal/storage/search`) a
  `score_components { exact float, fts float, vector float, rrf float,
  reranker float (nullable) }` plus `score_source enum` (which path won). Keep the
  current RRF `k=60` as the default fused number; the components are *additional*.
- **Risk if NOT adopted:** every future retrieval debug ("why did chunk X rank above
  chunk Y?") becomes archaeology against ephemeral logs; A/B-testing a reranker or
  weighted-sum requires re-instrumenting the whole pipeline.

### G.2 — Make the typed collection registry the **routing layer**, not a passive catalog

- **Summary:** MemGPT/Letta/A-MEM/Mem0/Cost-Sensitive-Routing all converge on: the agent
  picks a named tier, the tier carries its own retrieval policy + cost + freshness. The
  agent should **never** issue a blind cross-tier query.
- **Citations:** *MemGPT* ([arXiv:2310.08560](https://arxiv.org/abs/2310.08560));
  *A-Mem* ([arXiv:2502.12110](https://arxiv.org/abs/2502.12110));
  *Mem0* ([arXiv:2504.19413](https://arxiv.org/abs/2504.19413));
  *Did You Check the Right Pocket?*
  ([arXiv:2603.15658](https://arxiv.org/abs/2603.15658)).
- **Concrete next step for Aura:** the registry entry per collection (`wiki`, `sources`,
  `user_memory`, `archive`, `operational_memory`) should carry: `name`, `kind` enum,
  `default_retrieval_mode` (exact/fts/vector/hybrid), `embedding_model_id`,
  `index_build_id`, `expected_cost_class` (cheap/medium/expensive), `freshness_sla`,
  `parent_collection` (nullable, for chunk→page expansion). The agent's tool surface
  becomes `retrieve(collection: <name>, query, k)` — not `retrieve_anywhere(query, k)`.
- **Risk if NOT adopted:** Aura silently re-merges everything via RRF every turn, paying
  the cost of every collection even when one would suffice. Phase 7A's tool-noise fight
  re-emerges as retrieval-noise.

### G.3 — Treat `parent_doc_id` as a **first-class follow-up handle** on every chunk hit

- **Summary:** Small-to-big retrieval consistently outperforms flat chunk retrieval on
  multi-hop and global-context tasks. Aura's wiki pages are already the natural parent
  layer — no recursive summarization pass needed.
- **Citations:** *RAPTOR* ([arXiv:2401.18059](https://arxiv.org/abs/2401.18059));
  *HiChunk* ([arXiv:2509.11552](https://arxiv.org/abs/2509.11552));
  *Mix-of-Granularity* ([arXiv:2406.00456](https://arxiv.org/abs/2406.00456));
  *Hierarchical Text Segmentation Chunking*
  ([arXiv:2507.09935](https://arxiv.org/abs/2507.09935)).
- **Concrete next step for Aura:** add to every retrieval hit a
  `follow_up { parent_doc_id, parent_collection, parent_slug }` block, plus a
  registered `expand_parent(doc_id)` tool. Wiki chunks → wiki page; source chunks →
  `ocr.md`; conversation snippets → full turn. Make HiChunk's "Auto-Merge" the algorithm:
  if N sibling chunks hit, return the parent once, not N chunks N times.
- **Risk if NOT adopted:** the agent re-issues `search` to widen context (token waste +
  latency + the lost-in-the-middle effect on the next turn). RAPTOR's headline +20%
  QuALITY gain stays out of reach.

### G.4 — Encode **projection freshness** in the registry; pin retrieval to a version

- **Summary:** Embedding model swaps + partial re-indexes silently degrade RAG quality.
  Every cited 2025–2026 paper on the topic prescribes version tags on every retrieval,
  background rebuild jobs, and a freshness SLA per collection.
- **Citations:** *Query Drift Compensation*
  ([arXiv:2506.00037](https://arxiv.org/abs/2506.00037));
  *LiveVectorLake* ([arXiv:2601.05270](https://arxiv.org/abs/2601.05270));
  *Still Fresh? FreshStack* ([arXiv:2603.04532](https://arxiv.org/abs/2603.04532)).
- **Concrete next step for Aura:** the registry stores per collection
  `embedding_model_id`, `embedding_dim`, `index_build_id`, `last_full_rebuild_at`,
  `dirty_doc_count`, `freshness_sla`. Stamp every retrieval response with the
  `index_build_id` it was served from. Schedule a background reconciler (mirroring the
  Wave 2.10.b tool reconciler pattern; commit `2367f502`) that flips a collection to
  "stale" when `dirty_count > threshold` and triggers a rebuild.
- **Risk if NOT adopted:** the locked-in `embeddinggemma-300m` 256-d switch (per MEMORY)
  is already a model-swap event — any future swap will silently mix vector spaces. Old
  hits will rank against new queries; debugging is post-mortem only.

### G.5 — Distinguish **citation correctness** from **citation faithfulness** in the
       output contract

- **Summary:** Up to 57% of LLM citations are post-rationalized — the cited doc exists and
  supports the claim, but it's *not* what the LLM used to derive it. Aura's existing rule
  ("validate against the artifact, not the reply") is the literature's recommendation.
- **Citations:** *Correctness is not Faithfulness*
  ([arXiv:2412.18004](https://arxiv.org/abs/2412.18004));
  *RAGAS* ([ACL Anthology 2024.eacl-demo.16](https://aclanthology.org/2024.eacl-demo.16/));
  *FaithJudge* ([arXiv:2505.04847](https://arxiv.org/abs/2505.04847));
  *RAFT verbatim-citation training*
  ([arXiv:2403.10131](https://arxiv.org/abs/2403.10131)).
- **Concrete next step for Aura:** every `MemoryResult` returned to the agent already
  carries a stable `source_id` and chunk offset — surface those in the agent prompt
  schema. The system prompt should require **verbatim span quotation** for any factual
  claim (RAFT-style). Probes (per CLAUDE.md) re-fetch the cited artifact and assert the
  span exists — drop reliance on the reply text.
- **Risk if NOT adopted:** Phase 7B will ship typed retrieval but downstream evaluation
  will keep grading on tool-call counts (already flagged in MEMORY as a recurring
  superficial-test trap).

---

## H. ANTI-PATTERNS from the literature — what to NOT build

### H.1 — "More context is always better" / dumping retrieval into the prompt

- **Liu, Lin, Hewitt, Paranjape, Bevilacqua, Petroni & Liang (2024, TACL)** — *Lost in
  the Middle: How Language Models Use Long Contexts.*
  [arXiv:2307.03172](https://arxiv.org/abs/2307.03172).
  Primacy + recency bias; middle-of-context info underused.
- **"Long-Context LLMs Meet RAG: Overcoming Challenges for Long Inputs in RAG"** (2024).
  [arXiv:2410.05983](https://arxiv.org/abs/2410.05983). Explicitly: "more retrieved
  text does not guarantee improved performance — positional bias + dilution degrade it."
- **"Grounding Long-Context Reasoning with Contextual Normalization for RAG"** (2025).
  [arXiv:2510.13191](https://arxiv.org/abs/2510.13191). Same warning.
- **For Aura:** Phase 7B's parent-chunk expansion must respect a token budget. Don't
  return the whole parent every time. Auto-merge (HiChunk) is the disciplined version.

### H.2 — "Build a knowledge graph because it's modern"

- **"How Significant Are the Real Performance Gains? An Unbiased Evaluation Framework for
  GraphRAG"** (2025). [arXiv:2506.06331](https://arxiv.org/abs/2506.06331).
  Many GraphRAG/LightRAG headline win-rates collapse under controlled evaluation;
  sometimes NaiveRAG wins.
- **"When to use Graphs in RAG"** (2025).
  [arXiv:2506.05690](https://arxiv.org/abs/2506.05690).
  Use graphs only for multi-hop / cross-doc entity resolution / global sensemaking —
  otherwise the construction cost dominates.
- **"Towards Practical GraphRAG: Efficient Knowledge Graph Construction and Hybrid
  Retrieval at Scale"** (2025). [arXiv:2507.03226](https://arxiv.org/abs/2507.03226).
  Repeated LLM entity-extraction calls are the dominant cost.
- **For Aura:** matches MEMORY rule. Aura's `[[wiki-links]]` already carry the graph for
  free (writer-maintained). Phase 7B should expose that graph, not import KuzuDB/Neo4j.

### H.3 — "Fuse and forget" / opaque single-score retrieval

- **Bruch et al. (TOIS 2024 /
  [arXiv:2210.11934](https://arxiv.org/abs/2210.11934))** — RRF is parameter-sensitive
  in the rank tail; opaque-fusion systems can't diagnose why a hit was ranked.
- **"Balancing the Blend"** ([arXiv:2508.01405](https://arxiv.org/abs/2508.01405)) — no
  fusion strategy wins on every corpus.
- **For Aura:** never collapse `(exact, fts, vector)` into a single float without keeping
  the components addressable in the result struct.

### H.4 — "Re-embed everything when the model changes" / no version pinning

- **"Query Drift Compensation"** (2025).
  [arXiv:2506.00037](https://arxiv.org/abs/2506.00037). Full re-embedding is expensive
  enough to break SLAs; mismatched-version retrieval is the silent killer.
- **"Still Fresh?"** (2026). [arXiv:2603.04532](https://arxiv.org/abs/2603.04532).
  Drift is invisible without versioned eval.
- **For Aura:** every chunk + every retrieval response must carry `index_build_id`. The
  Wave 2.10.b reconciler pattern is the right shape; add a vector-collection variant.

### H.5 — "Trust the citation string"

- **"Correctness is not Faithfulness"** (2024).
  [arXiv:2412.18004](https://arxiv.org/abs/2412.18004). 57% of citations
  post-rationalized in tested systems.
- **For Aura:** matches CLAUDE.md "validate the artifact, not the reply." Probes must
  re-fetch the cited source and assert the claim against the bytes, not against the
  reply text. The registry's `source_id` + offset must be machine-checkable, not just
  human-readable.

---

## References cited (canonical IDs)

### Hybrid retrieval / fusion
- Cormack, Clarke & Büttcher (2009), SIGIR.
  [doi:10.1145/1571941.1572114](https://doi.org/10.1145/1571941.1572114)
- Bruch, Gai & Ingber (2024) / [arXiv:2210.11934](https://arxiv.org/abs/2210.11934)
- *From BM25 to Corrective RAG* (2025) /
  [arXiv:2504.01733](https://arxiv.org/abs/2504.01733)
- *Balancing the Blend* (2025) /
  [arXiv:2508.01405](https://arxiv.org/abs/2508.01405)
- Chen et al., M3-Embedding (2024) /
  [arXiv:2402.03216](https://arxiv.org/abs/2402.03216)
- Santhanam et al., ColBERTv2 (2022) /
  [arXiv:2112.01488](https://arxiv.org/abs/2112.01488)
- Santhanam et al., PLAID (2022) /
  [arXiv:2205.09707](https://arxiv.org/abs/2205.09707)
- Jha et al., Jina-ColBERT-v2 (2024) /
  [arXiv:2408.16672](https://arxiv.org/abs/2408.16672)
- Formal et al., SPLADE family /
  [arXiv:2109.10086](https://arxiv.org/abs/2109.10086),
  [doi:10.1145/3634912](https://doi.org/10.1145/3634912)
- Doshi et al., Mistral-SPLADE (2024) /
  [arXiv:2408.11119](https://arxiv.org/abs/2408.11119)

### Typed memory
- Packer et al., MemGPT (2023) /
  [arXiv:2310.08560](https://arxiv.org/abs/2310.08560)
- Xu et al., A-Mem (2025) /
  [arXiv:2502.12110](https://arxiv.org/abs/2502.12110)
- Chhikara et al., Mem0 (2025) /
  [arXiv:2504.19413](https://arxiv.org/abs/2504.19413)
- *Did You Check the Right Pocket?* (2026) /
  [arXiv:2603.15658](https://arxiv.org/abs/2603.15658)
- *Memory OS of AI Agent* (2025) /
  [arXiv:2506.06326](https://arxiv.org/abs/2506.06326)
- Asai et al., Self-RAG (ICLR 2024) /
  [arXiv:2310.11511](https://arxiv.org/abs/2310.11511)
- *Memory for Autonomous LLM Agents* survey (2026) /
  [arXiv:2603.07670](https://arxiv.org/abs/2603.07670)
- *Agentic RAG survey* (2025) /
  [arXiv:2501.09136](https://arxiv.org/abs/2501.09136)
- *Anatomy of Agentic Memory* (2026) /
  [arXiv:2602.19320](https://arxiv.org/abs/2602.19320)

### GraphRAG
- Edge et al., GraphRAG (2024) /
  [arXiv:2404.16130](https://arxiv.org/abs/2404.16130)
- Guo et al., LightRAG (2024) /
  [arXiv:2410.05779](https://arxiv.org/abs/2410.05779)
- *Unbiased GraphRAG eval* (2025) /
  [arXiv:2506.06331](https://arxiv.org/abs/2506.06331)
- *When to use Graphs in RAG* (2025) /
  [arXiv:2506.05690](https://arxiv.org/abs/2506.05690)
- *GraphRAG survey* (2024) /
  [arXiv:2408.08921](https://arxiv.org/abs/2408.08921)
- *Practical GraphRAG* (2025) /
  [arXiv:2507.03226](https://arxiv.org/abs/2507.03226)

### Projection freshness
- *Still Fresh? / FreshStack* (2026) /
  [arXiv:2603.04532](https://arxiv.org/abs/2603.04532)
- *Query Drift Compensation* (2025) /
  [arXiv:2506.00037](https://arxiv.org/abs/2506.00037)
- *LiveVectorLake* (2026) /
  [arXiv:2601.05270](https://arxiv.org/abs/2601.05270)
- *Dynamic RAG with Selective Memory* (2026) /
  [arXiv:2601.02428](https://arxiv.org/abs/2601.02428)

### Hierarchical / parent-chunk
- Sarthi et al., RAPTOR (2024) /
  [arXiv:2401.18059](https://arxiv.org/abs/2401.18059)
- *HiChunk / HiCBench* (2025) /
  [arXiv:2509.11552](https://arxiv.org/abs/2509.11552)
- *Hierarchical Text Segmentation Chunking* (2025) /
  [arXiv:2507.09935](https://arxiv.org/abs/2507.09935)
- *Mix-of-Granularity* (2024) /
  [arXiv:2406.00456](https://arxiv.org/abs/2406.00456)
- *H-RAG SemEval 2026 Task 8* (2026) /
  [arXiv:2605.00631](https://arxiv.org/abs/2605.00631)

### Citation faithfulness
- Es et al., RAGAS (EACL 2024) /
  [aclanthology 2024.eacl-demo.16](https://aclanthology.org/2024.eacl-demo.16/),
  [arXiv:2309.15217](https://arxiv.org/abs/2309.15217)
- *Correctness is not Faithfulness* (2024) /
  [arXiv:2412.18004](https://arxiv.org/abs/2412.18004)
- *FaithJudge* (2025) /
  [arXiv:2505.04847](https://arxiv.org/abs/2505.04847)
- Zhang et al., RAFT (2024) /
  [arXiv:2403.10131](https://arxiv.org/abs/2403.10131)
- *CiteGuard* (2025) /
  [arXiv:2510.17853](https://arxiv.org/abs/2510.17853)
- *Multi-Stage Self-Verification* (2025) /
  [arXiv:2509.05741](https://arxiv.org/abs/2509.05741)

### Anti-patterns
- Liu et al., Lost in the Middle (TACL 2024) /
  [arXiv:2307.03172](https://arxiv.org/abs/2307.03172)
- *Long-Context LLMs Meet RAG* (2024) /
  [arXiv:2410.05983](https://arxiv.org/abs/2410.05983)
- *Grounding Long-Context Reasoning* (2025) /
  [arXiv:2510.13191](https://arxiv.org/abs/2510.13191)
