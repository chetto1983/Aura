---
spike: 055
name: toolsearch-scaling-cliff
type: standard
validates: "Given the deferred corpus grown 53→115 by simulating runtime MCP mounts (github/slack/gdrive/jira/calendar generic-verb tools), when ranking quality, brute-force cosine latency, and per-mount incremental re-embed cost are measured as N grows, then we learn whether quality holds past the 30-50-tool cliff and whether brute-force suffices or an ANN/HNSW index is needed"
verdict: VALIDATED
related: [054-semantic-toolsearch-vs-bm25]
tags: [tool-search, scaling, latency, runtime-mount, embeddings, slice-tooling]
---

# Spike 055: Scaling cliff + runtime-mount cost

## What This Validates

The operator's point on spike 054: the deferred-tool corpus is **dynamic** — a user can
mount MCP servers at runtime (Claude-Code-style `mcp add`), so a semantic index can't be a
one-time static bank; it must re-embed/invalidate on mount. This spike measures the three
things that decide whether that is affordable and whether quality survives:

1. **Quality vs N** — top-1 / MRR@10 over 13 documented queries (golds stay in the corpus;
   the mounted servers are pure distractors). Does routing survive a flood of generic tools?
2. **Runtime re-embed cost** — time to embed ONLY the newly-mounted tools per stage.
3. **Brute-force cosine latency** — mean per-query rank time as N grows.

## How to Run

```bash
export OPENROUTER_API_KEY=dummy-for-config-load   # config.Load gate; mounts mail/whatsapp
go run ./.planning/spikes/055-toolsearch-scaling-cliff
```

Starts from the real 53-tool live corpus (spike 054), then mounts 5 synthetic enterprise MCP
servers (github 20, slack 14, gdrive 10, jira 10, calendar 8 = +62 generic-verb tools), one
stage at a time.

## Results

**VALIDATED — brute-force is trivially fast and runtime re-embed is cheap; the quality cliff
is real but gradual and bounded, and it points squarely at namespace-aware retrieval.**

| Stage | N | added | re-embed | mean rank | top-1 | MRR@10 |
|---|---|---|---|---|---|---|
| base-live | 53 | (53) | 1934 ms (cold) | <1 µs | 8/13 | 0.719 |
| +github | 73 | 20 | 150 ms | 41 µs | 8/13 | 0.699 |
| +slack | 87 | 14 | 87 ms | 83 µs | 8/13 | 0.692 |
| +gdrive | 97 | 10 | 70 ms | 91 µs | 8/13 | 0.686 |
| +jira | 107 | 10 | 71 ms | 82 µs | 7/13 | 0.647 |
| +calendar | 115 | 8 | 46 ms | 85 µs | 6/13 | 0.582 |

### Brute-force cosine is fine — no ANN needed

At N=115, ranking the whole corpus takes **85 µs** (query embed excluded) — sub-millisecond by
12×, and linear in N (384-dim dot products). Aura will not realistically mount thousands of
tools; **brute-force cosine over the cached bank is the right implementation**, no HNSW/ANN
index, no premature optimization. (The existing reasoning classifier ranks the same way.)

### Runtime mount is cheap — embed only the delta

Incremental re-embed is **~5–8 ms per tool**: a 20-tool server mounts for ~150 ms, an 8-tool
server for ~46 ms. The 1.9 s "base" figure is the one-time cold build of 53 tools. So the
production seam is: on `mcp add`, embed ONLY the newly-advertised tools and append to the
cached bank (the embedding analog of `ToolSearch.InvalidateIndex()` — but incremental, not a
full rebuild). It is a sub-second, post-mount, off-hot-path cost. The operator's concern is
fully answered: a dynamic corpus is affordable.

### The cliff is real, gradual, and a gravity-well effect

Mounting 62 generic enterprise tools degraded **top-1 8→6 (−25%)** and **MRR 0.719→0.582
(−19%)**, with the knee around **N≈100** (jira/calendar). It is not catastrophic collapse —
the strong cross-category cases (meteo/news/restaurant → web_search) never moved. What erodes
is the **borderline** routing: queries whose gold already sat in a narrow-margin cluster
(spike 054). Generic verbs — `create_*`, `search_*`, `send_*`, `update_*` from
github/slack/jira/calendar — are new gravity wells that crowd those margins. send_email and
document_search are the first to flip as slack/gdrive/jira pile on synonymous "send"/"search"
tools.

## Investigation Trail

- Chose realistic generic-verb enterprise servers as distractors precisely because spike 054
  showed the 16-tool *mail* namespace already acted as a gravity well. Random-noise tools would
  understate the cliff; generic-but-plausible tools are the real runtime-mount risk.
- The base-live `<1 µs` rank is a rounding floor (53×384 dot products complete faster than the
  microsecond clock resolves per query); the 41–91 µs readings at N≥73 establish the real
  linear trend.

## Signal for the Build

- **Implement the index as a cached embedding bank with incremental re-embed on MCP mount** —
  brute-force cosine, no ANN. Hang the cache off the same invalidation seam as the BM25 path
  (`InvalidateIndex()`), but embed only the delta (~7 ms/tool).
- **The cliff is the argument for namespace-aware retrieval** (carry to 056): cap each
  namespace's contribution to the candidate set, or do two-stage routing (query→namespace, then
  →tool), so 62 github/slack/jira tools can't crowd web_search/send_email out of the top-K.
- **Per-mount re-embed is async + off-hot-path** — never block the mount or the user turn on it.
- Combined with 054: semantic ranking is fast and affordable at Aura's scale; the open quality
  problem is intra-/cross-namespace disambiguation, not latency or corpus size.
