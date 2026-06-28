# retrieval rerank rag

Covers two tracks proven by spikes: (A) a GPU cross-encoder **reranker** added to the
retrieval pipeline (spike 070), and (B) the v1.0.0 **upload→chat RAG hardening** — Item 1
images-searchable, Item 2 knowledge-catalog injection, Item 3 gated RAGAS eval, plus the
RET native-pre-filter fix (spikes 075/076/077, Session-20, all VALIDATED + IMPLEMENTED 2026-06-28).

## Requirements

These are hard constraints from the MANIFEST (Session-20 + Session-11), not suggestions.

1. **Rerank ships as Qwen3-Reranker-0.6B Q4_K_M on GPU — CPU is forbidden.** CPU = 23 s for
   15 docs (dead, ~70–1000× too slow). GPU sidecar = 333 ms, perfect-1.000 correctness,
   Apache-2.0, **<1 GB VRAM** (949 MiB; co-resides with `aura-ocr-vl` on the 4 GB A2000).
   GPU-absent / CPU-only deployments run **rerank OFF → RRF fallback** (graceful degrade,
   never a hard fail). Self-learning is DEFERRED (overengineering until production miss-data exists).
   Mechanism: `aura-rerank` compose sidecar `:8085` (`ghcr.io/ggml-org/llama.cpp:server-cuda`),
   `internal/rerank/client.go` mirroring `internal/documents/embedder.go`, env `AURA_RERANK_BASE_URL`.

2. **Rerank pipeline order is vector/BM25 seeds → rerank the ~10 SEEDS → graph-expand the WINNERS.**
   NOT expand-then-rerank. Cost is `pool_size × doc_length`; reranking 27 expanded long docs = 1.4 s,
   reranking ~10 short seeds = 267 ms (5×). Seeds already contain the answer; expansion is for LLM
   context, not candidate generation. Keep RRF (spike 056) as the zero-model fusion + reranker-off fallback.

3. **Item 1 (images-searchable) is an `internal/assets` routing change, NOT a pipeline build.**
   The `documents` pipeline already OCRs + indexes images end-to-end (`documents.supportedDocumentExt`
   includes `.png/.jpg/.jpeg/.gif/.webp`; markitdown `_extract_image` OCRs via GLM-OCR → chunk →
   Granite embed → Neo4j). The only gap is `internal/assets/limits.go:documentExts` excluding images,
   so `InferModality` → `ModalityImage` → `ImageProcessor` (vision summary only). Give an uploaded
   image a **dual path**: keep `ImageProcessor` vision summary for inline chat AND run
   `DocumentProcessor` so it indexes. Run the two **sequentially** to avoid 4 GB-GPU contention.
   IMPLEMENTED: `internal/assets/image_document_processor.go` (`ImageDocumentProcessor`), wired in
   `cmd/aura/document_processor_wiring.go`; **no** `limits.go`/`InferModality` change (images stay `ModalityImage`).

4. **Scoped retrieval MUST use a native `document_id` PRE-filter, never global-top-k-then-filter.**
   `db.index.vector.queryNodes($k) … WHERE document_id=$id` post-filters, so a freshly-uploaded small
   doc is crowded out of the global top-k by the operator's large live graph for generic queries even
   when the answer is in the doc. Fix: `MATCH (:Chunk {document_id}) … vector.similarity.cosine` over
   that doc's own chunks. IMPLEMENTED: `internal/documents/retrieve.go` `vectorSeed` →
   `docScopedVectorSeedQuery` when `document_id` is set; unscoped path unchanged; fail-soft to fulltext
   when a chunk has no embedding yet. (Neo4j 5.26 has `vector.similarity.cosine`.)

5. **Item 2 (catalog injection) sources from `assets.Service.ListForThread` and injects via the
   cache-safe dynamic seam, NEVER `messages[0]`.** Block = one line per doc (`document_id` + filename +
   `Asset.Summary`), thread- + identity-(promoted-)scoped, framed as operator-pinned context, and MUST
   include the `document_id` so the agent scopes `document_search` (pairs with req 4). It changes
   per-conversation, so it belongs in the `ContextBlock`/search-context tail (the attachment-block family),
   never the static prefix. IMPLEMENTED: `internal/assets/context.go` (`BuildKnowledgeCatalog` +
   `WithContextBlocks`); injected per-turn in `internal/agui/server.go` (searchable docs minus this turn's
   attachments, cap 30 docs / 200-rune summaries) so `messages[0]/[1]` stay byte-stable.

