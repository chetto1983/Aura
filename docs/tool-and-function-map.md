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
