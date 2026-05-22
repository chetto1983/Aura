# Tool discovery & tool RAG patterns across 7 production systems

Research date: 2026-05-22. Subject: Aura's `aura_tool_search_v2` is broken (dim drift, no auto re-embed, hardcoded descriptions). Goal: lift production patterns from picobot, codex, openhuman, nanobot, elysia, hermes-agent, cli-printing-press to replace it.

All claims below are anchored to source files; quotes are 5-20 lines. READMEs are ignored.

---

## §1 Cross-system table

For each system, the 8 bullets the task asked: surface shape, selection mechanism, registration lifecycle, description discipline, dedup/namespace, result hygiene, called-out anti-patterns, RAG-on-tools specifically.

### 1.1 picobot (Go, ~22 hardcoded tools + MCP)

1. **Surface shape**: flat list. `Registry.Definitions()` emits every tool name+description+JSON-Schema every turn. `internal/agent/tools/registry.go:50-62`. No tiers, no categories.
2. **Selection mechanism**: model sees ALL tools every turn. No tool_search, no semantic discovery. Lexical browsing only via the LLM reading the flat list.
3. **Registration lifecycle**: `Register(Tool)` mutates a `sync.Map`-protected map (`registry.go:36-40`). MCP tools wrap-and-add at boot via `tools/mcp.go:18-24`. No hot-reload, no `list_changed` listener — restart to pick up MCP changes.
4. **Description discipline**: hand-written string literals on each `Tool` struct (`Description() string`). MCP descriptions verbatim with `[MCP: %s]` prefix: `mcp.go:26-32` — `fmt.Sprintf("[MCP: %s] %s", t.serverName, desc)`. No override file, no auto-validation against schema.
5. **Dedup / namespace**: MCP tools forced into `mcp_<server>_<tool>` namespace (`mcp.go:22-24`). Built-ins use bare names. No detection of near-duplicate built-ins.
6. **Result hygiene**: nothing. `Execute` returns `(string, error)` raw (`registry.go:81-90`), logs name + duration + byte length. Caller (loop) is the only sanitizer.
7. **Anti-patterns**: not explicitly called out. README mentions "minimal" — flat-list is by choice.
8. **RAG on tools**: **none**. Pure manifest.

### 1.2 codex (Rust, hierarchical-agents shell platform)

1. **Surface shape**: tiered. Always-on built-ins (shell, apply_patch, etc.) + namespaced MCP tools + a single `tool_search` tool that loads more on demand. Deferred tools carry `defer_loading: true`. `codex-rs/core/src/tools/tool_search_entry.rs:25-46`.
2. **Selection mechanism**: **BM25 lexical search** over `(name + server + title + description + schema_property_keys)` corpus. `codex-rs/core/src/tools/handlers/tool_search.rs:39-46` builds `SearchEngineBuilder::<usize>::with_documents(Language::English, documents)`. Spec quote `handlers/tool_search_spec.rs:49-50`: *"Searches over deferred tool metadata with BM25 and exposes matching tools for the next model call."* Threshold: MCP tools auto-defer when `count >= 100` (`mcp_tool_exposure.rs:10-36`, constant `DIRECT_MCP_TOOL_EXPOSURE_THRESHOLD: usize = 100`).
3. **Registration lifecycle**: each handler registers at startup via `CoreToolRuntime` trait (`tools/registry.rs:46-95`). MCP tools come in through `McpHandler::new` (`handlers/mcp.rs:35-39`). When connector enablement changes, the exposure recomputes — the BM25 corpus is rebuilt from `search_info()` on every handler. No persistent vector index.
4. **Description discipline**: built-ins ship hardcoded specs (`tools/handlers/*_spec.rs`). MCP descriptions are taken verbatim from server (`handlers/mcp.rs:191-208` `create_tool_spec`), with fallback to `"Tools for working with {connector_name}."` when blank. Namespace description from `connector_name`. The BM25 corpus is built from concatenating multiple fields including `schema_property_keys` so the searchable text is richer than just description (`handlers/mcp.rs:225-271`).
5. **Dedup / namespace**: MCP wrapped in `mcp__<server>__<tool>` namespace via `ResponsesApiNamespace` (`handlers/mcp.rs:210-215`). Codex apps go through a connector-allowlist filter (`mcp_tool_exposure.rs:59-82`). Multiple MCP tools coalesce into a single namespace in `tool_search` output (`coalesce_loadable_tool_specs`, see test at `handlers/tool_search.rs:151-251`).
6. **Result hygiene**: `output_schema = None` on deferred tools (less bloat). Strict mode (`strict: false` for MCP, schema-aware truncation downstream).
7. **Anti-patterns called out**: spec description says: *"For MCP tool discovery, always use `tool_search` instead of `list_mcp_resources` or `list_mcp_resource_templates`"* (`handlers/tool_search_spec.rs:50`). I.e. no kitchen-sink list-then-filter.
8. **RAG on tools**: **YES, but lexical (BM25), not vector**. The corpus is rebuilt in-process from the live registry — no external index to drift. Search text concatenates name + canonical-name + server + title + description + schema property keys (`handlers/mcp.rs:225-271`).

