---
spike: 054
name: semantic-toolsearch-vs-bm25
type: comparison
validates: "Given the live-mounted deferred-tool corpus and the documented mis-route queries, when each tool's doc is granite-embedded and queries are cosine-ranked vs the real production BM25 (ToolSearch.Execute), then the correct deferred tool ranks #1 where BM25 ranks it low/absent — quantified top-1 / recall@3 / MRR@10 for both rankers"
verdict: VALIDATED
related: [052-reasoning-tier-embed-classifier, 053-reasoning-classifier-active-learning, 032-agent-memory-mcp-live-mount]
tags: [tool-search, embeddings, bm25, granite, deferred-tools, slice-tooling]
---

# Spike 054: Semantic tool_search vs BM25

## What This Validates

Given/When/Then in the frontmatter. The premise from `project_tool_legibility_roadmap`
(step 2): today `tool_search` ranks deferred tools with an in-process Okapi BM25 over a
flattened search-doc (`internal/agent/tools/search.go` + `bm25.go`) — pure keyword. On
"prezzo bitcoin" the model never surfaced `web_search`. The question: does granite-embedding
cosine ranking actually beat BM25 at surfacing the right **deferred** tool by meaning, over a
realistic live-mounted corpus?

## Research

- **Prior art is the architecture.** `internal/agent/prompt/reasoning_classifier.go` already
  does granite cosine-centroid classification (spike 052: 90%/92% @~10ms CPU) and
  `internal/documents/embedder.go`'s `EmbeddingClient` already calls the live granite sidecar
  (`:8081`, 384d). This spike reuses that exact seam for tool ranking — no new dependency.
- **Authentic baseline, not a strawman.** The BM25 side drives the REAL production path
  `tools.ToolSearch.Execute` and parses its `## <name>` headers back out — the same code that
  ships. (Gotcha caught mid-spike: `Execute` builds its result via `tools.NewResult`, which
  *requires* `tools.WithToolCallContext(ctx, …)`; without it every call errors and BM25 looks
  like 0/15 — a false-red. Fixed; the real BM25 scores 4/15.)
- **External grounding (operator-supplied, arXiv 2604.01733v1, RAG retrieval strategies for
  text+table docs):** across 10 strategies / 23k queries, **Hybrid (BM25 + Dense via RRF)
  consistently beats either constituent**, and **cross-encoder reranking adds the single
  largest gain (+17.4%, Recall@5 0.695→0.816)**. BM25 even *out*-performs dense on precise
  domain terminology. The paper's verdict — hybrid is the minimum viable baseline, rerank for
  quality — maps directly onto this spike's findings and is the reason the production answer is
  **hybrid, not pure embedding** (carried into spike 056).

## How to Run

```bash
# stack up: aura-llama-embed (:8081), aura-agent-memory-mcp (:8091); mail/whatsapp in
# ~/.aura/mcp/servers.json. OPENROUTER_API_KEY only needs to be non-empty (config.Load gate).
export OPENROUTER_API_KEY=dummy-for-config-load
go run ./.planning/spikes/054-semantic-toolsearch-vs-bm25
# add SPIKE054_DUMP=1 to print the top-5 cosine scores per query (margin inspection).
```

## What to Expect

A 53-tool live corpus (in-repo deferred 8 + mail 16 + memory 17 + whatsapp 12), then a
head-to-head table: BM25 vs embedding-natural vs embedding-bm25doc, scored top-1 / recall@3 /
MRR@10 over 15 documented Italian mis-route queries. Exit 0 = VALIDATED.

## Results

**VALIDATED — semantic ranking roughly doubles BM25 on a real 53-tool corpus, but the win is
in cross-category routing; intra-namespace disambiguation is the cap, and the production shape
is hybrid + rerank, not pure embedding.**

| Ranker | top-1 | recall@3 | MRR@10 |
|---|---|---|---|
| bm25-production (shipped) | 4/15 (27%) | — | 0.267 |
| **embedding-natural** | **8/15 (53%)** | 11/15 | 0.640 |
| **embedding-bm25doc** | **8/15 (53%)** | 11/15 | **0.676** |

Latency: query-embed **~9–15 ms** over the 53-tool corpus (cosine included); one-time anchor
build of 53 tools ×2 doc variants = **5.1 s** (cached in prod like the classifier anchors).

### What embedding wins (BM25 returns nothing)

Pure-semantic routing BM25 can't touch — meteo, news, restaurant, "scarica articolo dal link",
"ricorda che preferisco…": all rank-1 embedding, all **absent** in BM25 (Italian query vs
English tool description = zero lexical overlap → BM25 0 score → the no-match orientation tail).

