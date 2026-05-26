# Aura Tool And Function Map

Role: reference.

This document is the tested routing map for Aura's LLM-visible tools, memory
layers, and wiki graph. It is intentionally written as a contract: when the
runtime tool allowlist or action enums change, update this file and the
matching regression test in the same slice.

## Sources

- Runtime allowlist: `compose.yaml` / `AURA_TOOL_ALLOWLIST`.
- Tool registry and schemas: `internal/agent/tools/registry/`.
- Runtime wiring: `cmd/aura/app.go`, `cmd/aura/app_wire.go`,
  `cmd/aura/mcp_runtime.go`.
- Toolset presets: `internal/agent/tools/sets/toolsets.go`.
- Skills roots and loading: `cmd/aura/helpers.go`,
  `internal/skills/loader.go`.
- Memory collections: `internal/storage/memoryindex/collections.go`,
  `internal/storage/memoryindex/store_core.go`.
- Conversation archive: `internal/conversation/archive.go`.
- Wiki graph: `internal/wiki/graph_index.go`, `internal/wiki/store_graph.go`,
  `internal/wiki/store_writes.go`.
- Prompt health metrics: `internal/db/migrations/m29_add_prompt_health_views.go`.

## Mental Model

```text
user turn
  -> stable system prompt + small runtime capsules
  -> decide the needed layer
     -> current conversation/context: already in prompt
     -> durable knowledge: search
     -> source bytes: source or search(action=read, slug=src_...)
     -> local files/skills: file, skill, tool_search
     -> public web: web
     -> calculation: mcp_calculator_*
  -> mutate only through the owner tool
     -> wiki_page for curated wiki pages
     -> source/create_document for source artifacts
     -> propose_patch for reviewed memory/write proposals
     -> task for schedules
     -> file only for workspace files and local skill authoring
  -> ask_user only for missing slots, approval, or real ambiguity
  -> text_response terminates with the user-visible answer
```

The system prompt should not describe every schema field. It should teach this
routing map, then trust the tool schemas as ground truth.

## Active Manifest

The production container allowlist is the source of truth for normal chat turns:

- `text_response`
- `agent_note`
- `web`
- `file`
- `wiki_page`
- `source`
- `create_document`
- `skill`
- `task`
- `propose_patch`
- `search`
- `ask_user`
- `tool_search`
- `mcp_calculator_*`

Dynamic AGENTDEF delegation can additionally expose `delegate_*` tools for the
active archetype. These are not registered in the base registry; they are
synthesized per turn from `internal/agent/agentdef/delegate.go`.

Retired or folded names must not be advertised as active tools:
`execute_code`, `execute_shell`, `doc`, `dev_tool`, `daily_briefing`,
`subagent_dispatch`, `read_tool_result`, `request_dashboard_token`,
`recall_operational`, `recall_user_memory`, `recall_god_nodes`,
`wiki_subgraph`, `wiki_path`, and `ask_user_clarification`.

## Tool Map

### `text_response`

- Purpose: terminal user-visible reply.
- Use when: the answer is ready and no more tool evidence is needed.
- Do not use for: intermediate notes, hidden reasoning, or tool output storage.
- Actions: none; required field is `text`.
- Source: `internal/agent/tools/registry/text_response.go`.

### `ask_user`

- Purpose: pause the run for a clarification, approval, or structured choice.
- Use when: a required slot is missing, a risky action needs approval, or two
  interpretations would materially change the result.
- Do not use for: routine progress updates or questions that can be answered by
  reading existing memory/tools.
- Actions: `clarification`, `approval`, `choice` (kind values; no action enum).
- Source: `internal/agent/tools/registry/ask_user.go`.

### `search`

- Purpose: read-only access to Aura knowledge and wiki graph.
- Use when: the agent needs durable memory, wiki pages, source snippets, graph
  neighborhoods, graph paths, graph diffs, gaps, surprises, suggested questions,
  user facts, or operational lessons.
- Do not use for: changing wiki/source/memory state.
- Actions: `search`, `list`, `read`, `lessons`, `user_facts`, `god_nodes`,
  `subgraph`, `path`, `diff`, `gaps`, `surprises`, `suggest_questions`.
- Source: `internal/agent/tools/registry/search.go`.

### `web`

