# openhuman vs graphify — broad-query handling

Contrast study. Companion to `graphify-broad-query.md`. All openhuman references
are to `D:/tmp/openhuman/src/openhuman/` (Rust). All graphify references are to
`D:/tmp/graphify/graphify/` (Python).

The host project is Aura — Go binary, SQLite + Qdrant + Markdown wiki (~150
pages). Today's broad queries thrash:

- "riassunto wiki, top 5 argomenti" — 26 tool calls, 60s, 491k tokens
- "Pirandello vs Calvino" — 18 tool calls, 48s
- "ho documenti su crittografia?" — 14 tool calls, 26s

---

## TL;DR (15 lines)

1. **openhuman = INCREMENTAL HIERARCHICAL DIGEST.** Three concentric tree kinds
   (`source`, `topic`, `global`) materialised at *ingest time* via a SQLite-
   backed job queue; the global tree's L0 is one node/day, L1 week, L2 month,
   L3 year (`tree_global/mod.rs:38-46`). Broad query = read one summary.
2. **graphify = ONE-SHOT REPORT + WIKI.** Single `GRAPH_REPORT.md` + per-cluster
   `<Community>.md` rebuilt offline; broad query = static Markdown read.
3. **MEMORY.md is a tiny session-frozen file (≤2000 chars)** distilled by an
   archivist *after each segment closes* (`agent/prompts/types.rs:26` cap;
   `agent/harness/archivist.rs:497-509` ingest hook). It is NOT the broad-query
   answer — it is the user-preference layer.
4. **The actual broad-query mechanism is the 7-day global digest auto-injected
   into the user message on session start** (`agent/tree_loader.rs:46-113`),
   refreshed every 30 minutes (`REFRESH_INTERVAL`). 3 hits × 500 chars cap.
5. **Tool surface for memory is ONE consolidated tool with 7 modes**
   (`tools/impl/memory/tree/mod.rs:39-163`): `search_entities`, `query_topic`,
   `query_source`, `query_global`, `drill_down`, `fetch_leaves`,
   `ingest_document`. No vector-only tool — every primitive is tree-aware.
6. **"X vs Y" path = `search_entities` → `query_topic` for each** (no graph
   shortest-path; openhuman has none). Result reranked by cosine when a `query`
   field is set. No graph BFS at all.
7. **Anti-thrash mechanisms**: (a) pre-injected 7-day digest, (b) singular
   consolidated tool (less router thrash), (c) hard `max_iterations=15` on the
   orchestrator (`agents/orchestrator/agent.toml:5`), (d) prompt-level
   "direct-first" rule, (e) per-hit content cap 500 chars in the digest loader,
   (f) `BTreeMap`-dedup inside `query_topic` (`retrieval/topic.rs:101-130`).
8. **Where openhuman beats graphify**: time-axis recap (1-day / 7-day /
   month / year resolution all answered from pre-baked nodes); entity-scoped
   "what do you know about X" via topic trees that lazily spawn on hotness.
9. **Where graphify beats openhuman**: structural graph signals — god nodes,
   cross-cluster surprising connections, ambiguous-edge audit, suggested
   questions, shortest path. openhuman has no graph layer at all.
10. **Convergence — the pre-baked digest pattern is identical conceptually.**
    Both materialise the broad-query answer offline; the question for Aura is
    only WHICH dimension to pre-bake (time vs link-cluster).
11. **Divergence — the ingest cost.** openhuman pays per write (job queue +
    seal-on-budget + LLM summarise + embed); graphify pays only at rebuild
    (one-shot Leiden + LLM label). Aura's wiki has slow churn → graphify-style
    rebuild is cheaper. openhuman-style continuous seal pays off only when
    the source firehoses (chat, email).
12. **Lift candidate #1 (hybrid)**: pre-bake `wiki/_INDEX.md` like graphify
    (clusters + god pages + samples) AND auto-inject the top into the system
    prompt like openhuman's `tree_loader`. Same content, different delivery —
    both kills thrash.