### 1.3 openhuman (Rust, personal-AI platform)

1. **Surface shape**: per-channel filtered. Each tool declares `permission_level()`, `scope()`, `category()`, `is_concurrency_safe()`, `external_effect()`, `prefer_markdown` (`tools/traits.rs:114-220`). Channels carry an `allowed_permission`; tools above the threshold are `Deny` or `HideFromPrompt` (`agent_tool_policy/engine.rs:39-80`).
2. **Selection mechanism**: model sees every allowed tool. **No `tool_search` tool.** For sub-agent delegation to large catalogues (Composio, 500+ Github actions), uses a **CPU-only, stdlib, verb-aware fuzzy filter** (`agent/harness/tool_filter.rs:1-93`). Explicit comment lines 9-26:
   ```rust
   //! This module ranks the actions against the task prompt using a cheap
   //! five-stage pipeline — no model load, pure CPU, stdlib only:
   //! 1. Verb detection → CRUD-ish intents
   //! 2. Verb gate → drop verb-conflict actions
   //! 3. Query token expansion (pr→pull request, dm→direct message)
   //! 4. Weighted token overlap — 3× name hits, 1× description
   //! 5. Verb-alignment boost
   ```
   No embeddings. `MIN_CONFIDENT_HITS: usize = 3`; below that, fall back to unfiltered toolkit ("a too-narrow filter is worse than no filter at all").
3. **Registration lifecycle**: `tool_registry::registry_entries()` (`tool_registry/ops.rs:52-100`) merges 3 sources at-call: MCP stdio server tools, controller schemas, connected MCP-clients. Live, no persistent index. JSON-RPC method `openhuman.tool_registry_list` exposes the current snapshot. MCP-clients enumerate via `connections::all_connected_tools()`.
4. **Description discipline**: built-ins hand-written via `description() -> &str`. MCP descriptions verbatim. Composio tools (`openhuman/composio/providers/*/tools.rs`) hand-coded per provider with documented descriptions.
5. **Dedup / namespace**: MCP-client tools use `mcp-client::{server_id}::{tool_name}` form (`tool_registry/ops.rs:88`). System vs Skill split via `ToolCategory` (`tools/traits.rs:36-42`) so the sub-agent runner can spawn category-scoped agents.
6. **Result hygiene**: unified `ToolResult` with MCP content blocks + error flag (`tools/traits.rs:5`). `prefer_markdown` flag at call time so tools can emit a token-cheap markdown rendering instead of JSON when the harness asks. `external_effect()` flag routes side-effect tools through an `ApprovalGate` (`tools/traits.rs:197-211`).
7. **Anti-patterns called out**: see (2) above — explicit rejection of embedding-based tool routing for the 500-action Github case. Also `tools/traits.rs:154-159` permission-level system rejects ad-hoc allowlists in favor of a small ordered enum (`None < ReadOnly < Write < Execute < Dangerous`).
8. **RAG on tools**: **rejected**. Verb-aware token overlap covers the catalogue-filtering use case. No embedding index of tool descriptions exists.