6. **Item 3 eval is a GATED RAGAS gate (faithfulness ≈ 1.0 + answer_correctness), NOT a single 0–10
   judge, and NOT in CI.** A context-free single-0–10 judge scored 10/10 to BOTH grounded and
   hallucinated answers — it cannot catch a fluent hallucination. Stop chasing a literal ">9.8".
   Load-bearing operationalization: **uv-managed Python 3.12 venv** (system 3.14 has no wheels);
   **pin `ragas==0.2.15` + langchain/-core/-community `>=0.3,<0.4` + langchain-openai `>=0.2,<0.3`**;
   judge = OpenRouter `deepseek/deepseek-v4-flash`; embeddings = free local granite `:8081`;
   `AnswerCorrectness` needs its `AnswerSimilarity(embeddings=…)` sub-metric wired explicitly.
   IMPLEMENTED: `scripts/eval/ragas/` + `docs/rag-answer-eval.md`; advisory rows in `docs/aura-quality-snapshot.md`.

Plus the standing Session-11 retrieval requirement: **production retrieval must down-rank
table-of-contents hits** (or resolve them to their actual section page) before showing a citation —
the reranker (req 1/2) is the mechanism that kills the TOC false-positive.

## How to Build It

### A. Rerank sidecar + Go client (spike 070, plan = `sources/070-.../RERANK-PLAN.md`)

Compose service (mirror `aura-ocr-vl`, lazy/optional, nvidia deploy block, loopback `:8085`):

```yaml
aura-rerank:
  image: ghcr.io/ggml-org/llama.cpp:server-cuda
  command: >
    --hf-repo Voodisss/Qwen3-Reranker-0.6B-GGUF-llama_cpp
    --hf-file Qwen3-Reranker-0.6B-Q4_K_M.gguf
    --reranking --pooling rank -ngl 99 -c 2048 -t 4
  # ports 8085 loopback; nvidia gpu deploy block
```

Go client `internal/rerank/client.go` — copy `internal/documents/embedder.go` (`EmbeddingClient`)
exactly: same construction, timeout, env (`AURA_RERANK_BASE_URL`). Shape:
`RerankClient{BaseURL, Client}.Rerank(ctx, query string, docs []string) ([]Scored, error)` →
`POST /v1/rerank`, results sorted desc. Fail-soft: sidecar down / GPU absent → return input order
unchanged (identity), log once, never block retrieval.

The proven `/v1/rerank` wire contract (from `sources/070-.../g220_rerank.py`):

```python
RERANK = "http://aura-rerank:8085/v1/rerank"
# cap per-pair tokens to fit -c 2048:
docs = [(d[:480] if d.strip() else "n/a") for d in docs]
r = requests.post(RERANK, json={"query": q[:480], "documents": docs}, timeout=60)
results = [(x["index"], x["relevance_score"]) for x in r.json()["results"]]
# map index back to your candidate list, sort by relevance_score desc
```

Wire into retrieval in the fast order (req 2): vector/BM25 seeds → rerank ~10 seeds → graph-expand
top-K winners for LLM context. Highest-value surfaces first: (1) memory recall (Phase-15 agent-memory
path — noisiest), (2) document retrieval, (3) `tool_search`/`internal/semindex` Ranker is LOW priority
(corpus small, embedding already wins per spikes 054–058). Add a non-monotonic guard: keep vector order
if the reranker's top score < threshold (prevents the "back-to-box" demotion seen in spike 070).

### B. Item 1 — images-searchable (spike 075, `sources/075-.../main.go`)

Done as `internal/assets/image_document_processor.go`: a fail-soft composite over the `Image` slot
that runs the vision summary AND delegates to one shared `DocumentProcessor` (sequentially). The image
→ markitdown `/extract` → GLM-OCR → chunk → Granite embed → Neo4j path is already live-proven; the
build was purely to add the second leg. Verify with `go test -race ./internal/assets/` (merge logic +
negative: an image with no text must NOT be falsely searchable).

### C. RET pre-filter (spike 075 finding → `internal/documents/retrieve.go`)

`vectorSeed` branches: when a `document_id` is supplied, run `docScopedVectorSeedQuery` —
`MATCH (:Chunk {document_id}) … vector.similarity.cosine` — instead of the global
`db.index.vector.queryNodes(k)`-then-`WHERE`. Unscoped retrieval is untouched. There is a live
`neo4j_integration` test proving a generic query scoped to a small seeded doc returns its chunk against
the live graph with a decoy present (the spike-075 crowding case, fixed).

### D. Item 2 — catalog injection (spike 077, `sources/077-.../main.go`)

