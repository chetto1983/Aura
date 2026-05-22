# Aura tool-surface audit — 2026-05-22

Scope: `internal/agent/tools/registry/`, `internal/agent/tools/index/`, `internal/agent/tools/swarm/`, `internal/mcp/`, plus the composition root in `cmd/aura/app_wire.go` + `cmd/aura/app.go` + `cmd/aura/main.go`.

Mandate (2026-05-22, verbatim): "mettere a posto tutti i tool pulire la merda che ci gira attorno. il rag deve auto-aggiornare sui nuovi tool, non hardcoded".

---

## 1. Tool inventory — what is actually registered

Counting only types that reach `toolRegistry.Register(...)` in production paths (`cmd/aura/app.go` + `cmd/aura/app_wire.go` + `cmd/aura/main.go`). Test-only and adapter-only types are NOT counted as registered native tools. MCP tools are dynamic (`mcp_<server>_<tool>`), `runtime-workspace/mcp.json` currently is `{}` so the live registered MCP count is `0`.

### Native tools actually registered (production)

| # | Name | Type | File | Reg site | Desc len | LOC |
|---|------|------|------|----------|---------:|----:|
| 1 | `agent_note` | `*AgentNoteTool` | `agent_note.go:38` | `app_wire.go:280` | 1100 | 218 |
| 2 | `apply_patch`* | — not directly registered — | `workspace_files.go:222` | — | — | — |
| 3 | `ask_user` | `*AskUserTool` | `ask_user.go:32` | `app_wire.go:306` | 1170 | 95 |
| 4 | `ask_user_clarification` | `*AskUserClarificationTool` | `ask_user_clarification.go:19` | `app_wire.go:307` | 600 | 103 |
| 5 | `daily_briefing` | `*DailyBriefingTool` | `daily_briefing.go:54` | `app_wire.go:287` | 280 | 274 |
| 6 | `dev_tool` | `*DevToolTool` | `tool_mgmt.go:38` | `app_wire.go:301` | 980 | 191 |
| 7 | `doc` | `*DocTool` | `doc.go:42` | `app_wire.go:312` | 1300 | 177 |
| 8 | `execute_code` | `*ExecuteCodeTool` | `exec.go:74` | `app_wire.go:295` | 1900 | 480 (file 499) |
| 9 | `execute_shell` | `*ExecuteShellTool` | `exec.go:351` | `app_wire.go:298` | 1750 | (shared in `exec.go`) |
| 10 | `file` | `*FileTool` | `file.go:51` | `app.go:363` | 1700 | 248 |
| 11 | `list_swarm_tasks` | `*ListSwarmTasksTool` | `swarm/tools.go:317` | `app.go:493` | 80 | (~60) |
| 12 | `read_swarm_result` | `*ReadSwarmResultTool` | `swarm/tools.go:374` | `app.go:496` | 90 | (~60) |
| 13 | `recall_god_nodes` | `*RecallGodNodesTool` | `wiki_godnodes.go:32` | `app_wire.go:318` | 280 | 175 |
| 14 | `recall_operational` | `*RecallOperationalTool` | `recall_operational.go:55` | `app_wire.go:601` | 260 | 223 |
| 15 | `recall_user_memory` | `*RecallUserMemoryTool` | `recall_user_memory.go:52` | `app_wire.go:605` | 340 | 203 |
| 16 | `request_dashboard_token` | `*RequestDashboardTokenTool` | `auth.go:43` | `app_wire.go:309` | 230 | (file ~140) |
| 17 | `run_aurabot_swarm` | `*RunAuraBotSwarmTool` | `swarm/tools.go:33` | `app.go:490` | 460 | (file 460) |
| 18 | `propose_patch` | `*ProposePatchTool` | `propose_patch.go:89` | `app_wire.go:220` | 660 | 450 |
| 19 | `search` | `*SearchTool` | `search.go:40` | `app_wire.go:597` | 700 | 283 |
| 20 | `search_memory` | `*SearchMemoryTool` | `memory_search.go:115` | `app_wire.go:594` | 800 | 450 |
| 21 | `source` | `*SourceTool` | `source_unified.go:80` | `app.go:451` | 2050 | 289 |
| 22 | `spawn_aurabot` | `*SpawnAuraBotTool` | `swarm/tools.go:150` | `app.go:487` | ~400 | (~150) |
| 23 | `subagent_dispatch` | `*SubagentDispatchTool` | `subagent.go:115` | `main.go:373` | 1200 | 385 |
| 24 | `task` | `*TaskTool` | `scheduler.go:96` | `app.go:397` | 1850 | 579 |
| 25 | `tool_search` | `*ToolSearchTool` | `tool_search.go:38` | `app_wire.go:304` | 600 | 129 |
| 26 | `web` | `*WebTool` | `web.go:34` | `app.go:380` | 940 | 160 |
| 27 | `wiki_page` | `*WikiPageTool` | `wiki.go:59` | `app_wire.go:315` | 2400 | 553 |
| 28 | `wiki_path` | `*WikiPathTool` | `wiki_path.go:32` | `app_wire.go:321` | 360 | 165 |
| 29 | `wiki_subgraph` | `*WikiSubgraphTool` | `wiki_subgraph.go:43` | `app_wire.go:324` | 360 | 355 |

**Total: 29 native LLM-facing tools registered in production.**

### Tool types that exist in code but are NEVER registered (DEAD CODE)

These types have full `Name()/Description()/Parameters()/Execute()` implementations, examples, and tests — but are not handed to `Registry.Register()` in any production path. They are reached either (a) internally via a dispatcher, or (b) not at all.