13. **Lift candidate #2 (openhuman)**: one consolidated `wiki_query` tool with
    `mode` enum (`broad_digest` / `cluster` / `page` / `compare`) instead of
    today's 5+ search variants. Mirrors openhuman's `memory_tree.mode` UX.
14. **Lift candidate #3 (graphify)**: per-cluster digest pages with explicit
    "this cluster connects to: X, Y, Z" cross-links. openhuman's topic trees
    lack cross-topic edges — graphify's `_community_article` does this well.
15. **Do NOT lift**: openhuman's job queue + cascade-seal — overengineered for
    ~150 slow-churn pages; would burn embedding budget for no win.

---

## Section A — openhuman analysis

### A.1. MEMORY.md / PROFILE.md / KV-cache contract

**`MEMORY.md` is small and session-frozen.**

- Hard cap **`USER_FILE_MAX_CHARS = 2000`** in `agent/prompts/types.rs:26`. Any
  body beyond that gets truncated with an ellipsis marker.
- Injection happens in `agent/prompts/mod.rs:482-510` (`UserFilesSection`):

```rust
if ctx.include_memory_md {
    if let Some(snap) = &ctx.curated_snapshot {
        inject_snapshot_content(&mut out, "MEMORY.md", &snap.memory, USER_FILE_MAX_CHARS);
        inject_snapshot_content(&mut out, "USER.md", &snap.user, USER_FILE_MAX_CHARS);
    } else {
        inject_workspace_file_capped(&mut out, ctx.workspace_dir, "MEMORY.md", USER_FILE_MAX_CHARS);
    }
}
```

- KV-cache contract documented at `agent/harness/definition.rs:79-92`:

> "once MEMORY.md is rendered into a session's system prompt the bytes are
> frozen for that session's lifetime. Archivist writes that land mid-session
> do not retroactively update the in-flight prompt — they are picked up on
> the next session."

**Distillation = per-segment Archivist hook.**

`agent/harness/archivist.rs:312-509` (`on_segment_closed`). On each closed
conversation segment (boundary detection in `memory/store/segments.rs`):

1. LLM-summarise the segment (`summarize_entries`, line 632, falls back to
   heuristic bookend if LLM unavailable).
2. Embed the summary (`segment_embedding_upsert`, line 384).
3. Heuristic event extraction → fact / preference rows that update
   `profile_*` tables (`events::extract_events_heuristic`, line 424).
4. Phase 2 (line 497-508): pipe RAW PROSE of the segment into the memory tree
   under `source_id="conversations:agent"`. Note: the LLM recap is NOT fed back
   — "evidence vs interpretation" policy (line 845-851).

The `update_memory_md` tool (`tools/impl/filesystem/update_memory_md.rs:29-70`)
exposes append + replace_section actions; LLM-driven, only the Archivist agent
holds it (`agent/agents/archivist/agent.toml:18`).

**Archivist agent definition** (`archivist/agent.toml`):

```toml
delegate_name = "archive_session"
when_to_use = "Background librarian — extracts lessons from a completed session,
updates MEMORY.md, and indexes to FTS5. Runs cheap and slow."
temperature = 0.4
max_iterations = 3
background = true
omit_memory_context = true  # archivist itself doesn't read MEMORY.md
[tools]
named = ["update_memory_md", "insert_sql_record", "memory_store"]
```

The archivist runs **AFTER** the user-facing turn completes (PostTurnHook). It
does NOT block reply latency.

### A.2. Broad-query path

**Three-layer answer:**

**Layer 1 — auto-injected 7-day digest** (`agent/tree_loader.rs:46-113`):

