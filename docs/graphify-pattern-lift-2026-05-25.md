# Graphify → Aura pattern lift audit (2026-05-25)

Source: `D:/tmp/graphify` (safishamsi/graphify, MIT).
Scope: knowledge-graph patterns only. Skipped AST extraction, language-family
heuristics, JSON key noise, code-graph-only file/source ranking, MCP stdio
plumbing, NetworkX/Leiden infra unless the analog is clean for Go.

Aura side audited before writing this doc:
- `internal/agent/tools/registry/search.go` (unified search action enum)
- `internal/agent/tools/registry/wiki_godnodes.go` (degree-based hubs)
- `internal/agent/tools/registry/wiki_subgraph.go` (PPR + hub-aware BFS + RRF seeding)
- `internal/agent/tools/registry/wiki_path.go` (BFS shortest path)
- `internal/agent/tools/registry/memory_search.go` + format helper
- `internal/storage/search/search.go` (RRF mergeHybridResults)
- `internal/wiki/godnodes.go`, `internal/wiki/graph_index.go` (NodeMeta + P99Degree + NeighborsHubAware + ShortestPath)

So Aura ALREADY has: god_nodes by degree, shortest path BFS, PPR-seeded
hub-aware BFS, RRF hybrid fusion, token-budgeted subgraph rendering,
edge confidence labels (EXTRACTED / related: tag), seed gap-ratio cutoff
(0.2), p99 hub-skip degree, half-life recency decay, snippet rendering,
follow-up handles. None of these are lifts — they are already present.

Sort: Impact desc, Effort asc. Twelve patterns. Six are HIGH-impact and four
are TRIVIAL/SMALL — those are the ones worth picking up next.

---

## 1. Surprise score for cross-zone edges (HIGH / SMALL)

- **Source**: `D:/tmp/graphify/graphify/analyze.py:177-248` (`_surprise_score`)
  with caller `_cross_file_surprises` at L251-311 and
  `_cross_community_surprises` at L314-399.
- **Aura analog state**: ABSENT. `search` has no action that surfaces
  surprising edges; the LLM only sees flat slugs and `[[wiki-links]]`. Edges
  carry an `EdgeType` (`mentions`/`related`/typed body link) but nothing
  ranks them by how unexpected the connection is.
- **Aura target surface**: new `search(action="surprises", top_k=5)` backed by a
  small `internal/wiki/surprise.go`. Reuses existing GraphIndex outbound/inbound
  iteration; no new heavyweight infra.
- **Effort**: SMALL — one story, ~150 LOC. Adaptation note below explains why.
- **Impact**: HIGH. graphify users report that the surprise panel is the #1
  reason they come back to the report. For Aura it converts a passive wiki
  into something that surfaces "you may have forgotten that X relates to Y".
- **Repro**: ask Aura "che connessioni non ovvie ci sono nella mia knowledge
  base?". Today she lists god_nodes or runs `subgraph`, neither answers the
  question. With this lift, she calls `search(action=surprises, top_k=5)` and
  returns a ranked list with a "why" sentence per pair.
- **Adaptation**: drop the language-family, file-category, and cross-repo
  bonuses (all code-graph specific). Keep the four signals that fit Aura's
  graph: (a) confidence weight via `EdgeType` — `related` frontmatter and
  typed body links score above bare `mentions`; (b) cross-category bonus
  using existing `NodeMeta.Category` (`person`+`technical` is more surprising
  than two `technical`); (c) cross-community bonus deferred until pattern 3
  ships; (d) peripheral→hub via existing `Degree(slug)` (low ↔ high). Sort
  by score, dedup by pair, return top-K with a one-line `why` string.

## 2. Suggested questions surface (HIGH / SMALL)

- **Source**: `analyze.py:402-524` (`suggest_questions`).
- **Aura analog state**: ABSENT. No tool surfaces "questions this wiki is
  uniquely positioned to answer". User has to know what to ask.
- **Aura target surface**: new `search(action="suggest_questions")`. Lives in
  `internal/wiki/questions.go`, called from a new
  `wiki_suggest_questions.go` registry delegate.
- **Effort**: SMALL — ~180 LOC. Four of the five graphify signals are cheap
  to compute on Aura's data; the fifth (low-cohesion community) waits on
  pattern 3.
- **Impact**: HIGH. Turns a cold-start prompt ("how can I help?") into
  concrete suggestions — perfect when Davide opens Telegram and forgot what
  was in flight. Direct UX win on a frozen interface.