- Purpose: current public web search and URL fetch.
- Use when: information is current, external, news-like, or not in Aura memory.
- Do not use for: local wiki/source/memory reads.
- Actions: `search`, `fetch`.
- Source: `internal/agent/tools/registry/web.go`.

### `tool_search`

- Purpose: fetch full schemas for deferred tools.
- Use when: a deferred tool is named in the compact manifest but its full schema
  is not present in the current LLM request.
- Do not use for: knowledge retrieval or user memory search.
- Actions: none; query forms are `select:<name>`, keyword search, and
  `+required_term` keyword search.
- Source: `internal/agent/tools/registry/tool_search.go`.

### `agent_note`

- Purpose: per-conversation scratchpad for the agent's own working memory.
- Use when: the task is multi-step and needs a private checklist, plan, or
  intermediate findings within this conversation.
- Do not use for: durable user memory, wiki knowledge, sources, or audit facts.
- Actions: `set`, `append`, `get`, `clear`.
- Source: `internal/agent/tools/registry/agent_note.go`.

### `wiki_page`

- Purpose: owner path for curated wiki page mutation.
- Use when: creating, replacing, editing, or appending semantic wiki knowledge.
- Do not use for: source artifact storage, raw chat logs, raw tool failures, or
  scratchpad state. Search/read before creating a page so existing slugs are
  reused.
- Actions: `create`, `replace`, `edit`, `append`.
- Source: `internal/agent/tools/registry/wiki.go`.
- Note: the implementation tolerates `read`/`view`/`get`/`show` as a
  compatibility passthrough, but the prompt should route reads through
  `search(action="read")`.

### `source`

- Purpose: owner path for uploaded or ingested raw source artifacts.
- Use when: listing, reading, storing text/URLs, reprocessing OCR/ingest,
  deleting, or linting sources.
- Do not use for: final document generation; use `create_document` for new
  PDF/XLSX/DOCX artifacts.
- Actions: `list`, `read`, `store`, `reprocess`, `delete`, `lint`.
- Source: `internal/agent/tools/registry/source_unified.go`.

### `create_document`

- Purpose: create PDF, XLSX, or DOCX artifacts in the source store.
- Use when: the user asks for a generated file/document/spreadsheet.
- Do not use for: editing arbitrary workspace files or verifying the artifact
  by doing another list/read call just because generation succeeded.
- Actions: `pdf`, `xlsx`, `docx` (format values; no action enum).
- Source: `internal/agent/tools/registry/create_document.go`.

### `skill`

- Purpose: installed skill lifecycle and catalog access.
- Use when: listing installed skills, searching the catalog, inspecting one
  installed skill, installing a catalog skill, or removing one.
- Do not use for: authoring a brand-new local `SKILL.md`. Local authoring is a
  workspace file operation and must create `<skills-root>/<name>/SKILL.md` with
  valid frontmatter.
- Actions: `list`, `catalog`, `info`, `install`, `remove`.
- Source: `internal/agent/tools/registry/skill.go`.

### `task`

- Purpose: schedule, inspect, cancel, or manually run saved tasks.
- Use when: the user asks for reminders, recurring jobs, maintenance schedules,
  or a manual fire of a saved task.
- Do not use for: immediate normal chat work; just do the work directly.
- Actions: `schedule`, `list`, `cancel`, `run_now`.
- Source: `internal/agent/tools/registry/scheduler.go`.

### `propose_patch`

- Purpose: reviewed mutation path for wiki and user memory proposals, plus
  gated operational lesson promotion.
- Use when: the agent has a candidate durable memory/write improvement but
  should not directly mutate the canonical store.
- Do not use for: scratchpad notes, source artifacts, or direct wiki writes that
  the user explicitly asked to make now through `wiki_page`.
- Actions: `wiki`, `user_memory`, `operational`.
- Source: `internal/agent/tools/registry/propose_patch.go`.

### `file`

- Purpose: safe workspace filesystem operations.
- Use when: reading/writing local workspace files, searching code/docs, editing
  local skills, or doing bounded file management.
- Do not use for: semantic wiki mutations that should refresh backlinks and
  indexes; use `wiki_page` unless deliberately editing a control file.
- Actions: `list`, `read`, `search`, `write`, `patch`, `grep`, `path_info`,
  `mkdir`, `rmdir`, `remove_file`, `move`, `copy`, `walk`, `pwd`.
