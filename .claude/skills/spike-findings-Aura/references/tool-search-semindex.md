# tool search semindex

Semantic `tool_search` for deferred-tool discovery + the unified `internal/semindex`
embedding-index core that fuses reasoning-tier classification AND tool ranking. Granite
cosine ranking ~2× BM25 for cross-category routing; production shape is EMBEDDING-PRIMARY
(not hybrid RRF); brute-force cosine holds to N≈115 with no ANN; one ~90-LOC core does both jobs.

## Requirements

Non-negotiables from MANIFEST Session-14 (lines 136-141) + the binding spike verdicts. A future
build MUST honor every one.

1. **The deferred-tool corpus is DYNAMIC — the index MUST re-embed/invalidate on MCP mount, never
   a one-time static bank.** Users mount MCP servers at runtime (Claude-Code-style `mcp add`). The
   embedding cache hangs off the *same seam* the BM25 path already exposes — `ToolSearch.InvalidateIndex()`
   (called on MCP reconnect) and the classifier's `Refresh()` — but embeds **only the newly-advertised
   tools** and appends to the bank (incremental `Add`), not a full rebuild. Per-mount re-embed is a
   budgeted metric (~7 ms/tool, spike 055), async + off-hot-path; never block the mount or the user turn.

2. **The embedding seam reuses `documents.EmbeddingClient` (granite sidecar `:8081`, 384d) — the same
   sidecar the reasoning classifier already depends on.** No new dependency, no new sidecar. `prompt.Embedder`
   and `documents.EmbeddingClient` already satisfy the identical `Embed(ctx, []string) ([][]float64, error)`
   signature.

3. **Production ranking shape = EMBEDDING-PRIMARY (bm25doc-enriched input), NOT hybrid RRF.** Do NOT ship
   naive or weighted RRF — both *regress* the dominant Italian-natural-language traffic (spike 056). If
   robustness across query styles is wanted, use **guarded fusion only** (embedding-primary; BM25
   contributes solely as a confident, intersection-gated tiebreak) — it is ≡ pure embedding on the IT
   corpus and strictly safer. The paper's "always hybrid" advice does NOT transfer here.

4. **Brute-force cosine over the cached bank — NO ANN/HNSW index.** At N=115 ranking the whole corpus
   is 85 µs; linear in N over 384-dim dot products. Aura will not mount thousands of tools. ANN is
   premature optimization (spike 055).

5. **ONE reusable `Index` core (`internal/semindex`) serves BOTH reasoning classification (Centroid mode)
   and tool ranking (PerItem mode).** The reasoning classifier and the tool ranker are the same machine
   (Embedder seam + labeled vector bank + cosine + top-2 margin + incremental cache + active-learning
   hooks). Migrate `reasoning_classifier.go` onto it; build the semantic `tool_search` ranker on the same
   package. Aligns with Anthropic's tool-search custom-embedding `tool_reference` path (Aura self-hosts the
   search because it is OpenRouter/OpenAI-compat, not the Anthropic API).