| # | Name | File | Used by | Status |
|---|------|------|---------|--------|
| 1 | `text_response` | `text_response.go:19` | `agent/toolexec.go:40`, `agent/terminal.go:225` — agent loop refers by NAME | **P0 — agent loop expects it but it is never registered** |
| 2 | `create_xlsx` | `files_xlsx.go:35` | `doc.go:36` (internal) | P1 — leaks 196 LOC of unused LLM contract |
| 3 | `create_docx` | `files_docx.go:32` | `doc.go:37` (internal) | P1 — 186 LOC |
| 4 | `create_pdf` | `files_pdf.go:33` | `doc.go:38` (internal) | P1 — 188 LOC |
| 5 | `read_source` | `source_read.go:23` | `source_unified.go:64` + `app_wire.go:596` (passed into SearchTool) | P1 — 1 caller, 2nd nested |
| 6 | `list_sources` | `source_list.go:20` | `source_unified.go:63` (internal) | P1 |
| 7 | `store_source` | `source_store.go:26` | `source_unified.go:65` (internal) | P1 |
| 8 | `ocr_source` | `source_ocr.go:27` | `source_unified.go:69` (internal) | P1 |
| 9 | `delete_source` | `source_delete.go:37` | `source_unified.go:75` (internal) | P1 |
| 10 | `lint_sources` | `source_list.go:81` | `source_unified.go:66` (internal) | P1 |
| 11 | `ingest_source` | `ingest.go:26` | `source_unified.go:72` (internal) | P1 — 65 LOC standalone file |
| 12 | `list_files` | `workspace_files.go:27` | `file.go:43` (internal) | P1 |
| 13 | `read_file` | `workspace_files.go:68` | `file.go:44` (internal) | P1 |
| 14 | `search_files` | `workspace_files.go:114` | `file.go:45` (internal) | P1 |
| 15 | `write_file` | `workspace_files.go:165` | `file.go:46` (internal) | P1 |
| 16 | `apply_patch` | `workspace_files.go:222` | `file.go:47` (internal) | P1 |
| 17 | `web_search` (SearXNG) | `searxng.go:33` | `web.go:Search` (internal) | P1 — only via `web` dispatcher |
| 18 | `web_fetch` (DirectFetch) | `direct_fetch.go:149` | `web.go:Fetch` (internal) | P1 |

So the registered LLM contract is **29 tools**, but the file count of types implementing `Tool` is **47** — meaning **18 types carry a public LLM-facing contract that the LLM never sees**. They keep their own `Description()` / `Parameters()` / `Examples()` even though only the dispatcher description is visible at runtime. This is the "merda che gira attorno" the user is pointing at.

### Critical regression — `text_response` is dead

- `internal/agent/terminal.go:222–227` hardcodes the assumption that `stats.TerminalTool == "text_response"`.
- `internal/agent/toolexec.go:40` lists `text_response` as a terminal tool.
- `internal/conversation/system_prompt.go` advertises `text_response` in the agent loop's tool surface.
- `internal/config/defaults/AGENT.md` instructs the LLM to call `text_response` to close a turn.

But there is no `Register(&TextResponseTool{})` line anywhere outside tests:

```
$ grep -nR 'TextResponseTool{' cmd/ internal/
internal/agent/tools/registry/description_audit_test.go:28
internal/agent/tools/registry/registry_scan_test.go:34
internal/agent/tools/registry/text_response_test.go:13
(no production callers)
```

When the LLM calls `text_response(...)`, `Registry.Execute` returns `errors.New("tool not found")` (registry.go:273). The agent loop then falls back to its non-terminal path and the prompt-level convergence claim (LAT-03) does not hold at runtime. **P0 — broken contract, ship-blocker.**

---

## 2. Duplication clusters

Cluster overlap measured by reading source. Percentage is rough LOC-overlap (logic + schema) between the dispatcher and the verb-tools.

### Cluster A — Memory recall (4 tools, ~1100 LOC)

| Tool | File | LOC | Purpose |
|------|------|----:|---------|
| `search` | `search.go` | 283 | Action-enum dispatcher (`search`/`list`/`read`) over `search_memory` + `wiki.Store` + `read_source` |
| `search_memory` | `memory_search.go` | 450 | Hybrid wiki + memoryindex retriever |
| `recall_operational` | `recall_operational.go` | 223 | FTS over `kind=operational` slice of `compact_memory_documents` |
| `recall_user_memory` | `recall_user_memory.go` | 203 | FTS over `kind=user_memory` slice |
| `memory_search_format.go` | (formatter) | 330 | Shared rendering |

**Overlap:**
- `search` (action=search) calls `search_memory` directly (`search.go:Execute` → `memory_search.SearchMemoryTool.Execute`). `search_memory.Description()` line 119 literally says: `"DEPRECATED: use search(action=search, query=…) instead — output is identical."` Both are still registered — the LLM sees both and the system prompt manifest costs ~1.6KB of redundant tool description.
- `recall_operational` and `recall_user_memory` are essentially the same code shape — FTS query against `compact_memory_documents` filtered by `Kinds=[]string{kind}` plus freshness annotation. Two near-identical 200-LOC files; a single `recall(kind=...)` enum-tool would collapse both to ~150 LOC.
- The `search` dispatcher already exposes a `zone` parameter (`wiki` / `source` / `all`). Adding `zone=operational|user_memory` would absorb the two `recall_*` tools.

**Estimated post-cleanup LOC for cluster: ~600 (delta -500).**

### Cluster B — Source surface (1 dispatcher + 7 verb files, ~1500 LOC)

| Tool / file | LOC |
|---|----:|
| `source_unified.go` (dispatcher) | 289 |
| `source.go` (shared formatters + `readBoundedFile`) | 282 |
| `source_read.go` | 200 |
| `source_list.go` (list + lint) | ~250 |
| `source_store.go` | ~150 |
| `source_ocr.go` | ~180 |
| `source_delete.go` | ~120 |
| `ingest.go` | 65 |

`source_unified.go:80` declares the `source` tool — every verb-tool is wired internally. The verb-tools each carry a complete `Description()`/`Parameters()`/`Examples()` that **the LLM never sees**. Removing the dead LLM-facing surface (Description/Parameters/curated examples) on the verb-tools, while keeping the internal Execute(), saves ~400 LOC of schema strings and matching tests in `source_test.go` (828 LOC currently).

**Overlap with dispatcher schema strings: ~70% per verb.** Estimated delta: **-450 LOC** + meaningful reduction in `source_test.go`.

### Cluster C — Workspace files (1 dispatcher + 5 verb tools, ~700 LOC)

| Tool / file | LOC |
|---|----:|
| `file.go` (dispatcher) | 248 |
| `workspace_files.go` (5 verb tools) | 311 |
| `workspace_validation.go` | ~140 |

Same shape as Cluster B. Five verb-tools (`list_files`/`read_file`/`search_files`/`write_file`/`apply_patch`) all carry their own LLM-facing strings + Definition() methods even though only `file` is registered. **Estimated delta: -200 LOC**.

### Cluster D — Web (1 dispatcher + 2 verb tools, ~820 LOC)

