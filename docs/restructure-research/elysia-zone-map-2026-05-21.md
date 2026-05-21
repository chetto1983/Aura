# Elysia Zone Map — How It Tells the LLM Where Data Lives

Date: 2026-05-21
Source repo: `D:/tmp/elysia/` (Weaviate-backed agentic RAG, Python+DSPy)

## TL;DR

Elysia does **not** enumerate data zones in the system prompt. Instead it uses two orthogonal mechanisms: **(1) a tiny tool surface** — 7 tools total, parameterised by a `collection_names: list[str]` input so the *same* tool spans every "zone" — and **(2) runtime metadata injection** — a `data_information` field built from pre-processed per-collection summaries + field schemas, handed to the decision LLM as a separate input variable on every routing call. The map is **data-driven from Weaviate**, never hard-coded in the prompt; the LLM picks *which collection* to read, not *which tool reads collection X*.

## Counts

- **Tool count: 7 leaf tools** (`elysia/tools/`): `query`, `aggregate`, `visualise`, `query_postprocessing` (SummariseItems), `cited_summarize`, `summarize` / `text_response`, `final_text_response`.
- **Zone count: dynamic** — every Weaviate collection is its own "zone"; the LLM is told about them at runtime via metadata, not at compile-time via prompt. There is no hard-coded "wiki / source / memory" partitioning.
- **Branches: 2 in `multi_branch_init`** (`base`, `search`) — `tree.py:214-248`. Branches are LLM-routing groupings, not data zones.

## How Elysia communicates "where what is" to the LLM

### 1. System prompt is **zone-agnostic**

`elysia/tree/prompt_templates.py:5-27` — `DecisionPrompt` docstring talks only about "selecting the most appropriate next task" from `available_actions`. **No data zones named.** No tool-to-zone mapping. The prompt is a generic router.

### 2. Tools own *capabilities*, not *zones*

`elysia/tools/retrieval/query.py:54-71` — `Query` tool:
```
description = "Retrieves and displays specific data entries from the collections."
inputs = { "collection_names": "the names of the collections most relevant to the query" }
```
The same `query` tool works across **all** Weaviate collections; the LLM passes the collection list as input. Identical pattern for `aggregate` (`tools/retrieval/aggregate.py:37-57`). The zone is a *parameter*, not a *tool selector*.

### 3. Zone catalog is **injected as a runtime input**, not into the system prompt

`elysia/tree/prompt_templates.py:193-219` — `FollowUpSuggestionsPrompt.data_information`:
```
{ "name": collection name,
  "length": object count,
  "summary": collection summary,
  "fields": [{ "name", "groups" (distinct values + counts), "mean", "range", "type" }] }
```
Wired in `elysia/tree/util.py:670` via `tree_data.output_collection_metadata(with_mappings=False)`. The decision LLM gets a **fresh, data-driven, per-call** map of every zone, including distinct values per field so it knows which collection actually contains "marketing reports" vs "user messages" — without anyone hand-writing that mapping.

### 4. Per-collection summaries are LLM-generated, not human-curated

`elysia/preprocessing/collection.py:413-508` — `preprocess()` samples each collection, asks an LLM to write `summary` + `field_descriptions`, persists to a special `ELYSIA_METADATA__` Weaviate collection. Loaded back lazily in `tree/objects.py:398-470`. Result: zone descriptions are **regenerated** when data shape changes, never stale, never wrong.

### 5. Tool availability gating ≠ tool ownership

`tools/retrieval/query.py:81-95` — `is_tool_available()` returns False if no Weaviate client OR no collections selected. Tools self-gate based on *runtime state*, surfacing themselves in `available_actions` only when usable. The LLM never has to guess "is the wiki online?".

### 6. Decision-tree branches are about *flow*, not data ownership

`elysia/tree/tree.py:214-248` (`multi_branch_init`):
- `base` branch: text-response + summarize tools
- `search` branch (child of base): `Query` + `Aggregate`

Branches partition **what the agent does next** (search vs respond), not **which data store it reads**.

## Patterns Aura Should Lift

1. **Replace "one tool per zone" with "one tool per capability, zone as a parameter"** — Aura today has `search_memory`, `list_memory`, `search_wiki`, `list_sources`, etc. Pattern: collapse to `retrieve(zone=["wiki"|"source"|"workspace"|"memory"], query=…)` + `list(zone=…)`. The LLM picks the zone via a typed input, not by guessing among 8 similarly-named tools. Reference: `tools/retrieval/query.py:54-69`.

2. **Build a runtime `data_information` block, inject it on every turn** — pre-compute (or cache) a JSON map: `{ "wiki": {count, summary, top_categories, last_modified}, "sources": {count, summary, recent_titles}, "workspace": {file_count, recent_files}, "memory": {note_count, top_tags}, "tasks": {pending, scheduled}, "web": {description: "live web search via SearXNG"} }`. Pass this as a separate prompt variable on every agent call. The LLM then has ground truth without a fragile prompt list. Reference: `tree/prompt_templates.py:193-219` + `tree/util.py:670`.

3. **LLM-generated zone summaries, refreshed on data drift** — for `wiki` and `sources`, periodically have an LLM regenerate the `summary` + `top_groups` based on a recent sample. Cache in SQLite (Aura already has this pattern for embeddings). When the wiki grows from 50 → 500 pages, the zone summary auto-updates. Reference: `preprocessing/collection.py:413-508`.

4. **`is_tool_available()` self-gating** — Aura tools should expose a runtime `Available()` check. `read_skill` is unavailable if no skills installed. `search_web` is unavailable if SearXNG is down. The agent loop strips unavailable tools from the manifest sent to the LLM. Already half-present in Aura (MCP gating) — generalise. Reference: `tools/retrieval/query.py:81-95`.

5. **Hierarchical branches for flow control, not zones** — `multi_branch_init` (`tree.py:214-248`) groups `query`+`aggregate` under a `search` parent. For Aura: group `retrieve`+`web_search` under "gather", `create_xlsx`+`create_docx`+`create_pdf` under "produce", `workspace_write`+`schedule_task` under "act". The decision LLM picks a category first, then a leaf. Smaller per-step decision surface = better routing on weak local LLMs.

## Anti-Patterns to Avoid

1. **Do NOT hard-code zone→tool maps in the system prompt** — Elysia explicitly avoids this. A prompt that says "use `read_wiki` for wiki content, `read_source` for sources" rots the moment you add a new zone or rename a tool, and the LLM still gets confused under paraphrase. Use the data-driven `data_information` injection pattern instead.

2. **Do NOT confuse decision-tree branches with data partitioning** — Elysia's `search` branch is not "the search-data zone"; it is "the action category where the agent gathers info". Conflating the two (tempting in Aura, e.g., "wiki branch", "source branch") fragments the tool tree without helping routing, and forces every new tool to be classified into an arbitrary bucket. Keep zone as a parameter, branches as flow categories.

---

Counts/refs verified by reading: `tree/prompt_templates.py` (265 LOC, entire), `tree/tree.py:214-280`, `tree/objects.py:380-545`, `tree/util.py:650-678`, `tools/retrieval/query.py:34-95`, `tools/retrieval/aggregate.py:30-95`, `tools/text/text.py` (entire), `tools/postprocessing/`+`tools/visualisation/` (tool name scan), `docs/creating_tools.md`, `preprocessing/collection.py:261-510`.