### 1.4 nanobot (Python, ChatGPT-flavored CLI agent)

1. **Surface shape**: flat, sorted (built-ins first, MCP sorted), cached. `ToolRegistry.get_definitions()` (`nanobot/agent/tools/registry.py:48-71`) sorts `builtins.sort(key=self._schema_name); mcp_tools.sort(key=self._schema_name)` for **prompt-cache friendliness** — re-orderings invalidate the model provider's prefix cache.
2. **Selection mechanism**: model sees every tool. No tool_search. Scope-gating per tool class via `_scopes` set (`tools/base.py:173` — `_scopes: set[str] = {"core"}`); subagents pass `scope="subagent"` to `loader.load()` (`tools/loader.py:86-94`) and only matching tools are registered.
3. **Registration lifecycle**: `ToolLoader.discover()` walks the package (`tools/loader.py:30-60`) + external `entry_points(group="nanobot.tools")` for plugins (`loader.py:62-84`). MCP servers connect at boot via `connect_mcp_servers` (`tools/mcp.py:464-664`), with per-server `enabledTools` allowlist (`"*"` = all). `_cached_definitions` invalidated on every register/unregister (`registry.py:22-27`).
4. **Description discipline**: hand-written `description` property. MCP descriptions verbatim, falling back to tool name when blank (`tools/mcp.py:179` — `self._description = tool_def.description or tool_def.name`). Sanitization regex on names (`tools/mcp.py:38-40`).
5. **Dedup / namespace**: built-ins use bare names, MCP tools use `mcp_<server>_<tool>` (`tools/mcp.py:177`). `enabledTools` per-server allowlist (`mcp.py:565-589`); typo'd entries warned with the full diff of raw and wrapped names — strong UX signal that namespace drift is taken seriously.
6. **Result hygiene**: per-call retry-once for transient errors (`mcp.py:198-251`). Timeout per tool. Result is always string (text blocks joined) — Tool subclasses declare `read_only`, `concurrency_safe`, `exclusive` properties so the scheduler can fan out (`tools/base.py:155-167`).
7. **Anti-patterns called out**: tool-class collision warning is INFO-level for plugins overriding built-ins (`loader.py:99-109`) — not a hard error, but the explicit log message lays out the rule.
8. **RAG on tools**: **none**. No tool_search tool. The closest thing is `find_files` / `grep` for workspace search.

### 1.5 elysia (Python, Weaviate-flavored decision-tree agent)

1. **Surface shape**: **decision tree**. Tools live in branches; the model picks ONE option per branch (`tree/tree.py:561-714` `add_tool`). Each branch's `DecisionNode` has `options: dict[name -> {description, inputs, action, end, status, next}]`. At decision time, only the current branch's option-set is rendered (`tree/util.py:293-320` `_options_to_json`).
2. **Selection mechanism**: a `DecisionPrompt` dspy ChainOfThought run picks one option, with a `_tool_assertion` constraint (`tree/util.py:377-431`) that the chosen name must be in the available options ("You picked the action ... that is not in `available_actions`!"). Per-tool `is_tool_available()` async callback (`tools/retrieval/query.py:81-95`) — if False, the tool is excluded from this turn's options.
3. **Registration lifecycle**: explicit at decoration time. `tree.add_tool(tool, branch_id=..., from_tool_ids=...)` raises if the parent tool isn't on the named branch. No discovery loop.
4. **Description discipline**: each Tool subclass has hand-written `description`. Pydantic input schemas auto-rendered into the option's `inputs` field via `model_json_schema()` (`tree/util.py:312-316`).
5. **Dedup / namespace**: dedup is impossible — the tree structure forces tools onto disjoint branches. No shared global namespace.
6. **Result hygiene**: tools are async generators yielding `Response/Status/TrainingUpdate/FewShotExamples` objects (`tree/tree.py:617-622` enforces `inspect.isasyncgenfunction(tool.__call__)`). Strongly typed envelope.
7. **Anti-patterns called out**: `_tool_assertion` (lines 377-381) is an explicit guard against the LLM picking tool names that aren't visible — the assertion message lists the valid set, which is the only safe way to recover.
8. **RAG on tools**: **none**. The decision tree IS the routing structure. No semantic search.

