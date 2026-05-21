# Online 2026 State-of-the-Art for Aura

**Compiled:** 2026-05-21
**Scope:** modern (2025-09 → 2026-05) techniques to make Aura (Go agent system) faster, leaner, smarter.
**Target metrics:** p95 ≤ 4 s on simple queries (today: 10 s+), every module ≤ 600 LOC, no feature regression.
**Sources:** Anthropic engineering blog, OpenAI cookbook, DeepSeek API docs, LMSYS/SGLang, Chroma/Morph (context rot), arXiv, ByteDance CloudWeGo, llama.cpp issues, hybrid-RAG production case studies. Date-biased 2025-09+.

---

## Top 10 picks (one-liner + Aura ROI)

| # | Technique | Source | Aura ROI | Apply where |
|---|-----------|--------|----------|-------------|
| 1 | **Provider prompt-cache breakpoints** (5-min + 1-h TTL) on system prompt + overlays + tool schemas | Anthropic, OpenAI auto-cache, DeepSeek auto | ★★★★★ — 70-90 % input cost down, **TTFT −13 to −31 %** | `internal/llm/client.go` request builder + per-turn breakpoint near current message |
| 2 | **JIT context loading + structured note-taking** (file-based memory, skill-style progressive disclosure) | Anthropic "Effective context engineering" 2025-09-29 | ★★★★★ — fixes context-rot at the root; aligns with wiki-as-memory invariant | `internal/conversation/system_prompt.go`, `internal/skills` already does this — extend to wiki/source body loading |
| 3 | **Tight tool surface — fewer, well-shaped tools, ≤25k-token outputs, pagination/filter built in** | Anthropic "Writing tools for agents" | ★★★★★ — Aura has ~22 tools, MCP overhead ~40-50 % of context in worst case | `internal/tools/registry.go` audit; collapse near-duplicates; enforce response_format enum |
| 4 | **Hybrid retrieval = BM25 + dense + cross-encoder rerank** | tianpan.co + tim-ponomarev/hybrid-rag, mbrenndoerfer.com 2026-04 | ★★★★★ — +27 % nDCG@10 over dense-only / +39.7 % MRR@3 from rerank | merge FTS5 + Qdrant + bge-reranker-v2-m3 sidecar; already started (Phase-WIKI-B) |
| 5 | **Parallel tool dispatch + streaming execution** (asyncio-style fan-out, results merged as they land) | codeant.ai, airbyte, tianpan.co 2026-04-10 | ★★★★★ — 1.4-3.7× speedup; Aura already runs Go-routines but doesn't stream-dispatch | `internal/chat/agentloop.go` + `internal/agent/loop`: fire tool exec inside stream-parse of tool_call fragments |
| 6 | **Speculative decoding for sidecar LLM** (1B draft → 8B target, 1.8-2.1×) | llama.cpp docs + Issue #21453 | ★★★★ — if Aura ever runs local LLM; not relevant while OpenAI-compat HTTP | Document only; skip until local LLM phase |
| 7 | **Sub-agent isolation via Task-style fork** for "verbose-output" jobs (tests, log scan, large file read) | Claude Code docs + InfoQ 2025-08 | ★★★★ — Aura's source-ingest already isolates; lift pattern to `web_fetch_large`, `read_skill`-style summarizer | New `internal/agent/subtask` package; child loop with capped tools |
| 8 | **Hard MaxIterations + early-stop generate ("answer with what you have")** | Strands/ADK + inforsome.com | ★★★★ — Aura already at 20 cap; needs the terminal "synthesize-now" round when capped | `internal/chat/agentloop.go` final-turn no-tool LLM call |
| 9 | **Compaction at 70-80 % budget, not 100 %** (Anthropic recommendation) | implicator.ai, victordibia, langchain | ★★★ — Aura sliding window=50 msg is a count cap, not a token-budget cap | `internal/conversation`: switch from msg-count to token-budget; emit summary when ≥ 0.7·max |
| 10 | **Qwen3-Embedding-0.6B + Qwen3-Reranker-0.6B** as multilingual baseline | Qwen blog 2025-06 + arXiv 2506.05176 | ★★★★ — IT/EN MTEB SOTA, 0.6B fits Aura's CPU sidecar budget; possible Wave-2 upgrade vs embeddinggemma | Optional swap in `internal/storage/qdrant` + new reranker sidecar |

---

## 1. LLM agent-loop optimization

### 1.1 Provider prompt caching — the highest single ROI

State of play 2026: **all four major providers ship prompt caching.** Anthropic (`cache_control` explicit breakpoints, 5-min TTL default since 2026-03-06, 1-h opt-in), OpenAI (automatic, ≥1024 tokens, 128-token granularity, opt-in `prompt_cache_key`), DeepSeek (automatic, on-disk), Google Gemini (context caching).