| Tool / file | LOC |
|---|----:|
| `web.go` (dispatcher) | 160 |
| `searxng.go` (`web_search`) | 164 |
| `direct_fetch.go` (`web_fetch`) | 493 |
| `web_common.go` (shared helpers) | 160 |

Same pattern. `web_search` and `web_fetch` exist as registered `Tool`s in the type system, with Description() / Parameters() that are never seen by the LLM. **Estimated delta: -250 LOC** of dead LLM-facing strings.

### Cluster E — File generation (1 dispatcher + 3 format tools, ~750 LOC)

| Tool / file | LOC |
|---|----:|
| `doc.go` (dispatcher) | 177 |
| `files_xlsx.go` | 196 |
| `files_docx.go` | 186 |
| `files_pdf.go` | 188 |
| `files.go` + `files_blocks.go` (shared) | ~220 |

Same shape: only `doc` is registered, the three format tools are wired internally. **Estimated delta: -250 LOC** of dead LLM-facing surface.

### Cluster F — ask_user (2 tools, ~200 LOC)

| Tool | File | LOC |
|---|------|----:|
| `ask_user` | `ask_user.go` | 95 |
| `ask_user_clarification` | `ask_user_clarification.go` | 103 |

`ask_user` already accepts `kind="clarification"|"approval"`. `ask_user_clarification` is a strict subset of `ask_user(kind="clarification")` with one additional field (`options[].value` machine-readable). The two could merge into `ask_user` with an optional `options` array. **Estimated delta: -90 LOC** (collapse to one) — and **+EN-only fix** (see §3 below). Note `ask_user_clarification` contains an Italian paragraph in its Description, breaking the EN-only rule.

### Cluster G — Swarm (4 tools, ~470 LOC)

| Tool | LOC |
|---|----:|
| `run_aurabot_swarm` | ~120 |
| `spawn_aurabot` | ~170 |
| `list_swarm_tasks` | ~60 |
| `read_swarm_result` | ~60 |
| `delegation_policy.go` | ~60 |

Each is a thin wrapper around `swarm.Manager`. Could collapse `spawn_aurabot` + `run_aurabot_swarm` + `list_swarm_tasks` + `read_swarm_result` into a single `swarm(action=spawn|run|list|read)` action-enum surface, mirroring `task`, `source`, `file`, `web`, `doc`. **Estimated delta: -150 LOC** (one file at ~320 LOC instead of `tools.go` + auxiliaries).

### Cluster H — Wiki readers (3 tools, ~700 LOC)

| Tool | LOC |
|---|----:|
| `recall_god_nodes` | 175 |
| `wiki_path` | 165 |
| `wiki_subgraph` | 355 |

All three operate on `wiki.Store`. They could merge into a single `wiki(action=top_nodes|path|subgraph)` dispatcher, parallel to the existing `wiki_page` writer dispatcher. **Estimated delta: -200 LOC.**

### Cluster summary

| Cluster | Files | Current LOC | Post-cleanup LOC | Delta |
|---------|------:|------------:|-----------------:|------:|
| A — Memory recall | 5 | 1500 | 600 | **-900** |
| B — Source | 8 | 1500 | 1050 | **-450** |
| C — Workspace files | 3 | 700 | 500 | **-200** |
| D — Web | 4 | 820 | 570 | **-250** |
| E — File gen | 5 | 750 | 500 | **-250** |
| F — ask_user | 2 | 200 | 110 | **-90** |
| G — Swarm | 4+aux | 470 | 320 | **-150** |
| H — Wiki readers | 3 | 700 | 500 | **-200** |
| **Total** | | **6640** | **4150** | **-2490** |

That's roughly **-37% LOC** in `internal/agent/tools/registry/`. Test files would shrink proportionally (~-30%).

---

## 3. Tool description quality audit

Reference: `internal/agent/tools/registry/description_audit_test.go` already enforces a "first line marker" (Destructive. / Read-only. / Returns / action-verb). It runs against 22 tools; results below add P0/P1/P2 grading per description.

| Tool | First line OK? | EN-only? | Length OK (≤500c top-line)? | Issue grade |
|------|:-:|:-:|:-:|:-:|
| `agent_note` | yes — "A per-conversation..." | yes | yes (~250c first line) | P2 |
| `ask_user` | yes — "Pause the current..." | yes | yes (~70c first line) | P2 |
| `ask_user_clarification` | yes — "Read-only. Pause..." | **NO — IT in body** | yes | **P1 — fix IT block, then merge into `ask_user`** |
| `daily_briefing` | yes — "Build a read-only..." | yes | yes | P2 |
| `dev_tool` | yes — "Manage the Python..." | yes | yes (~50c top line) | P2 |
| `doc` | yes — "Generate a document..." | yes | yes (~100c top line) | P2 |
| `execute_code` | yes — "Execute Python code..." | yes (rich + accurate) | **NO — 1900c, > 500** | P1 — pretty rich, but consider splitting "what it does" from "what NOT to do" |
| `execute_shell` | yes | yes | **NO — 1750c** | P1 |
| `file` | yes — "Read, write, list..." | yes | yes (~70c top line) | P2 |
| `propose_patch` | yes — "Submit a structured..." | yes | yes | P2 |
| `recall_god_nodes` | yes — "List the most..." | yes | yes | P2 |
| `recall_operational` | yes — "Surface Aura's validated..." | yes | yes | P2 — but **duplicates** `recall_user_memory` shape (Cluster A) |
| `recall_user_memory` | yes | yes | yes | P2 — same as above |
| `request_dashboard_token` | yes — "Mint a fresh..." | yes | yes | P2 |
| `search` | yes — "Read-only. Unified knowledge..." | yes | yes | P2 — **but `search_memory` is its DEPRECATED duplicate, both registered** |
| `search_memory` | yes — "Search Aura's persistent..." | yes | yes | **P1 — self-declared DEPRECATED, still registered** |
| `source` | yes — "Manage uploaded sources..." | yes | yes (~80c top line) | P2 |
| `subagent_dispatch` | yes — "Spawn up to 3..." | yes | yes | P2 |
| `task` | yes — "Manage scheduled tasks..." | yes | yes | P2 |
| `text_response` | yes — "Reply directly..." | **NO — IT paragraph in body** | yes | **P0 — not registered AND mixed-language** |
| `tool_search` | yes — "Search Aura's tool catalog..." | yes | yes | P2 — examples contain IT strings (fine, those are query examples, not prose) |
| `web` | yes — "Search the web or..." | yes | yes | P2 |
| `wiki_page` | yes — "Create, replace, edit..." | yes | yes (rich, ~2400c) | P1 — second-largest description; OK as long as the LLM benefits |
| `wiki_path` | yes — "Find the shortest..." | yes | yes | P2 |
| `wiki_subgraph` | yes — "Retrieve a token-budgeted..." | yes | yes | P2 |
| Swarm 4 | yes | yes | yes | P2 |
| MCP `*` | server-supplied + `[MCP: <name>]` prefix | depends on upstream | unbounded | P1 — see §4 |