### What BM25 wins (the regression risk for a pure switch)

BM25 scores 4 — exactly the queries with a literal lexical anchor: `web_fetch` ("…pagina
https://…"), `shell_exec` ("…script **python**…"), `whatsapp__send_message` ("…**whatsapp**…"),
`send_file` ("…**file** excel…"). Embedding also wins 3 of these — but **loses `shell_exec`**
("esegui questo script python" → embedding ranks web_search #1, shell_exec #4). This single
case is the concrete argument for **hybrid**: RRF-fusing BM25 would recover it (spike 056).

### The real mechanism — a narrow cosine band + a namespace gravity well

`SPIKE054_DUMP=1` shows every cosine sits in **0.70–0.84**; the discriminative signal is the
top ~0.05. Consequences:

1. **Verbose/conversational queries collapse the margin.** "quanto costa il bitcoin adesso?" →
   `web_search` is **not even top-5** (top = `mail__delete_mailbox` 0.747, all within ~0.01).
   The terse "prezzo bitcoin" recovers it to rank 3. The single *documented* query is
   embedding's **worst** case → reinforces the roadmap's pending decision: **un-defer
   web_search/web_fetch now** (always-visible, +~300 tok cached ~$0) is the robust fix for the
   bitcoin class; semantic search only helps once `tool_search` is invoked at all.
2. **A large generically-described namespace pollutes everything.** The 16 mail tools
   ("search", "send", "verify", "get" …) surface in the top-5 of bitcoin, restaurant, and
   python-script queries. This IS the 30–50-tool cliff (`reference_anthropic_tool_search_industrial_standard`)
   — it manifests as **intra-/cross-namespace confusion**, not graceful degradation.
3. **Right answers cluster in the top-3, not top-1, inside a namespace.** send_email is rank 3
   (mail__mark_email 0.809 / save_draft 0.806 / send_email 0.803 — 0.006 apart); search_emails
   rank 4 behind mail__advanced_search; document_search rank 3 behind mail__save_draft. Recall@3
   = 11/15 catches most. → return top-K (not top-1) and/or **rerank** (paper's +17.4% lever),
   and consider **namespace-aware two-stage routing** (query→namespace, then →tool).
4. **bm25doc > natural (MRR 0.676 vs 0.640).** Folding schema property keys into the embedded
   doc — the paper's "contextual retrieval" analog — measurably helps tool-jargon queries.
   Production doc should be enriched, not the bare summary.

## Investigation Trail

1. First run: BM25 0/15, embedding 7–8/15, VALIDATED — but the log showed every BM25 `Execute`
   erroring `missing tool-call context`. Caught it as a false-red (no-skip-as-green discipline):
   the baseline never ran. Added `WithToolCallContext`; BM25 rose to a fair 4/15.
2. First run also mounted only memory (25 tools): `config.Load` aborted on empty
   `OPENROUTER_API_KEY`, dropping mail/whatsapp, so their golds were "absent" for a corpus
   reason, not an embedding miss. Set a dummy key → 53-tool corpus (mail/whatsapp mount live).
3. The bitcoin "rank-10, top=mail__delete_mailbox" result was counterintuitive enough to verify
   rather than report — added `SPIKE054_DUMP`. The top-5 scores (all ~0.74, ~0.01 spread)
   confirmed it's a genuine narrow-margin property, not a degenerate-embedding bug, and surfaced
   the mail-namespace gravity-well finding.

## Signal for the Build

- **Build semantic `tool_search`** — it ~2× BM25 for cross-category routing, reuses the granite
  sidecar (zero new deps), and is fast (~10ms/query, anchors cached + invalidated on MCP mount
  like the existing `InvalidateIndex()`).
- **Ship it as HYBRID, not a pure swap** (RRF BM25+embedding) — recovers the lexical-anchor
  cases embedding loses (shell_exec) and is the paper's consistently-winning shape. → spike 056.
- **Return top-K + rerank inside a namespace**, or route query→namespace→tool, to fight the
  dense-cluster confusion. The narrow cosine band makes top-1 fragile; margin is the ambiguity
  signal (same signal the reasoning classifier already gates on).
- **Un-defer web_search/web_fetch independently of step 2** — the verbose-bitcoin failure proves
  semantic tool_search is not a substitute for keeping the two web tools always-visible.
- **Enrich the embedded tool doc** with schema keys (bm25doc beat natural).