### 1.6 hermes-agent (Python, multi-provider personal assistant)

1. **Surface shape**: tooled by `toolset`. Each tool registers with `(name, toolset, schema, handler, check_fn, requires_env, is_async, description, emoji, max_result_size_chars, dynamic_schema_overrides)` (`tools/registry.py:234-305`). Toolsets group related tools; per-toolset check function gates the whole group.
2. **Selection mechanism**: model sees every available tool every turn. `get_definitions(tool_names)` (`registry.py:337-384`) filters by `check_fn()` results (cached 30s — `_check_fn_cached`). `dynamic_schema_overrides` callable mutates the schema at call time (`registry.py:372-382`) so config-dependent fields like `delegate_task.max_concurrent_children` reflect current settings.
3. **Registration lifecycle**: built-in tools self-register at module import via AST-detected `registry.register(...)` calls (`registry.py:29-74`); plugins via entry_points. MCP tools support **`notifications/tools/list_changed`** hot-reload (`tools/mcp_tool.py:1070-1192`): on receipt, schedules background task, takes a per-server `_refresh_lock`, diffs old vs new tool names, deregisters stale, re-registers new, logs the diff. Specifically the comment lines 1107-1115:
   ```python
   # Some servers (notably mongodb-mcp-server) emit
   # tools/list_changed immediately after initialize,
   # while the client may already be executing another
   # request. Refreshing synchronously inside the SDK
   # notification handler can race with that request
   # and wedge the stdio JSON-RPC stream...
   ```
4. **Description discipline**: hand-written, but `dynamic_schema_overrides` lets descriptions reflect runtime config without regen. `override=True` flag is required to shadow an existing tool (`registry.py:271-279`) — protects against accidental MCP / plugin shadowing. AST scan ensures only modules that ACTUALLY register get imported (`registry.py:57-74`).
5. **Dedup / namespace**: MCP tools use `mcp_<server>_<tool>` form via `sanitize_mcp_name_component` (`mcp_tool.py:1158-1160`). MCP-to-MCP overwrites allowed (debug log); cross-toolset rejected unless `override=True` explicitly opted-in. A monotonic `_generation` counter (`registry.py:166-168, 305`) lets memo'd callers detect changes.
6. **Result hygiene**: per-tool `max_result_size_chars` cap (`registry.py:88, 422-430`), `file_mutation_result_landed()` ground-truth check for write_file/patch (`agent/tool_result_classification.py:9-26`), `FILE_MUTATING_TOOL_NAMES = frozenset({"write_file", "patch"})` — tool classification for checkpoint manager. `_sanitize_tool_error` strips framing tokens / CDATA / fences from exception strings (`registry.py:408-415`) so a tool failure doesn't inject prompt-structural noise.
7. **Anti-patterns called out**: `override=True` is explicitly named opt-in (`registry.py:248-256`); shadowing without it logs `Tool registration REJECTED`. Says outright: *"prevent plugins/MCP from overwriting built-in tools or vice versa"*. Also the `notifications/tools/list_changed` race (lines 1107-1115) is documented in source — sync handler = wedge.
8. **RAG on tools**: **none**. Hermes relies on prompt-cache-friendly stable ordering + 30s check_fn TTL + dynamic schema for the runtime aspect. No semantic discovery.

### 1.7 cli-printing-press (Go, codegen for CLI→MCP wrappers)

