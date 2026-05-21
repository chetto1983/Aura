# Graphify: broad-query handling — source-level study

Research target: how does graphify (a working graph-RAG over arbitrary corpora) respond
to broad questions like "what are the top topics in my graph" or "what do X and Y have in
common" without making the LLM thrash by reading source files one at a time?

All references are to `D:/tmp/graphify/graphify/*.py`. Quotes are verbatim from source
(line numbers stable as of the cloned snapshot, commit unknown but file timestamps in
the 2026-04 → 2026-05 window).

---

## TL;DR (12 lines)

1. Graphify does NOT do broad-query Q&A live. It pre-bakes one human-readable
   `GRAPH_REPORT.md` per corpus at build time.
2. The "broad question" path is: agent reads `GRAPH_REPORT.md` (or `wiki/index.md`)
   as plain Markdown. No tool call. No retrieval.
3. The report contains: corpus stats, god-node list (top-N most connected), 5
   surprising connections, **every community** with cohesion score + member sample,
   ambiguous-edge list, knowledge gaps, and 7 suggested questions.
4. Communities come from Leiden (graspologic) with Louvain fallback; the **LLM
   labels them with a 2-5 word name in a separate offline step** (skill.md Step 5).
5. The wiki export goes further: one `<CommunityName>.md` article per community +
   one `<GodNodeLabel>.md` article per god node + an `index.md` catalog with
   `[[wiki-links]]` between them.
6. Live tool surface is **7 MCP tools** in `serve.py`: `query_graph`, `get_node`,
   `get_neighbors`, `get_community`, `god_nodes`, `graph_stats`, `shortest_path`.
   Plus 3 PR tools (out of scope).
7. `query_graph` does **deterministic IDF-weighted seed selection → BFS/DFS with
   hub-bypass at p99-degree → token-bounded text rendering**. No vector search.
8. "X vs Y" → `shortest_path` (NetworkX `shortest_path`) on undirected view, with
   ambiguity warning when top-1 vs runner-up scoring is within 10%.
9. Anti-thrash mechanisms: (a) hard token budget with cut marker + narrowing hint,
   (b) hub-bypass during traversal, (c) seed gap-ratio (drop seeds <20% of top score),
   (d) the pre-baked report itself.
