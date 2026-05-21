# Graphify Full-Pipeline Deep-Dive — Lift Candidates for Phase-WIKI-B

Date: 2026-05-21. Source tree: `D:/tmp/graphify/graphify/`.

This document is the second pass on graphify after the `serve.py` retrieval-algorithm
extract. It maps every other module in the pipeline (`detect → extract → build →
cluster → analyze → wiki → llm → dedup → manifest`) against Aura's current
wiki+graph stack and identifies what is worth lifting for **Phase-WIKI-B**
(one-call token-budgeted subgraph retrieval), what to skip, and the surprising
shapes of their LLM ingest contract.

Aura already shipped from graphify (Phase-WIKI-A, 2026-05-20): god-node ranking,
`wiki_path` BFS, confidence labels on `related:` frontmatter. The remaining
lifts are heavier and concentrated in **extraction**, **clustering**, and
**dedup**.

---

## Architecture map

Single deterministic pipeline, each module a pure function over plain dicts +
`networkx.Graph` (`ARCHITECTURE.md:7–9`):

```
detect()  →  extract()  →  build_graph()  →  cluster()  →  analyze()  →  report()  →  export()/wiki()/serve()
```

No shared state. Output lands in `graphify-out/` only. Every stage can be
re-run independently against the pickled `graph.json`. This is the same
"single binary, single-process, single-output-dir" discipline Aura already
follows for the wiki — a clean target shape.

---

## Build (`graphify/build.py`)

**Where the messy reality of LLM extraction gets normalised before NetworkX
sees it.** Five mechanisms worth knowing:

1. **Three-layer node dedup** (`build.py:1–22`): within-file AST `seen_ids`
   set → between-file `add_node` overwrite (semantic wins over AST because
   it runs second) → cross-chunk `seen` set merged by the skill before
   `build()`.
2. **ID normalisation across AST and LLM** (`build.py:54–65`): `_normalize_id`
   does NFKC + `[^\w]+→_` + casefold so an LLM-emitted
   `Session_ValidateToken` reconciles with the AST's
   `session_validatetoken`. Edges whose endpoints fail strict lookup are
   re-mapped through this normalisation map before being dropped — this is
   how graphify keeps the LLM cooperative without rejecting drift
   (`build.py:163–177`).
3. **Direction-preserving undirected graphs** (`build.py:182–185`):
   `_src` and `_tgt` attrs are stored on every edge even in `nx.Graph` mode
   so the display layer can show real direction. `build_merge` reads JSON
   directly instead of round-tripping through `node_link_graph` because the
   NetworkX round-trip silently flips directional `calls` edges
   (`build.py:299–306`).
4. **`build_merge` with prune_sources** (`build.py:281–363`): incremental
   re-build that loads existing `graph.json`, merges new chunks, and prunes
   nodes/edges from deleted source files. Refuses to shrink silently —
   raises if `new_n < existing_n` without explicit `prune_sources`.
5. **`prefix_graph_for_global`** (`build.py:366–379`): for cross-repo
   graphs, prefixes IDs with `repo_tag::` and records `local_id` so
   per-repo dedup can run before merging — the only legitimate way to
   cross corpus boundaries without label-collision false positives.

Aura-relevant insight: Aura's wiki has its own deterministic-write contract
(`SCHEMA.md` + `temperature=0` + atomic write). Aura needs the
**ID-normalisation reconciliation** when it eventually adds LLM-suggested
related links — today the wiki schema is too rigid for that to happen, but
once Aura wants `related: [{slug, confidence, source: 'llm-inferred'}]`,
this is the file to copy.

---

## Extract (`graphify/extract.py`, 6660 LOC)

**Massive single file: 30+ language extractors, all tree-sitter based, plus
markdown and JSON.** Two extractors matter to Aura:

### `extract_markdown` (`extract.py:4837–4944`)

Pure line-by-line, NO tree-sitter. Produces:

- 1 node per file
- 1 node per heading (H1–H6), nested via `contains` edges based on heading
  level stack
- 1 node per fenced code block, attached to the nearest heading
- `references` edges when backtick-quoted identifiers in heading text match
  another known node (deferred to a later pass — the extractor itself only
  builds the local tree)