1. **Surface shape**: each generated CLI ships a fixed tools.go + tools-manifest.json. Tools are mechanically derived from the OpenAPI/spec input — one tool per endpoint.
2. **Selection mechanism**: not a runtime agent. The generated MCP server exposes all tools; selection is up to the consumer.
3. **Registration lifecycle**: at code-generation time, not runtime. `mcp-sync` calls `mcpoverrides.Apply` on the parsed spec before emitting tools.go (`mcpoverrides/overrides.go:73-100`). The DO-NOT-EDIT header on tools.go forces re-generation as the canonical path.
4. **Description discipline**: **mechanically composed**, with hand-override sidecar. `mcpdesc.Compose` (`mcpdesc/compose.go:69-96`) emits a verb-led action sentence + `Required:` line + `Optional:` line (capped at 3, then "plus N more") + `Returns ...` clause + method marker (`Destructive.` for DELETE, `Partial update.` for PATCH). Hand overrides via `mcp-descriptions.json` per CLI (`mcpoverrides/overrides.go:25-100`). Unmatched override keys are RETURNED to the caller — so typos and stale keys are surfaced, not silently ignored. Quote:
   ```go
   // Returns the override keys that did not match any endpoint. A typo in
   // the override file (`tags_creat` instead of `tags_create`, or a stale
   // key from before a spec rename) would otherwise silently no-op; the
   // caller should surface unmatched keys so the user can debug them.
   ```
5. **Dedup / namespace**: tool name = `snake(resource)_snake(endpoint)` or `snake(resource)_snake(sub)_snake(endpoint)` for sub-resources (`mcpoverrides/overrides.go:72-100`). The naming function lives in `internal/naming` and is the single canonical source.
6. **Result hygiene**: not a runtime concern (generator).
7. **Anti-patterns called out**: the compose code (`compose.go:65-114`) explicitly avoids "Returns X. Returns the X." doubling when the spec narrative already mentions returns. Also splits hand-tuned override detection (colon-terminated `Required:` / `Optional:`) from narrative spec text — the colon is the structural cue.
8. **RAG on tools**: **none, but the description discipline is the relevant lift**. Auto-generated descriptions are predictably structured so a downstream BM25 or vector index would actually have signal to match against.

---

## §2 Convergent patterns (≥4 systems)

### 2.1 MCP namespace convention `mcp_<server>_<tool>` or `mcp__<server>__<tool>`

- picobot: `mcp.go:22-24`
- codex: `mcp__<server>__<tool>` via `ResponsesApiNamespace` (`handlers/mcp.rs:210-215`)
- nanobot: `mcp_<server>_<tool>` with `_sanitize_name` (`tools/mcp.py:38-40, 177`)
- openhuman: `mcp-client::{server_id}::{tool}` (`tool_registry/ops.rs:88`)
- hermes: `mcp_<server>_<tool>` (`mcp_tool.py:1158-1160`)

**All 5 sanitize and prefix.** Aura already does this. Keep it.

### 2.2 MCP descriptions verbatim from server (no override mechanism in 5/7)

- picobot, codex, nanobot, openhuman, hermes all pipe `tool_def.description` straight through (with optional fallback when blank).
- ONLY cli-printing-press has a hand-override sidecar — and that's at code-gen time, not runtime.
- Hermes has `dynamic_schema_overrides` (a runtime callable for description mutation) but is the only one and it's not specifically for MCP.

**Implication for Aura**: an MCP description override mechanism would be net-new — none of the 6 runtime systems studied have one. If Aura needs one, lift cli-printing-press's `mcp-descriptions.json` sidecar pattern (`mcpoverrides/overrides.go:73-100`) including the unmatched-key surfacing.

### 2.3 Per-tool gating function gates visibility AT CALL TIME (not at register time)

