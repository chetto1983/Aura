# Reranker candidates — Phase-WIKI-B Wave C (2026-05-21)

## TL;DR

**Stick with `gpustack/gte-multilingual-reranker-base-GGUF` (Alibaba mGTE).** It is the only Apache-2.0 multilingual reranker in the 100M–500M band that (a) runs natively on llama-server via `/rerank`, (b) has published gains over BGE-v2-m3 on MIRACL (+2.8 nDCG@10 average) at half the parameters, and (c) is already on the user's radar with a verified GGUF quant. Every 2025/2026 "SOTA" alternative I evaluated trips on license (jina-v2 = CC-BY-NC, jina-v3 = unclear + llama.cpp fork-only), runtime (Qwen3-Reranker is broken on mainline llama-server per issue #16407, no native /rerank endpoint, requires yes/no logit trick), parameter budget (mxbai-rerank-large-v2 = 1.5B–2B), or language coverage (gte-reranker-modernbert-base = English-only). **Revisit in 6 months** once jina-reranker-v3 lands in llama.cpp mainline; until then mGTE is the lowest-risk choice.

## Comparison table

| Model | Params | Quant size (Q4_K_M) | License | Multilingual coverage | Italian? | Framework | CPU latency (rerank ~50 docs) | Gotchas |
|---|---|---|---|---|---|---|---|---|
| **gte-multilingual-reranker-base** (Alibaba mGTE) | 306M | 235 MB | Apache-2.0 | 70+ langs | Yes | **llama.cpp `/rerank` native** + transformers + sentence-transformers | Not published; extrapolated ~80–150 ms based on 14× encode speedup vs BGE-M3 and 306M < BGE-v2-m3's 568M | Trust_remote_code=true for transformers path; llama.cpp path avoids that |
| bge-reranker-v2-m3 (BAAI) | 568M | ~400 MB est. | Apache-2.0 | 100+ langs | Yes | llama.cpp `/rerank` native (sinjab/llamacpp-rerankers confirmed working) | **~350 ms for 3 docs on CPU** per BSWEN 2026 benchmark — extrapolates to ~5–6 s for 50 docs → unusable | Roughly 2× params of mGTE; MIRACL avg 65.7 vs mGTE 68.5 |
| jina-reranker-v2-base-multilingual | 278M | 222 MB | **CC-BY-NC-4.0** | 26+ langs incl. IT | Yes | llama.cpp `/rerank` (only model llama-server gets numerically right per issue #16407 reporter) | Not published | **License blocks self-hosted commercial use → OUT for Aura platform pivot** |
| jina-reranker-v3 | 595M (0.44B non-embed) | ~400 MB est. | Unclear / not stated on launch | 18 langs eval (IT not explicit; MKQA covers 25 langs incl. IT) | Likely | transformers + GGUF (jina-fork) + MLX; **mainline llama.cpp = open feature request (issue #17189, Nov 2025, stale)** | Not published for CPU | Best raw quality (MIRACL 66.83, BEIR 61.94) but listwise reranker → unclear if maps to /rerank pairwise endpoint at all |
| Qwen3-Reranker-0.6B | 600M | 395 MB | Apache-2.0 | 100+ langs incl. IT | Yes | transformers + vLLM; GGUF exists but **broken in mainline llama-server per issue #16407** (scores like 4.5e-23 instead of relevance). One verified-working GGUF discussion exists. Uses yes/no logit trick, not native /rerank | Not published | Multiple gotchas: broken pooling, requires custom logit-based scoring, sentence-transformers path = ~300–500 MB resident model. Best Mungert quant Q4_K_M = 395 MB but pooling needs "empty" not "rank" |
| mxbai-rerank-base-v2 (Mixedbread) | 500M | n/a (no GGUF in main repo; community 4-bit exists) | Apache-2.0 | 100+ langs | Yes | Python lib (`mxbai-rerank`) + Mixedbread API; **no llama.cpp /rerank path** | Not published for CPU; only A100 GPU latency reported (~670 ms/query on GPU) | Requires separate Python sidecar → second runtime to maintain. Mr.TyDi 28.56 nDCG@10 multilingual (lower than mGTE on MIRACL apples-to-oranges) |
| gte-reranker-modernbert-base (Alibaba) | 149M | n/a (no GGUF) | Apache-2.0 | **English-only** | **No** | transformers + TEI | n/a | Out — monolingual |
| mxbai-rerank-large-v2 | 1.5B | ~1 GB+ | Apache-2.0 | 100+ langs | Yes | Python lib + community Mungert GGUF exists | Not published | Above 500M target band; too slow on 4-thread CPU |
| BAAI bge-reranker-v2.5-gemma2-lightweight | 9B-based (lightweight ops) | n/a | Apache-2.0 | Multilingual | Yes | transformers | n/a | Way above param budget; built for layerwise compression on GPU |

## Top 3 detailed evaluation

### 1. gte-multilingual-reranker-base (mGTE) — RECOMMENDED

**Strengths.** Encoder-only architecture, 306M params, Apache-2.0, 70+ languages, native `/rerank` endpoint in llama-server via `gpustack/gte-multilingual-reranker-base-GGUF` (Q4_K_M = 235 MB, Q8_0 = 332 MB). MIRACL average nDCG@10 = 68.5 — beats BGE-reranker-v2-m3 (65.7) with half the parameters. Paper claims 14× encoding speedup over BGE-M3 family. Up to 8192-token context — matches our wiki chunk size. Same llama.cpp sidecar pattern we already run for embeddinggemma → zero new runtime.

**Weaknesses.** Per-language MIRACL breakdown for Italian not published in paper or model card; we'll need to measure locally on Aura's IT wiki test fixtures (Wikipedia IT + ISTAT). The original transformers path requires `trust_remote_code=true` (architecture is "NewForSequenceClassification") — irrelevant for llama.cpp path. No published CPU latency for 50-doc batch; extrapolation from architecture and 14× encode speedup suggests ~80–150 ms but must be measured.

**Integration cost.** Near zero. Add a second llama-server container clone pointing at the rerank GGUF on port :18081, expose env vars `RERANK_BASE_URL` + `RERANK_MODEL`, call `POST /rerank` (OpenAI-compatible body). ~50 LOC client wrapper in `internal/storage/search/` + compose service. Same Mini-PC CPU budget rules apply: cap at 4 threads, use --network=host or docker network (not 127.0.0.1 host loopback).

### 2. jina-reranker-v3 — REVISIT LATER

**Strengths.** Newest (Oct 2025) and highest reported quality in the band — MIRACL 66.83 nDCG@10, BEIR 61.94, beats Qwen3-Reranker-4B at 6× smaller size. 0.6B params with 0.44B non-embedding → effectively pays only for the smaller "active" forward pass. "Last but not late interaction" listwise design reranks N documents in a single forward pass instead of N pairwise scores — should be faster wall-clock for top-50 batches than any cross-encoder.

**Weaknesses.** **License not stated on launch page** — must be verified before commit (Jina's history: v2 = CC-BY-NC = non-commercial). Mainline llama.cpp does NOT support it (issue #17189 open & stale since Nov 2025); only a Jina fork works. Listwise endpoint shape differs from standard `/rerank` pairwise scoring — Aura's wrapper would need a v3-specific code path. Italian not in the 18-lang MIRACL eval; only MKQA covers IT.

**Integration cost.** High right now: would require shipping a Jina llama.cpp fork as our sidecar (parallel to mainline) OR a separate transformers runtime + Python sidecar. Both add ops surface. Revisit when issue #17189 closes or when Aura needs the quality bump for prod scale.

### 3. Qwen3-Reranker-0.6B — REJECTED

**Strengths.** Apache-2.0, 100+ languages confirmed incl. Italian, 32k context, strong MMTEB-R = 66.36 / MTEB-R = 65.80. Same Qwen3 family as widely deployed embedders. Mungert GGUF Q4_K_M = 395 MB. The user's memory `feedback_check_antirez_repos_for_inference_sidecars` calls out Qwen-family models as a strong CPU-sidecar fit (qwen-asr verified 7.99× RTF).

**Weaknesses.** **Broken in mainline llama-server `/rerank`** per issue #16407 (Oct 2025): produces scores like 4.5e-23 instead of relevance values. Native scoring uses a yes/no logit trick on a generative LM → requires custom client (logprob extraction), not the standard `/rerank` body. sinjab/llamacpp-rerankers lists Qwen as "pooling-incompatible / empty pooling". One community discussion claims a working Windows/Linux conversion exists but it's a one-off, not blessed. Forward pass is decoder-only LLM-style → slower per-doc than a 306M encoder for the same batch.

**Integration cost.** Medium-high: would need a custom client wrapper extracting yes/no logits from `/completion` or `/v1/chat/completions`, NOT the clean `/rerank` endpoint. Loses the architectural symmetry with our embedding sidecar.

## Llama.cpp reranker support

Status as of May 2026 (sources: llama.cpp PR #9510, issues #8555 / #16407 / #17189, sinjab/llamacpp-rerankers wrapper):

**Mainline `/rerank` endpoint** (since PR #9510 merged in 2024): requires `--embedding --pooling rank`.

**Confirmed working** with correct numeric scores:

- `bge-reranker-base`, `bge-reranker-large`
- `bge-reranker-v2-m3` (the canonical reference model)
- `bge-reranker-v2-gemma`
- `jina-reranker-tiny`, `jina-reranker-v1`, `jina-reranker-v2-multilingual`, `jina-reranker-m0`
- `msmarco-L4`, `msmarco-L12`
- **`gte-multilingual-reranker-base`** via gpustack GGUF (architecture "NewForSequenceClassification" was added)

**Broken or pooling-incompatible** (require `--pooling none` + custom client, NOT native /rerank):

- Qwen3-Reranker (0.6B / 4B / 8B) — scores collapse to ~1e-28
- mxbai-rerank-v2 (base + large)
- ColBERT-style models

**Not yet supported** (mainline):

- jina-reranker-v3 (feature request #17189 open and labeled stale since Nov 2025; works only on a Jina-maintained llama.cpp fork)

Practical implication for Aura: **the only Apache-2.0 multilingual rerankers that work natively on our existing llama.cpp sidecar pattern are gte-multilingual-reranker-base and bge-reranker-v2-m3.** mGTE wins on quality and size; BGE-v2-m3 is the fallback if mGTE's IT performance disappoints on local measurement.

## Recommendation — Story B12 update

**Wire `gpustack/gte-multilingual-reranker-base-GGUF` at Q5_K_M (248 MB) as the Phase-WIKI-B Wave C reranker.**

**Why Q5_K_M not Q4_K_M:** an extra 13 MB and a tiny CPU cost in exchange for the lowest published quality loss vs full-precision baseline; reranking is short-context and budget already absorbs it. If local benchmark shows Q5_K_M > 200 ms for 50 docs on 4 threads, fall back to Q4_K_M (235 MB).

**Integration path — llama.cpp sidecar (no new runtime).**

1. Add `aura-reranker` service to `compose.yaml` (clone the existing `aura-embed` llama-server pattern):

   - Image: same llama.cpp image as embed sidecar.
   - Args: `--hf-repo gpustack/gte-multilingual-reranker-base-GGUF --hf-file ...Q5_K_M.gguf --embedding --pooling rank --port 18081 --threads 4 -ngl 0`
   - Network: same docker network as `aura` (NOT host loopback — see memory `feedback_hyperv_port_forwarding_lie`).
   - Init: extend `aura-init-models` to also fetch the rerank GGUF on first boot (reuse `internal/install/` pattern from Wave 2.10).
2. Add config keys `RERANK_BASE_URL` (default `http://aura-reranker:18081`), `RERANK_MODEL`, `RERANK_TIMEOUT_MS` (default 2000) in `internal/config/`.
3. Add a `Reranker` interface + llama.cpp client in `internal/storage/search/rerank.go` calling `POST /rerank` (OpenAI-compatible body: `{"query": "...", "documents": ["...", "..."]}`). Keep retrieval and rerank separate — Reranker takes a `[]Candidate` and returns the same slice re-ordered with a new `RerankScore` field. Never mutate the upstream Qdrant scores.
4. Wire it into the retrieval pipeline AFTER Qdrant top-100 and BEFORE BFS graph expansion (so graph expansion has the cleanest seed set).
5. Settings catalog entry in `internal/api/settings.go` exposing the model toggle + threshold to the dashboard. Default OFF until Aura-side IT benchmark confirms gains; turn ON when WIKI-B Wave C test fixtures show measurable improvement.

**Expected CPU latency (mini-PC, 4 threads, Q5_K_M, batch of 50 docs at ~512 tokens each):** **target 80–200 ms total**, no published number exists. Will be measured against the WIKI-B test matrix fixtures (Apache Tika, ISTAT, arXiv, Wikipedia IT, Project Gutenberg IT). Budget gate: if rerank wall-clock + Qdrant top-100 exceeds 1 s on the Mini-PC, Q4_K_M fallback OR top-50 candidate cap kicks in.

**Verification (Aura-style probe, NOT just smoke):**

- E2E probe via `cmd/probe_chat` asking a question whose answer is split across 3+ IT wiki pages; assert that pre-rerank vs post-rerank top-5 differs AND the post-rerank top-5 contains the ground-truth page (verified against SQLite `wiki_documents` table). PASS iff (a) reply quality improves on at least 4 of 6 IT test queries, (b) wall-clock < 300 ms p95 rerank step, (c) rerank scores are non-degenerate (no all-zero, no all-equal). Smoke ("200 OK from /rerank") is necessary but not sufficient.

**Revisit triggers — switch off mGTE only if:**

- jina-reranker-v3 lands in mainline llama.cpp `/rerank` (track issue #17189) AND license clarified to Apache-2.0/MIT, OR
- Aura's IT benchmark on real fixtures shows < 5% gain over no-rerank baseline (mGTE under-delivers for our corpus), OR
- A new model in the 100M–500M band ships with explicit IT MIRACL nDCG@10 > 70 AND native llama.cpp support.

## Sources

- [gpustack/gte-multilingual-reranker-base-GGUF · Hugging Face](https://huggingface.co/gpustack/gte-multilingual-reranker-base-GGUF)
- [Alibaba-NLP/gte-multilingual-reranker-base · Hugging Face](https://huggingface.co/Alibaba-NLP/gte-multilingual-reranker-base)
- [mGTE paper (arXiv 2407.19669)](https://arxiv.org/html/2407.19669v1)
- [gpustack/jina-reranker-v2-base-multilingual-GGUF · Hugging Face](https://huggingface.co/gpustack/jina-reranker-v2-base-multilingual-GGUF)
- [Jina Reranker v3 announcement](https://jina.ai/news/jina-reranker-v3-0-6b-listwise-reranker-for-sota-multilingual-retrieval/)
- [Feature Request: jina-reranker-v3 in llama.cpp (#17189)](https://github.com/ggml-org/llama.cpp/issues/17189)
- [llama-server rerank broken with most models (#16407)](https://github.com/ggml-org/llama.cpp/issues/16407)
- [llama.cpp PR #9510 — add reranking support](https://github.com/ggml-org/llama.cpp/pull/9510)
- [sinjab/llamacpp-rerankers wrapper (supported model list)](https://github.com/sinjab/llamacpp-rerankers)
- [Qwen/Qwen3-Reranker-0.6B](https://huggingface.co/Qwen/Qwen3-Reranker-0.6B)
- [Mungert/Qwen3-Reranker-0.6B-GGUF](https://huggingface.co/Mungert/Qwen3-Reranker-0.6B-GGUF)
- [mixedbread mxbai-rerank-v2 blog](https://www.mixedbread.com/blog/mxbai-rerank-v2)
- [Alibaba-NLP/gte-reranker-modernbert-base (English-only)](https://huggingface.co/Alibaba-NLP/gte-reranker-modernbert-base)
- [BAAI/bge-reranker-v2-m3](https://huggingface.co/BAAI/bge-reranker-v2-m3)
- [BSWEN 2026 reranker CPU benchmark (BGE-v2-m3 350 ms/3 docs)](https://docs.bswen.com/blog/2026-02-25-best-reranker-models/)
- [FutureAGI Best Rerankers for RAG 2026](https://futureagi.com/blog/best-rerankers-for-rag-2026)