- Source: `internal/agent/tools/registry/file.go`.

### `mcp_calculator_*`

- Purpose: calculator MCP tools for math and symbolic/numeric computation.
- Use when: the user asks for arithmetic, algebra, statistics, or another math
  operation that should not require Python/shell exposure.
- Do not use for: general code execution or shell access.
- Actions: dynamic MCP tool names, exposed by `cmd/aura/mcp_runtime.go` from
  `/workspace/mcp.json`.
- Source: `runtime/mcp/aura-calculator-mcp`, `runtime-workspace/mcp.json`,
  `cmd/aura/mcp_runtime.go`.

### `delegate_*`

- Purpose: dynamic AGENTDEF delegation to a bounded child archetype.
- Use when: direct response and direct tools are insufficient, and the active
  archetype exposes an authorized subagent.
- Do not use for: hidden broad fanout, durable writes by children, or dumping
  child transcripts back into parent context.
- Actions: none; required field is `prompt`, optional field is `model`.
- Source: `internal/agent/agentdef/delegate.go`.

## Operational Prompt Capsules

These are mini system-prompt capsules for the active tool surface. They are
based on direct `/api/tools/call` probes run by Codex on 2026-05-26, without
asking Aura's chat model to decide anything. Tool schemas remain the ground
truth; these capsules teach routing, timing, and the gotchas that schemas alone
do not make obvious.

Direct API audit snapshot, 2026-05-26:

- `/api/tools` exposed 36 live registry tools: 13 core tools plus 23
  `mcp_calculator_*` tools.
- `tool_search(select:<tool>)` returned 57,059 bytes of full schema text across
  the live surface. Keep the largest tools deferred unless needed:
  `search` 5,589 bytes, `file` 4,685, `wiki_page` 4,679, `source` 4,127,
  `task` 3,960, `propose_patch` 3,559.
- Direct probes showed the first MCP-backed call can pay a warm/catalogue cost
  of about 2.2s. The runtime supervisor now keeps the 2s reconnect tick but
  throttles healthy MCP `tools/list` refreshes to 30s so schema discovery does
  not spam the sidecar.
- `wiki_page(create)` derives the slug from `title`; use the returned slug for
  follow-up `append`/`edit`/`search(read)`.
- `create_document` persists generated files as source artifacts. Use the
  returned `source_id` and `source(read, mode="metadata")` for metadata
  verification; the source-store file path is `original.<ext>`, not the display
  filename.
- `/api/tools/call` is a component test surface, not a full agent run. Direct
  calls now create a synthetic `runs` row with `channel="api"` and persist a
  metadata-only `tool_attempts` row. Benchmarks can assert the returned `run_id`
  plus `/api/maintenance/tool-attempts` or direct SQLite rows.
- Normal tool-layer misses, such as `skill(info)` for an unknown skill, now map
  to HTTP 404 with `tool_attempts.outcome="recoverable"`,
  `class="not_found"`, and `reason="not_found"`. Schema/argument errors map to
  HTTP 400 with `class="validation"` rather than the generic 500 path.

#### `text_response`

System prompt capsule:

> Use `text_response` exactly once when the user-visible answer is ready. Put
> only the final answer in `text`. Do not use it for scratch notes, hidden
> reasoning, or partial progress. After this call, stop calling tools.

Direct probe: passed with `text="codex-toolmap-direct-ok"` and returned that
text verbatim.

#### `ask_user`

System prompt capsule:

> Use `ask_user` only when progress genuinely requires user input: a missing
> slot, a risky approval, or a choice that materially changes the next action.
> Ask one specific question, include 2-4 options only when they are real
> choices, then wait. Do not use it for routine status updates or questions
> answerable through `search`, `source`, `file`, or `tool_search`.

Direct probe: the raw API returns an awaiting-user-input error; in a normal
agent run that is the expected pause boundary, not a reason to keep looping.

#### `search`

System prompt capsule:

> Use `search` as the read-only gateway to Aura's durable knowledge. Use
> `user_facts` for "what do you know about me?", `lessons` for operational
> tool experience, `god_nodes`/`subgraph`/`path` for wiki graph questions, and
> `read` for exact page/source lookup by slug. Do not mutate memory, wiki, or
> source state through this tool.

