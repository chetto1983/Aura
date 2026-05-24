# State of the Art — Graph-Based Retrieval + Agentic Wiki RAG (2026-05-21)

Research snapshot compiled for Aura's wiki retrieval roadmap. Aura today: ~60 markdown pages with `[[wiki-links]]`, hybrid cosine+FTS5+exact-lex with RRF fusion, GraphIndex with BFS/ShortestPath/Degree, confidence-labeled edges. Concrete pain point being optimised: "dammi il codice cliente di delta automazioni" — needs 5–7 tool calls and 20–30 s because the answer is split across an entity wiki summary (Quadristi, Costruttori Macchine) and a row-level `extract.md` table. Target: one retrieval call returns the right subgraph in <2 s.

This document surveys what production teams actually shipped or published between Nov 2025 and May 2026, then maps the strongest patterns to Aura's existing primitives.

---

## Microsoft GraphRAG / LazyGraphRAG

- **Current state (May 2026):** GraphRAG `v3.0.9` (13 Apr 2026), MIT, Python, modular pipeline. LazyGraphRAG (Microsoft Research, late 2025) is the headline 2026 variant: index cost ≈ vector-RAG (≈0.1 % of full GraphRAG); query cost ≈ 1/700 of GraphRAG Global Search at comparable quality. Won 96/96 head-to-heads against GraphRAG, RAPTOR and naive RAG on BenchmarkQED.
- **Key insight:** "Lazy" = skip the expensive upfront LLM community-summary phase. At query time it does a NL-to-query expansion → vector + community-graph hop → relevance-test only the top candidates with a small LLM. Indexing cost matches naive RAG, query quality matches full GraphRAG.
- **CPU feasibility:** Index is LLM-bound but the lazy variant is dominated by the same embedding+lexical pass Aura already runs. The graph layer itself is pure Python/igraph — fits Aura's existing GraphIndex.
- **License / repo:** MIT, `github.com/microsoft/graphrag`.

## HippoRAG / HippoRAG 2