### Top-3 description quality issues

1. **P0** — `text_response`: body carries Italian paragraph ("Per chiudere il turno con una risposta diretta..."), violating `feedback_all_prompts_in_english_only`. **And** the tool is not registered at all — fixing the IT block is moot until the registration is added.
2. **P1** — `ask_user_clarification`: body carries Italian paragraph ("Quando una richiesta è ambigua..."). Same EN-only violation. Best fix is to merge into `ask_user(kind=clarification)` and drop the file.
3. **P1** — `search_memory` self-declares DEPRECATED but is still registered, so the LLM sees both `search` (replacement) and `search_memory` in the same manifest. Two ways to call the same lookup; the deprecated one's description even says "output is identical" — pure token waste.

Honorable mentions:
- `execute_code` and `execute_shell` descriptions are 1900 and 1750 chars respectively. They carry important operational discipline phrases ("Read-only stdout, capped, results are INTERNAL — synthesize before replying") that `description_audit_test.go` enforces. P1 cleanup is to extract "what NOT to do" into a separate shorter "policy" section, but the size is not pathological for this risk level.
- Tool first-line marker is well enforced — none of the 22 tracked tools fails. Forward-looking gate works.

---

## 4. MCP layer audit

### Registered servers

`runtime-workspace/mcp.json` is currently `{}` (line 1: `{}`). The example file `mcp.example.json` shows two servers: `mail` (stdio, mail-mcp binary) and `database` (stdio, ea-database-server). In production right now: **zero MCP servers configured, zero MCP tools registered.**

### Discovery and registration path

1. `cmd/aura/app.go:508` — `mcp.LoadServers(cfg.MCPServersPath)` reads + validates `mcp.json`.
2. `cmd/aura/app.go:513–542` — per server: build client (stdio or HTTP), call `cli.Tools()`, register each via `tools.NewMCPTool(cli, name, t)` after consulting `mcp.ToolEnabledForAura(name, env, toolName)` policy gate.
3. Tool naming — `mcp.go:27` — `fmt.Sprintf("mcp_%s_%s", serverName, tool.Name)`. Collision-safe with native tools.
4. Description sourcing — `mcp.go:34–47` — upstream `tool.Description` prefixed with `"[MCP: <serverName>] "`. If upstream is empty, falls back to `"MCP tool <name> from server <serverName>"`.
5. Discovery hint — `mcp.go:43–45` — when the upstream schema declares a required `account_id`, the description appends a hint to call `mcp_<server>_list_all_accounts` first. This is a hard-coded `mail`-shaped pattern living in generic MCP code (P2 — small).

### Description override mechanism — DOES NOT EXIST

`mcp.go:34` blindly uses `t.tool.Description`. There is no:
- Per-server description override config (e.g. `mcp.json` entries like `"toolOverrides": {"toolName": {"description": "..."}}`),
- Truncation / sanitization layer for verbose upstream descriptions,
- IT/EN normalization for upstream descriptions written by third-party servers.

**P1 cleanup**: introduce an optional `tool_overrides` field in `mcp.ServerConfig` that lets the operator (a) shorten noisy upstream descriptions, (b) re-label IT-leaning servers, (c) hide tools the policy gate enabled but the LLM does not need (similar to the `MailIMAPMutationTools` env-flag block list, but per-tool description).

### Policy gating (`internal/mcp/policy.go`, 152 LOC)

Currently hard-coded for two server names: `mail` (allowlist + IMAP mutation + SMTP send + IMAP write env gate) and `database` (allowlist + write blocklist). Any new MCP server name falls through `default: return true` — fully opens the surface. This is fine as default, but the hard-coded `mail`/`database` policy belongs in a config file (or per-server env overrides), not in shipped Go source. P2 — works for current scope.

### Per-tool autonomous category