Hard numbers from the literature:
- **Anthropic:** 5-min cache write = 1.25× base, break-even after 2 reads. ~85 ms TTFT shaved on cached prefix. 5-10× input cost reduction on multi-turn loops with 10k-token system prompt. ([Anthropic prompt caching docs](https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching), [aicheckerhub.com 2026](https://aicheckerhub.com/anthropic-prompt-caching-2026-cost-latency-guide))
- **OpenAI:** auto on prompts ≥1024 tokens; up to **−80 % latency**, **−90 % input cost**. TTFT improves 7 % at 1024 tokens, 67 % at 150k. ([OpenAI prompt caching guide](https://developers.openai.com/api/docs/guides/prompt-caching), [Portkey deep dive](https://portkey.ai/blog/openais-prompt-caching-a-deep-dive/))
- **DeepSeek:** zero code change, on-disk cache, **price cut by ~10×** on cache hits. ([DeepSeek news 0802](https://api-docs.deepseek.com/news/news0802))
- **Anti-pattern (2026):** placing dynamic content (tool results, user turn) inside the cached prefix kills hit rate. Place stable content first, breakpoints around it, dynamic after. ([futureagi.com](https://futureagi.com/blog/understanding-prompt-caching-for-faster-ai-responses/))
- **Caveat:** Anthropic breakpoints can only see 20 content blocks behind themselves. Long agent loops need a sliding second breakpoint near the current turn. ([startdebugging.net 2026-04](https://startdebugging.net/2026/04/how-to-add-prompt-caching-to-an-anthropic-sdk-app-and-measure-the-hit-rate/))

**Aura applicability: ★★★★★.** Aura runs OpenAI-compatible HTTP — providers that proxy this (DeepSeek, Together, Anthropic via SDK adapter, OpenAI itself) all support prompt caching transparently or via small request-builder changes. Today Aura ships ~3-8k tokens of stable system prompt + overlays + tool schemas every turn; caching them once is the single biggest p95 win available.

**Go integration shape:**
- In `internal/llm/client.go` add an optional `CacheBreakpoint` field on request blocks; emit `cache_control` for Anthropic-style endpoints; let OpenAI/DeepSeek ignore.
- Order request blocks: `[soul, agent, user, tools_schema] → CACHE → [history pre-window] → CACHE → [last_2_turns] → user_message`.
- Log cache-hit metrics (`usage.cache_read_tokens`) per turn; expose via `/api/metrics`.
- Pair with TTL strategy: 1-h for system_prompt+tools; 5-min for recent history.

### 1.2 KV cache locality (provider-side)

SGLang and vLLM use **RadixAttention** to reuse KV pages across requests sharing prefixes. 75-95 % cache hit on multi-turn agent workloads when system+tools are constant. ([LMSYS RadixAttention blog](https://www.lmsys.org/blog/2024-01-17-sglang/), [Spheron 2026 guide](https://www.spheron.network/blog/sglang-production-deployment-guide/), [Medium SGLang Part 1](https://medium.com/@dharamendra1314.kumar/sglang-learning-series-part-1-shared-prefix-kv-cache-and-radixattention-d7a847d20b1f))

**Aura applicability: ★★★** — only if Aura adds an in-process LLM. With hosted LLMs, you piggyback on the provider's KV cache by holding prefix layout stable (same advice as §1.1).

### 1.3 Parallel tool dispatch + streaming tool execution

Mainstream by 2026. OpenAI Assistants API, Anthropic Claude (auto, when tools look independent), OpenClaw, OpenAI `parallel_tool_calls=true` default.
- Total wall-clock = slowest tool, not sum. **1.4-2.4× typical, 3.7× best-case.** ([codeant.ai](https://www.codeant.ai/blogs/parallel-tool-calling), [tianpan.co 2026-04-10](https://tianpan.co/blog/2026-04-10-parallel-tool-calls-hidden-coupling))
- Hidden coupling is the killer: if two "independent" tools both touch the same external rate-limited resource, you get a 429 cluster. The blog argues for **declared resource budgets per tool**.
- **Streaming dispatch:** parse incoming tool-call fragments from the LLM stream and start execution as soon as the JSON args close, don't wait for end-of-message. Asyncio-style "yield as it lands" merges results back into the LLM's next round.

**Aura applicability: ★★★★★.** Aura's `internal/llm/client.go` already accumulates tool fragments but executes only after stream close. The change is a Go-routine fan-out as each tool's args complete; channel-merge into the next-turn message payload.

**Go integration shape:**
- `internal/llm/client.go`: emit `OnToolCallReady(idx, name, args)` callback during `Stream()`.
- `internal/chat/agentloop.go`: `go execTool(ctx, name, args, resultsCh)` per ready call; barrier-wait before next LLM round; preserve registry order in the assistant message.
- Per-tool declared cost class (`network|disk|cpu|exclusive`) to gate fan-out; "exclusive" serializes — solves the hidden-coupling problem.

---

## 2. Tool surface design 2026

Anthropic's own "Writing tools for agents" guide (released alongside Claude Sonnet 4.5) is the single best reference. Core findings ([Anthropic engineering](https://www.anthropic.com/engineering/writing-tools-for-agents), [techwithibrahim.medium.com](https://techwithibrahim.medium.com/writing-effective-tools-for-ai-agents-production-lessons-from-anthropic-99ea76a7fcf0)):

1. **More tools ≠ better.** Common error: wrapping every existing API endpoint as a tool. Consolidate into purpose-shaped tools.
2. **Namespacing matters.** `wiki_search` and `web_search` beat a single overloaded `search`.
3. **Token-efficient outputs.** Pagination, filters, truncation by default. `response_format` enum to control verbosity. **Keep responses < 25k tokens.**
4. **Human-readable identifiers** in responses (slugs, titles) — not raw UUIDs/db-row IDs.
5. **Three-step iteration loop:** prototype → evaluate → collaborate (feed traces back to Claude/coding agent to refine descriptions).

**MCP tool-bloat is the dominant 2026 anti-pattern.** Merge CTO Gil Feig: tool metadata = 40-50 % of context in typical deployments. Field reports: 143k/200k tokens consumed by MCP alone. ([arXiv 2602.14878](https://arxiv.org/html/2602.14878v1), [versalence.ai](https://blogs.versalence.ai/mcp-model-context-protocol-evolution-2026))

Augmenting tool descriptions naively: **+5.85 pp task success but +67.46 % execution steps** and 16.67 % regression rate. Trade-off must be measured. ([arXiv 2602.14878](https://arxiv.org/html/2602.14878v1))

**Aura applicability: ★★★★★.** Aura has ~22 tools; some are clearly purpose-shaped (`schedule_task`, `execute_code`) but the search/source/memory cluster has overlap. Audit candidates:
- `search_memory`, `list_memory`, `read_memory`, `forget_memory` — collapse to one `memory` tool with action enum? (Anthropic says namespacing > overloading. Keep separate but tighten descriptions.)
- `web_search` + `web_fetch` — two tools, fine.
- `store_source`, `ocr_source`, `read_source`, `ingest_source` — pipeline stages, leave separate but doc the workflow once at top of TOOLS.md.
- `create_xlsx` / `create_docx` / `create_pdf` — three small tools, fine.

**Concrete Go action:**
- Add `MaxResponseBytes` field on every tool's `Spec()`; enforce in `registry.Execute()` (truncate + tail "...[truncated]" sentinel).
- Add a `response_format` argument to verbose tools (`search_memory`, `web_search`, `read_source`): `concise|standard|full`.
- Generate tool catalogue token cost once at boot (`internal/tools/registry.go` MeasureTokens) and log; gate on a budget alarm.

---

## 3. Context engineering — what changed in last 6 months

The **canonical reference** is Anthropic's "Effective context engineering for AI agents" (2025-09-29). Four named techniques ([Anthropic blog](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents), [MarkTechPost 2025-10-20](https://www.marktechpost.com/2025/10/20/a-guide-for-effective-context-engineering-for-ai-agents/), [Atlan 2026](https://atlan.com/know/context-api-for-ai/)):

### 3.1 Offloading (tool-result summarization)
Tools return summaries + a reference to retrieve full content if needed. Aura's `read_source` already exposes pagination; lift the pattern to `web_fetch` (return summary + URL hash key for body retrieval).

### 3.2 Reduction / Compaction (≤ 100 %, not at 100 %)
**2026 consensus:** start summarizing at ~70-80 % of context budget, not at the hard cap. ([implicator.ai](https://www.implicator.ai/anthropic-openai-google-tell-developers-to-budget-ai-context-windows/), [langchain](https://www.langchain.com/blog/context-engineering-for-agents)) Aura's current 50-msg sliding window is a count cap not a token cap; switch to token-budget compaction.

### 3.3 Retrieval (JIT / progressive disclosure)
Claude Code combines a small static pre-load (file map, skill manifests) with on-demand JIT calls. Skills do this for Aura already; the wiki + source layer should too — **list candidate page slugs by title; load body only when chosen.** ([newsletter.victordibia.com](https://newsletter.victordibia.com/p/context-engineering-101-how-agents))

### 3.4 Isolation (sub-agents)
Each sub-agent: own context, scoped tools, summary returned. ([Anthropic docs](https://code.claude.com/docs/en/sub-agents), [InfoQ 2025-08](https://www.infoq.com/news/2025/08/claude-code-subagents/)) Multi-agent research projects beat single-agent because narrow context per role > one giant context. **Split-and-merge** pattern: fan out up to 10 sub-agents in parallel.

### 3.5 Structured note-taking (external memory)
Persist notes to a file (`NOTES.md`) or memory store outside the context window; pull back when needed. Aura's wiki **is** this pattern at the system level — but in-loop note-taking for multi-step jobs is new. Could be added as a `note_save(key,value)` / `note_load(key)` tool pair backed by SQLite per conversation. ([Anthropic engineering](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents))

### 3.6 Context rot — the validating evidence
Chroma's 2025 study tested 18 frontier models (GPT-4.1, Claude Opus 4, Gemini 2.5 Pro, Qwen3-235B): **all degrade with longer input even on trivial tasks.** Length alone hurts — independent of distractor density. ([trychroma.com/research/context-rot](https://www.trychroma.com/research/context-rot), [morphllm.com](https://www.morphllm.com/context-rot), [arXiv 2510.05381](https://arxiv.org/pdf/2510.05381)) **Implication:** keeping context lean is not just cost — it's accuracy.

**Aura applicability: ★★★★★** for #3.1-3.3, ★★★★ for #3.4 (some isolation already), ★★★ for #3.5 (wiki covers strategic memory, in-loop notes is a smaller win).

**Go integration shape:**
- `internal/conversation`: replace 50-msg cap with token-budget cap (e.g. 70 % of model context window). Trigger compaction LLM call at threshold; emit `summary_v1` block.
- `internal/agent/subtask` new package: `Run(ctx, role, prompt, tools[]string) (summary string, err)`. Fresh history, capped tools, ≤ 5 iterations.
- Lift `read_skill`-style "manifest list + body-on-demand" to wiki: tool returns titles+slugs; new `read_wiki_page(slug)` loads body.

---

## 4. Go-specific agent patterns

### 4.1 Eino (ByteDance CloudWeGo) — the credible Go contender
[github.com/cloudwego/eino](https://github.com/cloudwego/eino), [muleai.io 2026-03](https://muleai.io/blog/2026-03-17-eino-bytedance-golang-llm-framework/), [docs](https://www.cloudwego.io/docs/eino/).

- Battle-tested at ByteDance >6 months pre-OSS (Doubao, TikTok recs).
- Component abstractions: `ChatModel`, `Tool`, `Retriever`, `Embedding`, `ChatTemplate`.
- Graph orchestration (nodes = components, edges = data flow). Type-checked at compile time, streaming first-class.
- ADK sub-module: ready-to-use agent patterns (tool use, multi-agent coord, interrupt/resume HITL).
- Native integration with Hertz/Kitex (CloudWeGo's HTTP/RPC stack).
- Quoted scale claim: 10k+ req/s with built-in circuit breakers, exponential backoff, bulkhead isolation.

**Aura applicability: ★★ — not as a wholesale adoption.** Aura's substrate is already in place; lifting Eino wholesale would violate the "no big-bang rewrite" rule. **But** specific patterns worth studying:
- Graph-as-orchestration for the source-ingest pipeline (today it's an ad-hoc chain in `internal/storage/sources`).
- Streaming-aware component interface — particularly `ChatModel` returning typed stream-readers.
- ADK interrupt/resume HITL pattern for the approval-gate anti-pattern (§8).

### 4.2 LangChainGo — the older option
Functionally similar to Eino but with less production trail and more "Go-flavored Python idioms" criticism. ([reliasoftware blog](https://reliasoftware.com/blog/golang-ai-agent-frameworks), [appliedgo](https://appliedgo.net/spotlight/ai-and-go/)) Pass for Aura.

### 4.3 Native Go strengths to lean on
([vanducng.dev 2026-02](https://vanducng.dev/2026/02/28/From-Theory-to-Gateway-Building-a-Production-AI-Agent-System-in-Go/))
- Goroutines for parallel tool dispatch (already in Aura, formalize).
- Single ~15 MB binary (Aura ships this).
- `context.Context` for cancellation propagation (Aura uses; reinforce in tool layer).

---

## 5. Local-first LLM optimization (sidecars)

### 5.1 Speculative decoding (CPU-viable in 2026)
[llama.cpp speculative docs](https://github.com/ggml-org/llama.cpp/blob/master/docs/speculative.md), [Issue #21453](https://github.com/ggml-org/llama.cpp/issues/21453), [vucense.com](https://vucense.com/dev-corner/speculative-decoding-explained-2x-faster-local-llms-ollama-llama-cpp-2026/), [SpeCache arXiv 2503.16163](https://arxiv.org/pdf/2503.16163).

- 1B draft + 8B target → **180+ tok/s** on llama.cpp, ~2× over baseline. Quality unchanged (verification preserves target distribution).
- Best pairs 2026: Llama 3.2 1B → 3.3 70B (2.1×), Qwen3 0.6B → 8B (1.9×), Gemma 3 1B → 27B (1.8×).
- llama.cpp: `--model-draft` + `llama-speculative` binary or `llama-server`.
- CPU is memory-bandwidth bound; speculative decoding batches the verification work and reclaims idle wait.

**Aura applicability: ★** in current state (Aura uses hosted LLM over HTTP). Becomes ★★★★★ if Aura ever runs a local main-LLM sidecar — document as the **first thing to enable** at that time.

### 5.2 Embedding sidecar (already locked in Aura)
Aura uses embeddinggemma-300m via llama.cpp; matches the 2026 CPU sidecar consensus (cf. [knightli 2026-04-23](https://www.knightli.com/en/2026/04/23/llama-cpp-8g-vram-32k-64k-kv-cache-tuning/) on KV cache quantization). The memory `feedback_embedding_backend_stays_mistral` is the lock.

### 5.3 Reranker sidecar candidates
Phase-WIKI-B is teeing this up. Best 2026 picks (CPU-fit):
- **`BAAI/bge-reranker-v2-m3`** — 278M, multilingual, ~130ms / 16-pair batch on CPU. ([markaicode](https://markaicode.com/bge-reranker-cross-encoder-reranking-rag/), [zeroentropy](https://www.zeroentropy.dev/articles/ultimate-guide-to-choosing-the-best-reranking-model-in-2025))
- **`Qwen3-Reranker-0.6B`** — newer (June 2025), exceeds bge on retrieval suite. ([HF model card](https://huggingface.co/Qwen/Qwen3-Reranker-0.6B), [arXiv 2506.05176](https://arxiv.org/html/2506.05176v1))
- Latency ladder: bge-reranker-base ~130 ms / 50 pairs CPU; bge-reranker-large ~250 ms but precision up to 0.87.

**Aura recommendation:** start bge-v2-m3 in Wave B (lower risk, more docs); spike Qwen3-Reranker-0.6B as drop-in swap once Wave B is green.

### 5.4 vLLM/SGLang — not relevant on mini-PC CPU
Both are GPU-shaped. Aura's locked CPU budget makes them out-of-scope unless a GPU is added. ([Spheron vLLM 2026](https://www.spheron.network/blog/vllm-production-deployment-2026/)) Skip.

---

## 6. Hybrid retrieval 2026

**Consensus stack (winning across multiple production benchmarks):**

```
Query → [ BM25 || dense(embedding) ] → RRF fusion → top-20 → cross-encoder rerank → top-3 → LLM
```

Numbers from production reports:
- **Hybrid > dense-only:** +27 % nDCG@10 for ~90 ms extra latency. ([tianpan.co](https://tianpan.co/blog/2026-04-12-hybrid-search-production-bm25-dense-embeddings))
- **Rerank > no rerank:** MRR@3 0.433 → 0.605 (+39.7 % relative); Recall@5 0.695 (RRF) → 0.816 (RRF + rerank, +17.4 %). ([tim-ponomarev/hybrid-rag](https://github.com/tim-ponomarev/hybrid-rag), [arXiv 2604.01733](https://arxiv.org/html/2604.01733v1))
- **BM25 still beats dense alone** on identifiers, code, rare terms — and on most metrics with text-embedding-3-large in head-to-head. ([ranjankumar.in](https://ranjankumar.in/bm25-vs-dense-retrieval-for-rag-engineers))

**RRF (Reciprocal Rank Fusion)** is the default fusion: `score_i = sum(1 / (k + rank_i))` over each retriever, `k=60`. No score normalization needed. ([mbrenndoerfer](https://mbrenndoerfer.com/writing/hybrid-search-bm25-dense-retrieval-fusion))

**BGE-M3 alternative** — generates dense, sparse, ColBERT vectors from one model — interesting but moves away from a clean BM25 separation. For Aura the FTS5 + embeddinggemma + reranker stack is cleaner and easier to debug. ([HF BGE-M3](https://huggingface.co/BAAI/bge-m3))

**Aura applicability: ★★★★★.** Phase-WIKI-B is exactly this. Concrete additions to the existing plan:
- Default fusion = RRF, k=60. Don't invent a new fusion function.
- Reranker batch budget: top-20 candidates → 1 batch → ≤ 150 ms p95 on CPU.
- **Zone-map** integration (per the locked post-DRIFT sequence): the reranker pass also returns a per-document zone (e.g. "front-matter | section-A | section-B") so subsequent reads can slice into the right zone. New `read_wiki_page(slug, zone)` tool argument.

---

## 7. Latency techniques

### 7.1 Prompt caching
Covered in §1.1 — single biggest dial.

### 7.2 Parallel + streaming tool dispatch
Covered in §1.3.

### 7.3 Speculative decoding
Covered in §5.1 — only relevant if local LLM.

### 7.4 Batched inference
Provider-side; not addressable from Aura.

### 7.5 KV cache reuse across turns
Provider-side; addressable indirectly by keeping prefix stable (§1.1, §1.2).

### 7.6 First-token streaming UX (already in Aura)
Aura's progressive Telegram edits (~600 ms throttle) hide LLM latency. Keep. Consider tightening to ~400 ms when wall-clock budget is tight; trade-off is more API edit calls.

### 7.7 Tool result truncation
Direct cause of context bloat → context rot → re-prompting → more iterations → more latency. Cap every tool response (§2).

### 7.8 Hard MaxIterations + synthesis turn
20-iter cap (Aura's current setting) is fine. **Add** the early-stop synthesize round when cap hit ([inforsome.com](https://inforsome.com/agent-max-iterations-fix/), [aibmag.com](https://www.aibmag.com/ai-business-case-studies-and-real-world-enterprise-use-cases/max-iterations-in-ai-agents-key-insights-for-leaders-in-march-2026/)): one final no-tool LLM call with the prompt "You've reached the max steps. Provide your best answer based on the work so far." This converts a hard error into a graceful degradation.

---

## 8. Anti-patterns being abandoned in 2026

### 8.1 Fast-path classifier router for "simple" queries
Aura already learned this. The community lesson is **not** that routers are evil — it's that **a pre-LLM intent classifier that bypasses the agent loop creates a new failure mode + maintenance burden + class skew over time.** ([Aura memory `feedback_check_tmp_sources_then_brainstorm_best`])

Counter-evidence: **vLLM Semantic Router** (2025-09) is shipping a fast-path router that uses ModernBERT for intent classification ([vLLM blog](https://blog.vllm.ai/2025/09/11/semantic-router.html)). But notice — that's *cluster-level routing* (which serving replica), not *bypass the agent loop*. The Aura-relevant anti-pattern (bypass loop on "easy" queries) is still abandoned by mature systems (codex, elysia, nanobot, openhuman). **Verdict: hold the line.**

### 8.2 "Wrap every API endpoint as a tool"
Anthropic's "Writing tools for agents" explicitly calls this out. Aura should audit MCP cards for endpoint-wrappers and either consolidate or drop. ([Anthropic engineering](https://www.anthropic.com/engineering/writing-tools-for-agents))

### 8.3 Compaction at 100 % context
Wait until last-token-possible → forced summarization is rushed and loses signal. Compact at 70-80 %. ([implicator.ai](https://www.implicator.ai/anthropic-openai-google-tell-developers-to-budget-ai-context-windows/))

### 8.4 All-or-nothing autonomy
Granting full autonomy with no approval gates for high-risk actions is in the 2026 anti-pattern catalog. ([atlan.com](https://atlan.com/know/agent-harness-failures-anti-patterns/), [Medium DSC](https://medium.com/data-science-collective/why-ai-agents-keep-failing-in-production-cdd335b22219)) Aura's `execute_code` sandbox is partial mitigation; `request_dashboard_token` is correct gating. Audit which tools could mint cost (Mistral OCR, future cloud TTS) and decide on per-tool budget caps.

### 8.5 Unmonitored schema drift on tool calls
n8n 2.4.7 → 2.6.3 case study: schema change broke every consuming agent silently. ([atlan.com](https://atlan.com/know/agent-harness-failures-anti-patterns/)) **Aura mitigation:** every tool's `Parameters()` JSON-schema is hashed at boot; on hash change emit a wiki "schema_drift" event and surface in dashboard.

### 8.6 Naive max-tokens-as-the-only-budget
Production teams now track **(latency budget, token budget, tool-call budget, iteration budget) per request class.** Aura is partway there (iteration budget) — extend to latency + tool-call budgets per turn.

### 8.7 Massive monolithic prompts
Refactored out: separation of concerns wins. Specialized sub-agents with narrow tasks > one giant prompt. ([Google Developers Blog](https://developers.googleblog.com/production-ready-ai-agents-5-lessons-from-refactoring-a-monolith/), [DSC Medium](https://medium.com/data-science-collective/why-ai-agents-keep-failing-in-production-cdd335b22219)) Aura overlays (SOUL/AGENT/USER/TOOLS) are already in this direction — keep slim and EN-only (per the 2026-05-21 lessons memo).

### 8.8 Confidence in "tool was called → task done"
Chronic 3-15 % tool-call failure rate per call → compounds across multi-step. ([atlan.com](https://atlan.com/know/agent-harness-failures-anti-patterns/)) Aura's CLAUDE.md "verify the artifact, not the reply" rule is the right policy. Codify per-tool result-validation expectations.

---

## Cross-cutting Go integration recipe

A single Aura coding agenda from this research, ordered by effort × ROI:

1. **Provider prompt-cache breakpoints** (`internal/llm/client.go` request layout + per-provider adapter). 1-2 days. ★★★★★
2. **Tool-output byte cap + `response_format` enum** on verbose tools (`internal/tools/registry.go` + each verbose tool). 1 day. ★★★★★
3. **Streaming tool dispatch** (`internal/llm/client.go` callback + `internal/chat/agentloop.go` fan-out). 2 days. ★★★★★
4. **Early-stop synthesize turn at MaxIterations** (`internal/chat/agentloop.go`). 2-4 hours. ★★★★
5. **Hybrid retrieval RRF + bge-reranker-v2-m3 sidecar** (Phase-WIKI-B already planned). ~1 week. ★★★★★
6. **Token-budget compaction at 70 %** (`internal/conversation`). 1-2 days. ★★★★
7. **Sub-agent package for verbose-output isolation** (`internal/agent/subtask`). 2-3 days. ★★★
8. **Tool catalogue token-cost audit + dedup pass** (audit script + `internal/tools/registry.go` cleanup). 1-2 days. ★★★★
9. **Speculative decoding spike** — only if/when local main-LLM sidecar lands. ★ (until then)
10. **Qwen3-Reranker-0.6B swap as Wave-B-follow-up spike** once Wave B baseline locked. ★★★

Total addressable latency drop: realistic estimate **p95 10s → 3-5s** if 1+2+3+4 land together. Quality drop from context rot avoidance (6, 8): **+5-15 % strict-pass rate** on bench.

---

## Sources (consolidated)

**Anthropic engineering**
- [Effective context engineering for AI agents (2025-09-29)](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents)
- [Writing effective tools for AI agents](https://www.anthropic.com/engineering/writing-tools-for-agents)
- [Prompt caching docs](https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching)
- [Building effective AI agents](https://resources.anthropic.com/building-effective-ai-agents)
- [Sub-agents in Claude Code](https://code.claude.com/docs/en/sub-agents)
- [Context engineering cookbook](https://platform.claude.com/cookbook/tool-use-context-engineering-context-engineering-tools)

**OpenAI / DeepSeek / Google**
- [OpenAI prompt caching guide](https://developers.openai.com/api/docs/guides/prompt-caching)
- [OpenAI Prompt Caching 201](https://developers.openai.com/cookbook/examples/prompt_caching_201)
- [DeepSeek context caching news 0802](https://api-docs.deepseek.com/news/news0802)
- [DeepSeek KV cache guide](https://api-docs.deepseek.com/guides/kv_cache)

**Engineering blogs / industry**
- [TianPan — Parallel Tool Calls Hidden Coupling (2026-04-10)](https://tianpan.co/blog/2026-04-10-parallel-tool-calls-hidden-coupling)
- [TianPan — Hybrid Search in Production (2026-04-12)](https://tianpan.co/blog/2026-04-12-hybrid-search-production-bm25-dense-embeddings)
- [Morph — Context Engineering: Why More Tokens Makes Agents Worse](https://www.morphllm.com/context-engineering)
- [Morph — Context Rot Guide](https://www.morphllm.com/context-rot)
- [Chroma — Context Rot research](https://www.trychroma.com/research/context-rot)
- [LangChain — Context Engineering for Agents](https://www.langchain.com/blog/context-engineering-for-agents)
- [Implicator.ai — AI Agents Need Context Compaction Before 100%](https://www.implicator.ai/anthropic-openai-google-tell-developers-to-budget-ai-context-windows/)
- [LMSYS — RadixAttention + SGLang](https://www.lmsys.org/blog/2024-01-17-sglang/)
- [Spheron — SGLang Production Deployment 2026](https://www.spheron.network/blog/sglang-production-deployment-guide/)
- [Spheron — vLLM Production Deployment 2026](https://www.spheron.network/blog/vllm-production-deployment-2026/)
- [Atlan — AI Agent Harness Failures: 13 Anti-Patterns](https://atlan.com/know/agent-harness-failures-anti-patterns/)
- [Google Developers Blog — Production-Ready AI Agents: 5 Lessons](https://developers.googleblog.com/production-ready-ai-agents-5-lessons-from-refactoring-a-monolith/)
- [InfoQ — Claude Code Subagents (2025-08)](https://www.infoq.com/news/2025/08/claude-code-subagents/)
- [vLLM blog — Semantic Router (2025-09)](https://blog.vllm.ai/2025/09/11/semantic-router.html)
- [AIBmag — Max Iterations Insights March 2026](https://www.aibmag.com/ai-business-case-studies-and-real-world-enterprise-use-cases/max-iterations-in-ai-agents-key-insights-for-leaders-in-march-2026/)
- [aicheckerhub.com — Anthropic Prompt Caching 2026](https://aicheckerhub.com/anthropic-prompt-caching-2026-cost-latency-guide)
- [dev.to/whoffagents — TTL drop 1h→5m](https://dev.to/whoffagents/anthropic-silently-dropped-prompt-cache-ttl-from-1-hour-to-5-minutes-16ao)
- [startdebugging.net — Adding prompt caching to Anthropic SDK](https://startdebugging.net/2026/04/how-to-add-prompt-caching-to-an-anthropic-sdk-app-and-measure-the-hit-rate/)
- [Portkey — OpenAI Prompt Caching Deep Dive](https://portkey.ai/blog/openais-prompt-caching-a-deep-dive/)
- [vucense.com — Speculative Decoding Explained 2026](https://vucense.com/dev-corner/speculative-decoding-explained-2x-faster-local-llms-ollama-llama-cpp-2026/)
- [markaicode — BGE Reranker Cross-Encoder Guide](https://markaicode.com/bge-reranker-cross-encoder-reranking-rag/)
- [zeroentropy.dev — Ultimate Guide to Choosing Best Reranking Model 2026](https://www.zeroentropy.dev/articles/ultimate-guide-to-choosing-the-best-reranking-model-in-2025)
- [mbrenndoerfer.com — Hybrid Search BM25 + Dense Retrieval](https://mbrenndoerfer.com/writing/hybrid-search-bm25-dense-retrieval-fusion)
- [ranjankumar.in — BM25 vs Dense Retrieval](https://ranjankumar.in/bm25-vs-dense-retrieval-for-rag-engineers)
- [github tim-ponomarev/hybrid-rag](https://github.com/tim-ponomarev/hybrid-rag)
- [futureagi.com — Prompt Caching 2026](https://futureagi.com/blog/understanding-prompt-caching-for-faster-ai-responses/)
- [codeant.ai — Parallel Tool Calling](https://www.codeant.ai/blogs/parallel-tool-calling)
- [airbyte — Parallel Tool Calls](https://airbyte.com/agentic-data/parallel-tool-calls-llm)
- [newsletter.victordibia.com — Context Engineering 101](https://newsletter.victordibia.com/p/context-engineering-101-how-agents)
- [MarkTechPost — Guide for Effective Context Engineering (2025-10-20)](https://www.marktechpost.com/2025/10/20/a-guide-for-effective-context-engineering-for-ai-agents/)
- [versalence.ai — Long Live MCP 2026](https://blogs.versalence.ai/mcp-model-context-protocol-evolution-2026)
- [Atlan — Context API for AI 2026](https://atlan.com/know/context-api-for-ai/)

**Go frameworks**
- [github.com/cloudwego/eino](https://github.com/cloudwego/eino)
- [Eino User Manual](https://www.cloudwego.io/docs/eino/)
- [muleai.io — Eino: ByteDance's Golang LLM Framework](https://muleai.io/blog/2026-03-17-eino-bytedance-golang-llm-framework/)
- [vanducng.dev — Production AI Agent in Go (2026-02)](https://vanducng.dev/2026/02/28/From-Theory-to-Gateway-Building-a-Production-AI-Agent-System-in-Go/)
- [reliasoftware — Top 7 Best Golang AI Agent Frameworks 2026](https://reliasoftware.com/blog/golang-ai-agent-frameworks)

**llama.cpp / local**
- [llama.cpp speculative decoding docs](https://github.com/ggml-org/llama.cpp/blob/master/docs/speculative.md)
- [Issue #21453 — Speculative Decoding for Low-Latency CPU Inference](https://github.com/ggml-org/llama.cpp/issues/21453)
- [knightli.com — llama.cpp 8GB VRAM tuning 2026-04](https://www.knightli.com/en/2026/04/23/llama-cpp-8g-vram-32k-64k-kv-cache-tuning/)

**Embeddings / rerankers**
- [Qwen3 Embedding blog](https://qwenlm.github.io/blog/qwen3-embedding/)
- [Qwen3 Embedding arXiv 2506.05176](https://arxiv.org/html/2506.05176v1)
- [BAAI/bge-m3 HF](https://huggingface.co/BAAI/bge-m3)
- [Qwen/Qwen3-Reranker-0.6B HF](https://huggingface.co/Qwen/Qwen3-Reranker-0.6B)

**Papers**
- [arXiv 2601.06007 — Don't Break the Cache: Prompt Caching for Long-Horizon Agentic Tasks](https://arxiv.org/pdf/2601.06007)
- [arXiv 2602.14878 — MCP Tool Descriptions Are Smelly](https://arxiv.org/html/2602.14878v1)
- [arXiv 2510.05381 — Context Length Alone Hurts LLM Performance](https://arxiv.org/pdf/2510.05381)
- [arXiv 2604.01733 — From BM25 to Corrective RAG: Benchmarking](https://arxiv.org/html/2604.01733v1)
- [arXiv 2503.16163 — SpeCache: Speculative Key-Value Caching](https://arxiv.org/pdf/2503.16163)
- [arXiv 2603.22862 — Evolution of Tool Use in LLM Agents](https://arxiv.org/html/2603.22862v1)