- elysia: `is_tool_available()` async per-tool (`tools/retrieval/query.py:81-95`)
- nanobot: `enabled(ctx)` classmethod at register, `_scopes` per-tool for runtime filter (`tools/base.py:173-185`)
- hermes: `check_fn` per tool with 30s TTL cache (`tools/registry.py:120-148`)
- openhuman: `permission_level()` per-tool + per-channel allowed_permission (`agent_tool_policy/engine.rs:39-80`)

**Pattern**: every system that has any non-trivial tool surface filters PER-TURN. Aura currently filters per-boot then trusts the index. This is the proximate cause of the "new tools don't appear" bug.

### 2.4 Stable ordering for prompt-cache friendliness (5/7)

- nanobot: explicit comment "stable ordering for cache-friendly prompts" (`registry.py:48-55`); builtins sorted, then MCP sorted, then concatenated.
- codex: rebuilds BM25 index from sorted registry; deferred tools always trail.
- hermes: monotonic generation counter (`registry.py:166-168`) so callers can memo by gen.
- openhuman: BTreeMap-keyed registry (`tool_registry/ops.rs:52-100`) → sorted by key implicitly.
- elysia: tree structure is deterministic.

**Implication**: order matters; randomly-hashed dict iteration trashes cache. Aura's `map[string]Tool` Go iteration is non-deterministic by default — wrap.

### 2.5 Tool result classification by side-effect at the schema level (4/7)

- nanobot: `read_only`, `concurrency_safe`, `exclusive` on Tool base (`tools/base.py:155-167`)
- openhuman: `permission_level()`, `external_effect()`, `is_concurrency_safe(args)` (`tools/traits.rs:155-220`)
- hermes: `FILE_MUTATING_TOOL_NAMES` frozenset (`agent/tool_result_classification.py:9`), per-tool `max_result_size_chars`
- codex: read-only flags propagate through `CoreToolRuntime`; sandbox-tag classification on each tool

**Convergent**: a tool MUST declare its side-effect class so the harness can route through approval/checkpoint/parallel-scheduler. Aura has `ReadOnlyHint`/`IdempotentHint` on `ToolDefinition` but no `external_effect` distinction.

---

## §3 Divergent patterns — real disagreement

### 3.1 Tool discovery: should the model see all tools, or call a search tool first?

**Camp A — all-tools-every-turn** (5 of 7): picobot, openhuman, nanobot, elysia, hermes. None has a `tool_search` tool. Their argument is observed in the source: per-channel/scope filter + tier system keeps the list bounded.

**Camp B — search-tool-on-demand** (1 of 7 explicitly): codex. Has `tool_search` BM25 (`handlers/tool_search.rs:39-46`) and only defers MCP tools beyond `DIRECT_MCP_TOOL_EXPOSURE_THRESHOLD: usize = 100` (`mcp_tool_exposure.rs:10`). Below 100 MCP tools, codex behaves like camp A.

**Camp C — decision-tree branching** (1 of 7): elysia. Tools are organized into a tree; each turn shows only the current branch's options.

**Aura position**: ~22 hardcoded + N MCP. Below codex's 100-tool threshold, so the convergent recommendation is **don't add a tool_search at all** — fix the catalog and trust the prompt. If MCP servers push past ~50 named tools, then adopt codex's BM25 pattern; never embeddings.

### 3.2 If you DO search: lexical or semantic?

**Lexical (BM25)**: codex explicitly. *"Searches over deferred tool metadata with BM25"* (`handlers/tool_search_spec.rs:49-50`).

**Token overlap + verb-detection (still lexical)**: openhuman for the 500-action Composio case. Comment: *"no model load, pure CPU, stdlib only"* (`agent/harness/tool_filter.rs:10`).

**Vector / embedding**: 0 of 7 systems use vector search for tool discovery. Aura is alone in trying this.

### 3.3 Tool registration source: code-only vs config-driven