`internal/assets/context.go`: `BuildKnowledgeCatalog` builds one compact line per doc
(`document_id` + filename + `Asset.Summary`) from `assets.Service.ListForThread`; `WithContextBlocks`
attaches it. `internal/agui/server.go` injects it per-turn (searchable docs minus this turn's
attachments, cap 30 docs / 200-rune summaries) prepended to the user turn alongside the attachment block,
leaving `messages[0]/[1]` byte-stable. Frame the block as operator-pinned context ("not a request, not
untrusted tool output"). Telegram-channel adoption is a follow-up. Verify with
`go test ./internal/agui/ -run TestServerRun` + `go test -race ./internal/assets/`.

### E. Item 3 — gated RAGAS eval (spike 076, `sources/076-.../ragas_probe.py` + `run.sh`)

Reusable harness lives at `scripts/eval/ragas/`: `rag_answer_eval.py` reads a committed
`reference_qa.json` + thresholds; `--dry-run` validates wiring for free; `run.sh` provisions the pinned
uv venv. Stack wiring (proven):

```python
# judge = OpenRouter; embeddings = local granite (free, no extra sidecar)
judge = ChatOpenAI(base_url="https://openrouter.ai/api/v1", model="deepseek/deepseek-v4-flash")
emb   = OpenAIEmbeddings(base_url="http://aura-llama-embed:8081/v1",
                         check_embedding_ctx_length=False)
# AnswerCorrectness MUST get its similarity sub-metric explicitly:
answer_correctness = AnswerCorrectness(answer_similarity=AnswerSimilarity(embeddings=emb))
```

Gate = faithfulness ≈ 1.0 AND answer_correctness above the task threshold + a rubric. Run it gated/manual
(like `internal/eval` cot_eval), ~35 bounded OpenRouter calls/run (well under a cent). The committed
baseline is spike 076's validated numbers (faithfulness 1.000/0.000, answer_correctness 0.980/0.229).

## What to Avoid

- **Do NOT run the reranker on CPU.** 23,130 ms for 15 docs (14.9–35.8 s). Dead. GPU is mandatory.
- **Do NOT use jina-reranker-v3.** IQ3_XXS returns **all-zero scores** (broken — llama.cpp issue
  #17189, listwise arch not supported yet) AND it is **CC BY-NC** (cannot ship in the commercial
  appliance). Rejected on both counts.
- **Do NOT pick a lower-bit quant thinking it's faster/smaller.** Q3_K_M = 447 ms (SLOWER than
  Q4_K_M's 333 ms — worse GPU kernel) with no quality gain; on GPU the cost is pass-count, not model
  size. Q2_K / IQ3 unusable. **Q4_K_M only.**
- **Do NOT use a random community GGUF conversion.** Use the **`Voodisss`** GGUF; other conversions
  miss `cls.output.weight` → garbage scores.
- **Do NOT expand-then-rerank.** Reranking the 27-node expanded pool of long docs = 1.4 s (5× slower).
  Rerank the ~10 seeds, expand the winners afterward.
- **Do NOT trust rerank blindly — it is not monotonic.** It mildly HURT one case ("back-to-box",
  demoting the clean answer) and was neutral on an already-correct one. Needs an eval + a confidence
  guard, not blind trust.
- **Do NOT build the self-learning loop now.** No production miss-data exists; the free oracle is
  self-limited (spike 057). Building it now is overengineering — ship the static reranker first.
- **Item 1: do NOT frame it as a "bigger asset-modality refactor."** The pipeline already exists. It is
  a small routing change. Do NOT change `limits.go`/`InferModality` (keep images `ModalityImage`); add
  the second leg in a composite processor and run it sequentially (4 GB GPU contention).
- **Do NOT keep the global-top-k-then-filter scoped seed.** A small/new doc gets crowded out for generic
  queries against a populated graph (the failure that made spike 075 v1 fail two assertions). Use the
  native `document_id` pre-filter.
- **Do NOT inject the catalog into `messages[0]`** (or `messages[1]`). It changes per-conversation and
  would break the prompt cache. Use the dynamic ContextBlock/tail seam.
- **Item 3: do NOT keep a single 0–10 LLM judge / a ">9.8" target.** It scored 10/10 to BOTH a grounded
  AND a hallucinated answer (×3) — a fluency score cannot detect a fluent hallucination, and "9.8" is
  uncalibrated noise.
- **RAGAS install landmines:** system Python 3.14 has **no wheels** for the RAGAS stack — must use uv
  py3.12. Unpinned `uv` resolves langchain 1.x → `ragas==0.2.15` **crashes importing**
  `langchain_community.chat_models.vertexai` (removed in 0.4). The 0.3.x pin is load-bearing.
  `AnswerCorrectness` without an explicit `AnswerSimilarity(embeddings=…)` → `AssertionError:
  AnswerSimilarity must be set`.
- **Harness gotcha (spike 077):** zero-value web-tool structs are `Spec()`-safe but their `Execute`
  panics on nil internals; `execute_batch`'s recover catches it (`recovered panic site=execute_batch`).
  Spec-safe ≠ Execute-safe — don't register zero-value tools in production.

## Constraints

- **Reranker model:** Qwen3-Reranker-0.6B Q4_K_M, GGUF repo `Voodisss/Qwen3-Reranker-0.6B-GGUF-llama_cpp`,
  file `Qwen3-Reranker-0.6B-Q4_K_M.gguf`. **Apache-2.0** (ships commercially). 949 MiB VRAM (<1 GB).
- **Reranker latency (GPU, `server-cuda -ngl 99`):** Q4_K_M 333 ms (15 docs) / 267 ms (fast-path, 10
  seeds, short docs) / p95 target <400 ms for 10 short docs; Q3_K_M 447 ms. CPU 23,130 ms (dead).
- **Reranker sidecar:** `ghcr.io/ggml-org/llama.cpp:server-cuda`, flags
  `--reranking --pooling rank -ngl 99 -c 2048 -t 4`, loopback `:8085`, endpoint `POST /v1/rerank`
  (`{query, documents}` → `{results:[{index, relevance_score}]}`). Cap query + each doc to ~480 chars
  to fit `-c 2048`. Env `AURA_RERANK_BASE_URL`.
- **Pipeline stage timings (live, G220, Neo4j Bolt):** vector seed p50 9.7 / p95 12.4 ms; 1-hop
  `:NEXT_CHUNK` graph expand p50 6.7 / p95 14.7 ms; rerank dominates. End-to-end p95 budget <500 ms on
  the seed-rerank path.
- **GPU envelope:** 4 GB A2000; reranker (<1 GB) co-resides with `aura-ocr-vl`. Primary local LLM
  (~3.7 GB) does NOT co-reside with everything — that's why Item 1's dual path runs **sequentially**.
- **Graph DB:** Neo4j 5.26 (Community + APOC + GDS). Has `vector.similarity.cosine` (used by the
  `document_id` pre-filter). Rerank track is DB-agnostic (talks to sidecars, not the graph) — no migration.
- **Granite embeddings:** local sidecar `:8081`, `/v1/embeddings`, 384d. Free (no paid API) for both
  retrieval and the RAGAS answer-similarity metric.
- **RAGAS pins (load-bearing):** Python 3.12 via `uv venv --python 3.12`; `ragas==0.2.15`;
  `langchain` / `langchain-core` / `langchain-community` `>=0.3,<0.4`; `langchain-openai` `>=0.2,<0.3`.
  Judge `deepseek/deepseek-v4-flash` via OpenRouter (`OPENROUTER_API_KEY`); embeddings local granite.
  ~35 paid calls/run (<1¢). Gated/manual, NOT CI.
- **RAGAS construct-validity numbers (committed baseline):** faithfulness grounded 1.000 / hallucinated
  0.000 (×3, stdev 0.000); answer_correctness grounded 0.980 / hallucinated 0.229 / partial 0.734
  (faithful-but-incomplete — the two metrics are orthogonal).
- **OCR fidelity (Item 1, GLM-OCR via markitdown):** robust to heavy JPEG compression — 3/3 specs
  survived a 9 KB / 0.45× / q30 JPEG; only an alphanumeric marker corrupted (0→8). Dense discrimination:
  g220 image 0.835 vs coffee decoy 0.777 (margin +0.0585).
- **Catalog scope/caps:** thread- + identity-(promoted-)scoped; cap 30 docs / 200-rune summaries; one
  line per doc; include `document_id`.

## Origin

Synthesized from spikes: 070, 075, 076, 077.
Source files in: `sources/070-rerank-value-or-overengineered/` (README, RERANK-PLAN.md, g220_rerank.py,
g220_graphrag.py), `sources/075-image-ocr-searchable-chunks/` (README, main.go, run.sh),
`sources/076-ragas-faithfulness-discriminates/` (README, ragas_probe.py, run.sh),
`sources/077-catalog-injection-recall/` (README, main.go, run.sh).
Verdicts: 070 VALIDATED (rerank worth it, GPU-mandatory, Qwen3-Reranker-0.6B Q4_K_M; self-learning
DEFERRED), 075 VALIDATED (Item 1 = assets-routing change; RET pre-filter finding), 076 VALIDATED
(Item 3 = gated RAGAS, pinned stack), 077 VALIDATED (Item 2 = catalog injection via cache-safe seam).
All four implemented 2026-06-28 (Items 1/2/3 + RET; rerank track planned in RERANK-PLAN.md, not yet built).
