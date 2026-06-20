---
id: 070-rerank-value-or-overengineered
title: Is reranking worth it for Aura, or overengineered? (+ GPU + model bench)
date: 2026-06-20
status: VALIDATED — rerank WORTH IT, but GPU-mandatory; ship Qwen3-Reranker-0.6B Q4_K_M on GPU
type: standard
tags: [rerank, graphrag, retrieval, gpu, llama-cpp, qwen3-reranker, jina, self-learning, g220]
related: .planning/spikes/068-arcadedb-pipeline/LEIDEN-AND-RERANK.md, .planning/spikes/069-arcadedb-vs-neo4j-realdata
---

# Spike 070 — rerank: worth it or overengineered?

## Idea
Before planning a rerank + self-learning track, gate it on evidence: does a cross-encoder reranker
measurably beat pure vector retrieval on Aura's real data, or is it overengineering? Then operator
directives mid-spike: make it industrial (test **connected nodes in Neo4j**, not flat vector),
**measure timing**, and **use GPU (CPU will never work)**. Corpus = the real 830-pg Siemens G220 manual
(600 chunks, live Granite-97m 384d). Reranker candidates: Qwen3-Reranker-0.6B (Apache-2.0),
jina-reranker-v3 (CC BY-NC).

## Q1 — Does rerank improve quality? YES (6/8, neutral 1, mild-hurt 1)
Pure granite cosine top-15 vs cosine→Qwen3-Reranker, real queries:
- ✅ **Kills lexical/TOC false-positives** (the big win): "tightening torque" — vector #1 = p14 *"4.11.18.4 Load monitoring for torque…623"* (table-of-contents!), rerank #1 = p156 *"screws with a tightening torque of 2 Nm… Mount the terminal covers"* (the actual instruction). Same for "F code" p9→p11.
- ✅ **Sharpens flat bi-encoder clusters**: "ambient temp" — vector 0.82–0.84 flat blob → rerank p83 **0.978** vs rest ~0.01.
- ✅ **Better chunk-on-page**: "wire thickness" → the real *"cross-section = 16 mm²"* chunk.
- ⚠️ Neutral on already-correct "factory reset"; mildly **hurt** "back to box" (demoted the clean answer) — rerank is not monotonic; needs eval, not blind trust.

G220 is the bi-encoder's *easy* case (distinct technical terms) — rerank still helped, so it will help **more** on noisy conversational memory recall. **Verdict: worth it, not overengineered.**

## Q2 — Connected-nodes GraphRAG + timing (industrial, not toy)
Neo4j `:Chunk` nodes + `:NEXT_CHUNK` reading-order edges (600 nodes / 599 edges). Pipeline
**vector seed → 1-hop graph expand → rerank**, timed per stage (p50/p95, ms):

| Stage | p50 | p95 |
|---|---|---|
| vector seed (Bolt `db.index.vector.queryNodes`) | 9.7 | 12.4 |
| graph expand (1-hop `:NEXT_CHUNK`, ~27-node pool) | 6.7 | 14.7 |
| rerank — **expand-then-rerank, 27 long docs (GPU)** | 1434 | 2104 |
| END-TO-END (this naive order) | 1456 | 2120 |

Vector + graph are trivially cheap (~10ms). **Rerank dominates** and scales with `pool_size × doc_length`.

## Q3 — GPU vs CPU (operator: "CPU will never work") — CONFIRMED
| Config | rerank latency (15 docs) | correctness | license |
|---|---|---|---|
| **CPU** Qwen3-0.6B Q4_K_M | **23,130 ms** (14.9–35.8s) | good | Apache-2.0 |
| GPU jina-reranker-v3 **IQ3_XXS** | 221 ms | ❌ **all scores 0.000 (broken)** | CC BY-NC |
| GPU Qwen3-0.6B **Q3_K_M** | 447 ms | ✅ 0.999 | Apache-2.0 |
| **GPU Qwen3-0.6B Q4_K_M** ⭐ | **333 ms** | ✅ **1.000** | Apache-2.0 |
| GPU Qwen3 Q4_K_M, **fast-path** (10 seeds, short docs) | **267 ms** | ✅ 1.000 | Apache-2.0 |

- **CPU is ~70–1000× too slow (23s) — dead.** GPU is mandatory (operator correct).
- **jina-reranker-v3 rejected**: IQ3_XXS returns all-zero scores (llama.cpp issue #17189 — listwise arch not properly supported yet) **and** CC BY-NC (can't ship commercially).
- **Q3_K_M rejected**: *slower* than Q4_K_M (447 vs 333ms — lower-bit K-quant has worse GPU kernels; on GPU cost is pass-count, not model size) with no quality gain.
- ⭐ **Winner: Qwen3-Reranker-0.6B Q4_K_M on GPU** — 333ms, perfect correctness, Apache-2.0, **<1 GB VRAM** (949 MiB; coexists with `aura-ocr-vl` in the 4 GB A2000).

## Q4 — The speed fix is architectural, not just GPU
`pool_size × doc_length` is the cost. **Rerank the ~10 vector SEEDS (not the 27-node expanded pool), with short truncation → 267 ms** (vs 1.4s expand-then-rerank). Then graph-expand the *winners* for LLM context. Correct order: **vector → rerank seeds → expand top-K for context.** 5× faster, quality preserved (seeds already contain the answer; expansion is for context, not candidate generation).

## Self-learning — DEFER (avoid overengineering now)
The reranker is a fixed model that already works well (267ms, accurate). Aura's `semindex`/`activelearn`
substrate (spikes 052–058) could learn on rerank feedback, but spike 057 showed the free oracle is
self-limited, and there's no production miss-data yet to learn from. Building a learning loop now =
overengineering. Ship the static reranker first; revisit self-learning only if production eval shows a
persistent, patterned rerank miss.

## Verdict
Rerank is **worth building** — measurable precision win (esp. killing TOC/lexical false-positives), and
on GPU it's a snappy 267ms with the seed-rerank order. **Ship: Qwen3-Reranker-0.6B Q4_K_M GPU sidecar,
vector→rerank-seeds→expand-winners pipeline, RRF as the free fallback. Defer self-learning.** DB-agnostic
— upgrades the current Neo4j stack; no migration needed. Plan: `RERANK-PLAN.md`.