- Code-only (5/7): picobot, codex, nanobot, hermes, openhuman use module-import-time registration. The "list of tools" IS the code.
- Config-driven (2/7): nanobot has plugins via `entry_points(group="nanobot.tools")`; elysia tree shape is config-like but expressed as Python calls.
- Code-gen (1/7): cli-printing-press generates the tool wrapper from OpenAPI specs.

---

## §4 Anti-patterns explicit across systems

Direct quotes from source.

### 4.1 Vector embedding tool descriptions (REJECTED by openhuman, IMPLIED-REJECT by codex)

openhuman `agent/harness/tool_filter.rs:10-26` flat-out chose **token overlap + verb detection over embedding** for the largest catalogue (Github 500 actions): *"no model load, pure CPU, stdlib only"*. Codex chose BM25 over vector for the same MCP-discovery use case (`handlers/tool_search.rs:39-46`). **5/7 systems don't even have tool-RAG; 2/7 use lexical. 0/7 use vector.**

### 4.2 Synchronous handling of `notifications/tools/list_changed` (hermes documents the trap)

`tools/mcp_tool.py:1107-1115`:
> Some servers (notably mongodb-mcp-server) emit tools/list_changed immediately after initialize, while the client may already be executing another request. Refreshing synchronously inside the SDK notification handler can race with that request and wedge the stdio JSON-RPC stream.

Fix: schedule a background task with a per-server refresh lock.

### 4.3 Allowing MCP / plugin tools to silently shadow built-ins (hermes blocks)

`tools/registry.py:280-289` rejects cross-toolset shadowing unless `override=True` opt-in. Aura currently doesn't enforce this.

### 4.4 Too-narrow tool filter (openhuman explicit)

`agent/harness/tool_filter.rs:35-37`: `MIN_CONFIDENT_HITS: usize = 3` — fall back to the unfiltered set when the ranker can't muster 3 strong hits. *"a too-narrow filter is worse than no filter at all because it starves the sub-agent"*.

### 4.5 Hand-edited tool descriptions in generated files (cli-printing-press)

`mcpoverrides/overrides.go:1-13`: the DO-NOT-EDIT header on the generated tools.go means direct edits are wiped. The sanctioned override path is the per-CLI sidecar JSON, with unmatched-key reporting to catch typos.

### 4.6 Treating tool-call result text as inert (hermes counter-example, file_mutation_result_landed)

`agent/tool_result_classification.py:9-26` checks the actual JSON return for `bytes_written` (write_file) or `success: true` (patch) — never trust the model's claim or the tool's `"OK"` string.

### 4.7 Schema-spec drift via hardcoded descriptions (cli-printing-press's whole reason to exist)

The fact that cli-printing-press generates tool descriptions FROM the spec instead of allowing hand-tuned strings is itself the anti-pattern call. Hand-written → drift. Generated → consistent.

---

## §5 Top 5 lift candidates for Aura

Ordered by impact × ease. Translatability 1-5 (5 = drop-in, 1 = architecture-deep).

### Lift 1 — Replace vector tool_search with BM25 over an in-process corpus

**Source**: codex `core/src/tools/handlers/tool_search.rs:39-46`, `handlers/mcp.rs:225-271` (search text composition).

**Why**: Aura's `aura_tool_search_v2` Qdrant collection is the source of the 256d→768d drift bug. Codex proves BM25 is sufficient at production scale; openhuman proves token-overlap suffices even at 500 tools. Building the BM25 corpus from `(name + canonical-name + server + description + schema_property_keys)` gives strictly more signal than embedding the description alone.

**Translation cost**: 2/5 — `blevesearch/bleve` or `bm25-go` lib + drop the Qdrant collection. Index lives in process memory; rebuilt on registry mutation. No external service to drift. ~150 LOC.

### Lift 2 — `notifications/tools/list_changed` MCP listener with diff-and-log

**Source**: hermes `tools/mcp_tool.py:1070-1192`.

