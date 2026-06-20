# Phased plan — GPU reranking for Aura retrieval (self-learning deferred)

**Date:** 2026-06-20 · **Gated by:** spike 070 (rerank worth it ✓, GPU-mandatory, Qwen3-Reranker-0.6B Q4_K_M).
**Principle:** minimal industrial shape — ship the static reranker (high value, simple), defer the
self-learning loop (overengineering until production miss-data exists). DB-agnostic — no migration.

Feed this to `/gsd-plan-phase` for the full PLAN.md, or execute phase-by-phase. Estimates are focused-dev.

---

## Phase 1 — Rerank sidecar + Go client (foundation) · ~3–4 d
**Goal:** a fail-soft GPU reranker Aura can call, mirroring the embed seam.
- **Compose:** add `aura-rerank` service — `ghcr.io/ggml-org/llama.cpp:server-cuda`, `--hf-repo Voodisss/Qwen3-Reranker-0.6B-GGUF-llama_cpp --hf-file Qwen3-Reranker-0.6B-Q4_K_M.gguf --reranking --pooling rank -ngl 99 -c 2048 -t 4`, loopback `:8085`, nvidia deploy block (mirror `aura-ocr-vl`). Lazy/optional like the multimodal sidecars.
- **Go:** `internal/rerank/client.go` — `RerankClient{BaseURL, Client}.Rerank(ctx, query string, docs []string) ([]Scored, error)` calling `POST /v1/rerank`, sorted desc. Mirror `internal/documents/embedder.go` (`EmbeddingClient`) exactly — same construction, timeout, env (`AURA_RERANK_BASE_URL`).
- **Fail-soft:** sidecar down / GPU absent → return input order unchanged (identity), log once; never block retrieval.
- **Acceptance:** unit + live `rerank_integration` tier; `Rerank` returns correct order on the spike's injected-answer case; p95 < 400ms for 10 short docs on GPU; identity fallback verified with sidecar stopped. Coverage ≥85%.

## Phase 2 — Wire into retrieval (the value) · ~4–6 d
**Goal:** two-stage retrieval on the surfaces that matter, in the **fast order**.
- **Pipeline order (spike 070 Q4):** vector/BM25 first stage → **rerank the ~10 seeds** → graph-expand the top-K *winners* for LLM context. NOT expand-then-rerank.
- **Surfaces:** (1) **memory recall** (Phase-15 / agent-memory retrieval path) — the highest-value, noisiest surface; (2) **document retrieval** (the G220-style ingest/recall); (3) `tool_search` (`internal/semindex` `Ranker`) — optional, lower priority (corpus is small, BM25/embedding already good per spikes 054–058).
- **RRF baseline:** keep Reciprocal Rank Fusion (spike 056) as the zero-model fusion + the fallback when the reranker is off.
- **Acceptance:** on a labeled query set (Phase 3), reranked Recall@5/nDCG@10 ≥ pure-vector **by a measured margin**; the TOC/lexical false-positive cases (torque, F-code) fixed; end-to-end p95 < 500ms (seed-rerank path); reranker-off path = RRF order, no regression vs today.

## Phase 3 — Eval harness + non-monotonic guard · ~3–4 d
**Goal:** prove the win and stop rerank from ever *hurting* (the "back-to-box" case).
- **Eval:** labeled query→relevant-chunk set (start ~30 queries, pooled judgments) → nDCG@10 / MRR / Recall@5, vector vs vector+rerank. Wire as a `cot_eval`-style gated tier (like `internal/eval`), not in default CI.
- **Guard:** apply rerank only when it improves confidence (e.g., keep vector order if the reranker's top score < threshold, or blend) — prevents the non-monotonic demotion seen in spike 070.
- **Acceptance:** documented nDCG@10 lift ≥ target; zero queries where rerank regresses vs vector beyond noise; p95 budget met. Becomes the regression gate for any future change.

## Phase 4 — Self-learning · DEFERRED (build only on evidence)
**Goal (conditional):** improve retrieval from production feedback — **only if** Phase 3 / production eval shows a *persistent, patterned* rerank miss.
- Reuse `internal/activelearn` + `internal/semindex` (spikes 053/058) with the **two-tier oracle** (free ranker for the confident majority + LLM escalation on the low-margin tail — spike 057, since the free-only oracle is self-limited). Learn query→good-result associations or a learned fusion weight; the cross-encoder itself stays fixed (online fine-tuning is out of scope).
- **Do NOT build speculatively.** No production miss-data today → building now is overengineering (the spike's explicit finding).

---

## Cross-cutting notes
- **GPU is a hard dependency** for rerank (CPU = 23s, dead). Reranker uses <1 GB VRAM and coexists with `aura-ocr-vl` on the 4 GB A2000 (and the DGX-Spark appliance has ample GPU). **CPU-only / GPU-absent deployments run with rerank OFF → RRF fallback** (graceful degradation, no hard fail).
- **License clean:** Qwen3-Reranker-0.6B = Apache-2.0 (ships in the commercial appliance). jina-reranker-v3 is excluded (CC BY-NC + broken in llama.cpp).
- **Model pin:** use the `Voodisss` GGUF (community conversions miss `cls.output.weight` → garbage scores). Q4_K_M only (Q3_K_M slower, Q2_K/IQ3 unusable).
- **No migration:** this whole track is DB-agnostic — it talks to sidecars, not the graph. It rides unchanged onto Neo4j today (and onto ArcadeDB later if that ever happens).
- **Total Phases 1–3 ≈ 1.5–2 weeks.** Phase 4 deferred indefinitely pending evidence.