- **Current state:** HippoRAG (NeurIPS '24, OSU+UIUC) → HippoRAG 2 (early 2026). Both are MIT/Apache-style open source at `github.com/OSU-NLP-Group/HippoRAG`.
- **Key insight:** Personalized PageRank over a schemaless LLM-extracted KG, **seeded by query concepts** (NER + phrase nodes). One retrieval step yields a *multi-hop* subgraph — exactly Aura's "delta-automazioni" use-case. HippoRAG 2 adds phrase↔passage node balancing via PPR reset-probability tuning, and a **query-to-triple** linker that lifts Recall@5 by +12.5 % vs NER-to-node.
- **Numbers:** +7 % on associative-memory tasks over leading embedding models; reported to beat GraphRAG, RAPTOR, and LightRAG on multi-hop while using *less* offline compute. Online latency stays embedding-class because PPR runs on the static graph.
- **CPU feasibility:** PPR on a 60–10k-node graph is sub-100 ms on CPU (NetworkX/igraph). Aura's `GraphIndex` already has BFS/Degree — adding PPR is a small primitive.

## LightRAG (HKUDS)

- **Current state:** EMNLP 2025 paper, very active repo (`github.com/HKUDS/LightRAG`). March 2026 update added OpenSearch backend, a setup wizard, multimodal (RAG-Anything) via Markdown/PDF/Office/tables, Neo4j+Qdrant backends, and a WebUI graph visualiser.
- **Key insight:** **Dual-level retrieval** — low level returns specific entities + their direct edges (entity codes, emails, IDs), high level returns the thematic community summary. The retrieval fuses both. This *exactly* mirrors Aura's split between row-level `extract.md` and entity-level summary pages.
- **CPU feasibility:** Indexing is LLM-bound (entity+relation extraction at write time, which Aura already partially does via wiki link extraction). Query is one LLM-keyword-pass + two graph lookups — fast on CPU.
- **License:** MIT.

## GraphReader / GraphPlanner

- **GraphReader (arXiv 2406.14550):** A graph-walker agent. Builds nodes from chunks, then an LLM agent issues `read_neighbor`/`read_node` calls in a planned exploration. Beats GPT-4-128k with a 4k window on LV-Eval, HotpotQA, NarrativeQA across 16k–256k contexts.
- **GraphPlanner (ICLR 2026, `github.com/ulab-uiuc/GraphPlanner`):** Adds graph-memory-augmented routing for multi-agent orchestration. Less directly applicable to Aura's single-agent loop, but the **planner-then-walker** decomposition is reusable.
- **Aura mapping:** Aura's agent already has BFS/ShortestPath tools — wrapping them as `graph_walk(seed, hops, budget)` exposes the GraphReader pattern without changing the loop.

## HyDE / HyPE

- **HyDE classic:** LLM hallucinates a hypothetical answer document, embeds it, retrieves nearest real docs. Best when the query is short or under-specified — exactly the Italian "dammi il codice cliente …" case where "dammi" carries no semantic weight.
- **2026 variants:**
  - **Ensemble HyDE:** generate 5 hypothetical answers, average the embeddings — kills variance from a single bad sample.
  - **HyPE (Hypothetical Prompt Embeddings):** flip it — at *index time*, generate likely queries per chunk, embed those, index alongside. Reports +42 pp precision / +45 pp recall on some datasets, **zero query-time generation cost**. Pairs naturally with Aura's wiki-page atomic writes.
  - **Domain-conditioned prompting** ("as a CRM analyst…") materially changes output quality.
- **CPU/latency:** HyPE wins because it amortises the LLM cost into the existing wiki ingestion path. Aura already has deterministic temp=0 LLM writes; adding "generate 3 expected queries" per page is a 1-line cost increase.

## ColBERT v2 / ColPali

- **State:** ColBERTv2 (2021) is the late-interaction baseline; PLAID engine reports **9–45× CPU speedup** over vanilla ColBERTv2 with same quality, scaling to 140M passages with tens-to-hundreds of ms CPU latency. Jina-ColBERT-v2 adds multilingual (incl. Italian) at general-purpose quality.
- **ColPali:** visual-document late-interaction — embeds PDF page images directly, skips OCR. Strongest for figure-heavy / scanned docs. ECIR 2026 has a dedicated "Late Interaction and Multi-Vector Retrieval" workshop, signalling the field's gravity.
- **CPU feasibility for Aura:** Jina-ColBERT-v2 on CPU via PLAID-style index is plausible at Aura's scale (60–10k pages). Storage is the cost: ~6–10× larger index than single-vector. Probably not worth it until Aura ingests 1k+ PDFs.

## Reranking 2026

| Model | Type | Quality (rel.) | p50 latency added | Cost (1k queries) | Notes |
|---|---|---|---|---|---|
| Cohere Rerank 3.5 | API cross-encoder | ★★★★★ | 50–120 ms | ~$2 | Production default; reliability/SLA premium |
| Cohere Rerank 4 Pro | API cross-encoder | ★★★★★ | 80–200 ms | ~$3 | 32k ctx, business/finance strong |
| Voyage rerank-2 | API cross-encoder | ★★★★☆ | 80–180 ms | ~$1.50 | Multilingual; competitive vs Cohere |
| ZeroEntropy zerank-2 | API cross-encoder | ★★★★ | 100–250 ms | ~$0.05 | **40× cheaper than Cohere**, calibrated multilingual |
| BGE-reranker-v2-m3 | Local cross-encoder | ★★★★ | 200–600 ms CPU / 30 ms GPU | free | ~10× Cohere latency on CPU |
| ms-marco-MiniLM-L-6-v2 | Local cross-encoder | ★★★ | 30–80 ms CPU | free | English-only; fast prototyping |
| FlashRank | Local | ★★★ | 20–50 ms CPU | free | Quantised; lowest local CPU cost |

Reported impact: **+33–40 % accuracy for ~+120 ms latency** on multi-hop queries — ROI is strongest where Aura hurts (multi-hop wiki lookups). For Aura's CPU-only mini-PC budget, **BGE-reranker-v2-m3 quantised** or **ZeroEntropy** (cheap API) are the realistic picks; Cohere is overkill until throughput justifies the bill.

---

## Hybrid Scoring 2026 Consensus

**Convex combination beats RRF on labelled data, RRF wins zero-shot.** The 2023 paper (ACM TOIS, arXiv 2210.11934) is now the consensus reference; multiple 2026 practitioner posts confirm:

- **RRF (Reciprocal Rank Fusion):** parameter-free, robust, no training data, no shared score scale required. Best when you ship blind into a new domain or merge >2 ranking lists. **This is Aura's current setup.**
- **Convex combination (`α·norm(cosine) + (1-α)·norm(lex)`):** outperforms RRF on **every** BEIR corpus in NDCG; sample-efficient (a few hundred labelled tuples tune α). More fragile under domain shift — but Aura's domain *is* the user's own wiki, which doesn't shift.
- **Learned fusion (LightGBM / cross-encoder over feature vector):** highest ceiling, needs the most labels, hardest to debug.

**2026 take:** start RRF (you already are), instrument a labelled set from real Aura conversations, switch to convex combo with tuned α once you have ≥200 labelled queries. Keep RRF as the cold-start fallback for new MCP-backed sources.

## Agentic Retrieval — Self-RAG, CRAG, Adaptive RAG

- **Self-RAG (ICLR 2024, still the reference):** LLM emits reflection tokens (`[Retrieve]`, `[IsRel]`, `[IsSup]`, `[IsUseful]`) — model decides whether to retrieve and whether each passage helps. Requires fine-tuning ⇒ not aligned with Aura's OpenAI-compatible-only constraint.
- **CRAG (Corrective RAG):** small external evaluator scores retrieval confidence → 3 paths: (a) high → strip + generate; (b) ambiguous → keep DB results + web; (c) low → discard, search web. Adoptable as-is in Aura because the evaluator can be a tiny prompt + threshold, no fine-tune.
- **Adaptive RAG:** classifier routes "no-retrieval / single-hop / multi-hop" before paying for retrieval. Cheap and effective.
- **2026 reality:** Context windows ≥1 M tokens mean small KBs **skip retrieval entirely**. Sweet spot for agentic RAG is large dynamic KBs (Aura's wiki + MCP).

## Local Subgraph Extraction — Token-Budgeted

Consensus playbook from HippoRAG 2, LazyGraphRAG, and a Temporal-GraphRAG ICLR 2026 paper:

1. **Seed selection:** PPR seeded on query-entity nodes (NER on the question). PageRank centrality also used as a *negative* signal — avoid hub nodes that dominate every walk.
2. **Expand:** BFS to 2–3 hops, cap fan-out per node (8–16). Edge-weight = confidence label × type-affinity (entity↔entity > entity↔passage).
3. **Prune:** rank candidate nodes by `PPR_score × edge_relevance`, drop tail to fit a token budget (e.g. 2k tokens for context, 8k for full inline).
4. **Render:** linearise as `entity (alias) — relation → entity ; supporting passage ¶`. Inline tables get row-level chunks (see next section).
5. **Hub avoidance:** explicitly down-weight nodes with degree > μ + 2σ. Otherwise every walk drowns in the wiki's index page.

Temporal-GraphRAG (ICLR 2026) reports **+9.1 % answer accuracy at 97 % fewer tokens** vs SOTA using exactly this seeded-PPR + prune recipe.

## Multi-hop Reasoning over Markdown Wikis

The "Karpathy LLM-wiki" pattern (Apr 2026 gist, widely cited) crystallised what Obsidian/Logseq communities already practised: **the wiki itself is the graph**, `[[wiki-links]]` *are* the edges. Aura already lives here. Three patterns to lift:

- **claude-obsidian ingestion agent (Apr 2026):** doesn't summarise sources, it **creates entity pages + concept pages + cross-references + flags contradictions** during ingestion. Aura already does the first two via ingest pipeline — adding contradiction-detection is one extra LLM pass at ingest time.
- **Neural Composer / Smart Connections (Obsidian):** local embedding + on-graph layer overlay. The 2026 "actually works" plugins both run on-device with quantised embedding models — confirms Aura's `embeddinggemma-300m` choice is on-trend, not contrarian.
- **Multi-hop via pre-synthesis:** the wiki *answers* common multi-hop questions on pages, so retrieval is one-hop. Encoded in Aura as: when ingest produces a new fact that bridges two existing pages, write a synthesis bullet on the older page too.

## Tabular Data Ingestion Patterns

Aura's "delta automazioni" failure is exactly this category. 2026 patterns:

- **Row-as-node (TabRAG, arXiv 2511.06582, Nov 2025):** every row becomes a node, columns become typed edges to value-nodes. Customer codes / emails become first-class entities. **+45 % accuracy on table-heavy QA** vs flat-chunked baselines.
- **Structure-Aware Chunking (STC, arXiv 2605.00318):** row-level chunks with a hierarchical "row tree" preserving header context. Each row chunk = `{table_title, headers, this_row_kv}`. **40–56 % fewer chunks** than recursive/key-value baselines at same recall.
- **Entity extraction at cell level (TaBERT lineage, TableLLama):** treat each cell as a candidate entity mention, link to the wiki entity graph. Highest quality, highest LLM cost.
- **Recommended for Aura:** STC + row-as-node, *plus* entity-extraction on a small whitelist of columns ("customer code", "email", "VAT"). Bridge: a row referencing "Delta Automazioni" becomes a `mentions` edge on the company's wiki entity, so PPR seeded on "delta automazioni" pulls in the matching row in one hop.

---

## Top 5 Techniques to Adopt in Aura

Ordered by impact-per-effort, with mappings to existing primitives.

### 1. Personalized PageRank over the wiki graph (HippoRAG 2 pattern)
- **Why:** directly solves the failure case. Seed PPR on `[delta automazioni]` → returns Quadristi page + the `extract.md` row in one shot.
- **Effort:** ~1 day. `internal/storage/search/graph.go` already has `Degree` and BFS. Add `PersonalizedPageRank(seeds, alpha, iters)` using the existing adjacency. NetworkX-style PPR is ~30 lines of Go.
- **Mapping:** new tool `graph_seek(seeds, budget)` → returns ranked node list. Existing retrievers stay; this becomes the third leg of the hybrid fusion.

### 2. Row-as-node ingestion for tables (TabRAG + STC)
- **Why:** the "delta automazioni" row exists but isn't a first-class node. Today it's hidden inside a markdown table that the chunker treats as opaque text.
- **Effort:** ~2 days. Extend `internal/storage/sources/ingest` to recognise pipe-tables and Excel rows, emit each row as a wiki node with `mentions` edges to entity pages whose `[[wiki-link]]` appears in the row.
- **Mapping:** GraphIndex already supports arbitrary node types — just add a `row` kind. FTS5 keeps indexing the cells.

### 3. HyPE — index-time hypothetical query embeddings
- **Why:** Italian queries with bag-of-stopwords ("dammi", "voglio", "fammi vedere") match badly. HyPE materialises the expected queries at *write time* using Aura's existing temp=0 wiki write pipeline.
- **Effort:** ~1 day. On every wiki write, ask the LLM "list 5 likely Italian questions answered by this page". Embed each, index alongside the page embedding. Zero query-time cost.
- **Mapping:** `internal/wiki` already runs a deterministic LLM pass per write. Add one more field to the embedding sidecar.

### 4. Local cross-encoder reranking (BGE-reranker-v2-m3 quantised or ZeroEntropy)
- **Why:** +33–40 % accuracy on multi-hop for +120 ms. Aura's current hybrid returns the right page in top-10 but the agent burns tool calls picking. Rerank to top-3 with a real cross-encoder.
- **Effort:** ~2 days (local) or ~half-day (ZeroEntropy API). Mini-PC CPU budget is the constraint — BGE-m3 quantised int8 should fit at ≤300 ms p50 for top-50→top-5.
- **Mapping:** new stage between `internal/storage/search` fusion output and the agent context renderer.

### 5. CRAG-style retrieval evaluator + adaptive routing
- **Why:** today the agent always retrieves; CRAG saves the no-retrieval and low-confidence cases. Combined with Adaptive RAG classifier — "is this a fact lookup, a multi-hop reasoning, or chit-chat?" — Aura skips the wiki for chit-chat and escalates to graph+web for the hard cases.
- **Effort:** ~1 day. Small prompt + threshold; no fine-tuning needed (Aura runs OpenAI-compatible only).
- **Mapping:** pre-tool-call hook in `internal/chat/agentloop.go`; returns one of `{skip, single_shot, multi_hop_graph, fallback_web}`.

### Honourable mention — Convex-combination fusion
Worth adopting *after* you have a labelled probe set (the existing probe_chat harness can generate it). Until then, RRF stays.

---

## Pitfalls Bank

Failure modes documented by practitioners between Nov 2025 and May 2026. Read before adopting anything above.

- **Leiden community detection over-fragments knowledge graphs.** On low-average-degree graphs (typical wikis), there are exponentially many near-optimal modularity partitions ⇒ communities are **non-reproducible run-to-run**. Resolution-limit issues compound. (arXiv 2603.05207, Core-based Hierarchies for Efficient GraphRAG.)
- **Entity-resolution by name collapses distinct entities** sharing a label ("Mistral" the company vs the model vs the API). Microsoft GraphRAG docs flag this explicitly. Mitigation: secondary alias graph + ambiguity flag on the entity node.
- **Full GraphRAG indexing costs are catastrophic at scale.** $33k for one enterprise corpus reported in 2024; LazyGraphRAG fixes it but the lesson is: **never run upfront community-summarisation on a wiki whose pages change**. Aura's wiki rebuilds nightly — lazy patterns only.
- **Cross-encoder reranking adds 100–400 ms p50 and spikes under load.** If your retriever already returns the right doc top-1, rerank only hurts. Always A/B with the rerank disabled.
- **Embedding-only retrieval misses named entities** when the query is short or stop-word-heavy. ("dammi il codice cliente di X" — only "X" carries signal.) The fix is exact-lex (already in Aura) **plus** entity-NER on the query (not yet in Aura).
- **Phantom-tool / corrupted-output bugs hide behind PASS-only smoke tests.** Aura's own MEMORY rule. Every retrieval probe must verify the artifact (the actual subgraph nodes, not "200 OK").
- **PageRank picks hub nodes** unless you explicitly down-weight by degree. Aura's index page would dominate every walk.
- **HyDE injects hallucinations into the retrieval signal** when the LLM is wrong. Use the ensemble variant (avg of 3–5) or HyPE (index-time) to dampen.
- **Self-RAG requires fine-tuning** ⇒ not viable on Aura's OpenAI-compatible-HTTP-only constraint. Use CRAG instead.
- **`temperature > 0` on wiki writes breaks reproducibility** of the graph. Aura already enforces temp=0; do not regress this when adding HyPE.
- **Reranker cost at >1 M queries/month is non-trivial** but at Aura's single-user scale it's noise. Don't pre-optimise.
- **Context-window growth (1 M+ tokens) tempts teams to skip retrieval entirely.** For a 60-page wiki, that *almost* works — but graph traversal still wins on freshness, citation accuracy, and tool-call budget.
- **OCR for PDFs duplicates table content as flat text;** ColPali-style visual retrieval skips OCR but explodes index size 6–10×. Aura keeps OCR + STC chunking for now.

## Evaluation Benchmarks

For measuring Aura post-adoption:

- **RAGTruth (arXiv 2401.00396):** 18k responses annotated for hallucination type+severity. Use for the new evaluator + retrieval-confidence layer.
- **BEIR:** 18 datasets / 9 task types; the standard zero-shot retrieval benchmark — useful for sanity-checking new embedding swaps.
- **GraphRAG-Bench (ICLR 2026, `github.com/GraphRAG-Bench/GraphRAG-Benchmark`):** end-to-end graph-RAG eval covering construction, retrieval, and generation across difficulty tiers. Closest to Aura's actual surface.
- **BenchmarkQED (Microsoft, 2026):** the framework Microsoft used to validate LazyGraphRAG — local + global queries, automated judge.
- **EnterpriseRAG-Bench (arXiv 2605.05253):** internal-knowledge corpora — closer to Aura's "personal second brain" shape than BEIR.
- **LiveRAG (arXiv 2511.14531):** difficulty-tiered Q&A; useful for routing classifier (Adaptive RAG).
- **FreshQA:** time-sensitive Q&A — relevant once Aura's web_search + wiki freshness signals are wired.

---

## Sources

- [Microsoft GraphRAG repository](https://github.com/microsoft/graphrag)
- [LazyGraphRAG sets a new standard for GraphRAG quality and cost (Microsoft Research)](https://www.microsoft.com/en-us/research/blog/lazygraphrag-setting-a-new-standard-for-quality-and-cost/)
- [BenchmarkQED: Automated benchmarking of RAG systems (Microsoft Research)](https://www.microsoft.com/en-us/research/blog/benchmarkqed-automated-benchmarking-of-rag-systems/)
- [From Local to Global: A Graph RAG Approach to Query-Focused Summarization (arXiv 2404.16130)](https://arxiv.org/pdf/2404.16130)
- [GraphRAG-Bench (ICLR 2026)](https://github.com/GraphRAG-Bench/GraphRAG-Benchmark)
- [HippoRAG repository (OSU-NLP-Group)](https://github.com/osu-nlp-group/hipporag)
- [HippoRAG: Neurobiologically Inspired Long-Term Memory for LLMs (arXiv 2405.14831)](https://arxiv.org/pdf/2405.14831)
- [HippoRAG 2: Advancing Long-Term Memory and Contextual Retrieval (MarkTechPost, Mar 2026)](https://www.marktechpost.com/2025/03/03/hipporag-2-advancing-long-term-memory-and-contextual-retrieval-in-large-language-models/)
- [LightRAG repository (HKUDS, EMNLP 2025)](https://github.com/hkuds/lightrag)
- [LightRAG: Simple and Fast Retrieval-Augmented Generation (arXiv 2410.05779)](https://arxiv.org/html/2410.05779v1)
- [LightRAG Open-Source Framework Delivers Graph-Based Dual-Level Retrieval (Proudfrog, Feb 2026)](https://proudfrog.com/en/news/2026-02-24-lightrag-open-source-framework-delivers-graph-based-dual-level)
- [GraphReader: Graph-Based Agent for LLMs (arXiv 2406.14550)](https://arxiv.org/abs/2406.14550)
- [GraphPlanner: Graph Memory-Augmented Agentic Routing (ICLR 2026)](https://github.com/ulab-uiuc/GraphPlanner)
- [An Analysis of Fusion Functions for Hybrid Retrieval (arXiv 2210.11934, ACM TOIS)](https://dl.acm.org/doi/10.1145/3596512)
- [Advanced RAG — Understanding Reciprocal Rank Fusion in Hybrid Search (glaforge, Feb 2026)](https://glaforge.dev/posts/2026/02/10/advanced-rag-understanding-reciprocal-rank-fusion-in-hybrid-search/)
- [PLAID: An Efficient Engine for Late Interaction Retrieval (arXiv 2205.09707)](https://arxiv.org/pdf/2205.09707)
- [ColBERTv2: Effective and Efficient Retrieval via Lightweight Late Interaction (arXiv 2112.01488)](https://arxiv.org/abs/2112.01488)
- [Jina-ColBERT-v2: A General-Purpose Multilingual Late Interaction Retriever (arXiv 2408.16672)](https://arxiv.org/pdf/2408.16672)
- [LIR: First Workshop on Late Interaction and Multi-Vector Retrieval @ ECIR 2026](https://arxiv.org/html/2511.00444)
- [Best Reranker Models for RAG: Open-Source vs API Comparison 2026 (BSWEN)](https://docs.bswen.com/blog/2026-02-25-best-reranker-models/)
- [Reranking in RAG: Cross-Encoders, Cohere Rerank & FlashRank (Mar 2026)](https://medium.com/@vaibhav-p-dixit/reranking-in-rag-cross-encoders-cohere-rerank-flashrank-c7d40c685f6a)
- [Cohere Rerank documentation](https://docs.cohere.com/docs/rerank)
- [Self-RAG: Learning to Retrieve, Generate, and Critique through Self-Reflection (arXiv 2310.11511)](https://arxiv.org/pdf/2310.11511)
- [Agentic RAG: The 2026 Production Guide (MarsDevs)](https://www.marsdevs.com/guides/agentic-rag-2026-guide)
- [Graph RAG in 2026: A Practitioner's Guide to What Actually Works (Graph Praxis, Medium)](https://medium.com/graph-praxis/graph-rag-in-2026-a-practitioners-guide-to-what-actually-works-dca4962e7517)
- [Obsidian, Wikis, and Agentic RAG (KAIRI / Medium, Apr 2026)](https://medium.com/kairi-ai/obsidian-wikis-and-agentic-rag-which-knowledge-base-gives-you-the-edge-dd496914404e)
- [LLM Wiki Karpathy: Knowledge Base with Claude and Obsidian (Apr 2026)](https://anthemcreation.com/en/artificial-intelligence/karpathy-llm-wiki-claude-obsidian/)
- [TabRAG: Improving Tabular Document Question Answering (arXiv 2511.06582)](https://arxiv.org/pdf/2511.06582)
- [Structure-Aware Chunking for Tabular Data in RAG (arXiv 2605.00318)](https://arxiv.org/html/2605.00318)
- [TaBERT: Pretraining for Joint Understanding of Textual and Tabular Data](https://www.researchgate.net/publication/343300349_TaBERT_Pretraining_for_Joint_Understanding_of_Textual_and_Tabular_Data)
- [Core-based Hierarchies for Efficient GraphRAG (arXiv 2603.05207)](https://arxiv.org/pdf/2603.05207)
- [Youtu-GraphRAG: Vertically Unified Agents for Graph Retrieval-Augmented Complex Reasoning (arXiv 2508.19855)](https://arxiv.org/pdf/2508.19855)
- [HyDE — Using Hypothetical Document Embeddings to Improve Retrieval (Haystack/deepset)](https://haystack.deepset.ai/cookbook/using_hyde_for_improved_retrieval)
- [Better RAG with HyDE (Zilliz Learn)](https://zilliz.com/learn/improve-rag-and-information-retrieval-with-hyde-hypothetical-document-embeddings)
- [RAGTruth: A Hallucination Corpus for Trustworthy RAG (arXiv 2401.00396)](https://arxiv.org/pdf/2401.00396)
- [LiveRAG: A Diverse Q&A Dataset for RAG Evaluation (arXiv 2511.14531)](https://arxiv.org/pdf/2511.14531)
- [EnterpriseRAG-Bench: A RAG Benchmark for Company Internal Knowledge (arXiv 2605.05253)](https://arxiv.org/html/2605.05253v1)
- [RAG Benchmarks Leaderboard 2026 (Awesome Agents)](https://awesomeagents.ai/leaderboards/rag-benchmarks-leaderboard/)