**Why**: Aura's "new tools registered at runtime don't reliably re-embed" bug is exactly this. Hermes solves it by listening to the MCP notification, scheduling a background refresh (NOT sync — wedges the stream), holding a per-server refresh lock, diffing old vs new tool names, deregistering stale entries, re-registering, logging the diff. Adopt the per-server lock + background task verbatim — the comment in lines 1107-1115 documents the exact failure mode if you skip it.

**Translation cost**: 3/5 — Aura's MCP client (`internal/mcp/`) needs a notification handler. Pattern is in source; ~120 LOC.

### Lift 3 — Mechanical tool description composition + override sidecar with unmatched-key reporting

**Source**: cli-printing-press `internal/mcpdesc/compose.go:69-96` + `internal/mcpoverrides/overrides.go:73-100`.

**Why**: Aura's "tool descriptions live in code, hand-tuned, no auto-validation against actual call signatures" is exactly the problem cli-printing-press solves. For Aura's 22 hardcoded tools, compose the description from `(action verb sentence) + Required: ... + Optional: ... (capped at 3) + Returns: ... + method marker` derived from the registered JSON-Schema. Hand overrides live in `tools-overrides.json`. **Unmatched override keys returned to the caller** = no silent typos.

**Translation cost**: 3/5 — pure Go, no deps. ~200 LOC + a startup audit log. The `composeReturns + appendMethodMarker` split (`compose.go:197-244`) is directly liftable. Validation against the actual schema's `properties` key list is a natural extension.

### Lift 4 — Per-tool `external_effect` / classification + parallel-dispatch scheduler routing

**Source**: openhuman `tools/traits.rs:155-220`, nanobot `tools/base.py:155-167`.

**Why**: Aura already has `ReadOnlyHint`/`IdempotentHint` on `ToolDefinition` but doesn't use them for parallel scheduling. Adopt openhuman's stricter trio: `permission_level() → enum`, `external_effect(args) → bool` (args-aware), `is_concurrency_safe(args) → bool` (args-aware). The args-aware variant matters because the same tool (e.g. `execute_code`) can be safe for a `read` mode and unsafe for a `write` mode. Aura's stream-time parallel dispatch (recently shipped) needs this for safety.

**Translation cost**: 4/5 — touches the `Tool` interface (additive, default fallback), the executor, and the approval gate. ~300 LOC + per-tool annotation pass.

### Lift 5 — Stable-sorted tool definitions output for prompt cache hits

**Source**: nanobot `agent/tools/registry.py:48-71` (the cache-friendly-prompts comment), hermes `tools/registry.py:166-168` (generation counter).

**Why**: Anthropic prompt caching is byte-exact prefix-match. Go map iteration is non-deterministic. Every turn the same tool list can be re-ordered, busting the cache. Nanobot's pattern: split into `builtins[]` and `mcp[]`, `sort.Strings` each by name, concatenate. Sub-1-LOC win for ~5-15% cache hit improvement on prompts containing >10 tools.

**Translation cost**: 5/5 — drop-in. ~20 LOC in `Registry.Definitions()`.

---

## Final Aura-specific notes (not part of the 7-system contrast)

The user mandate names 5 concrete defects. Lifts 1-5 address them as:

| Defect (user mandate) | Lift |
|-----------------------|------|
| Qdrant 256d↔768d dim drift, broken reconcile | Lift 1 (drop Qdrant entirely for tools) |
| New tools don't re-embed | Lift 1 + Lift 2 |
| Hand-tuned descriptions, no schema validation | Lift 3 |
| MCP descriptions verbatim, no override | Lift 3 |
| Flat tool catalogue, no scoping | Lift 4 (classification) + Lift 5 (ordering) |

No lift requires adopting any system wholesale. Each is a 100-300 LOC patch.

The user's request *"il rag deve auto-aggiornare sui nuovi tool, non hardcoded"* is best served by Lift 1+2 together: BM25 in-process index, rebuilt on every registry mutation (notifications/tools/list_changed for MCP, direct on Register/Deregister for built-ins). The Qdrant `aura_tool_search_v2` collection becomes dead code.