- **Repro**: at session start ask "che domande mi consigli di farti partendo
  dalla wiki?". Today: generic "posso aiutarti con…". With this lift: 4-5
  pointed questions tied to specific slugs and confidence labels.
- **Adaptation**: Map graphify's five buckets onto Aura's data:
  (1) AMBIGUOUS edges → use entries in `proposed_updates` table + any
  `edge_type=related` with a low `confidence` frontmatter tag, prompt "what
  is the exact link between [[A]] and [[B]]?"; (2) bridge nodes by
  betweenness → use existing `P99Degree` hubs and check which ones connect
  multiple `Category` buckets; (3) god_nodes with many INFERRED edges →
  existing `TopNodes(10)` ∩ frontmatter `sources:` count, prompt "are the N
  links from [[X]] all still accurate?"; (4) isolated nodes (`InDegree==0
  && OutDegree<=1`) → "what connects [[A]], [[B]], [[C]] to the rest?";
  (5) low-cohesion → deferred. Output: JSON `{type, question, why}` array
  the LLM reads directly.

## 3. Community detection + cohesion score (HIGH / MEDIUM)

- **Source**: `cluster.py:86-183` (Leiden/Louvain with hub exclusion +
  oversized split + cohesion re-split + community ID remapping in
  `remap_communities_to_previous` at L219-267).
- **Aura analog state**: ABSENT. Aura has zero clustering; god_nodes are
  degree-ranked but the model never sees "this hub belongs to which cluster
  of related pages". No way to ask "show me the clusters in my wiki".
- **Aura target surface**: `internal/wiki/cluster.go` building on existing
  GraphIndex; expose via `search(action="clusters")` returning `[{id, label,
  cohesion, size, top_slugs[]}]`. Cluster label generation is a separate
  small task (one LLM call, cache to SQLite).
- **Effort**: MEDIUM (2-3 stories). The Leiden algorithm is non-trivial in
  Go — see infra cost flag below. Cohesion score itself is trivial
  (intra-edges / max_possible).
- **Impact**: HIGH. Unlocks (a) the low-cohesion question type from
  pattern 2, (b) the cross-community bonus in pattern 1, (c) "community
  hub navigation" in pattern 9, (d) more meaningful subgraph capsules
  (current capsule treats every page equally). Multiplies the value of
  three other patterns in this doc.
- **Repro**: ask "mostrami le aree tematiche della mia wiki". Today: no
  answer possible. With this lift: 4-6 clusters with a label
  (LLM-generated), cohesion score, top member slugs.
- **Adaptation**: graphify uses graspologic Leiden with Louvain fallback.
  **Go dependency cost**: no production-grade Leiden in pure Go. Options:
  (a) port the Louvain algorithm — ~400 LOC, manageable, slightly worse
  quality but stable; (b) shell out to a Python sidecar like
  `aura-graphify-sidecar` — adds container, fits the existing sidecar
  pattern (whisper/piper/markitdown), gives us real Leiden quality. Pick
  (a) first; revisit (b) only if quality is visibly worse.
  Hub-exclusion logic from graphify (`exclude_hubs_percentile`) is the
  important detail — without it, super-hub pages like `davide-marchetto`
  drag every cluster together. Reuse existing `P99Degree()`.

## 4. Graph diff between two snapshots (HIGH / TRIVIAL)

- **Source**: `analyze.py:527-608` (`graph_diff`).
- **Aura analog state**: ABSENT. Aura has Git tracking on wiki files
  (`go-git/v5`) but no semantic diff at the graph level. The LLM cannot
  answer "what changed in the wiki since yesterday/last session/last
  commit?" except by reading raw `git log`.
- **Aura target surface**: `internal/wiki/diff.go` + `search(action="diff",
  since=<rfc3339 or commit>)`. Reuse the existing GraphIndex; build a
  second index from the historical Git tree via `go-git` and diff.
- **Effort**: TRIVIAL → SMALL (~120 LOC). The diff itself is 4 set
  operations; the Git tree walk is the only mechanical work.
- **Impact**: HIGH. Persistent-memory product needs "what's new" answers.
  Powers daily-briefing tool, session-resume context, audit trail.
- **Repro**: ask "cosa è cambiato nella wiki dall'ultima sessione?". Today:
  Aura at best lists raw git diff. With this lift: `{new_nodes, new_edges,
  removed_nodes, removed_edges, summary: "3 new pages, 7 new links"}`
  — the LLM reads the summary and writes a narrative.
- **Adaptation**: Aura's nodes are slugs (not graphify's `node_id`), edge
  comparison key is `(src_slug, tgt_slug, edge_type)`. Compare against a
  Git commit hash (default `HEAD~1` for "since last session" or the commit
  pinned by a `last_seen_commit` setting). Cache the historical index for
  popular `since=` values.

## 5. AMBIGUOUS / INFERRED / EXTRACTED edge confidence tags (HIGH / SMALL)

- **Source**: edges throughout (e.g. `analyze.py:194-213`, `report.py:35-43`).
  Frontmatter on every wiki edge: `confidence: EXTRACTED|INFERRED|AMBIGUOUS`.
- **Aura analog state**: PARTIAL. Aura's wiki frontmatter has a `related:`
  list where each entry CAN carry `confidence: AUTO|HUMAN|...` (already used
  by `wikiSubgraph.go:298-300` to build `confMap`). But body `[[wiki-link]]`
  edges default to `"EXTRACTED"` (hard-coded in `wiki_subgraph.go:312`) and
  no upstream writer sets a less-confident tag for LLM-proposed edges. The
  AMBIGUOUS bucket — the one that drives surprise-score and
  suggest-questions — is unused.
- **Aura target surface**: extend `internal/wiki/writer.go` (and the
  `propose_patch` flow) to set `confidence: INFERRED` when the LLM proposes
  an edge from a low-similarity recall hit; `AMBIGUOUS` when a `related:`
  entry resolves to multiple candidate slugs. Patterns 1 and 2 then have
  real signal to weight.
- **Effort**: SMALL (~80 LOC + test fixtures). The writer change is one
  function; the harder work is deciding the heuristic for AUTO→INFERRED.
- **Impact**: HIGH but only as a precondition for patterns 1/2/9. Standalone
  it just labels the wiki — no LLM-visible change.
- **Repro**: write a wiki edge from a recall hit with cosine score 0.42.
  Today: serialized as `confidence: AUTO`. With this lift: `INFERRED 0.42`
  visible in subgraph capsules and surfaceable via suggest_questions.
- **Adaptation**: Use the recall hit's score: ≥0.75 → `EXTRACTED`,
  0.55-0.75 → `INFERRED <score>`, <0.55 → `AMBIGUOUS`. Treat user-typed
  `related:` entries as `EXTRACTED` (human authored ≡ ground truth).

## 6. Bridge-node detection (high betweenness, not just degree) (MEDIUM / SMALL)

- **Source**: `analyze.py:432-454` (NetworkX betweenness, top-3, skipping
  file hubs).
- **Aura analog state**: ABSENT. `god_nodes` ranks by `InDegree+OutDegree`
  only. Betweenness picks DIFFERENT nodes — bridges between clusters, not
  just the most-linked pages. graphify's `suggest_questions` uses this to
  ask "why does X connect cluster A to cluster B?".
- **Aura target surface**: `internal/wiki/centrality.go` adding
  `Betweenness(topK int)` to GraphIndex; expose via `search(action="god_nodes",
  metric="betweenness")` — extend existing action, no new one.
- **Effort**: SMALL (~150 LOC). Brandes' algorithm is well-documented Go
  implementations exist; for Aura's ~50 nodes today, even the naive O(V³)
  works.
- **Impact**: MEDIUM. By itself it's "another god_nodes flavor"; combined
  with cluster labels (pattern 3) it becomes "show me the architectural
  bridges" — high-signal on a mature graph (>200 pages), low-signal now.
- **Repro**: ask "quali pagine collegano aree diverse della mia
  conoscenza?". Today: `god_nodes` lists hubs but they may all live inside
  one cluster. With betweenness: returns the actual bridges.
- **Adaptation**: cap k-sample to 100 for graphs >1000 nodes (graphify
  does this at L433). Aura's graph is far below this — full betweenness is
  fine. Use a deterministic seed so output is stable across runs.

## 7. Knowledge gaps detector (isolated + thin communities) (MEDIUM / TRIVIAL)

- **Source**: `report.py:163-188` (assembles the "Knowledge Gaps" section).
- **Aura analog state**: ABSENT. Aura has no "what's missing / under-linked"
  signal. The LLM never sees a list of orphan pages.
- **Aura target surface**: `search(action="gaps")` listing isolated slugs
  (`InDegree+OutDegree <= 1`) and (after pattern 3) thin clusters.
  ~60 LOC against the existing GraphIndex.
- **Effort**: TRIVIAL (≤50 LOC for the isolated-only flavor).
- **Impact**: MEDIUM. Operationally important for "wiki is the bedrock"
  (per project_wiki_is_bedrock memory) — surfaces pages that the LLM keeps
  writing but never links. Today Davide finds these by accident.
- **Repro**: ask "quali pagine wiki sono isolate?". Today: no answer.
  With lift: `{count: 7, slugs: [...], hint: "possible orphans"}`.
- **Adaptation**: 1:1 port using `Degree(slug)`. The `_is_file_node` /
  `_is_concept_node` filters from graphify don't apply — Aura has no
  synthetic nodes. Filter only operational slugs via existing
  `IsOperationalSlug`.

## 8. IDF-weighted node label scoring with three-tier match (MEDIUM / SMALL)

- **Source**: `serve.py:73-95` (`_compute_idf` cached on graph object) and
  `serve.py:98-121` (`_score_nodes` with exact 1000× / prefix 100× /
  substring 1× tiers, plus 0.5× source bonus).
- **Aura analog state**: PARTIAL. Search uses Qdrant cosine + SQLite FTS5
  via RRF; pure-title exact match is not boosted enough to always win.
  Hybrid fusion treats "page slug literally equals query" as just another
  signal. graphify's pattern reliably surfaces the exact-named entity at
  position 1 even when cosine puts it at position 5.
- **Aura target surface**: `internal/storage/search/search.go` —
  pre-search exact/prefix slug match check; inject as ScoreExact group 0
  with a calibrated weight so it always tops the RRF fusion.
- **Effort**: SMALL (~80 LOC). Most of it is the IDF cache lifecycle.
- **Impact**: MEDIUM. Fixes the "user asks for slug X, gets slug X-related
  fluff at position 1, slug X at position 3" failure mode. Solves a real
  rank-ordering bug rather than adding a feature.
- **Repro**: ask "leggi la pagina davide-marchetto" — Aura already handles
  this fine (we have an `action=read`). The bigger repro: type just
  `davide-marchetto` as a query. Today: cosine puts it mid-pack behind
  other Davide-mentioning pages. With this lift: position 1.
- **Adaptation**: Aura has no labels distinct from slugs, so the exact tier
  collapses to a single `slug == normalized_query` check. Add an
  IDF cache keyed on the GraphIndex generation count (invalidate on
  `RefreshPage` / `RemoveNode`). The 1000/100/1 ratios are graphify's
  empirical choice — keep them.

## 9. Community navigation hubs (LOW / SMALL — gated on pattern 3)

- **Source**: `report.py:86-93` (community hub links in GRAPH_REPORT.md)
  + the underlying `_COMMUNITY_<safe_name>.md` files produced by
  `export.py` (not read; one-line cited).
- **Aura analog state**: ABSENT. Aura has no "cluster overview pages".
- **Aura target surface**: optional auto-generated wiki page per cluster,
  written deterministically (temperature=0) into
  `wiki/cluster-<safe-name>.md`. Reuses existing wiki writer.
- **Effort**: SMALL after pattern 3 ships; otherwise impossible.
- **Impact**: LOW for Aura — the dashboard already lists pages; the LLM
  has `god_nodes`. Adds an organizational layer that humans visiting the
  wiki vault appreciate but the LLM mostly bypasses.
- **Repro**: open the wiki vault filesystem and look for any thematic
  index. Today: only `log.md` and `SCHEMA.md`. With lift: cluster-X.md
  pages with linked members.
- **Adaptation**: graphify writes Markdown for Obsidian. Aura's wiki IS
  Markdown — same format works. The `_safe_community_name` regex
  (`report.py:8-12`) is portable, but Aura already has `Slug()` —
  reuse it.

## 10. Watch+debounce incremental rebuild guard (LOW / SMALL)

- **Source**: `watch.py:641-721` (3 s debounce, batched rebuild, per-repo
  flock at L15-72, `.graphifyignore` short-circuit at L678).
- **Aura analog state**: PARTIAL. Aura has fsnotify on `mcp.json` (per
  `2_10_b_tool_reconciler_shipped` memory) and the wiki writer is
  synchronous (no debounce). The wiki graph index is refreshed by
  `RefreshPage`/`RemoveNode` per write — not by a watcher.
- **Aura target surface**: NOT NEEDED for wiki (Aura owns every write
  path; no external editor races). Where it MIGHT help: skills directory
  + prompt overlays directory — both are externally editable. Could fold
  into existing fsnotify code.
- **Effort**: SMALL (~100 LOC) but unclear value.
- **Impact**: LOW. Aura is single-writer for the wiki; the index is
  always consistent. The debounce pattern is a defense graphify needs
  because users `git checkout` between branches; Aura's deployment
  doesn't have that profile.
- **Repro**: edit a skill `SKILL.md` externally. Today: reconciler
  triggers via existing fsnotify (per memory). No regression to fix.
- **Adaptation**: skip unless we ship a "user can edit wiki via
  filesystem outside Aura" feature. Then port the rebuild lock (flock
  pattern at `watch.py:15-72` — uses `fcntl`; Go equivalent is
  `golang.org/x/sys/unix` `Flock` on Linux, file-rename trick on
  Windows). Note for the future, not now.

## 11. Hot-reload state via (mtime_ns, size) tuple (LOW / TRIVIAL)

- **Source**: `serve.py:410-418` (graph.json hot-reload key) +
  surrounding `_maybe_reload` logic just below.
- **Aura analog state**: PARTIAL. Aura's GraphIndex is in-memory and
  refreshed on every WritePage — no need for file-watching. But the
  pattern itself is reusable for the upcoming `bench_dataset.json` and
  any other read-only on-disk index Aura ships.
- **Aura target surface**: no immediate use; bank as a helper for the
  next on-disk artifact.
- **Effort**: TRIVIAL (≤30 LOC reusable helper).
- **Impact**: LOW.
- **Repro**: n/a today.
- **Adaptation**: Go's `os.Stat()` gives `ModTime()` (no `mtime_ns` on
  Windows pre-1.18 stable; use `Size()+ModTime().UnixNano()`).

## 12. Cohesion-based community re-split (LOW — gated on pattern 3)

- **Source**: `cluster.py:170-179` (second-pass split when a community has
  >=50 nodes AND cohesion < 0.05).
- **Aura analog state**: gated entirely on pattern 3 shipping.
- **Aura target surface**: same module as pattern 3.
- **Effort**: 20 LOC inside the clustering module.
- **Impact**: LOW until graph is large (>500 pages); irrelevant today
  at 45 pages.
- **Adaptation**: identical to graphify; port the constants
  (`_COHESION_SPLIT_THRESHOLD = 0.05`, `_COHESION_SPLIT_MIN_SIZE = 50`).

---

## Patterns I checked and rejected

- **Concept node / file node filters** (`analyze.py:40-82`): code-graph-specific.
  Aura has no synthetic file/method-stub nodes. Skip.
- **Cross-language family bonus** (`analyze.py:9-32`): code-graph-only.
- **JSON key noise filter** (`analyze.py:68-82`): code-graph-only.
- **Rationale-node sanitation** (`semantic_cleanup.py:159-283`): solves a
  problem Aura doesn't have (Aura's writer doesn't emit free-text nodes).
- **MCP stdio plumbing** (`serve.py:395+`): Aura has its own tool registry;
  not a lift.
- **graspologic suppression of ANSI escapes** (`cluster.py:11-19`): only
  relevant if we adopt graspologic, which the dependency-cost flag in
  pattern 3 already covers.
- **Dedup pipeline with MinHash + JaroWinkler + Union-Find** (`dedup.py`):
  Aura's slugs are already canonical via `Slug()`; dedup happens at the
  Markdown writer layer. No analog work to do.
- **PR-comment annotation** (`prs.py`): out of scope.
- **HTML graph visualisation** (`callflow_html.py`, `tree_html.py`): the
  dashboard already has its own visualization story.
- **Mermaid neighborhood diagrams**: searched `serve.py`/`callflow_html.py`
  and graphify's mermaid output lives in `export.py`/`callflow_html.py` and
  is code-call-flow specific (not a knowledge-graph neighborhood). The
  obvious "mermaid for a node's neighborhood" pattern is NOT present in
  graphify — only call-flow mermaid is. Skip.

---

## Recommended sequencing

1. **Pattern 5** (confidence tags) first — it's a precondition that costs ~80 LOC
   and unblocks patterns 1, 2, 9.
2. **Pattern 4** (graph_diff) — trivial, immediate UX win, independent.
3. **Pattern 7** (gaps) — trivial, independent.
4. **Pattern 1** (surprise score) — small, depends on pattern 5.
5. **Pattern 2** (suggest_questions) — small, depends on pattern 5; partial
   without pattern 3, fully realised after.
6. **Pattern 8** (IDF exact-match boost) — small, fixes a real ranking bug.
7. **Pattern 3** (clustering) — medium, gates patterns 6/9/12 and improves 1/2.
8. Defer 6 / 9 / 10 / 11 / 12 until after pattern 3 lands and the wiki passes
   ~150 pages.

Total scope through step 6 (the "fast unlocks"): ~600 LOC, 4-6 stories,
no new Go dependencies, all reads against the existing in-memory
GraphIndex. Step 7 is the architectural decision — Louvain port vs Python
sidecar — and should be its own discuss-phase.