10. **Lift 1**: Pre-bake `GRAPH_REPORT.md` at wiki-write/ingest time
    (`report.py:generate` mapped to Aura's wiki build).
11. **Lift 2**: Cluster wiki pages by `[[wiki-link]]` graph, write one
    `_COMMUNITY_<name>.md` digest per cluster (`cluster.py` + `wiki.py:_community_article`).
12. **Lift 3**: Replace Aura's thin TOC with two-level catalog: cluster digests at
    top, then page slugs; god-page list under (`wiki.py:_index_md`).

---

## 1. Index/TOC shape — what graphify exposes as the "manifest"

**Two layers, both materialized to disk at build time:**

### Layer 1: `GRAPH_REPORT.md` (single file)

Built by `graphify/report.py:generate` (lines 15–203). Sections in order:

```
# Graph Report - <root>  (<date>)
## Corpus Check          — file count, word count, "verdict"
## Summary               — N nodes · M edges · K communities ; EXTRACTED/INFERRED/AMBIGUOUS %
## Graph Freshness       — built-from-commit hash (optional)
## Community Hubs (Navigation)   — [[_COMMUNITY_<name>]] wikilinks
## God Nodes             — top-10 most-connected (with degree)
## Surprising Connections — top-5 cross-community edges with "why"
## Hyperedges (if any)
## Communities (N total, K thin omitted)
  ### Community N - "<LLM-assigned label>"
  Cohesion: 0.NN
  Nodes (8): A, B, C, ... (+M more)
## Ambiguous Edges - Review These
## Knowledge Gaps        — isolated-nodes list, ambiguity %
## Suggested Questions   — 7 questions the graph is uniquely positioned to answer
```

Concrete example output: `worked/httpx/GRAPH_REPORT.md` (78 lines). For a 144-node
graph, the entire report is ~3 KB / ~800 tokens. That single file is the "TOC"
for the whole knowledge graph.

Source — `report.py:67-75`:
```python
lines += [
    "",
    "## Summary",
    f"- {G.number_of_nodes()} nodes · {G.number_of_edges()} edges · {len(communities)} communities"
    + (f" ({shown_count} shown, {thin_count_summary} thin omitted)" if thin_count_summary else ""),
    f"- Extraction: {ext_pct}% EXTRACTED · {inf_pct}% INFERRED · {amb_pct}% AMBIGUOUS"
    + (f" · INFERRED: {len(inf_edges)} edges (avg confidence: {inf_avg})" if inf_avg is not None else ""),
    f"- Token cost: {token_cost.get('input', 0):,} input · {token_cost.get('output', 0):,} output",
]
```

### Layer 2: wiki — one Markdown article per community + per god-node

Built by `graphify/wiki.py:to_wiki` (lines 181–259). Output structure:

```
<output_dir>/
  index.md                  — catalog of all communities + god nodes
  <CommunityName>.md        — one per community (key concepts, cross-community links, sources, audit trail)
  <GodNodeLabel>.md         — one per god node (connections grouped by relation)
```

`_index_md` (`wiki.py:141-178`) emits just two lists: community names with node
counts, god nodes with degrees. No prose.

`_community_article` (`wiki.py:37-102`) emits per community:
- top-25 nodes by degree, with source file & degree
- cross-community link counts (which other communities does this one most connect to)
- list of source files (≤20)
- confidence breakdown (EXTRACTED/INFERRED/AMBIGUOUS counts)

**Crucially**, community names are NOT auto-generated; the orchestrator (an LLM
following `skill.md` Step 5) writes a 2-5 word label per community:

`skill.md:564-602` — `"For each community key, look at its node labels and write a
2-5 word plain-language name (e.g. 'Attention Mechanism', 'Training Pipeline',
'Data Loading')."` The labels are then re-fed into `report.generate` and
`to_wiki` and persisted to `.graphify_labels.json`.

---

## 2. Broad-query path — how "summarize my graph" gets answered

**There is no programmatic broad-query handler.** The pattern is:

> Agent reads the pre-baked `GRAPH_REPORT.md` (or `wiki/index.md`) directly as
> Markdown. No tool call, no retrieval, no LLM round-trip beyond the agent's own.

Evidence from `__main__.py` line 295 (system-prompt boilerplate the installer
injects into Claude/Gemini/Cursor/etc.):

```
- For codebase questions, first run `graphify query "<question>"` when
  graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships
  and `graphify explain "<concept>"` for focused concepts. These return a scoped
  subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of
  raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when
  query/path/explain do not surface enough context.
```

The instruction explicitly routes broad questions to a static file read, not a
tool invocation. This is the entire mechanism: the precomputed Markdown digest
**is** the broad-query answer.

There is no `summarize_graph` MCP tool, no `top_topics` tool, no community-report
generator at query time. The closest thing is `graph_stats` (`serve.py:639-649`)
which returns only node/edge/community counts + confidence %.

---

## 3. Multi-hop comparison — "X vs Y"

Handled by `shortest_path` tool (`serve.py:651-704`) and CLI
`graphify path "X" "Y"` (`__main__.py:1579-1660`).

Algorithm:
1. Score nodes for both query labels with IDF-weighted token matching
   (`_score_nodes`, `serve.py:84-107`).
2. Top-scoring node ID for each side is the seed.
3. If both resolve to the same node → return ambiguity error.
4. If runner-up is within 10% of top, attach an ambiguity warning (lines 667–675):
   ```python
   for name, scored in (("source", src_scored), ("target", tgt_scored)):
       if len(scored) >= 2:
           top, runner = scored[0][0], scored[1][0]
           if top > 0 and (top - runner) / top < 0.10:
               warnings.append(
                   f"warning: {name} match was ambiguous "
                   f"(top score {top:g}, runner-up {runner:g})"
               )
   ```
5. `nx.shortest_path(G.to_undirected(as_view=True), src_nid, tgt_nid)` — pure
   NetworkX BFS on undirected view.
6. Cap at `max_hops` (default 8); if exceeded, return "Path exceeds max_hops".
7. Render path with relation labels and confidence tags.

**No vector search anywhere.** No graph-embedding similarity. Just lexical
IDF match → graph BFS.

For arbitrary "what do X and Y have in common" (not strictly a path), graphify
has no built-in handler. The skill.md walkthrough for ad-hoc questions defers to
`graphify query "<question>"` which does a single multi-seed BFS — see §4.

---

## 4. Tool surface

MCP server registers exactly **10 tools** (`serve.py:434-555`). 7 are graph-query;
3 are PR-impact (specific to graphify's GitHub integration).

Graph-query tools:

| Tool | Inputs | Algorithm | Source |
|------|--------|-----------|--------|
| `query_graph` | question, mode, depth, token_budget, context_filter | IDF score → BFS/DFS depth≤6 → text render with hub-bypass | `serve.py:557-570` → `_query_graph_text:300-325` |
| `get_node` | label | substring match on label/id; return first match's metadata | `serve.py:572-587` |
| `get_neighbors` | label, relation_filter | find best node, list in+out edges | `serve.py:589-615` |
| `get_community` | community_id | dump member labels + source files | `serve.py:617-630` |
| `god_nodes` | top_n | call `analyze.god_nodes` | `serve.py:632-637` |
| `graph_stats` | () | node/edge/community counts + confidence % | `serve.py:639-649` |
| `shortest_path` | source, target, max_hops | IDF score both → undirected BFS | `serve.py:651-704` |

**Total: 7 tools. None are LLM-driven; all are deterministic graph operations
returning pre-shaped text.** Tool descriptions are short (1-2 sentences) — see
e.g. line 487-489: `"Return the most connected nodes - the core abstractions of
the knowledge graph."`

`query_graph` is the workhorse. Its descriptor:

```
"Search the knowledge graph using BFS or DFS. Returns relevant nodes and edges
 as text context."
```

with mode hint: `"bfs=broad context, dfs=trace a specific path"` (line 444).

---

## 5. Anti-thrash mechanisms

Five distinct mechanisms, all in `serve.py`:

### 5a. Pre-baked digests (the real anti-thrash)
The pattern of writing `GRAPH_REPORT.md` + `wiki/index.md` at build time means
the broad-question answer is a single file read. No iterative tool spam needed.

### 5b. Hard token budget with cut marker + narrowing hint
`_subgraph_to_text` (`serve.py:247-297`) measures char_budget = token_budget × 3,
cuts at the last newline before the budget, then appends:

```
"... (truncated — {cut_count} more nodes cut by ~{token_budget}-token budget.
 Narrow with context_filter=['call'] or use get_node for a specific symbol)"
```

The truncation message **suggests the next tool call** to refine — this is a
single-shot escape hatch instead of letting the agent grow context indefinitely.

### 5c. Hub-bypass during BFS/DFS
`_bfs` (`serve.py:191-218`) and `_dfs` (`serve.py:221-244`) refuse to traverse
THROUGH high-degree nodes (p99-degree, floor 50):

```python
degrees_sorted = sorted(degrees)
p99_idx = int(len(degrees_sorted) * 0.99)
hub_threshold = max(50, degrees_sorted[p99_idx])
...
for n in frontier:
    # Don't expand through high-degree hubs (except seeds - a hub that
    # is the starting node should still be explored).
    if n not in seed_set and G.degree(n) >= hub_threshold:
        continue
    for neighbor in G.neighbors(n):
        ...
```

This is the structural equivalent of "don't follow `error.go` into every
module" — a god-node would otherwise flood the BFS frontier.

### 5d. Seed gap-ratio (kills noise seeds)
`_pick_seeds` (`serve.py:110-126`):

```python
top_score = scored[0][0]
seeds = []
for score, nid in scored[:max_k]:
    if seeds and score < top_score * gap_ratio:   # gap_ratio = 0.2
        break
    seeds.append(nid)
return seeds
```

When `FooBarService` scores 1000 (exact match) and `error` scores 1.0 (substring),
only `FooBarService` becomes a seed. The 20% gap threshold prevents
high-frequency noise terms from stealing seed slots.

### 5e. IDF weighting on query terms
`_compute_idf` (`serve.py:59-81`) caches IDF per term on the graph object;
common terms like `error`/`exception` get low weights, rare identifiers like
`FooBarService` get high weights. Combined with the three-tier match precedence
(exact > prefix > substring, `serve.py:97-104`), this concentrates seed selection
on rare informative tokens.

### What graphify does NOT have

**No "you've already explored enough" signal** beyond the cut marker. No
per-session budget cap. No agent-side iteration limit. Graphify trusts the
**output shape** to discourage thrash, not a runtime watchdog.

---

## 6. Concrete file:line citations (consolidated)

| Pattern | File | Function | Lines |
|---------|------|----------|-------|
| Pre-baked report generation | `graphify/report.py` | `generate` | 15–203 |
| Wiki community articles | `graphify/wiki.py` | `_community_article`, `to_wiki` | 37–102, 181–259 |
| Wiki index catalog | `graphify/wiki.py` | `_index_md` | 141–178 |
| Leiden + Louvain clustering | `graphify/cluster.py` | `cluster`, `_partition` | 86–183, 22–77 |
| Cohesion scoring | `graphify/cluster.py` | `cohesion_score` | 204–212 |
| God-node detection | `graphify/analyze.py` | `god_nodes` | 85–104 |
| Surprising connections | `graphify/analyze.py` | `surprising_connections`, `_cross_file_surprises`, `_cross_community_surprises` | 107–399 |
| Suggested questions | `graphify/analyze.py` | `suggest_questions` | 402–524 |
| Graph diff | `graphify/analyze.py` | `graph_diff` | 527–608 |
| MCP server entry | `graphify/serve.py` | `serve`, `list_tools` | 381–555 |
| IDF + seed selection | `graphify/serve.py` | `_compute_idf`, `_score_nodes`, `_pick_seeds` | 59–126 |
| BFS / DFS w/ hub bypass | `graphify/serve.py` | `_bfs`, `_dfs` | 191–244 |
| Token-budgeted rendering | `graphify/serve.py` | `_subgraph_to_text` | 247–297 |
| Query entry + context-filter inference | `graphify/serve.py` | `_query_graph_text`, `_resolve_context_filters`, `_infer_context_filters` | 300–325, 152–171 |
| Shortest-path with ambiguity warn | `graphify/serve.py` | `_tool_shortest_path` | 651–704 |
| CLI `query` | `graphify/__main__.py` | (inline) | 1491–1559 |
| CLI `path` | `graphify/__main__.py` | (inline) | 1579–1660 |
| CLI `explain` | `graphify/__main__.py` | (inline) | 1662–1714 |
| Skill flow with LLM community-labeling | `graphify/skill.md` | Step 5 | 564–602 |

---

## 7. Lift candidates for Aura

Rated 1 (don't bother) → 5 (drop-in win). Aura context: 150 Markdown pages in 5
categories, SQLite, optional Qdrant, Go.

### Lift 1 — Pre-baked `GRAPH_REPORT.md` for the wiki (rating: 5)

What: at every wiki write (or batched periodically), regenerate one
`wiki/_INDEX.md` (or `_GRAPH_REPORT.md`) containing:
- per-category node count + sample titles
- top-N most-linked pages (god pages, ranked by inbound `[[wiki-links]]`)
- per-cluster summary (see Lift 2)
- ambiguity / orphan list (pages with ≤1 wikilink)

Why: Aura's broad questions ("riassunto wiki, top 5 argomenti") today force
the LLM to read pages one at a time precisely because there is no digest. A
single ~2-3 KB file read collapses 14-26 tool calls to 1.

How: port `graphify/report.py:generate` semantics. The graph is the
wikilink graph (already implicit in Aura's Markdown). Run on every
`wiki_writes` commit or on a debounce.

Translatability: Go port is small (~200 LOC); the algorithm is just counts +
sorts + a string template. Persist as a regular wiki page so it shows up in
existing search.

### Lift 2 — Cluster wiki pages, write one digest per cluster (rating: 4)

What: build a NetworkX-equivalent graph from Aura's wikilinks, run Leiden
(or Louvain via a Go port / Python sidecar / graspologic), label each cluster
with an LLM **once at build time**, materialize `wiki/_CLUSTER_<name>.md` per
cluster containing key concepts + cross-cluster links.

Why: directly answers "top 5 argomenti" without LLM-time computation. Also
gives "X vs Y" a structural anchor: are they in the same cluster? If not,
what's the shortest path between them?

How: port `graphify/cluster.py:cluster` + `graphify/wiki.py:_community_article`.
Aura already has a wiki-write pipeline at `internal/wiki/store_writes.go`; the
cluster compute can hook into the existing rebuild path. LLM labeling step
already familiar (Aura uses `temperature=0` deterministic prompts).

Risk: Leiden is in Python (`graspologic`); Louvain has Go ports
(`github.com/spaolacci/murmur3`, etc.) but quality is worse. A Python sidecar
container (Aura already runs sidecars) might be cleanest.

### Lift 3 — Two-level catalog in system prompt (rating: 5)

What: today's TOC is `slug + title + 120 chars`. Replace with:
- **top**: cluster digests (5-10 entries, ~50 chars each)
- **middle**: god pages (top 10 by inbound links, label only)
- **bottom**: full page slug+title list (compact, one per line)

Why: gives the LLM the structural overview *before* it considers reading any
page. Mirrors `graphify/wiki.py:_index_md` (lines 141–178) which is exactly
two sections of compact lists.

How: regenerate inline at every system-prompt build. Cost is one
`internal/wiki/toc.go` rewrite (file already exists in current branch per git
status).

### Lift 4 — Token-budgeted output + "narrow with" hint (rating: 4)

What: every tool that returns wiki content should cut at a budget and append
the same kind of narrowing hint graphify uses:

> `... (truncated — N more nodes cut by ~2000-token budget. Narrow with
>  category=… or use read_page for a specific slug)`

Why: the cut **+ the suggested next action** is what stops the LLM from
calling the same tool with bigger N. Today Aura's tool outputs cut silently
or not at all, encouraging retries.

How: small change to `internal/agent/tools/registry/files_*.go` and wiki/read.

### Lift 5 — Hub-bypass during wikilink BFS (rating: 3)

What: when Aura does a multi-hop wikilink walk, refuse to traverse through
pages with degree > p99. Today no such walk exists, so this lift is
contingent on building one.

Why: Aura has high-degree "concept" wiki pages (`Andrea`, `Aura`, the user)
that would flood any BFS. graphify's `serve.py:191-218` shows the exact
pattern.

How: would only matter once Aura builds a `wiki_graph_query` tool. Park for
the future.

### Lift 6 — Suggested questions section (rating: 3)

What: at the bottom of the index/report, list 5-7 questions the wiki is
"uniquely positioned to answer" — ambiguous edges, bridge pages, isolated
pages, low-cohesion clusters. See `graphify/analyze.py:suggest_questions`
(lines 402-524).

Why: cute pattern for onboarding new users / Aura herself; gives the
language model affordances to suggest its own exploration.

How: medium effort (~300 LOC Go). Lower priority than Lifts 1-3.

### NOT recommended

- **Vector search at query time**: graphify does none of this; pure lexical
  IDF + structure is enough for ~3K-node graphs. Aura already has Qdrant if
  needed but should not lean on it for broad questions — those are
  pre-baked, not retrieved.
- **Graph embeddings**: graphify uses none.
- **Live community detection**: graphify computes Leiden once at build, not
  per query. Same model for Aura — never re-cluster at query time.

---

## 8. Honest gaps in graphify's coverage

For completeness — places graphify has no clean answer to the question:

- **"Are there docs on crittografia?" (existence query)**: graphify has no
  category-existence shortcut. `query_graph "crittografia"` works but returns
  a BFS subgraph, not a clean "yes, here are N pages" answer. The closest
  thing is `god_nodes` + manual inspection of `GRAPH_REPORT.md`. Aura would
  need its own dedicated tool here.

- **Cluster-of-clusters / hierarchy**: graphify's clustering is flat (with a
  split-oversized-community pass at `cluster.py:162-179`, but still flat).
  No "topic of topics" abstraction. For 150 wiki pages Aura is probably fine
  flat, but worth flagging.

- **Cross-document semantic similarity at scale**: graphify has
  `semantically_similar_to` edges (treated as a relation type, see
  `analyze.py:235-237`) but these come from the LLM extractor, not from
  graph runtime. Aura's existing Qdrant cosine could substitute.

- **Multi-hop "common ancestor" queries beyond pairwise**: `shortest_path` is
  pairwise only. For "what do X, Y, Z have in common" graphify falls back
  to user inspection of `GRAPH_REPORT.md`.

---

## 9. Translation cheat-sheet (graphify → Aura)

| graphify concept | Aura equivalent |
|------------------|-----------------|
| `nodes` | wiki pages |
| `edges` | `[[wiki-links]]` + `related` frontmatter |
| `file_type` | `category` frontmatter (entity/concept/source/project/personale) |
| `community` | cluster of pages by link density |
| `god_node` | wiki page with highest inbound-link count |
| Leiden | (port to Go OR Python sidecar) |
| `GRAPH_REPORT.md` | `wiki/_INDEX.md` or system-prompt TOC |
| `wiki/<Community>.md` | `wiki/_CLUSTER_<name>.md` |
| `query_graph` (MCP) | new `wiki_graph_query` tool — optional |
| `shortest_path` | new `wiki_shortest_path` tool — optional |
| `graph_stats` | already exists conceptually in dashboard |

---

## 10. Concrete first deliverable for Aura (smallest viable)

If only one lift ships: **Lift 1 (pre-baked digest) + Lift 3 (two-level TOC)**
together. They share infrastructure (graph compute + Markdown template) and
deliver the most direct kill of the 14-26 tool-call thrash on the three
example broad questions:

- "fammi un riassunto wiki, top 5 argomenti" → answer is the digest body
- "ho documenti su crittografia?" → answer is grep against the digest /
  cluster list
- "cosa hanno in comune Pirandello e Calvino?" → answer is "same cluster:
  Letteratura italiana 900" OR "different clusters, shortest link path
  through `Modernismo`"

All three become 1-tool-call (or 0-tool-call when the digest is already in
the system prompt) instead of N-page-read fan-outs.