Every edge confidence is `EXTRACTED`. Heading IDs collide on duplicate
titles and are disambiguated by appending the line number
(`extract.py:4929–4930`).

**Aura lift**: this is the cheapest way to turn a wiki page into a
**multi-node fragment** rather than a single document blob. Today
`internal/wiki/index.go` treats one `.md` file as one node — graphify
treats each `##` heading as its own node, which lets BFS surface "the
heading that mentions `delta automazioni`" instead of "a 3000-line wiki
page that contains it somewhere". For Aura's "code-cliente" use-case this
is the biggest single retrieval improvement.

### `extract_json` (`extract.py:6002–6200+`)

Tree-sitter-json based with hard 1 MiB cap and depth=6 cap. Walks
key/value pairs to depth 6, with `contains` edges from parent key to child
key. Special-cases dependency arrays (`dependencies`,
`devDependencies` …) and `extends` arrays → external `ref_*` nodes so
external package refs don't collide with file nodes. Per-pair cap of 500
prevents exploding on giant fixtures.

**Aura lift**: this is the closest analog to Aura's xlsx-summary problem.
Graphify does NOT extract row-level cell data — it extracts STRUCTURE only
(sheet → table → column-header), see `detect.xlsx_extract_structure`
(`detect.py:251–338`, behind a feature flag, currently NOT wired into the
dispatcher per F-035). Each xlsx becomes file → sheets → named tables →
column headers, with EXTRACTED `contains` edges. Aura today loses ALL row
data; graphify still loses row data but at least keeps the table shape.
This is a partial win — see "What NOT to lift" for why row-level extraction
is intentionally avoided.

### Other extractors

Every language extractor follows the same pattern (`ARCHITECTURE.md:58–64`):
tree-sitter parse → walk nodes → collect `nodes` and `edges` → call-graph
second pass for `INFERRED` calls edges. The `LanguageConfig` dataclass at
`extract.py:156–197` is the unified schema — adding a language is purely
config + tree-sitter wheel + AST node-type names. **Aura should NOT lift
any of this** — Aura's "wiki" is not a code corpus.

---

## Cluster (`graphify/cluster.py`)

**Leiden community detection via graspologic, with Louvain fallback.**
Pure NetworkX in, `{community_id: [node_ids]}` out. Six knobs:

1. **`resolution`** (`cluster.py:22, 86`): default 1.0. >1 → more, smaller
   communities. <1 → fewer, larger. Passed straight to graspologic.