Direct probes passed for `search`, `list`, `read`, `lessons`, `user_facts`,
`god_nodes`, `subgraph`, `path`, `diff`, `gaps`, `surprises`, and
`suggest_questions`.

#### `web`

System prompt capsule:

> Use `web` only for current or external public information that Aura should
> not be expected to already know. Use `search` for discovery and `fetch` for
> one URL you need to quote or inspect. Prefer Aura memory first for personal,
> project, wiki, or source facts.

Direct probes passed for `fetch(url="https://example.com")` and
`search(query="example domain")`.

#### `tool_search`

System prompt capsule:

> Use `tool_search` before calling any deferred tool whose full schema is not
> already present in the request. Use `query="select:<tool_name>"` for an exact
> schema fetch. After reading the schema, call the target tool with its exact
> required field names; do not guess argument names from prose.

Direct probes passed for every active core tool and calculator MCP schema.

#### `agent_note`

System prompt capsule:

> Use `agent_note` as a private per-conversation scratchpad for multi-step
> work. Use `set` with `content`, `append` with `line`, `get` to reload, and
> `clear` when the scratchpad is no longer useful. Never use it as durable user
> memory, wiki knowledge, or audit evidence.

Direct probe gotcha: sending `note` instead of `content`/`line` returned success
but stored an empty note. The prompt must name the exact keys.

#### `wiki_page`

System prompt capsule:

> Use `wiki_page` only to create or mutate curated wiki pages. Search or read
> first and reuse an existing slug when possible. For creation, provide
> `title` and `body`; the slug is derived. For updates, use `replace`, `edit`,
> or `append` with the exact existing slug. Do not use `file` for ordinary wiki
> knowledge writes because wiki writes must refresh metadata, backlinks, and
> indexes.

Direct probes passed for `create`, `append`, `edit`; readback was verified
through `search(action="read")` using the slug returned by `create`.

#### `source`

System prompt capsule:

> Use `source` for raw evidence artifacts: list, read, store text/URL sources,
> reprocess ingest/OCR, delete sources, or lint the corpus. Use `source_id`
> exactly as returned. Remember that deleting a source removes source files and
> memory-index entries, but wiki pages that referenced it remain unchanged.

Direct probes passed for `list`, `store(kind="text")`, `read` in default,
`metadata`, `excerpt`, and `ocr` modes, `lint`, and `delete`.

#### `create_document`

System prompt capsule:

> Use `create_document` when the user asks Aura to generate a PDF, XLSX, or
> DOCX artifact. Always call it as `{"format":"pdf|xlsx|docx","spec":{...}}`;
> never put title/body/content at the top level. On success, report the
> returned `source_id` and filename. Do not call `source(read)` merely to
> verify generation unless the user asked for artifact inspection.

Direct probes passed for PDF, XLSX, and DOCX specs. Metadata readback and
source directory inspection worked; OCR/extract readback is not the
verification path for newly generated binary docs.

#### `skill`

System prompt capsule:

> Use `skill` to list installed skills, search the catalog, inspect a skill, or
> install/remove catalog skills when the user asks for that lifecycle action.
> `install` and `remove` are admin/capability-gated; if denied, explain the
> gate instead of retrying. Use `file`, not `skill`, to author a brand-new local
> `SKILL.md`.

Direct probes passed for `list`, `catalog(query="prompt")`, and
`info(name="aura-runtime-safety")`. A missing skill returns HTTP 404 and a
recoverable `tool_attempts` row; do not retry unless the name was wrong.

#### `task`

System prompt capsule:

> Use `task` only for saved future work: reminders, recurring jobs, wiki
> maintenance, or manual firing of a saved task. For `schedule`, provide
> `name`, `kind`, optional `payload`, and exactly one schedule field (`in`,
> `at_local`, `at`, `daily`, or `every_minutes`). Reusing a name updates the
> existing task. Use `cancel` by name to stop future fires.

Direct probes passed for `list`, `schedule(in="10m")`, and `cancel`.

#### `propose_patch`

System prompt capsule:

> Use `propose_patch` when a durable memory/wiki/operational change should be
> reviewed or provenance-gated instead of written directly. Use `wiki` for page
> proposals, `user_memory` for user facts/preferences that need review, and
> `operational` for validated tool lessons from a real run. Do not use it for
> scratchpad notes or direct user-requested wiki writes.