`policy.go:64–73` — `ToolAutonomousForAura` decides whether an MCP tool gets `CategoryAutonomous` (i.e. usable inside `execute_code`'s internal tool-call manifest). Same hard-coded mail/database split; everything else is non-autonomous. P2.

### Watcher

`internal/mcp/watcher.go` (180 LOC) — fsnotify on `mcp.json` with debounce; callback fires `r.Notify(toolindex.ReasonMCPConfig)` on the tool index reconciler. The reconciler then re-snapshots `Registry.Definitions()` — **but the registry never reloads the actual MCP clients after boot**. So if you edit `mcp.json` to add a new server, the reconciler fires, finds no new tools (the registry was not rebuilt), and the operator has to restart Aura. **P1 — incomplete reload**: the watcher fires the toolindex reconciler but not a `Registry.Reset() + re-register-MCP` sweep. Mentioned in the open-gaps inventory (Wave 2.10.c "MCP reload") and still unfixed today.

---

## 5. Tool index reconciler (`internal/agent/tools/index/`)

Files (1130 LOC total + 905 LOC test):

| File | LOC | Purpose |
|------|----:|---------|
| `types.go` | 94 | Interfaces (`ToolProvider`, `Embedder`, `QdrantClient`, `StateStore`, `StateRow`, `EmbeddingTextFn`) |
| `hash.go` | 138 | `ContentHash` over (name, description, input_schema, embed_text, embed_model) with canonical JSON |
| `state.go` | 81 | `SQLiteStateStore` over `tool_index_state` table (5 cols) |
| `reconciler.go` | 427 | Diff engine + Notify channel + Run goroutine + debounce |
| `*_test.go` | 905 | Coverage for hash determinism, state CRUD, reconcile diff buckets, drift-recovery, eval top-k |
| `eval_topk_fixture.json` | (data) | Eval queries → expected tool name pairs |

### Triggers (`Reason` enum, reconciler.go:22–28)

1. `ReasonBoot` — synchronous one-shot at boot. `app_wire.go:93` calls `r.Reconcile(ctx, ReasonBoot)` before spinning up `Run`.
2. `ReasonSkillsChanged` — sent from the skill loader (US-OP02 file watcher). Coverage: skill install/delete hooks call `Notify(ReasonSkillsChanged)`.
3. `ReasonMCPConfig` — `app_wire.go:110` — fsnotify on `mcp.json` debounced 500ms.
4. `ReasonManual` — dashboard `POST /api/tools/reindex` handler (api router; not inside the index package).
5. `ReasonPeriodic` — `cfg.Periodic` (default 10 min) safety-net ticker. `<=0` disables.

### Upsert path

`reconciler.go:235–408`. Steps:

1. Snapshot `wanted` (Registry.Definitions()) + compute per-tool `wantedHash` via `ContentHash` (hash.go:28).
2. Load `indexed` from SQLite (`tool_index_state`).
3. **Drift recovery** (line 266–290): if SQLite says we have N indexed rows but `Qdrant.CollectionInfo` reports `PointsCount == 0` or `Status == ""`, wipe the SQLite state and treat every tool as new. Good defensive code, covers the "operator wiped Qdrant volume" pitfall.
4. Diff into `toUpsert` (changed or new) + `toDelete` (in state but not in wanted).
5. Apply deletes first (line 315–333) to avoid stale-vs-new coexistence for a renamed tool.
6. Apply upserts (line 338–404): embed each tool individually via `r.cfg.Embedder.Embed(ctx, text)`; bunch into one `Qdrant.Upsert` round-trip; write per-tool state row.

### Why the dim-mismatch is silent

Source of the symptom: reconciler does NOT check Qdrant collection's vector size against `cfg.VectorDim` (`tools.ToolVectorDim(cfg.EmbeddingOutputDim)`). The pre-flight `CollectionInfo` call (line 344) only checks `info.Status == ""` to decide whether to create the collection; it ignores `info.Config.Params.Vectors.Size`.

Concretely:
- Operator runs Aura at `EMBEDDING_OUTPUT_DIM=768` → reconciler creates `aura_tool_search_v2` at 768.
- Operator switches to `EMBEDDING_OUTPUT_DIM=256` (Matryoshka truncation) → reconciler does NOT delete + recreate. Every `Qdrant.Upsert` returns `wrong number of dimensions` from Qdrant.
- The error is appended to `report.Errors` (`fmt.Sprintf("qdrant upsert: %v", err)`, reconciler.go:385) — **per-tool**, so 50 tools → 50 entries.
- `logReport` (line 410–427) emits ONE `slog` line with `errors=50` + `error_details=[...]` as a slice. The line is at WARN level. Operators reading default log levels will see `"toolindex reconcile" errors=50` and may not realize what failed unless they inspect `error_details`.
- `tool_index_state` is never updated (upserts failed) → next reconcile reproduces the same 50 errors.
- `tool_search` quietly falls back to FTS (per `ToolVectorIndex.Search` snapshot pattern in `registry_search_vector.go:146–169`). The lex backend still works, so a casual eval looks "fine".

**P0 — silent failure**:
1. Add a pre-flight check: `if info.Config.Params.Vectors.Size != cfg.VectorDim { return errors.New("dim mismatch") }` (and surface ONCE per reconcile, with the desired action — drop+recreate or refuse).
2. Surface a single high-level error in `logReport` ("tool index dim mismatch: collection=768, wanted=256, refusing to upsert; run /api/tools/reindex?recreate=1 or set EMBEDDING_OUTPUT_DIM consistently") instead of N per-tool warnings.
3. Optionally: when `report.Reason == ReasonBoot` and `info.Config.Params.Vectors.Size != cfg.VectorDim`, refuse to start and fail fast (bootstrap discipline). Today the reconciler errors are non-fatal at boot.

### Other reconciler observations

- `cfg.EmbedModel` is `search.EmbedCacheNamespace(EmbeddingBaseURL, EmbeddingModel)` — a string like `"http://embed:8080/embed:embeddinggemma-300m"`. That's bound into every ContentHash, so a re-host of the sidecar from `embed:8080` to `embed:8888` invalidates every tool hash and forces a full re-embed. Intentional (different URL = different model identity), but operators changing only the model API key get a free re-embed too. P2 — acceptable.
- `defaultEmbedText` (line 140) is `def.Name + ": " + def.Description` — fallback only. Production passes `tools.SearchableEmbeddingTextForLLMDef` which uses the richer ToolRAG-style format from `registry_search.go:122`. Good.
- The reconciler runs `Periodic = 10 minutes` by default. With a fast local sidecar this is cheap (hash match → unchanged → no embed call). P2.
- `Reconcile` is synchronous + mutex-guarded. Manual `/api/tools/reindex` and the periodic ticker serialize cleanly. P2.

---

## 6. `tool_search` tool

`internal/agent/tools/registry/tool_search.go` (129 LOC). Backed by `Registry.Search`, which falls back to FTS when no vector index is wired. The vector backend is the Qdrant collection maintained by the reconciler above.

- Tier: `VisibilityAlwaysOn` (declared in `tool_search.go:69`) — always in the LLM's tool pool.
- Inputs: `query` (required, multilingual), `limit` (1–10, default 5).
- Output: JSON with `query` + `hits[].{name, description, input_schema, score}`.
- LLM usage: the system prompt advertises a 1-line manifest of every registered tool; `tool_search` returns full schemas for top matches; the agent loop adds those schemas to the per-turn tool pool (deferred discovery pattern).

**Retrieval shape:**
- Vector path: `ToolVectorIndex.Search` (registry_search_vector.go) → embed query → Qdrant cosine — wired ONLY when `ToolSearchBackend != "fts"` and Qdrant is healthy.
- FTS path: `registry_search.go:scoreToolText` — name (×12) + body (×3) per term substring match. Crude lexical scoring.
- Description embedded by reconciler is `searchableToolEmbeddingText` (registry_search.go:156) — `tool_name(spaces): description\n    param [type]: param_description` (ToolRAG format, ~3500 char cap).
- Examples in `tool_search.Definition()` (lines 70–84) are IT — fine, they are query strings the LLM should mimic, not prose.

**P1 cleanup hooks** (already opened in §1 cleanup):
- Once dead-verb tools are deregistered from the type system, the FTS scorer stops indexing their dead schema strings — `tool_search` precision and recall both improve.
- The Description() text on the dispatcher tools is what the embedding sees; richer per-action descriptions (already true for `source`, `wiki_page`, `file`, `web`, `doc`, `task`) feed the reconciler well.

---

## 7. Dead / orphan tools (catalogue alignment)

Cross-referencing the registered set against:
- `internal/agent/loop_test.go` and adjacent test fixtures,
- `cmd/probe_chat/` (canonical end-to-end harness),
- `internal/conversation/system_prompt.go` (what the manifest mentions),
- `internal/config/defaults/AGENT.md` (what the overlay tells the LLM exists).

| Tool | In prompt manifest? | In probe_chat cases? | Loop tests? | Status |
|------|:-:|:-:|:-:|:-:|
| `text_response` | yes (AGENT.md) | yes | tests reference name | **P0 dead — not registered** |
| `ingest_source` (verb) | no | no | no | P1 dead — replaced by `source(action=reprocess, stages=[ingest])` |
| `web_search`, `web_fetch` (verbs) | no | no | uses `web` dispatcher only | P1 dead — replaced by `web` |
| `create_xlsx/docx/pdf` (verbs) | no | no | no (uses `doc`) | P1 dead — replaced by `doc` |
| `list_files`, `read_file`, `search_files`, `write_file`, `apply_patch` (verbs) | no | no | uses `file` dispatcher only | P1 dead — replaced by `file` |
| `read_source`, `list_sources`, `store_source`, `ocr_source`, `delete_source`, `lint_sources` (verbs) | no | no | uses `source` dispatcher only | P1 dead — replaced by `source` |
| `search_memory` | yes (legacy advert) | yes | yes | **P1 zombie** — registered and used by tests + system prompt, but self-declared deprecated. Plan: pin manifest to `search` only, deprecate prompt entry, drop registration in a follow-up. |
| `agent_note` | yes | yes (since US-P03) | yes | live |
| `dev_tool` | deferred (only after `tool_search`) | rare | yes | live, low traffic |
| `recall_god_nodes` | yes | yes | yes | live |
| `wiki_path` | yes | yes | yes | live |
| `wiki_subgraph` | yes | yes | yes | live |
| `propose_patch` | yes | yes | yes | live |
| `subagent_dispatch` | yes | yes | yes | live |
| All swarm tools | conditional on AURABOT_ENABLED | yes | yes | live (conditional) |
| `request_dashboard_token` | yes (allowlist-gated) | yes | yes | live |
| `ask_user`, `ask_user_clarification` | yes (always_on tier) | yes | yes | both live, merge candidate (Cluster F) |
| `tool_search` | yes (always_on) | yes | yes | live, vital for deferred discovery |
| `daily_briefing` | yes | yes | yes | live |

### Top-3 dead/orphan tools

1. **`text_response`** — P0. Loop refers by name, AGENT.md instructs the LLM to call it, but `Register(&TextResponseTool{})` is missing.
2. **The eleven verb-tools** (`web_search`, `web_fetch`, `create_xlsx`, `create_docx`, `create_pdf`, `read_source`, `list_sources`, `store_source`, `ocr_source`, `delete_source`, `lint_sources`, `ingest_source`, `list_files`, `read_file`, `search_files`, `write_file`, `apply_patch`) — P1. Each is a type with full LLM-facing contract (Description/Parameters/Definition/Examples) that ships in the binary but is never registered. Their internal use as private "verb helpers" inside dispatchers does not justify keeping the LLM contract or the tests of that contract.
3. **`search_memory`** — P1. Self-declared deprecated, identical output to `search`, still registered, still embedded in the tool index, still costs tokens in the system prompt manifest.

---

## 8. God files (>500 LOC) in `internal/agent/tools/registry/`

CLAUDE.md cap is 600 LOC. Files trending toward / over the cap:

| File | LOC | Cap status | Notes |
|------|----:|:----------|-------|
| `scheduler.go` | 579 | borderline | Already flagged in `registry/README.md:152`. Schedule-write tool vs read tools split candidate. |
| `wiki.go` | 553 | borderline | WikiPageTool with rich description prose. Split off the body-prose writer from the dispatcher? Maybe not worth it — description is the value. |
| `memory_search.go` | 450 | OK but rich | Already flagged in README. Score helpers + render helpers split candidate. |
| `exec.go` | 499 | OK | Both `execute_code` AND `execute_shell` in the same file. Split candidate. |
| `propose_patch.go` | 450 | OK | Has security helper in `propose_patch_security.go`; main file is action-enum. |
| `subagent.go` | 385 | OK | Spawn + collect in one file. |
| `wiki_subgraph.go` | 355 | OK | Single tool, focused. |
| `memory_search_format.go` | 330 | OK | Pure rendering. |
| `registry_search_vector.go` | 323 | OK | Qdrant reader. |
| `workspace_files.go` | 311 | OK | 5 verb tools — would shrink to 0 if dead-verb cleanup applied. |

Test files over 500 LOC (also matter for refactor velocity):
- `memory_search_test.go` (949), `source_test.go` (828), `scheduler_test.go` (781), `registry_test.go` (667), `propose_patch_test.go` (593).

### Top-3 god files

1. **`scheduler.go` (579)** — borderline + 781-LOC test file. Split into `scheduler_dispatcher.go` (TaskTool action-enum, ~250 LOC) + `scheduler_actions.go` (schedule/list/cancel/run_now logic, ~330 LOC).
2. **`wiki.go` (553)** — WikiPageTool action-enum with rich description. Less urgent — the description prose is load-bearing and splitting it just moves bytes. Mark P2.
3. **`exec.go` (499)** — both `execute_code` and `execute_shell` live here. The shell tool's description (1750 chars) shares a lot of policy text with execute_code. Split into `exec_code.go` + `exec_shell.go` + `exec_policy.go` (shared "INTERNAL — synthesize" prose constants). P1.

---

## 9. Test coverage map

For each registered tool file, does a `*_test.go` exist next to it, and is it more than smoke?

| Tool file | Test file | Test style |
|-----------|-----------|------------|
| `agent_note.go` | `agent_note_test.go` (218) | table-driven action coverage |
| `ask_user.go` | `ask_user_promptfx_test.go` (in agent pkg) | promptfx; channel-level coverage |
| `ask_user_clarification.go` | `ask_user_clarification_test.go` (~75) | table-driven |
| `auth.go` | `auth_test.go` (181) | fixture-driven |
| `daily_briefing.go` | `daily_briefing_test.go` (251) | fixture-driven (SQLite + filesystem) |
| `direct_fetch.go` | shared with `web_test.go` + `web_dispatcher_test.go` | httptest server-driven |
| `doc.go` | `doc_test.go` (~110) | dispatch coverage |
| `exec.go` | `exec_test.go` (462) | rich — manifest + escalation + per-tool blocklist |
| `file.go` | `file_test.go` (250) | dispatcher coverage |
| `files_xlsx.go` + `_docx` + `_pdf` | `files_test.go` (477) | artifact-verify (xlsx/docx/pdf bytes inspected) |
| `ingest.go` | only smoked via `source_test.go` | thin |
| `mcp.go` | `mcp_test.go` (165) | unit |
| `memory_search.go` | `memory_search_test.go` (949) | rich — corpus + scoring + freshness |
| `precall_validator.go` | `precall_validator_test.go` (392) | fixture-driven |
| `propose_patch.go` | `propose_patch_test.go` (593) | fixture-driven w/ SQLite |
| `recall_operational.go` | `recall_operational_test.go` (~130) | fixture-driven |
| `recall_user_memory.go` | `recall_user_memory_test.go` (~120) | fixture-driven |
| `registry.go` | `registry_test.go` (667) | rich (auth, categorization, definitions) |
| `registry_search*.go` | `registry_search*_test.go` (~250+) | scorer + vector reader |
| `scheduler.go` | `scheduler_test.go` (781) | rich — schedule kinds + cron + run_now |
| `search.go` | `search_test.go` (~200) | dispatcher coverage |
| `source_unified.go` | `source_test.go` (828) | rich — every action + lint + reprocess |
| `subagent.go` | `subagent_test.go` (324) | spawn/collect + tier coverage |
| `text_response.go` | `text_response_test.go` (~80) | smoke ✱ |
| `tool_mgmt.go` | `tool_mgmt_test.go` (~110) | dispatcher coverage |
| `tool_search.go` | `tool_search_test.go` (~150) | scorer-driven |
| `web.go` | `web_test.go` (184) | dispatcher coverage |
| `wiki.go` | `wiki_test.go` (381) | action coverage |
| `wiki_godnodes.go` | (no dedicated test file) ✱ | covered indirectly via `wiki_subgraph_test.go` |
| `wiki_path.go` | (no dedicated test file) ✱ | covered indirectly |
| `wiki_subgraph.go` | `wiki_subgraph_test.go` (210) | fixture-driven |
| `workspace_files.go` | `workspace_files_test.go` (375) | rich path/denylist coverage |
| Swarm 4 tools | `swarm/tools_test.go` | rich — registry + manager mock |
| `internal/agent/tools/index/*` | `reconciler_test.go` (554), `hash_test.go` (166), `state_test.go`, `eval_topk_test.go` | rich — diff buckets + drift + eval fixture |

### Coverage gaps

1. **`wiki_path` and `recall_god_nodes`** — no dedicated `*_test.go`. They are simple enough that the indirect coverage via subgraph tests probably hits them, but a 30-line table-driven test is cheap. P2.
2. **`ingest.go`** — has only path-of-least-resistance coverage via `source_test.go`. A direct test would catch idempotency regressions on the standalone surface. P2 (and may go to zero if we remove the file as part of the dead-verb cleanup).
3. **`text_response_test.go`** — solid unit coverage of Execute() return path, but does NOT verify the tool is registered in the live composition root. A `cmd/aura/app_wire_test.go::TestProductionRegistryContainsTextResponse` would catch the P0 above. **P0 gap.**

---

## 10. Comment rot + TODO/FIXME audit

```
internal/agent/tools/registry/memory_search.go:119  "DEPRECATED: use search(action=search, query=…) instead — output is identical."
internal/agent/tools/registry/memory_search.go:225  "[DEPRECATED: use search(action=search) instead of search_memory]"
internal/agent/tools/registry/search.go:20          "// descriptions and result envelopes now carry DEPRECATED redirect hints."
internal/agent/tools/registry/source_unified.go:113 "max_bytes (legacy hint)"
internal/agent/tools/registry/tool_definitions.go:96 "content": "TODO:\n- verify X\n- summarize Y\n- report Z"  // example fixture; harmless
```

Other rot signals:
- `internal/agent/tools/registry/exec.go:351` — `ExecuteShellTool` and `ExecuteCodeTool` share the file; the shell tool was added after-the-fact. The README acknowledges the split candidate.
- `registry/README.md:144–152` documents three files trending toward the LOC cap (memory_search 569, scheduler 540, exec 480) — current line counts are 450/579/499, so scheduler has grown PAST the README's snapshot. Comment rot.
- `internal/mcp/policy.go:5` — `MailIMAPWriteEnabledEnv` env-var-name constant is hardcoded mail-specific while the file claims to be the generic `policy` layer. P2 — refactor when a third MCP server lands.

### P0/P1/P2 from rot:

- **P1** — Memory `DEPRECATED` strings inside live descriptions are an unfinished refactor. Either fix the registration (remove `search_memory` from production) or remove the DEPRECATED text (keep the tool). The current state is the worst of both worlds.
- **P2** — `registry/README.md` LOC table is stale (scheduler.go grew from 540 to 579).
- **P2** — `tool_definitions.go:96` "TODO:" string is fixture content, not actual TODO. Comment is misleading on first read but harmless.

---

## 11. Total LOC delta — if all flagged cleanup is applied

| Bucket | LOC delta |
|--------|----------:|
| Cluster A — memory recall consolidation | **-900** |
| Cluster B — source verb cleanup | **-450** |
| Cluster C — workspace verb cleanup | **-200** |
| Cluster D — web verb cleanup | **-250** |
| Cluster E — file gen verb cleanup | **-250** |
| Cluster F — ask_user merge | **-90** |
| Cluster G — swarm enum-tool consolidation | **-150** |
| Cluster H — wiki readers merge | **-200** |
| God-file splits (scheduler.go + exec.go) | 0 net (move, don't delete) |
| Test files shrink proportionally | **-700 to -1100** (rough; ~30% of removed-tool test code) |
| Reconciler dim-mismatch pre-flight check | +25 |
| Register `text_response` | +1 (one line in app_wire.go) |
| Drop search_memory registration | -1 in app_wire.go, -800 if file deleted entirely (currently 450 prod + 949 tests) |
| **Registry source delta** | **~-2500** |
| **Tests delta** | **~-1500** |
| **Net repo delta** | **~-4000 LOC** |

That's roughly **-25% of `internal/agent/tools/registry/`** combined (prod + tests) for the cleanup-only path, with no behavior change visible to the LLM other than:
- Fewer redundant tools in the manifest (manifest shrinks ~30%, fewer tokens billed).
- `text_response` actually works.
- Dim-mismatch failures are visible at boot instead of buried in 50 warn-line spam.
- MCP description override mechanism in place for the inevitable third-party noise.

---

## 12. Priority-stamped fix list

### P0 — broken

1. **Register `text_response` in `cmd/aura/app_wire.go`.** One-line fix: `a.deps.Tools.Register(&tools.TextResponseTool{})` next to the other always-on tools (`ask_user`, `ask_user_clarification`). Add an `app_wire_test.go` that asserts `_, ok := reg.Get("text_response"); !ok { t.Fatal }`.
2. **Reconciler dim-mismatch pre-flight.** In `reconciler.go:344`, after `CollectionInfo`, compare `info.Config.Params.Vectors.Size` against `r.cfg.VectorDim`. On mismatch: surface ONE high-level error and refuse upserts (current behavior is per-tool spam + silent FTS fallback). Optionally fail fast at `ReasonBoot`.
3. **Fix the IT block in `text_response.go:24`** — strip the Italian paragraph; the English half already says everything (LAT-03 lesson).

### P1 — cleanup (the user's "merda che ci gira attorno")

4. **Drop dead verb-tool LLM contracts.** Remove `Description()` / `Parameters()` / `Definition()` / `Examples()` / dedicated tests from the 11 verb tools that are only used internally by their dispatcher (Clusters B/C/D/E). Keep `Execute()`. Cleanup delta: ~-1100 LOC.
5. **Drop `search_memory` registration + delete the file.** It's deprecated, redundant with `search(action=search)`. Migration: pin `search` in the manifest; delete `memory_search.go` (450 LOC) + `memory_search_test.go` (949 LOC) once tests of the new path are green. Update `internal/conversation/system_prompt.go` to drop the legacy advert.
6. **Merge `ask_user_clarification` → `ask_user(kind=clarification, options=...)`.** Drop the 103-LOC file + its IT block. Update the channel renderer to look at `args["options"]` on the merged path.
7. **Add `tool_overrides` to `mcp.ServerConfig`.** Lets operator shorten/translate noisy upstream descriptions per server, per tool. ~+40 LOC + tests.
8. **MCP reload watcher (Wave 2.10.c).** `watcher.go` already fires on `mcp.json` edits, but only the toolindex reconciler reacts; the registry never re-pulls server tools. Either: (a) emit a process-restart signal; or (b) implement `Registry.ResetMCP()` + per-server re-bootstrap. Track in `prd.md` as a real story; the comment in `cmd/aura/app.go:507` already says this is the future.
9. **Reconciler logReport: collapse N per-tool upsert errors into one summary line** with `first_error_tool` + `error_count` + `error_class` enum, while still attaching full `error_details` for debug. Operator readability win.
10. **Split `scheduler.go` (579 LOC).** Per CLAUDE.md ≤600 cap. Pattern: `scheduler.go` (dispatcher) + `scheduler_schedule.go` (schedule write logic + cron parsing) + keep list/cancel/run_now in scheduler.go.
11. **Split `exec.go` (499 LOC).** `exec_code.go` + `exec_shell.go` + shared `exec_policy.go` for the "INTERNAL — synthesize" prose constant.
12. **Stale `registry/README.md` LOC table.** Update or delete the snapshot.

### P2 — nits

13. Add dedicated `wiki_path_test.go` and `recall_god_nodes_test.go`.
14. Refactor `mcp/policy.go` from hardcoded mail/database server-name strings to a per-server policy struct loaded with the rest of `mcp.json`. Defer until a third MCP server lands.
15. Reconcile `tool_definitions.go:96` example fixture so the literal "TODO:" string is not mistaken for a real TODO grep hit.

---

## 13. Files inspected (absolute paths)

Composition root:
- `d:/Aura/cmd/aura/app.go:340..550`
- `d:/Aura/cmd/aura/app_wire.go:60..330, 580..610`
- `d:/Aura/cmd/aura/main.go:360..395`

Registry layer:
- `d:/Aura/internal/agent/tools/registry/registry.go`
- `d:/Aura/internal/agent/tools/registry/definition.go`
- `d:/Aura/internal/agent/tools/registry/{agent_note,ask_user,ask_user_clarification,auth,daily_briefing,direct_fetch,doc,exec,file,files_xlsx,files_docx,files_pdf,ingest,memory_search,propose_patch,recall_operational,recall_user_memory,scheduler,search,searxng,source_unified,subagent,text_response,tool_mgmt,tool_search,web,wiki,wiki_godnodes,wiki_path,wiki_subgraph,workspace_files}.go`
- `d:/Aura/internal/agent/tools/registry/{registry_search,registry_search_vector,description_audit_test,registry_scan_test}.go`
- `d:/Aura/internal/agent/tools/registry/README.md`

Index reconciler:
- `d:/Aura/internal/agent/tools/index/{types,hash,state,reconciler}.go`
- `d:/Aura/internal/agent/tools/index/eval_topk_fixture.json`

Swarm tools:
- `d:/Aura/internal/agent/tools/swarm/tools.go`

MCP layer:
- `d:/Aura/internal/mcp/{client,config,policy,watcher}.go`
- `d:/Aura/runtime-workspace/mcp.json` (currently `{}`)
- `d:/Aura/mcp.example.json`

Cross-references:
- `d:/Aura/internal/agent/{terminal,toolexec}.go` (text_response wiring)
- `d:/Aura/internal/conversation/system_prompt.go` (manifest)
- `d:/Aura/internal/config/defaults/AGENT.md` (overlay)
- `d:/Aura/internal/storage/qdrant/client.go:140..230` (CollectionInfo / CreateCollection)