```rust
const DEFAULT_WINDOW_DAYS: u32 = 7;
const REFRESH_INTERVAL: Duration = Duration::from_secs(30 * 60);
const MAX_CONTENT_CHARS: usize = 500;
const MAX_HITS: usize = 3;
const HEADER: &str = "[Memory tree — last 7 days]\n";

pub async fn load(config: &Config) -> anyhow::Result<String> {
    let resp = match query_global(config, DEFAULT_WINDOW_DAYS).await { ... };
    if resp.hits.is_empty() { return Ok(String::new()); }
    let mut out = String::with_capacity(...);
    out.push_str(HEADER);
    for hit in resp.hits.iter().take(MAX_HITS) {
        let snippet = if hit.content.chars().count() > MAX_CONTENT_CHARS {
            truncate_with_ellipsis(&hit.content, MAX_CONTENT_CHARS)
        } else { hit.content.clone() };
        out.push_str(&format!("- [{}] {}\n", hit.tree_kind.as_str(), snippet.replace('\n', " ")));
    }
    Ok(out)
}
```

Rides on the **user message**, NOT the system prompt — keeps the KV-cache
prefix stable. So "what's been going on?" gets answered from the prefetch
block without a tool call.

**Layer 2 — global recap on demand** (`memory/tree/tree_global/recap.rs:49-81`).
Maps window duration → level via `pick_level` (line 85-98):

```rust
pub fn pick_level(window: Duration) -> u32 {
    if window < Duration::days(2)   { 0 }  // daily
    else if window < Duration::days(14)  { 1 }  // weekly
    else if window < Duration::days(60)  { 2 }  // monthly
    else                                  { 3 }  // yearly
}
```

Walks down from the picked level to 0 looking for material; falls back to lower
levels when higher hasn't sealed yet. Returns one folded recap covering the
window — exposed as `query_global` retrieval surface
(`memory/tree/retrieval/global.rs:24-48`).

**Layer 3 — drill-down** (`memory/tree/retrieval/drill_down.rs:41-92`). One-step
walk of `child_ids` from any summary node. Optional cosine rerank against a
query embedding when the LLM passes one. `max_depth` capped at 3.

**End-of-day digest construction** (`memory/tree/tree_global/digest.rs:73-279`):

