---
spike: 058
name: unified-embedding-index
type: standard
validates: "Given the shared shape of the reasoning classifier (052/053) and the tool ranker (054-057), when both are re-expressed as one generic embedding Index (Add/Rank/margin, two modes) and run live over the granite sidecar, then the SAME core correctly does tier classification (Centroid mode) AND tool ranking (PerItem mode) — proving one reusable industrial core fuses tools, reasoning, and future routing"
verdict: VALIDATED
related: [052-reasoning-tier-embed-classifier, 053-reasoning-classifier-active-learning, 054-semantic-toolsearch-vs-bm25, 057-toolselection-oracle-signal]
tags: [embeddings, reusable-core, architecture, reasoning, tool-search, slice-tooling]
---

# Spike 058 (capstone): one reusable embedding-index core

## What This Validates

Operator directive (2026-06-12): *"create reusable code to fuse all tool, reasoning and future
improvement on Aura — the industrial way,"* alongside Anthropic's official tool-search-tool
(which sanctions a custom embedding search returning `tool_reference` blocks). The thesis: the
reasoning-tier classifier (spikes 052/053) and the semantic tool ranker (spikes 054-057) are the
SAME machine — an `Embedder` seam, a labeled vector bank, cosine + a top-2 margin, an
incremental cache, and active-learning hooks (all already present in
`internal/agent/prompt/reasoning_classifier.go`). This spike extracts that into one generic
`Index` and runs it both ways, live, to prove the reuse is real, not aspirational.

## How to Run

```bash
export OPENROUTER_API_KEY=dummy-for-config-load
go run ./.planning/spikes/058-unified-embedding-index
```

## Results

**VALIDATED — one ~90-LOC `Index` core does both jobs correctly: reasoning classification 6/6
(Centroid mode) and tool ranking 8/11 (PerItem mode), reproducing spikes 052 and 054
respectively. The only per-consumer difference is `Mode` and the items added.**

```
CLASSIFY (Centroid)  6/6   buongiorno→none  meteo→low  "scrivi funzione go"→high  ...
TOOLRANK (PerItem)   8/11  meteo→web_search  preferenza→memory_add_preference  python→shell_exec ...
                            misses: whatsapp→get_last_interaction, mail→mark_email, documenti→smtp_info
```

The tool-rank misses are the exact intra-namespace / gravity-well failures spikes 054/055
catalogued — and every one has a **low top-2 margin** (0.002–0.016), so the uniform margin
signal flags precisely the turns the two-tier oracle (spike 057) should escalate. The same
signal, the same gate, both consumers.

### The proposed industrial core (what the spike demonstrates)

```go
type Embedder interface { Embed(ctx, []string) ([][]float64, error) } // = documents.EmbeddingClient = prompt.Embedder today

type Index struct { /* embedder, items, centroids, mode */ }
func NewIndex(e Embedder, mode Mode) *Index
func (ix *Index) Add(ctx, []Item) error              // embed + append; incremental (runtime MCP mount = Add new tools only)
func (ix *Index) Rank(ctx, query) ([]Scored, margin, error) // cosine; PerItem (tools) or Centroid (tiers); margin = top1-top2
```

- **Reasoning classifier** = `Index(Centroid)` over 3 tier groups (def+seeds) → argmax tier.
- **Tool ranker** = `Index(PerItem)` over N tool docs → top-K names (Anthropic `tool_reference`s).
- **Future routers** (channel selection, skill selection, memory-tool routing) = the same `Index`.
- **Active-learning** (spike 053): one `Learner`/`ExampleStore` pair attaches to `Index` and
  serves every consumer — low-margin observe → oracle label → fold example → centroid refresh.

One core. One embedding sidecar (granite :8081). One Neo4j example store. One margin gate. One
two-tier oracle. Every embed-classify-or-retrieve feature on Aura instantiates it.

## Investigation Trail

- Centroid mode had to be a first-class mode, not a special case: the classifier ranks the MEAN
  of each tier's vectors (robust to a single noisy label — spike 053), whereas the tool ranker
  ranks each item individually. Both are the same dot-product-then-sort with a different
  candidate set, which is exactly why one `Rank` serves both.
- Ran the tool-rank corpus live (53 tools) rather than a fixture, so the 8/11 is the real 054
  number reproduced through the generic core — not a toy.

## Signal for the Build

- **Extract `internal/semindex` (or `embedindex`)** — the generic `Index{Embedder, mode, bank,
  margin, Add, Rank}` plus the spike-053 `Learner`/`ExampleStore` hooks. Migrate
  `reasoning_classifier.go` onto it (it already has every piece) and build the semantic
  `tool_search` ranker on the same package. This is the operator's "fuse it all, industrial way."
- **It aligns with Anthropic's official architecture**: their tool-search supports a custom
  embedding search that returns `tool_reference` blocks; Aura's `Index(PerItem)` IS that search,
  self-hosted (Aura is OpenRouter/OpenAI-compat, not the Anthropic API, so it implements the
  search itself — which it already does in `ToolSearch`).
- **Cost of reuse is negative**: less code than 4 bespoke implementations, one place to add
  margin/active-learning/rerank, one sidecar, one store. The pattern is proven 4× (052/053/054/
  058). The risk is only the migration, not the design.