2. **`exclude_hubs_percentile`** (`cluster.py:89, 114–122, 143–159`): nodes
   above this degree percentile are excluded from partitioning and
   reattached to their majority-vote neighbour community afterwards.
   Prevents `CLAUDE.md`-style super-hubs from absorbing all subsystems
   into one mega-community (issue #919).
3. **`_MAX_COMMUNITY_FRACTION = 0.25`** (`cluster.py:80`): communities >25%
   of total nodes get re-clustered via a second Leiden pass on the
   subgraph (`_split_community`, `cluster.py:186–201`).
4. **`_COHESION_SPLIT_THRESHOLD = 0.05`** (`cluster.py:82`): low-cohesion
   communities (>50 nodes, cohesion <0.05) get a second split pass too —
   this catches the case where a doc-hub bridges unrelated subsystems
   (`cluster.py:170–179`).
5. **Stable deterministic input** (`cluster.py:34–46`): nodes sorted by
   `str(id)`, edges sorted by `(src, tgt, json.dumps(attrs))` before
   handing to Leiden — same input → same community IDs across runs.
6. **`remap_communities_to_previous`** (`cluster.py:219–267`): greedy
   one-to-one overlap matching against a previous partition so
   incremental rebuilds keep community IDs stable for downstream
   consumers (the wiki article filenames, for instance, depend on this).

**Cohesion score** (`cluster.py:204–212`): `intra-edges / n*(n-1)/2` — the
ratio of actual to possible internal edges. Used both for low-cohesion
re-splitting and as a display metric on wiki article frontmatter.

**Query-time use is intentionally absent**: communities are exposed as a
separate MCP tool (`serve.py:617–630` `get_community`) and surface in
`surprising_connections` (cross-community edges = surprising) and
`suggest_questions` (low-cohesion communities → "should this be split?")
but the actual BFS/DFS query path does NOT use community membership for
seed picking or expansion. **This is the gap Aura's Phase-WIKI-B should
close**: community-aware retrieval is the obvious lift graphify itself
hasn't done.

---

## Analyze (`graphify/analyze.py`)

Five outputs, all read directly from the in-memory graph (no LLM, no I/O):

### `god_nodes(G, top_n=10)` (`analyze.py:85–104`)

Top-N by degree, excluding three categories:
- **`_is_file_node`** (`analyze.py:40–65`): label == basename OR starts
  with `.` and ends with `()` (AST method stub) OR ends in `()` and
  degree ≤1 (isolated module function stub).
- **`_is_concept_node`** (`analyze.py:139–155`): empty `source_file` OR
  source has no extension (synthetic concept).
- **`_is_json_key_node`** (`analyze.py:76–82`): `.json` source + label in
  a hardcoded noise set (`start`, `end`, `name`, `id`, `type`,
  `dependencies`…).

Aura already lifted this in Phase-WIKI-A. The noise-filter set is more
aggressive than Aura's current version.

### `surprising_connections(G, communities, top_n)` (`analyze.py:107–311`)

Two branches:

- **Multi-source corpora** (`_cross_file_surprises`): cross-file edges
  between non-concept, non-file-hub nodes, scored on a composite
  surprise function (`_surprise_score`, `analyze.py:177–248`):
  - Confidence weight: AMBIGUOUS=3, INFERRED=2, EXTRACTED=1
  - Cross file-type (code↔paper): +2
  - Cross-repo (different top-level dir): +2
  - Cross-community: +1
  - `semantically_similar_to` relation: ×1.5 multiplier
  - Peripheral→hub (deg ≤2 reaches deg ≥5): +1
  - **Suppression**: INFERRED calls/uses crossing language families OR
    code↔doc get conf_bonus zeroed AND structural bonuses suppressed,
    because cross-language `calls` is almost always resolver pollution.

- **Single-source corpora** (`_cross_community_surprises`,
  `analyze.py:314–399`): cross-community edges sorted
  AMBIGUOUS→INFERRED→EXTRACTED, deduplicated by community pair so one
  bridge node doesn't dominate all results. Falls back to edge
  betweenness centrality (`nx.edge_betweenness_centrality`) when no
  community info is available, with a 5000-node cap to avoid blowing up.

Each result has a human-readable `why` field — this is the audit trail
graphify produces for the LLM consumer.

### `suggest_questions(G, communities, community_labels, top_n=7)` (`analyze.py:402–524`)

Five question generators:
1. **`ambiguous_edge`**: every AMBIGUOUS edge → "What is the exact
   relationship between A and B?"
2. **`bridge_node`**: top-3 betweenness centrality nodes (k=100 sampling
   above 1000 nodes) → "Why does X connect A to B,C?"
3. **`verify_inferred`**: top-5 god nodes with ≥2 INFERRED edges → "Are
   these N inferred relationships actually correct?"
4. **`isolated_nodes`**: degree ≤1 non-file-non-concept nodes → "What
   connects these to the rest of the system?"
5. **`low_cohesion`**: communities with cohesion <0.15 and ≥5 nodes →
   "Should this be split?"

Empty signal → single "no_signal" question explaining why.

### `graph_diff(G_old, G_new)` (`analyze.py:527–608`)

Set-difference based: returns added/removed nodes+edges with a one-line
summary. Edge keys are `(min(u,v), max(u,v), relation)` for undirected
graphs. Used by the `--update` flow to surface what changed since the
last extraction.

---

## Wiki (`graphify/wiki.py`)

**Wikipedia-style markdown vault, one article per community + one per god
node.** Pure-Python output, no templates. Article schema
(`wiki.py:37–102` `_community_article`):

```markdown
# {community_label}
> {N} nodes · cohesion {0.NN}
## Key Concepts
- **{node_label}** ({degree} connections) — `{source_file}`
- ...
## Relationships
- [[{other_community}]] ({N} shared connections)
- ...
## Source Files
- `{path}`
## Audit Trail
- EXTRACTED: {N} ({P}%)
- INFERRED: {N} ({P}%)
- AMBIGUOUS: {N} ({P}%)
---
*Part of the graphify knowledge wiki. See [[index]] to navigate.*
```

God-node articles (`wiki.py:105–138`) group neighbours by relation type
(`calls`, `references`, …) — this is a per-node detail page.

`to_wiki` (`wiki.py:181–259`) clears all `.md` files at the start of each
call (the LLM-generated community labels are non-deterministic across
runs, so orphan files would accumulate). It refuses to clear if the
`communities` dict is empty — explicit safety check.

**No frontmatter.** Plain markdown body only. `[[wiki-link]]` syntax is
used between community articles and god-node articles. Filenames are
sanitised for Windows-reserved chars (`<>:"/\|?*`).

Aura already has a richer wiki schema (YAML frontmatter with
`schema_version`, `prompt_version`, `related: [{slug, confidence}]`).
Aura should NOT switch to graphify's flatter format. **What Aura SHOULD
lift is the "one article per community" idea** — generate
`community-<slug>.md` overlay pages alongside the existing per-entity
pages so BFS has a natural top-down entry point. Today Aura's wiki has
no community-level summaries.

---

## LLM (`graphify/llm.py`)

**The full ingest LLM contract.** Seven backends (Claude, Kimi K2.6,
Gemini 3 Flash, OpenAI, DeepSeek V4, Bedrock, claude-cli, Ollama). One
extraction system prompt for all (`llm.py:133–147`):

```
You are a graphify semantic extraction agent. Extract a knowledge graph
fragment from the files provided.
Output ONLY valid JSON — no explanation, no markdown fences, no preamble.

Rules:
- EXTRACTED: relationship explicit in source (import, call, citation,
  reference)
- INFERRED: reasonable inference (shared data structure, implied
  dependency)
- AMBIGUOUS: uncertain — flag for review, do not omit

Node ID format: lowercase, only [a-z0-9_], no dots or slashes.
Format: {stem}_{entity} ...

Output exactly this schema: {nodes, edges, hyperedges, input_tokens,
output_tokens}
```

**Key shapes**:

- **No few-shot examples in the system prompt** — only the schema and the
  confidence ladder. Graphify relies on the model already understanding
  knowledge-graph structure from its training corpus.
- **`temperature=0` always** (`llm.py:54, 69, 88, 97`) — deterministic
  extraction.
- **Hard 16 MB / 10 MB JSON guards** (`llm.py:166–192`): output stripped
  of markdown fences, capped before `json.loads` to prevent memory
  exhaustion from hostile/runaway models.
- **`_response_is_hollow` detection** (`llm.py:194–212`): empty/null
  content OR no nodes+no edges+no hyperedges → re-labelled as
  `finish_reason="length"` so the adaptive-retry layer bisects the
  chunk. Critical for local models under VRAM pressure.
- **Adaptive retry by bisection** (`llm.py:683–810`): three signals all
  funnel through the same recovery — `finish_reason=length`,
  `_looks_like_context_exceeded` (substring match on error msg), and
  hollow responses. Bisects the chunk recursively up to depth 3 (8×
  expansion max).
- **Token-budgeted chunking** (`llm.py:614–651`): `_pack_chunks_by_tokens`
  groups by parent directory first, then greedy-packs to a budget
  (default 60_000 tokens). Cross-file edges are more likely to be found
  inside one chunk than across chunks, hence the directory grouping.
- **Thread-pool parallel extraction** (`llm.py:813–936`): default 4
  workers, Ollama forced serial unless `GRAPHIFY_OLLAMA_PARALLEL=1`,
  claude-cli forced serial unless `GRAPHIFY_CLAUDE_CLI_PARALLEL=1`.
- **Loud failure summary** (`llm.py:929–935`): failed chunks counted and
  surfaced at the end of the run, not buried mid-log.

The richer extraction prompt actually used by the Claude Code subagent
path lives in `skill.md` (the full instructions to subagents, lines
350–430) — that one DOES include the confidence-score discrete rubric
(`0.95 / 0.85 / 0.75 / 0.65 / 0.55`), the language family rule, the
semantic-similarity edge guidance, the hyperedge contract, and the
file_type vocabulary. The `_EXTRACTION_SYSTEM` in `llm.py` is the
stripped-down version for direct API calls; the skill prompt is the
full version.

**Aura lift**: the confidence-score discrete rubric (`0.95 / 0.85 / 0.75
/ 0.65 / 0.55`, never 0.5 default) is directly portable to Aura's
`related: [{slug, confidence}]` schema. Today Aura's confidence field
is freeform — the discrete rubric forces models to commit.

---

## Dedup (`graphify/dedup.py`)

**Pipeline: exact-normalise → entropy gate → MinHash/LSH blocking →
Jaro-Winkler verify → community boost → union-find merge → optional LLM
tiebreak.** Cleanest module in the project. Six layers:

1. **Exact normalisation** (`dedup.py:17–19`): `_norm` = lowercase +
   collapse non-alphanumeric to space. Pass 1 only merges within the
   same `source_file` — cross-file exact matches fall through to fuzzy
   (`dedup.py:181–192`).
2. **Entropy gate** (`dedup.py:22–31`): `_ENTROPY_THRESHOLD = 2.5` bits/char.
   Below this, label is too low-entropy ("test", "main") to fuzzy-match
   safely. High-entropy candidates only enter the fuzzy pipeline.
3. **MinHash/LSH blocking** (`dedup.py:41–46, 205–217`): 128 permutations,
   `_LSH_THRESHOLD = 0.7`, 3-gram char shingles (spaces stripped so
   "graph extractor" and "graphextractor" share shingles). LSH gives
   sub-linear candidate enumeration.
4. **Jaro-Winkler verify** (`dedup.py:234`): rapidfuzz
   `JaroWinkler.normalized_similarity × 100`. Merge at
   `_MERGE_THRESHOLD = 92.0`.
5. **Short-label guard** (`dedup.py:53–85`): two filters block fuzzy
   merges that would be wrong:
   - `_is_variant_pair`: `cranel/cranelr`, `M1/M1 Pro` — same stem +
     digit/letter suffix on labels <12 chars never merge.
   - `_short_label_blocked`: short labels (<12 chars) only merge on
     same-length single-char Damerau-Levenshtein substitution
     (`Extractor/Extractar` typo, not abbreviations).
6. **Community boost** (`dedup.py:120, 241–245`): both nodes in same
   community AND both labels ≥12 chars → +5.0 score bonus.
   Cross-community fuzzy merges DO NOT get the boost — community
   membership is treated as a tiebreaker, not a primary signal.
7. **Optional LLM tiebreak** (`dedup.py:322–414`): pairs scoring in
   `[75, 92)` get batched into a YES/NO LLM prompt (max 30 pairs per
   batch). Used only when `--dedup-llm` is passed.

**Cross-repo guard** (`dedup.py:147–153`): refuses to dedup if nodes
span multiple `repo` attrs. Cross-project label coincidence is too
risky.

**Aura lift**: this is the most directly applicable module. Aura today
has zero entity dedup beyond exact slug match. Aura's wiki has a known
issue: "delta automazioni" might also appear as "Delta Automazioni"
(label drift) or `delta-automazioni-srl` (slug variant). The
exact-norm → entropy → MinHash → Jaro-Winkler → community-boost
pipeline ports almost verbatim — entirely in-process, no external
deps beyond `datasketch` and `rapidfuzz` which Aura can vendor.

---

## Detect (`graphify/detect.py`)

**File discovery + classification + manifest-based incremental.** Three
notable surfaces:

### Classification (`detect.py:136–163`)

`FileType` enum: CODE / DOCUMENT / PAPER / IMAGE / VIDEO. Extension-based
dispatch with three escape hatches:
- **`.blade.php`** compound extension before suffix check
- **Shebang sniff** for extensionless files
  (`_shebang_file_type`, `detect.py:115–133`): reads first 128 bytes,
  detects `python`, `node`, `bash`, etc.
- **`_looks_like_paper`** (`detect.py:93–101`): `.md`/`.txt` files
  scanned for ≥3 of 13 academic-paper signals (arXiv, DOI, abstract,
  `\cite{`, numbered citations, "we propose", …) → reclassified as
  PAPER.

### `.graphifyignore` (`detect.py:464–574`)

Full gitignore-spec implementation: last-match-wins, negation patterns,
parent-exclusion rule, ancestor walks up to VCS root, JSONC strip for
tsconfig parsing. **This is way more complete than what most graph
tools ship.** A complementary `.graphifyinclude` allowlist
(`detect.py:576–650`) opts hidden files back in.

### Sensitive-file guard (`detect.py:41–60, 81–90`)

`_SENSITIVE_DIRS`: `.ssh`, `.gnupg`, `.aws`, `secrets`, `credentials`.
`_SENSITIVE_PATTERNS`: `.env*`, `.pem/.key/.p12`,
`credential/secret/password`, `token` (with `(?![a-zA-Z])` lookahead so
`tokenizer.py` doesn't match), `id_rsa*`, `.netrc/.pgpass`.

Skipped silently before classification — never sent to LLM, never
indexed.

### Manifest-based incremental (`detect.py:864–1019`)

`save_manifest` writes `{mtime, ast_hash, semantic_hash}` per file with
three modes: `ast`, `semantic`, `both`. `detect_incremental` returns
`new_files` (changed/missing hash for the requested kind) and
`unchanged_files` (clean). Also reports `deleted_files` (in manifest
but not on disk) so the caller can pass them to `prune_sources`.

Fast path: `mtime` unchanged → skip MD5. Slow path: `mtime` bumped →
MD5 check before re-extracting (covers touch-without-edit). Two
separate hashes (`ast_hash` vs `semantic_hash`) so AST-only `graphify
update` doesn't invalidate the expensive LLM cache.

**Aura lift**: this is the cleanest incremental pattern. Aura's wiki
rebuild today is all-or-nothing; an mtime+hash manifest with separate
"structural" and "semantic" hashes would let Aura cheaply re-extract
only changed wiki pages while preserving expensive LLM-derived edges
(if/when Aura adds them).

---

## Cache (`graphify/cache.py`)

Implied from `skill.md:Step B0` and `extract.py:cache_root`. Semantic
cache keys by content hash — files unchanged since last extraction skip
the LLM round entirely. The cache lives under `.graphify/` per-file
(see `detect.py:402` skip-list for the cache dir). Not heavily explored
here — same pattern as Aura's embedding cache (SHA-keyed SQLite).

---

## Patterns NOT yet lifted by Aura

| Pattern | Source | Aura mapping |
|---------|--------|-------------|
| **Heading-level node decomposition** for markdown | `extract.py:4837-4944` | Today `internal/wiki/index.go` indexes 1 file = 1 node. Add a parallel "heading-node" index: each H1/H2/H3 becomes a sub-node with a `parent_slug` field. BFS surfaces precise headings, not whole pages. **Biggest single retrieval lift.** |
| **Leiden community detection** | `cluster.py` | New `internal/wiki/cluster.go`. Once per index rebuild. Store `community_id` on each Page in SQLite. Use for "community summary" overlay pages. |
| **Cohesion-driven re-splitting** | `cluster.py:170-179` | Same module. Detect mega-communities (>25%) and low-cohesion ones (<0.05) and re-cluster the subgraph. |
| **Stable community ID across runs** | `cluster.py:219-267` | Critical so `community-<slug>.md` overlay pages survive rebuilds. Greedy overlap match. |
| **Discrete confidence-score rubric (0.95/0.85/0.75/0.65/0.55)** | `skill.md:350-430` | Update Aura's wiki frontmatter contract for `related: [{slug, confidence}]` to enforce one-of-five — drop continuous range. |
| **Entity dedup pipeline (exact→entropy→MinHash→JW→community-boost)** | `dedup.py` | New `internal/wiki/dedup.go`. Vendor `rapidfuzz` Go port or implement Jaro-Winkler inline. MinHash via the `github.com/dchest/siphash` family. Run during wiki rebuild. |
| **Three confidence labels everywhere** (EXTRACTED/INFERRED/AMBIGUOUS) | All modules | Aura today has `confidence` as a freeform string. Replace with this enum. Surface AMBIGUOUS in dashboard as a "review queue". |
| **Composite surprise score for cross-doc edges** | `analyze.py:177-248` | New "non-obvious connections" widget in dashboard. Same scoring formula. |
| **Suggested-questions generator** | `analyze.py:402-524` | The 5-category question generator works on any graph — port as `internal/wiki/questions.go`. Useful in onboarding ("what should I ask Aura?") and dashboard "discover" view. |
| **Token-budget bisection retry** | `llm.py:683-810` | Aura's LLM client already has retry but not signal-driven bisection. Lift the recursive halving pattern for any future Aura ingest pipeline (xlsx breakdown, PDF extraction). |
| **Hollow-response detection** | `llm.py:194-212` | Aura's local llama.cpp setup will hit this. Empty content / null nodes / null edges → treat as truncation. |
| **Manifest with separate ast_hash and semantic_hash** | `detect.py:864-1019` | Aura's wiki rebuild today is binary; this lets it re-run cheap structural extraction independently of expensive LLM extraction. |
| **Cross-community deduplication of surprises** | `analyze.py:390-399` | Without this, one bridge node dominates results. Same fix needed wherever Aura surfaces "interesting connections". |
| **Direction-preserving `_src/_tgt` edge attrs** | `build.py:182-185` | Aura's wiki edges via `related:` are inherently undirected; this is the trick to add direction without changing storage format. |
| **`prefix_graph_for_global` for multi-corpus merge** | `build.py:366-379` | Future: when Aura joins multiple users' wikis (B2B marketplace direction), prefix-then-merge is the pattern. |

---

## Top 5 lifts for Phase-WIKI-B (prioritised by impact-per-LOC)

### 1. Heading-level node decomposition for wiki markdown (HIGHEST IMPACT)

- **Source**: `extract.py:4837-4944`
- **Aura target**: `internal/wiki/index.go` add `extractHeadings()` that
  produces sub-nodes per heading (H1/H2/H3) with `parent_slug` and
  `heading_path` (`["Clienti", "Delta Automazioni"]`).
- **Effort**: ~150 LOC Go + index migration.
- **Why**: solves the "dammi il codice cliente di delta automazioni"
  case directly. BFS today returns the whole `clienti.md` page; with
  heading decomposition BFS returns the `## Delta Automazioni`
  subtree, which is small enough to drop into the LLM context whole.
- **Risk**: medium — needs care with how the existing `related:`
  resolution chains across parent/child heading nodes.

### 2. Entity dedup pipeline (HIGH IMPACT)

- **Source**: `dedup.py` entire file.
- **Aura target**: new `internal/wiki/dedup.go` runs during rebuild,
  before `BuildVectorIndex`.
- **Effort**: ~250 LOC Go. MinHash from `github.com/dchest/siphash`,
  Jaro-Winkler hand-rolled or via `github.com/agnivade/levenshtein`
  cousin libs.
- **Why**: today Aura silently maintains "Delta Automazioni" and
  "delta automazioni srl" as separate pages — BFS misses cross-page
  edges entirely. One pass at index time fixes this for all future
  retrieval.
- **Risk**: low — graphify's guards (entropy gate, short-label block,
  variant suffix detect) translate directly. The cross-repo guard is
  irrelevant for Aura.

### 3. Community detection + community overlay pages (HIGH IMPACT)

- **Source**: `cluster.py` + `wiki.py:_community_article`.
- **Aura target**: new `internal/wiki/cluster.go` (Leiden in Go — use
  `github.com/petar/GoLLRB` adjacency or call out to a Python sidecar
  one-shot during rebuild). Generate `_community-<slug>.md` pages with
  the schema from `wiki.py:37-102`.
- **Effort**: ~200 LOC Go if calling out to Python; ~500 LOC if pure
  Go (Leiden is moderately complex but Louvain is simpler and
  good-enough as fallback).
- **Why**: community pages are natural BFS entry points for broad
  questions ("dimmi tutto sui clienti del settore automazioni") and
  let the agent answer broad questions without expanding to thousands
  of nodes.
- **Risk**: medium — Leiden quality matters; resolution tuning will
  need experiment.

### 4. Discrete confidence-score rubric on `related:` edges (LOW EFFORT, HIGH SIGNAL)

- **Source**: `skill.md:350-430` (the confidence rubric) +
  `analyze.py:177-248` (how it's used downstream).
- **Aura target**: `internal/wiki/page.go` schema update + writer
  prompt update.
- **Effort**: ~50 LOC + prompt change + migration script for existing
  pages.
- **Why**: today Aura's confidence field is a number from 0 to 1
  generated freeform. The discrete rubric (0.95/0.85/0.75/0.65/0.55,
  never default to 0.5) forces models to commit and gives consistent
  downstream signal for ranking.
- **Risk**: very low — additive change.

### 5. Suggested-questions generator (MEDIUM IMPACT, LOW EFFORT)

- **Source**: `analyze.py:402-524`.
- **Aura target**: new `internal/wiki/questions.go` invoked from a new
  `/api/wiki/discover` endpoint and surfaced in the dashboard sidebar.
- **Effort**: ~120 LOC Go. Betweenness centrality already in
  `internal/wiki/GraphIndex` (or add via NetworkX-equivalent).
- **Why**: huge UX win for "I don't know what to ask Aura". Five
  categories (ambiguous edges, bridge nodes, unverified inferred, isolated
  nodes, low-cohesion communities) all derivable from existing wiki +
  community data once #3 ships.
- **Risk**: very low — pure analytics, no storage changes.

---

## What NOT to lift

| Anti-pattern | Why skip |
|--------------|----------|
| **Tree-sitter based per-language extractors** (`extract.py:2091-6660`) | Aura's "wiki" is not source code. The 30+ language extractors are dead weight for Aura. Markdown extractor is the only useful one. |
| **Neo4j export / Cypher generation** (`export.py`) | Aura's graph IS the markdown wiki — that's the project core (see MEMORY.md). Adding Neo4j is the opposite of project strategy. |
| **GraphML / Gephi export** | Same reason. Aura has a dashboard already; external graph viewers are a distraction. |
| **MCP stdio server** (`serve.py:381-`) | Aura already exposes wiki queries via its own HTTP API and Telegram. Adding MCP-stdio is duplicate surface. |
| **Whisper video transcription** (`transcribe.py`) | Aura has its own audio stack (Wave 3 TTS + Whisper for STT) — different architecture entirely. |
| **Google Workspace converter** (`google_workspace.py`) | Aura should add this through MCP servers later (per the MCP roundup milestone), not as a bespoke pipeline. |
| **Cross-repo prefix-and-merge** (`build.py:366-379`) | Useful much later (B2B marketplace direction), not for Phase-WIKI-B. |
| **`xlsx_extract_structure`** (`detect.py:251-338`) | Currently behind a feature flag and NOT wired into the dispatcher. F-035 warns about zip/XML bomb risk via openpyxl. Aura's existing markitdown sidecar (Wave 2.9) is the better path for xlsx. |
| **Direct API backends in `llm.py`** | Aura already has its own OpenAI-compatible client with retry, streaming, and embedding endpoints. Don't duplicate. |
| **`_call_claude_cli` shell-out** (`llm.py:423-486`) | Aura is the agent runtime, not a Claude Code consumer. |
| **`.graphifyignore` full gitignore implementation** (`detect.py:464-650`) | Aura indexes a curated wiki dir, not an open user-tree. Overkill. |
| **`graphify-out/converted` sidecar dir for office files** (`detect.py:341-367`) | Aura's source-ingestion pipeline (`internal/storage/sources`) is already cleaner. |
| **Per-language `LanguageConfig` dataclass** (`extract.py:156-197`) | Only useful if Aura ever ingests code, which is not in scope. |

---

## Surprising finding (single biggest insight)

**Graphify clusters at INGEST time but does NOT use the community
membership at QUERY time.** `serve.py:_query_graph_text` does pure
BFS/DFS over the LLM-named seed nodes, never biased by community. The
community structure is exposed only as: (a) a separate `get_community`
MCP tool, (b) the `surprising_connections` analysis ("cross-community =
surprising"), (c) the suggested-questions generator, and (d) the wiki
article filenames.

This means **graphify itself has not closed the community-aware retrieval
loop** — that lift is open territory for Aura. The shape of the lift is:
during BFS, when crossing a community boundary, decay the token budget
faster (because cross-community jumps are usually noisy); when staying
inside a community, expand more aggressively (because intra-community
edges are higher-cohesion). The cohesion score is already computed per
community — use it as the decay weight directly. This is the natural
Phase-WIKI-B retrieval enhancement that graphify designed everything
for but never built.

Secondary surprise: **markdown extraction is regex-only, no tree-sitter,
and it's the single most directly portable extractor** — 100 lines of
Python, would be ~150 lines of Go. The extractor that matters most for
Aura is the simplest one in the entire 6660-LOC `extract.py`.