6. **Step-3 self-improvement loop = TWO-TIER oracle, NOT a free-only self-bootstrap.** Detect mis-routes
   cheaply on every turn (shell/fs-fallback used OR used-tool ≠ ranker top-1, R=1.0); label the confident
   majority free with the ranker; **escalate the low-margin / disagreement tail to a stronger LLM oracle
   (spike 053's validated labeler)** — it is the only tier that can fix the ranker's own gravity-well
   errors. Free-ranker-only would amplify its systematic bias. ORTHOGONAL lever: keep `web_search`/`web_fetch`
   **un-deferred** (always-visible, +~300 tok cached ~$0) — semantic `tool_search` only helps once
   `tool_search` is invoked at all, and verbose queries ("quanto costa il bitcoin adesso?") are its worst case.

## How to Build It

SHIPPED state: this landed as `internal/semindex` (`core.go`, `classifier.go`, `ranker.go`,
`semindex.go`) + `internal/activelearn`, with `internal/agent/prompt/reasoning_classifier.go`
migrated onto it (project memory: "Unified semindex substrate", phase 08.2). The recipe below is
the proven spike shape that produced it; rebuild against it.

### 1. The reusable core (spike 058, ~90 LOC, stdlib + 1 seam)

The whole core is the `Index` from `sources/058-unified-embedding-index/main.go`:

```go
type Embedder interface { Embed(ctx context.Context, texts []string) ([][]float64, error) }
// satisfied today by documents.EmbeddingClient AND prompt.Embedder — identical signature.

type Mode int
const ( PerItem Mode = iota; Centroid )

type Item   struct { Group, Doc string; vec []float64 } // Group = label (tier name, or tool's own name)
type Scored struct { Label string; Score float64 }

type Index struct { embedder Embedder; mode Mode; items []Item; groups []string; groupVec map[string][]float64 }
func NewIndex(e Embedder, mode Mode) *Index

func (ix *Index) Add(ctx, []Item) error                       // embed + l2-normalize + append; Centroid rebuilds centroids
func (ix *Index) Rank(ctx, query) ([]Scored, margin float64, error) // cosine; margin = top1 - top2
```

- `Add` is the **incremental, invalidate-on-change seam**: a runtime MCP mount calls `Add` with ONLY
  the new tools (~7 ms/tool), never a full rebuild.
- `Rank` is the same dot-product-then-sort for both consumers — only the candidate set differs.
  PerItem ranks every item (tools → top-K names = Anthropic `tool_reference`s); Centroid ranks the
  mean vector of each group (tiers → argmax). Sort is `sort.SliceStable` by descending score, tie-break
  by `Label` ascending.
- Math primitives are the only numeric code: `l2` (L2-normalize, returns input if norm 0), `dot`
  (returns -2 on length mismatch), `meanNormalize` (mean of vecs then `l2`). Vectors are normalized at
  `Add` time so `dot` == cosine.

Two-mode proof (live, granite `:8081`): **classifier 6/6 (Centroid)** reproducing spike 052,
**tool-rank 8/11 (PerItem)** reproducing spike 054. Every tool-rank miss had a low top-2 margin
(0.002–0.016) — the uniform margin signal flags exactly the turns the two-tier oracle should escalate.

### 2. Wire the live granite embedder

```go
embedder := &documents.EmbeddingClient{
    BaseURL:    "http://127.0.0.1:8081",           // AURA_EMBED_BASE_URL overrides
    Client:     &http.Client{Timeout: 60 * time.Second},
    Dimensions: knowledge.DefaultEmbedDimensions,   // 384
}
```

### 3. Build the tool corpus doc (enrich it — bm25doc beats natural)

Per-tool embedded document (spike 058 `buildLiveCorpus`):

```go
doc := strings.TrimSpace(strings.ReplaceAll(s.Name, "_", " ") + ": " + s.Summary + ". " + s.Description)
// only tools with s.Deferred == true enter the corpus
```

Spike 054 measured **bm25doc (schema property keys folded in) > natural** (MRR 0.676 vs 0.640) — the
"contextual retrieval" analog. Production doc should be enriched with schema keys, not the bare summary.

### 4. Drive the real BM25 baseline correctly (gotcha)

The authentic baseline drives `tools.ToolSearch.Execute` and parses its `## <name>` headers back out.
**`Execute` builds its result via `tools.NewResult`, which REQUIRES `tools.WithToolCallContext(ctx, …)`;
without it every call errors `missing tool-call context` and BM25 looks like 0/15 (false-red).** With
the context it scores a fair 4/15. (No-skip-as-green: verify the baseline actually ran.)

### 5. Ranking results to target (spikes 054 + 056, live 53-tool corpus)

| Ranker | top-1 | recall@3 | MRR@10 |
|---|---|---|---|
| bm25-production (shipped) | 4/15 (27%) | — | 0.267 |
| embedding-natural | 8/15 (53%) | 11/15 | 0.640 |
| **embedding-bm25doc** | **8/15 (53%)** | 11/15 | **0.676** |

Spike 056 mixed-set (7 semantic + 6 lexical): **pure embedding 9/13, MRR 0.799** == **guarded RRF 9/13,
0.799**; naive RRF 6/13 (0.601), weighted RRF 3:1 6/13 (0.657), bm25-production 4/13 (0.319).

### 6. Guarded fusion (only if you want cross-query-style safety)

From `sources/056-hybrid-fusion-vs-pure/main.go` `rrfGuarded` (constants `rrfK = 60`, `rankK = 10`):
BM25 contributes a `1/(rrfK+rank+1)` term **only when** `len(bm25Rank) > 0 && len(bm25Rank) <= 5`
(small set, not a noisy flood) **AND** the tool is in embedding's top-15 (intersection gate). Otherwise
BM25 is ignored and the order is pure embedding. On the IT corpus this degenerates to pure embedding
exactly, but breaks ties when a genuinely lexical/English query gives BM25 real signal.

### 7. Latency / cost budget (spike 055)

- Query embed + cosine: **~9–15 ms** over 53 tools; **85 µs** rank (embed excluded) at N=115.
- One-time cold anchor build of 53 tools (×2 doc variants) = **5.1 s** / `1934 ms` for one variant —
  cached in prod like the classifier anchors.
- Incremental re-embed on mount: **~5–8 ms/tool** (~7 ms). 20-tool server ≈ 150 ms; 8-tool ≈ 46 ms.

### 8. Step-3 self-improvement loop (spike 057, PARTIAL — build with the two-tier oracle)

Detection (cheap, no embedding needed for the best signal) from `sources/057-toolselection-oracle-signal/main.go`:

```go
var fallbackTools = map[string]bool{ // "the model improvised instead of a dedicated tool"
    "shell_exec": true, "shell_poll": true, "shell_kill": true,
    "fs_read": true, "fs_glob": true, "fs_grep": true, "skill": true,
}
const marginFloor = 0.02 // low-margin GATE (when to spend an oracle call), NOT a standalone flag
sigShell    := fallbackTools[tr.used]
sigDisagree := !sameTool(tr.used, rankerTop1)
sigUnion    := sigDisagree || sigShell          // P=0.88 R=1.00
```

`sameTool` is order-insensitive identity (`a == b || HasSuffix(a,"__"+b) || HasSuffix(b,"__"+a)`) — the
first run logged a false positive comparing bare `send_message` vs namespaced `whatsapp__send_message`;
making identity symmetric raised precision 0.78→0.88.

Detection metrics: `used ≠ ranker-top1` P=0.88 R=1.00; **shell/fs-fallback P=0.88 R=1.00 (best single
signal, zero-cost, no embedding)**; low-margin (<0.02) P=0.60 R=0.86 (noisy alone — use as gate). UNION
P=0.88 R=1.00 (one benign FP: the efficient `shell_exec` turn).

Labeling = TWO TIERS (the free ranker alone is self-limited): free ranker labels **4/7** mis-routes
right; on **3/7** the ranker is itself wrong (bitcoin→`mail__delete_mailbox`, documents→`web_fetch`,
mail→`mail__mark_email` — the gravity-well cases) and would teach the WRONG label. So:
1. Detect on every turn (union, R=1.0).
2. Label confident high-margin cases free with ranker top-1.
3. **Escalate low-margin/disagreement tail to the LLM oracle** (local Gemma-E2B or DeepSeek router, spike
   053's proven labeler, ~3% noise tolerated by centroid averaging) — async, post-turn, margin-gated.
4. Fold confirmed `(request-embedding → correct-tool)` pairs into the per-tool bank; refresh centroids
   (spike 053 mechanism, content-hash dedup, curated anchors authoritative). `internal/activelearn` holds
   the `Learner`/`ExampleStore` (Neo4j) hooks — they attach to `Index` and serve every consumer.

## What to Avoid

- **DO NOT ship naive or weighted RRF (spike 056 INVALIDATED both).** They look right (it's the paper's
  recommendation) but *regress*: naive 6/13 vs pure 9/13; weighting embedding 3:1 still 6/13. BM25 scores
  0/7 on Italian semantic queries yet returns weak partial matches that RRF's `1/(k+rank)` boost drags the
  correct embedding-#1 down — "che tempo fa" 1→4, "trova un ristorante" 1→4, "scarica articolo" 1→off
  top-10. Fusing a retriever with no signal on your queries adds noise, not recall.
- **DO NOT trust the arXiv 2604.01733 "hybrid is the minimum viable baseline" claim verbatim.** It holds
  when BOTH retrievers are individually competent (English financial docs: BM25 R@5 0.644, dense 0.587).
  Aura's setting is asymmetric — BM25 is near-useless on IT-natural-language tool queries, so there is no
  second signal to fuse. Verify retriever competence before fusing. (The cross-encoder rerank finding —
  +17.4%, R@5 0.695→0.816 — is still the real quality lever, but for intra-namespace disambiguation, not
  fusion.)
- **DO NOT build an ANN/HNSW index (spike 055).** Brute-force cosine is 85 µs at N=115 — 12× under a
  millisecond. ANN is premature optimization at Aura's scale.
- **DO NOT make the semantic index a static one-time bank.** The corpus is dynamic; a non-incremental
  rebuild on every mount is the wrong seam. Embed only the delta.
- **DO NOT use the free semantic ranker as the SOLE step-3 teacher (spike 057 PARTIAL).** It can only
  confirm what it already knows — it labels 3/7 hard mis-routes WRONG and a free-only loop amplifies its
  own gravity-well bias. The LLM escalation tier is mandatory for convergence, not optional.
- **DO NOT treat semantic `tool_search` as a substitute for keeping web tools visible.** Verbose
  conversational queries collapse the cosine margin: "quanto costa il bitcoin adesso?" → `web_search` is
  not even top-5 (top = `mail__delete_mailbox` 0.747). The terse "prezzo bitcoin" recovers it to rank 3.
  Un-deferring `web_search`/`web_fetch` is the orthogonal, near-zero-cost fix for "the model never called
  `tool_search`".
- **DO NOT use low-margin as a mis-route FLAG (P=0.60, noisy).** The cosine band is narrow everywhere, so
  margin is the right *gate* for "should we spend an oracle call" but the wrong *flag* for "was this a mis-route".
- **DO NOT call `ToolSearch.Execute` without `tools.WithToolCallContext`** — silent `missing tool-call
  context` error → false 0/N (false-red).
- **DO NOT compare bare vs namespaced tool names with an order-sensitive suffix match** — use symmetric
  `sameTool`, else spurious false positives.

## Constraints

- **Embedding sidecar:** granite-embedding (IBM Granite, Apache-2.0), llama.cpp serve, `127.0.0.1:8081`,
  **384 dimensions** (`knowledge.DefaultEmbedDimensions`), CPU (~10 ms/query). Vector store Neo4j HNSW for
  the example bank. Reused by both the reasoning classifier and the tool ranker — one sidecar, one store.
- **Live corpus for spikes:** 53 tools = in-repo deferred 8 + mail 16 + memory 17 + whatsapp 12. agent-memory
  MCP on `:8091` (`AURA_AGENT_MEMORY_MCP_URL` / `AURA_AGENT_MEMORY_MCP_PORT`, default `http://127.0.0.1:8091/mcp/`);
  mail/whatsapp from `~/.aura/mcp/servers.json`.
- **Env vars:** `OPENROUTER_API_KEY` must be non-empty or `config.Load` aborts and drops mail/whatsapp from
  the corpus (set `dummy-for-config-load` for spikes). `AURA_EMBED_BASE_URL` overrides the granite URL.
  `AURA_RUN_DIR` defaults to temp if unset. `SPIKE054_DUMP=1` prints top-5 cosine scores for margin inspection.
- **Cosine band:** every score sits in **0.70–0.84**; the discriminative signal is the top **~0.05**. This is
  why top-1 is fragile and margin is the ambiguity gate.
- **Scaling cliff:** mounting +62 generic-verb enterprise tools (github 20 / slack 14 / gdrive 10 / jira 10
  / calendar 8) degraded top-1 8→6 (−25%) and MRR 0.719→0.582 (−19%), knee at **N≈100**. Gradual gravity-well
  erosion of borderline routing, not catastrophic collapse — argues for namespace-aware retrieval / per-namespace
  caps / two-stage query→namespace→tool routing (the remaining quality lever, NOT lexical fusion).
- **Fusion constants (if guarded):** `rrfK = 60`, `rankK = 10`; BM25 gate `len(bm25Rank) <= 5` AND tool in
  embedding top-15.
- **Active-learning oracle noise budget:** spike 053's LLM labeler ~3% noise, tolerated by centroid averaging;
  curated seed anchors stay authoritative; content-hash dedup.
- **Prior-art numbers:** spike 052 granite cosine 90%/92% @~10 ms CPU; spike 053 async centroid-refresh +7pts→97%.

## Origin

Synthesized from spikes: **054, 055, 056, 057, 058**. Source files in:
`sources/054-semantic-toolsearch-vs-bm25/`, `sources/055-toolsearch-scaling-cliff/`,
`sources/056-hybrid-fusion-vs-pure/`, `sources/057-toolselection-oracle-signal/`,
`sources/058-unified-embedding-index/` (each README.md + main.go).

Verdicts: 054 VALIDATED (semantic ~2× BM25, cross-category routing; intra-namespace cap; ship hybrid+rerank
per arXiv 2604.01733 — superseded by 056) · 055 VALIDATED (brute-force suffices to N≈115, no ANN; incremental
re-embed ~7 ms/tool; gradual gravity-well cliff) · 056 VALIDATED (production shape EMBEDDING-PRIMARY, NOT hybrid;
naive/weighted RRF regress; guarded ≡ pure embedding) · 057 PARTIAL (detection solved cheap R=1.0; free ranker
teacher self-limited → two-tier oracle required) · 058 VALIDATED (one ~90-LOC `Index` core does classification +
tool ranking; extract `internal/semindex`).

Binding MANIFEST source: Session-14 requirements (`.planning/spikes/MANIFEST.md` lines 136-141) + Session 14
session note (line 31). SHIPPED: `internal/semindex` + `internal/activelearn`, `reasoning_classifier.go` migrated
(project memory, phase 08.2).