Direct probe gotcha: `operational` proposals from the raw direct API were
denied with `provenance: missing run_id`. Use this path inside real agent runs
where provenance exists.

#### `file`

System prompt capsule:

> Use `file` for safe workspace filesystem work under the tool workspace root:
> read, list, grep/search, write, patch, move/copy, and bounded cleanup. Keep
> paths relative, inspect before destructive edits, and use the owner tools for
> semantic stores (`wiki_page` for wiki knowledge, `source` for sources,
> `skill` for installed skill lifecycle).

Direct probes passed for `pwd`, `mkdir`, `write`, `read`, `grep`, `search`,
`path_info`, `walk`, `copy`, `move`, `remove_file`, and `rmdir`. The live root
was `/workspace`.

#### `mcp_calculator_calculate`

System prompt capsule:

> Use `mcp_calculator_calculate` for direct arithmetic and mathematical
> expressions. Use normal math exponentiation (`^`) or Python-style
> exponentiation (`**`); the calculator MCP normalizes `^` before evaluation.

Direct probes passed for arithmetic; `(2+3)^2` returned `25`.

#### `mcp_calculator_confidence_interval`

System prompt capsule:

> Use this for a confidence interval over numeric samples. Provide `data` as a
> numeric list and optional `confidence` such as `0.95`. Report the interval and
> the confidence level.

Direct probe passed with five samples and `confidence=0.95`.

#### `mcp_calculator_correlation_coefficient`

System prompt capsule:

> Use this for Pearson correlation between two numeric series. Provide
> `data_x` and `data_y` with matching lengths. Do not use it for regression
> parameters; use `mcp_calculator_linear_regression` for slope/intercept.

Direct probe passed with perfectly correlated samples.

#### `mcp_calculator_differentiate`

System prompt capsule:

> Use this for symbolic derivatives. Provide `expression` and, when useful,
> `variable` (usually `x`). Return the symbolic derivative exactly as the tool
> gives it.

Direct probe passed for `x^3`, returning `3*x**2`.

#### `mcp_calculator_expand`

System prompt capsule:

> Use this to algebraically expand symbolic expressions. Provide one
> `expression`; do not use it for numeric evaluation.

Direct probe passed for `(x+1)^2`.

#### `mcp_calculator_factorize`

System prompt capsule:

> Use this to factor symbolic expressions. Provide one `expression`; report the
> factorized form.

Direct probe passed for `x^2-1`.

#### `mcp_calculator_integrate`

System prompt capsule:

> Use this for symbolic indefinite integrals. Provide `expression` and the
> integration `variable`. If the user asks for bounds, use another tool or
> compute carefully from the returned antiderivative.

Direct probe passed for `2*x`.

#### `mcp_calculator_linear_regression`

System prompt capsule:

> Use this for slope/intercept over numeric `(x,y)` points. Provide `data` as a
> list of two-number points. Report both slope and intercept.

Direct probe passed for points on `y=2x`.

#### `mcp_calculator_matrix_addition`

System prompt capsule:

> Use this to add same-shaped numeric matrices. Provide `matrix_a` and
> `matrix_b` as lists of numeric rows.

Direct probe passed for two 2x2 matrices.

#### `mcp_calculator_matrix_determinant`

System prompt capsule:

> Use this for the determinant of a square numeric matrix. Provide `matrix` as
> rows.

Direct probe passed for a 2x2 matrix.

#### `mcp_calculator_matrix_multiplication`

System prompt capsule:

> Use this for matrix multiplication when inner dimensions match. Provide
> `matrix_a` and `matrix_b` as numeric row arrays, then report the resulting
> matrix.

Direct probe passed for 2x2 multiplication.

#### `mcp_calculator_matrix_transpose`

System prompt capsule:

> Use this to transpose a numeric matrix. Provide `matrix` as rows and return
> the transposed row arrays.

Direct probe passed for a 2x3 matrix.

#### `mcp_calculator_mean`

System prompt capsule:

> Use this for the arithmetic mean of a numeric list. Provide `data`; report
> the numeric result.

Direct probe passed for `[2,4,6,8]`.

#### `mcp_calculator_median`

System prompt capsule:

> Use this for the median of a numeric list. Provide `data`; report the numeric
> result.

Direct probe passed for `[1,9,3]`.