- Runs once per calendar day, kicked by scheduler (`memory/tree/jobs/mod.rs:20`:
  *"scheduler (1 task) → daily wall-clock tick: enqueues digest_daily(yesterday)
  + flush_stale(today)"*).
- For every active source tree, picks the most-relevant summary for that day
  (`pick_source_contribution`, line 335-406). Priority: L1+ summary
  intersecting the day → tree root → skip.
- Folds the per-source contributions through the `Summariser` trait (real LLM
  via `LlmSummariser` or deterministic fallback `InertSummariser`).
- Embeds the output, stores as `mem_tree_summaries` row with
  `tree_kind='global'`, `level=0`, `time_range_start/end` covering the day.
- Cascade-seal upward if thresholds crossed: 7 daily → 1 weekly,
  4 weekly → 1 monthly, 12 monthly → 1 yearly (constants in
  `tree_global/mod.rs:38-46`).

**Cost per write — non-trivial**: per ingested chunk, the worker pool runs
`extract_chunk → append_buffer → seal → topic_route` and on segment close the
archivist runs LLM-recap + embed. Plus one daily digest LLM call.

### A.3. Multi-hop / comparison ("X vs Y")

**openhuman has NO graph shortest-path.** No NetworkX, no BFS over relations.

The "X vs Y" pattern is:

1. `memory_tree.mode = "search_entities"` with `query = "Pirandello"` →
   `entity_id = "topic:pirandello"` (`memory/tree/retrieval/search.rs:34-83`).
   SQL LIKE on `mem_tree_entity_index.entity_id OR surface`, grouped by
   canonical id, ordered by `mention_count DESC, last_seen_ms DESC`.
2. Same again for `"Calvino"` → `entity_id = "topic:calvino"`.
3. Two `query_topic` calls — one per entity_id — return the per-entity topic
   tree's root summary + every cross-tree mention from `mem_tree_entity_index`
   (`memory/tree/retrieval/topic.rs:45-166`). Optionally cosine-reranked by
   a free-text `query`.

The LLM then composes the comparison from the two retrievals in its own
context. There is no "find the intersection" primitive. Implicit overlap is
visible only because topic trees fan out per entity and share the same
chunk pool — but you have to read both and compose.

Honest gap: openhuman has no `compare(X, Y)` tool. graphify's `shortest_path`
is more direct for "what connects A to B".

### A.4. Tool surface for memory

Consolidated into ONE LLM-facing tool: `memory_tree`
(`tools/impl/memory/tree/mod.rs:35-163`). Schema:

```json
{
  "mode": "search_entities | query_topic | query_source | query_global |
           drill_down | fetch_leaves | ingest_document",
  "query": "...",            // search_entities, query_topic, query_source
  "kinds": [...],            // search_entities filter
  "entity_id": "...",        // query_topic
  "source_kind": "...",      // query_source (chat / email / document)
  "time_window_days": N,     // query_source, query_topic, query_global alias
  "window_days": N,          // query_global native
  "node_id": "...",          // drill_down
  "max_depth": N,            // drill_down (1, cap 3)
  "chunk_ids": [...],        // fetch_leaves (cap 20)
  "title": "...",            // ingest_document
  "body": "...",             // ingest_document
  "source_id": "...",        // ingest_document / query_source
  "limit": N
}
```

`description()` for the LLM: short single sentence per mode. Returns a uniform
`RetrievalHit` struct (`memory/tree/retrieval/types.rs`) with `node_id`,
`node_kind`, `tree_id`, `tree_kind`, `level`, `content`, `entities`, `topics`,
`time_range_start/end`, `score`, `child_ids`, `source_ref`. Drill-down works
the same way regardless of which mode returned the parent hit.

The orchestrator also has 4 other memory tools (`agents/orchestrator/agent.toml:101-114`):
`query_memory`, `memory_store`, `memory_forget` (legacy KV memory), and
`whatsapp_data_*` (WhatsApp-specific). Plus `current_time`, `cron_*`,
`spawn_*`, `update_*`, `todowrite`, `plan_exit`, etc. for non-memory work.

### A.5. Anti-thrash mechanisms

1. **Pre-loaded digest in user message** — broad questions answered without a
   tool call (`tree_loader.rs:46-113`).
2. **`max_iterations = 15`** on orchestrator (`agents/orchestrator/agent.toml:5`)
   — hard cap regardless of how lost the agent gets.
3. **Direct-first prompt rule** (`agents/orchestrator/prompt.md:13-46`): step
   1 = "can I answer without tools?", step 2 = "live integration?", step 3 =
   "direct tool?", step 4 = "specialised delegate?". `memory_tree` is step 3,
   not step 1.
4. **Consolidated single tool** — fewer routing decisions for the LLM than 6
   separate `memory_query_*` tools would force.
5. **Per-hit 500-char cap** in the eager prefetch (`tree_loader.rs:40`).
6. **30-minute refresh interval** (`REFRESH_INTERVAL`) — long sessions don't
   re-fetch the digest every turn.
7. **`BTreeMap` dedup** in `query_topic` (`retrieval/topic.rs:101-130`) so
   the same node never appears twice when both the entity index AND the topic
   tree root surface it.
8. **`max_depth ≤ 3`** on drill-down (`tools/impl/memory/tree/mod.rs:99`).
9. **`fetch_leaves` cap of 20 chunks** (`retrieval/fetch.rs`, README §retrieval).
10. **Empty global digest returns empty string**, not an error — so an early-life
    workspace doesn't poison every turn (`tree_loader.rs:87-90`).

### A.6. Where the LLM is invoked (cost model)

- **At ingest time**: `extract_chunk` LLM pass for entity extraction
  (job queue), `append_buffer` no LLM, `seal` triggers `Summariser.summarise`
  when the L0 buffer hits `INPUT_TOKEN_BUDGET = 50_000` tokens
  (`tree_source/types.rs:188`).
- **At cascade**: each higher-level seal fires when `SUMMARY_FANOUT = 10`
  children accumulate (`tree_source/types.rs:205`) → another LLM summarise.
- **At end of day**: one cross-source LLM summarise for the daily digest
  (`tree_global/digest.rs:140-145`).
- **At weekly / monthly / yearly seal**: one LLM summarise each
  (`tree_global/seal.rs:append_daily_and_cascade`).
- **Per segment**: archivist's `LlmSummariser` produces a recap
  (`harness/archivist.rs:632-724`).
- **At query time**: zero LLM. All retrieval is SQLite + cosine. The LLM only
  consumes the retrieved hits in its own turn.

---

## Section B — Side-by-side comparison

| Dimension | openhuman | graphify |
|-----------|-----------|----------|
| **Broad-query answer source** | Pre-built `global` tree summary at L0-L3 (time-axis) + auto-injected 7-day digest in user message | Pre-built `GRAPH_REPORT.md` + `wiki/<Community>.md` (link-cluster axis), read as static Markdown |
| **When is it built?** | Continuously: on every ingest (extract / seal); end-of-day digest fires once per UTC day on scheduler tick | Offline batch: `graphify build`. LLM labels communities once. No background work after build. |
| **Granularity** | Time (day / week / month / year) + entity (per-topic lazy trees) | Structural (per-community + per-god-node + per-file) |
| **Multi-hop / X vs Y** | Two `query_topic` calls then LLM composes. No graph primitive. | `shortest_path` (NetworkX BFS on undirected view) with ambiguity warning when top-2 scores are within 10% |
| **Existence query** ("any docs on X?") | `search_entities` SQL LIKE on `mem_tree_entity_index` | Honest gap — read `GRAPH_REPORT.md` + grep |
| **Tool surface** | 1 `memory_tree` tool with 7 modes (5 query, 1 drill, 1 write) | 7 MCP tools (`query_graph`, `get_node`, `get_neighbors`, `get_community`, `god_nodes`, `graph_stats`, `shortest_path`) + 3 PR tools |
| **Anti-thrash** | Auto-inject digest + max_iter=15 + direct-first prompt + content cap | Pre-baked digest + IDF seed gap + hub-bypass BFS + token-budget cut-marker + narrow-hint |
| **MEMORY.md role** | 2000-char session-frozen user-preference file written by archivist after segment close. Does NOT answer broad queries. | No equivalent. |
| **Vector search** | Used inside `query_topic` / `drill_down` rerank (optional, when LLM passes `query=`). Cosine on stored embeddings. | None. Pure lexical IDF + structure. |
| **Graph layer** | None | NetworkX graph + Leiden / Louvain clustering |
| **God node / hub detection** | None | `analyze.god_nodes` (top-N by degree); BFS bypasses hubs at p99 |
| **Suggested questions** | None | `analyze.suggest_questions` (lines 402-524) |
| **Cluster cross-links** | Topic trees are siloed by entity_id | `_community_article` lists which other communities this one connects to most |
| **Cost per write** | Heavy: extract LLM + seal LLM + embed + daily digest LLM | Zero. All cost is at `build` time. |
| **Cost per query** | Zero LLM (SQLite + cosine + return) | Zero LLM (read static Markdown) |
| **Best fit corpus** | High-churn streams (chat, email, segments) where "what happened recently" dominates | Slow-churn corpus (codebase, knowledge base) where "what topics exist" dominates |
| **Honest gap** | Stale tree when ingest is slow; no graph view; no "uniquely positioned to answer X" surfacer | No time-axis; can't answer "what happened last week" without rebuild; flat clusters (no hierarchy) |

### Convergence points

1. **Both pre-bake the broad-query answer.** Neither does live BFS+rerank at
   query time. The cost is moved to write time (openhuman) or to a rebuild
   step (graphify), and the read is cheap.
2. **Both use ONE tool / file as the primary entry.** openhuman:
   `memory_tree.mode`. graphify: `query_graph` (with `GRAPH_REPORT.md` as
   the zero-tool path).
3. **Both keep tool outputs small via explicit caps.** openhuman: 500 chars/hit
   × 3 hits. graphify: token budget × cut marker.
4. **Both cap iteration / depth.** openhuman: `max_iterations=15`,
   `max_depth=3`. graphify: `max_hops=8`, BFS depth ≤ 6.

### Divergence points

1. **Axis of pre-bake.** openhuman = time (day/week/month/year + per-entity).
   graphify = link-cluster (community + god-node).
2. **Maintenance model.** openhuman = continuous incremental seals (write-time
   amortised LLM calls). graphify = batch rebuild (offline LLM labelling
   step).
3. **Comparison handling.** openhuman = retrieve both, compose in LLM.
   graphify = explicit `shortest_path`.
4. **MEMORY.md.** openhuman has a tiny session-frozen user-preference file.
   graphify has nothing equivalent; the wiki is the durable surface.

---

## Section C — Lift recommendations for Aura (Q1 / Q3 / Q4)

**Aura today**: ~150 Markdown wiki pages with frontmatter (`category`, `tags`,
`related`, `sources`), in-process Qdrant for vector search, SQLite for state,
wikilink-aware writes. Slow churn: ~1-10 pages/day, not a chat firehose.

### Q1 — "fammi un riassunto wiki, top 5 argomenti"

**Best lift: graphify (rebuild-time digest) + openhuman (auto-inject)**.
Rating: **5 (drop-in win)**.

What:

- Build `wiki/_INDEX.md` at every wiki write (debounced, ≤5s) containing
  (a) per-cluster name + member count + 1-line summary, (b) god-page list
  (top 10 by inbound `[[wiki-links]]`), (c) "top 5 topics" derived from the
  cluster ranking. Mirrors `graphify/wiki.py:_index_md` + `report.py:generate`.
- Auto-inject the index head (or a 500-char digest of it) into the user
  message on session start, like
  `agent/tree_loader.rs:46-113`. Refresh every N minutes only on long sessions.

Why over alternatives:

- openhuman's time-axis trees are wrong for Aura's slow-churn wiki — the cost
  of continuous seals would be wasted (most days have 0 ingest).
- graphify's link-cluster axis maps directly to Aura's wikilink graph.
- The auto-inject pattern from openhuman closes the loop: even with a
  digest on disk, today's broad questions still trigger 14-26 tool calls
  because the LLM doesn't know the digest EXISTS. Putting the digest head
  into the user message means the LLM never needs to search.

How (Go):

- Port `graphify/cluster.py:cluster` semantics (Louvain or Python sidecar
  with Leiden). Aura already has a wiki-write hook in `internal/wiki/store_writes.go`.
- Port `wiki.py:_index_md` template — pure string formatting, ~100 LOC Go.
- Add `internal/wiki/digest_loader.go` mirroring `tree_loader.rs` — call from
  `internal/conversation/system_prompt.go` or the user-message build path.

Concrete expected effect on Q1: today 26 tool calls / 60s → 0-1 tool calls.
The digest body IS the answer.

### Q3 — "cosa hanno in comune Pirandello e Calvino?"

**Best lift: graphify (`shortest_path`)**. Rating: **4 (high value, medium effort)**.

What: implement a `wiki_relate` tool that does:

1. Lookup the two page slugs (or substring-match titles).
2. Run NetworkX-style BFS on the undirected `[[wiki-link]]` graph; cap
   `max_hops = 8`.
3. Return the path as a list of `(slug, title, [[link]])` triples.
4. Attach the same ambiguity warning graphify uses when top-1 vs top-2 score
   delta < 10% (`serve.py:667-675`).

Why over openhuman: openhuman has no path primitive. It would need two
`query_topic` calls + LLM composition, which is exactly the thrash pattern
Aura is fighting (more tool calls, more context, more risk of hallucination).
graphify's path is one tool call returning a small structured result.

Why over native vector "find similar to both": cosine on (Pirandello +
Calvino) returns pages close to the centroid — usually noise. The wikilink
path is structurally meaningful ("both are linked through `Modernismo`").

How (Go):

- Build the in-memory `[[wiki-link]]` graph from existing parsed frontmatter
  + body links (cache in SQLite; invalidate on wiki write).
- Add `internal/wiki/graph.go` with `ShortestPath(a, b string, maxHops int)`.
- Register as `wiki_relate` tool. Wire into the tool registry next to
  `wiki_search`.

When **openhuman's `query_topic` approach** would be preferable: when the user
asks about *open-ended overlap* not pairwise — e.g. "what topics involve
both letteratura italiana AND ottocento?" — that's a multi-entity intersection,
which a graph path can't express cleanly. For pairwise, graphify wins.

### Q4 — "ho documenti su crittografia? se sì dimmi i titoli"

**Best lift: openhuman (`search_entities`-style index)**. Rating: **5 (small,
drop-in)**.

What: add a `wiki_exists` tool that:

1. Does `LIKE` (or FTS5) against indexed page titles + tags + frontmatter
   `related[]` + first 200 chars of body.
2. Returns `{count, titles[]}` — explicit count, no body content.
3. Optional: if `count == 0`, fall back to category-name match
   ("crittografia" → category `sicurezza`).

Why over graphify: graphify's `god_nodes` + `GRAPH_REPORT.md` does not
expose "does X exist?" cleanly — the only path is grep the report or run
`query_graph` and parse a BFS subgraph. openhuman's `search_entities` is
exactly this: a fast SQLite LIKE on a pre-built index, grouped and counted
(`memory/tree/retrieval/search.rs:34-83`).

Why this kills today's thrash: Q4 today is 14 tool calls because Aura's
search returns chunks/pages, the LLM scans each for relevance, then synthesises.
A `count + titles` shape lets the LLM answer in ONE call:

> "yes, 4: «Crittografia simmetrica», «Curve ellittiche», «PGP basics»,
>  «Quantum-resistant»."

How (Go):

- Aura already has FTS5 on wiki pages. Add a `wiki_exists(query, limit)` tool
  whose response shape is intentionally tiny — no body content, only
  `[]{slug, title}` + `total`.
- Description for the LLM: *"Use when the user asks 'do I have docs on X?'
  or 'list pages about Y'. Returns titles only, never bodies."*

When **graphify's `god_nodes` would be preferable**: when the user asks
"what's the most-connected page about X?" — that's degree-centrality, which
needs the wikilink graph. For pure existence + titles, openhuman wins.

---

## Section D — Lift summary, ranked

| # | Source | What | Aura translatability | Kills query |
|---|--------|------|---------------------|-------------|
| 1 | **hybrid** | `wiki/_INDEX.md` pre-baked + auto-injected into system or user message | 5 | Q1 |
| 2 | **graphify** | `wiki_relate` shortest-path tool | 4 | Q3 |
| 3 | **openhuman** | `wiki_exists` (titles-only count + list) | 5 | Q4 |
| 4 | **openhuman** | Consolidate Aura's wiki search tools into one `wiki_query.mode` tool (mirrors `memory_tree.mode`) | 4 | reduces LLM router thrash on all queries |
| 5 | **graphify** | Per-cluster digest pages (`wiki/_CLUSTER_<name>.md`) with cross-cluster link counts | 4 | Q1 secondary |
| 6 | **openhuman** | KV-cache-stable digest injection (user message, not system prompt; refresh every 30 min) | 5 | Q1 |
| 7 | **graphify** | Token-budgeted cut marker + "narrow with X" hint in every tool output | 4 | indirect — slows agent retries |
| 8 | **openhuman** | Hard `max_iterations` cap (Aura's `AURA_AGENT_LOOP_MAX_STEPS` already exists at 5 — verify it's enforced on all paths) | 5 | indirect — guarantee against runaway |

---

## Section E — Honest gaps in openhuman's coverage

1. **No graph view of the corpus.** openhuman cannot answer "what's the most
   bridging concept between my email and chat history?" — it can only ask
   per-entity / per-source. graphify's god-node + cross-community surprises
   would surface that for free.
2. **No `compare(X, Y)` primitive.** openhuman makes the LLM compose comparison
   from two retrievals; this re-introduces the very tool-call inflation
   graphify's `shortest_path` avoids.
3. **No "suggested questions"** like `analyze.suggest_questions`. openhuman
   doesn't surface "things you have material on but never asked about".
4. **Tree staleness when ingest is slow.** openhuman's seal triggers are
   token-based (50k for L0→L1) or count-based (10 for L1+). A slow source can
   sit unsealed forever — only the time-based `flush_stale_buffers` saves it
   (`memory/tree/tree_source/flush.rs`). For Aura's slow-churn wiki the
   token gate would basically never fire.
5. **MEMORY.md is bounded at 2000 chars.** Beyond that, content is truncated.
   This is fine for user-preference distillation but not for "wiki summary"
   purposes. For Aura's "summarise my wiki" use case, MEMORY.md is the wrong
   primitive — the global tree digest (Layer 1) is the right one.

---

## Section F — Translation cheat-sheet (openhuman → Aura)

| openhuman concept | Aura equivalent |
|-------------------|-----------------|
| `mem_tree_chunks` | wiki page body (already on disk as Markdown) |
| `mem_tree_summaries` (L1+) | wiki/_CLUSTER_<name>.md (NEW, lift from graphify) |
| `mem_tree_entity_index` | extant FTS5 + wiki tags + `related` frontmatter |
| `tree_source` (per source) | one wiki/_CLUSTER per link community |
| `tree_topic` (per entity) | wiki page already IS this — the page slug *is* the topic id |
| `tree_global` | wiki/_INDEX.md (NEW, lift #1) |
| `tree_loader.rs` eager prefetch | new `internal/wiki/digest_loader.go` (NEW, lift #1) |
| `memory_tree.mode` consolidated tool | new `wiki_query.mode` (NEW, lift #4) |
| `search_entities` SQL LIKE | new `wiki_exists` tool (NEW, lift #3) |
| `query_global` recap | reads `wiki/_INDEX.md` (no separate primitive needed) |
| `drill_down` | already covered by `read_wiki_page` |
| `update_memory_md` | already exists conceptually as Aura's `memory_store` |
| Archivist post-turn hook | NOT NEEDED — Aura's wiki writes are the durable surface; no segments to seal |

The big simplification: **Aura doesn't need openhuman's job queue + cascade-seal
machinery**, because Aura's writes are already LLM-driven Markdown pages.
The wiki IS the L1+ summary tree. Lift the *retrieval shape* and the
*auto-inject* pattern, skip the ingest pipeline.

---

## Section G — Concrete first deliverable

Two stories, sequenced:

**Story 1 (kills Q1 + Q4)**: pre-bake `wiki/_INDEX.md` on every wiki write
(graphify-shaped content) + auto-inject the top 1000 chars into the user
message on session start with 30-min refresh (openhuman-shaped delivery).
Add `wiki_exists` tool. Effort: ~400 LOC Go + 1 sidecar Python (or pure-Go
Louvain) for cluster compute.

**Story 2 (kills Q3)**: build the wikilink graph in SQLite, add `wiki_relate`
tool with NetworkX-equivalent BFS. Effort: ~250 LOC Go.

After both ship, all three pathological queries become 0-1 tool calls:

- Q1 → 0 tool calls (digest in user message)
- Q3 → 1 tool call (`wiki_relate`)
- Q4 → 1 tool call (`wiki_exists`)

That's a 20-26× reduction in tool-call thrash on the three benchmarked queries.