#### `mcp_calculator_mode`

System prompt capsule:

> Use this for the mode of a numeric list. Provide `data`; if multiple modes
> are possible, report exactly what the tool returns rather than inventing tie
> handling.

Direct probe passed for `[1,2,2,3]`.

#### `mcp_calculator_plot_function`

System prompt capsule:

> Use this when the user explicitly wants a quick function plot from the
> calculator MCP. Provide `expression` and optional numeric `start`, `end`, and
> `step`. Report the returned status plus `format`, `points`, and `bytes` when
> present; do not invent an image attachment if only metadata is returned.

Direct probe returned `Plot generated successfully.` with `format="png"`,
`points=5`, and a nonzero byte count.

#### `mcp_calculator_solve_equation`

System prompt capsule:

> Use this for one-variable algebraic equations in `x`. Provide `equation`
> with exactly one equals sign. Report all returned solutions.

Direct probe passed for `x^2 - 5*x + 6 = 0`.

#### `mcp_calculator_standard_deviation`

System prompt capsule:

> Use this for standard deviation over a numeric list. Provide `data` and
> report the numeric result; if the user needs population vs sample semantics,
> clarify or state the tool result as-is.

Direct probe passed for `[2,4,6,8]`.

#### `mcp_calculator_summation`

System prompt capsule:

> Use this for finite summations over `x`. Provide `expression`, `start`, and
> `end`; report the resulting sum.

Direct probe passed for summing `x` from 1 to 5.

#### `mcp_calculator_variance`

System prompt capsule:

> Use this for variance over a numeric list. Provide `data` and report the
> numeric result; clarify sample/population semantics when it matters.

Direct probe passed for `[2,4,6,8]`.

#### `mcp_calculator_vector_cross_product`

System prompt capsule:

> Use this for vector cross products. Provide `vector_a` and `vector_b` as
> numeric lists; the fork exposes normal variable-length numeric arrays.

Direct probe passed for `[1,0,0] x [0,1,0]`, returning `[0,0,1]`.

#### `mcp_calculator_vector_dot_product`

System prompt capsule:

> Use this for dot products over equal-length numeric vectors. Provide
> `vector_a` and `vector_b` as numeric lists and report the scalar result.

Direct probe passed for `[1,2,3] dot [4,5,6]`, returning `32`.

#### `mcp_calculator_vector_magnitude`

System prompt capsule:

> Use this for vector magnitude over a numeric list. Provide `vector` as a
> numeric list and report the scalar norm.

Direct probe passed for `[3,4]`, returning `5`.

## Toolsets And Roles

Toolsets are not LLM tools. They are runtime presets for limited contexts such
as cron and swarm:

- `memory_read`: `search`, `file`, `source`.
- `wiki_review`: `search`, `file`, `source`.
- `skills_read`: `file`.
- `web_research`: `web`.
- `scheduler_safe`: `search`, `file`, `source`, `web`.

Role presets:

- `librarian`: `search`, `file`, `source`.
- `critic`: `search`, `file`, `source`.
- `researcher`: `web`.
- `skillsmith`: `file`.
- `synthesizer`: `search`, `file`, `source`.

## Memory Layers

| Layer | Canonical store | Read path | Write path | Rule |
| --- | --- | --- | --- | --- |
| Active turn context | in-memory `conversation.Context`, LLM messages, run state | already in prompt | agent loop only | Short-lived runtime continuity, not durable truth. |
| Conversation archive | SQLite `conversations`, `conversation_compactions`, compact projection kind `archive` | `search(action="search")`, dashboard/API, prompt-health views | archive appender | Replay/debug evidence; tool turns stay out of compact memory. |
| Scratchpad | SQLite `agent_notes` | `agent_note(action="get")` | `agent_note` | Private per-conversation working memory. |
| User memory | `compact_memory_documents.kind='user_memory'` | `search(action="user_facts")` | `propose_patch(action="user_memory")` plus approval/promoter | Durable facts/preferences about the user. Ambiguity asks the user. |
| Operational memory | `compact_memory_documents.kind='operational'`, `data/operational_lessons.md` ingest | `search(action="lessons")`, prompt pinned lessons | `propose_patch(action="operational")`, opsfile watcher | Validated tool/procedure lessons, not raw failures. |
| Wiki | Markdown pages under `/workspace/wiki`, plus `wiki_documents` and Qdrant projections | `search`, `search(action="read")`, graph actions | `wiki_page` or wiki Store/API | Curated knowledge graph. Do not dump raw chat or scratchpad notes. |
| Sources | `/workspace/sources/src_*`, source store metadata, compact memory source chunks | `source`, `search(action="read", slug="src_...")` | `source`, `create_document`, upload pipeline | Raw evidence and generated artifacts. |
| Proposals | `proposed_updates`, mirrored/indexed proposal memory kind `proposal` where applicable | dashboard/API, memory proposal flows | `propose_patch`, summarizer applier | Review queue, not final truth until approved. |
| Skills | `/workspace/skills` and configured extra skill roots | skill manifest, `skill(action="info")`, `file` for local bodies | `skill` for catalog install/remove, `file` for local authoring | Procedural memory. A valid skill is a directory containing `SKILL.md` with frontmatter `name`. |
| MCP config | `/workspace/mcp.json`, runtime MCP server processes | API/MCP runtime, tool manifest | dashboard/file/admin flow | Defines dynamic MCP tools. Server install/bootstrap is product-owned. |
| Projections/cache | `projection_state`, FTS tables, Qdrant collections, embedding cache | retrieval layers | rebuild/reindex jobs | Acceleration only. Delete/rebuild must not change truth. |

## Wiki Graph Map

```text
/workspace/wiki/*.md
  -> wiki.Store.ReadPage / WritePage / DeletePage
  -> GraphIndex in memory
       page node: slug
       subnode: slug#heading-anchor
       body edge: [[slug]]
       typed body edge: [[slug|label]] plus ExtractWikiLinksTyped type
       frontmatter edge: related:
       provenance: sources: and ^[src_xxx]
  -> materialized graph files under /workspace/wiki/graph/
  -> search projections: wiki_documents FTS + aura_memory_v1 Qdrant
```

Routing:

- Known page or source id: `search(action="read", slug="...")`.
- Unknown subject: `search(action="search", query="...")`.
- Neighborhood: `search(action="subgraph", query="...", depth=1..3)`.
- Connection between pages: `search(action="path", from_slug="...", to_slug="...")`.
- Recent graph change: `search(action="diff", since="...")`.
- Graph maintenance: `search(action="gaps")`, `search(action="surprises")`,
  `search(action="suggest_questions")`, `search(action="god_nodes")`.
- Mutation: `wiki_page`; this preserves validation, atomic write, backlinks,
  `GraphIndex`, materialized graph refresh, and reindex submission.

Do not read a full graph dump for normal answers. Use bounded graph actions.

## Skill Routing Details

Runtime roots come from `skillSearchRoots`:

- `SKILLS_PATH` (`/workspace/skills` in the container).
- `.agents/skills`.
- `.claude/skills`.
- `SKILLS_PATH/.agents/skills`.
- `SKILLS_PATH/.claude/skills`.
- `SKILLS_INSTALL_PROJECT_DIR/.agents/skills`.
- `SKILLS_INSTALL_PROJECT_DIR/.claude/skills`.

Rules:

- `skill(action="list")` and `skill(action="info")` operate on installed
  skills that pass `internal/skills/loader.go` validation.
- A valid local skill must be:
  `<skill-root>/<skill-name>/SKILL.md`
  with YAML frontmatter containing `name`; `description` is optional but is the
  routing signal.
- Local creation/editing is done with `file`, not `skill`.
- Catalog install/remove is done with `skill`, gated by `SKILLS_ADMIN` and
  identity capabilities.

## Prompt Implications

The system prompt should contain:

- A short tool policy: tool schemas are ground truth; do not invent tools or
  parameters; fix named schema errors once; stop repeated retries.
- A routing policy: choose the memory/tool owner from this map.
- A memory policy: distinguish runtime, scratchpad, user memory, operational
  memory, wiki, sources, skills, proposals, projections, and cache.
- A wiki graph policy: wiki is a graph; use bounded graph actions; mutate via
  `wiki_page`.
- A skills policy: read skill bodies only when the manifest description applies;
  create local skills as filesystem `SKILL.md` files; use `skill` only for
  lifecycle/catalog operations.

The system prompt should not contain stale overlay names, retired tools, raw
schema dumps, or long operational logs.
