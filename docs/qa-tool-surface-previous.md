# Tool Surface — 2026-05-18 — git_head: 3652fdf2

Inventory of every LLM-callable tool that Aura registers into the agent loop's `*tools.Registry`. Built-in static tools live in `internal/agent/tools/registry/`; production wiring (`cmd/aura/app.go` + `cmd/aura/app_wire.go` + `cmd/aura/main.go`) registers a deliberately small picobot surface — the verb-flavored sub-tools (`list_files`, `read_file`, `store_source`, `web_search`, `create_xlsx`, etc.) exist as helpers inside the consolidated `file` / `source` / `web` / `doc` tools and are NOT independently registered in the production registry (they ARE registered in `cmd/debug_*/main.go` smoke binaries). Swarm tools live in `internal/agent/tools/swarm/tools.go`; MCP tools are wrapped dynamically per server in `internal/agent/tools/registry/mcp.go`. There is no separate `read_skill` tool — skill bodies are surfaced through the `file` tool's `action="read"` against the `SKILLS_PATH` roots, and `internal/agent/untrusted.go:24` treats results tagged with that synthetic name as untrusted output. The `internal/agent/tools/sets/toolsets.go` file is NOT an additional source of tools surfaced to the LLM: it is a curated set of *built-in tool names* used by the swarm role presets to restrict child worker allowlists; the names themselves resolve back to entries in this inventory. Default capability gate (from `internal/agent/tools/registry/definition.go:88`) is `tool.execute`; only the swarm tools override to `swarm.spawn`.

## Inventory

| name | file:line | capability_gate | category | args | source | gate_flag |
|------|-----------|-----------------|----------|------|--------|-----------|
| file | `internal/agent/tools/registry/file.go:177` (helpers: `workspace_files.go:62,104,155,201,266`) | tool.execute | storage-write | action, path, limit, max_bytes, content, pattern, globs, old, new, replace_all | static-registry | `AURA_WORKSPACE_TOOLS=enabled` (cmd/aura/app.go:322) |
| source | `internal/agent/tools/registry/source_unified.go:207` (helpers: `source_list.go:50,94`, `source_read.go:55`, `source_store.go:54`, `source_ocr.go:46`, `source_delete.go:56`, `ingest.go:45`) | tool.execute | storage-write | action, source_id, status, kind, limit, max_bytes, mode, byte_start, byte_end, filename, content, stages | static-registry | always (registered in cmd/aura/app.go:415 when sourceTool != nil) |
| web | `internal/agent/tools/registry/web.go:115` (helpers: `searxng.go:74`, `direct_fetch.go:168`) | tool.execute | external-API | action, query, max_results, category, language, time_range, url | static-registry | `WEB_SEARCH_PROVIDER=searxng` (cmd/aura/app.go:343) |
| doc | `internal/agent/tools/registry/doc.go:150` (helpers: `files_xlsx.go:87`, `files_docx.go:98`, `files_pdf.go:99`) | tool.execute | storage-write | action, filename, deliver, caption, title, sheets, blocks | static-registry | always (cmd/aura/app_wire.go:240) |
| wiki_page | `internal/agent/tools/registry/wiki.go:200` | tool.execute | storage-write | action, title, slug, body, expected_updated_at, category, tags, related, sources, old_text, new_text, heading, source_id | static-registry | always (cmd/aura/app_wire.go:243) |
| task | `internal/agent/tools/registry/scheduler.go:210` | tool.execute | storage-write | action, name, kind, payload, in, at_local, at, daily, weekdays, every_minutes, status | static-registry | always (cmd/aura/app.go:360 — requires schedStore) |
| search_memory | `internal/agent/tools/registry/memory_search.go:161` | tool.execute | read-only | query, scope, limit, chat_id, source_id | static-registry | always (cmd/aura/app_wire.go:482) |
| recall_operational | `internal/agent/tools/registry/recall_operational.go:83` | tool.execute | read-only | query, tool_name, error_class, limit | static-registry | always (cmd/aura/app_wire.go:488) |
| recall_user_memory | `internal/agent/tools/registry/recall_user_memory.go:81` | tool.execute | read-only | query, category, limit | static-registry | always (cmd/aura/app_wire.go:492) |
| agent_note | `internal/agent/tools/registry/agent_note.go:99` | tool.execute | memory-write | action, content, line | static-registry | always (cmd/aura/app_wire.go:200) |
| daily_briefing | `internal/agent/tools/registry/daily_briefing.go:83` | tool.execute | read-only | limit | static-registry | always (cmd/aura/app_wire.go:216) |
| propose_patch | `internal/agent/tools/registry/propose_patch.go:127` | tool.execute | memory-write | action, target_slug, body, fact, category, tool_name, error_class, lesson, change_summary | static-registry | always (cmd/aura/app_wire.go:194) |
| ask_user | `internal/agent/tools/registry/ask_user.go:91` | tool.execute | read-only | question, options, kind | static-registry | always (cmd/aura/app_wire.go:236) |
| execute_code | `internal/agent/tools/registry/exec.go:148` | tool.execute | sandbox-exec | code, timeout, tools_allowed, max_calls | static-registry | always (cmd/aura/app_wire.go:224 — gated nil-safe via SandboxMgr) |
| execute_shell | `internal/agent/tools/registry/exec.go:416` | tool.execute | sandbox-exec | command, timeout | static-registry | always (cmd/aura/app_wire.go:227 — gated nil-safe via SandboxMgr) |
| dev_tool | `internal/agent/tools/registry/tool_mgmt.go:119` | tool.execute | storage-write | action, name, description, params, code, usage | static-registry | always (cmd/aura/app_wire.go:230 — requires ToolReg) |
| tool_search | `internal/agent/tools/registry/tool_search.go:87` | tool.execute | read-only | query, limit | static-registry | always (cmd/aura/app_wire.go:233) |
| request_dashboard_token | `internal/agent/tools/registry/auth.go:65` | tool.execute | external-API | (none) | static-registry | always (cmd/aura/app_wire.go:237) |
| subagent_dispatch | `internal/agent/tools/registry/subagent.go:193` | tool.execute | sandbox-exec | action, nodes, child_run_ids | static-registry | always (cmd/aura/main.go:369 — after Hub construction) |
| run_aurabot_swarm | `internal/agent/tools/swarm/tools.go:71` | swarm.spawn | sandbox-exec | goal, mode, roles | swarm | `AURABOT_ENABLED=true` (cmd/aura/app.go:436, +deps.LLM != nil) |
| spawn_aurabot | `internal/agent/tools/swarm/tools.go:202` | swarm.spawn | sandbox-exec | name, role, task, tools, mode | swarm | `AURABOT_ENABLED=true` (cmd/aura/app.go:436) |
| list_swarm_tasks | `internal/agent/tools/swarm/tools.go:347` | tool.execute | read-only | run_id | swarm | `AURABOT_ENABLED=true` (cmd/aura/app.go:436) |
| read_swarm_result | `internal/agent/tools/swarm/tools.go:404` | tool.execute | read-only | task_id | swarm | `AURABOT_ENABLED=true` (cmd/aura/app.go:436) |
| mcp_&lt;server&gt;_&lt;tool&gt; | `internal/agent/tools/registry/mcp.go:79` (Execute); discovery loop `internal/mcp/client.go:203` (`loadTools()` → JSON-RPC `tools/list` at boot for each server); registration callback `cmd/aura/app.go:493-505` filters per server via `mcp.ToolEnabledForAura` then `toolRegistry.Register` | tool.execute | external-API | (server-defined per upstream `inputSchema`; argument keys are passthrough — values redacted by `sensitiveArgKeyRe` in `registry.go:400` before logging) | mcp-dynamic | always per advertised tool, conditional on `MCP_SERVERS_PATH` (cmd/aura/app.go:473) loading without error AND each tool passing `mcp.ToolEnabledForAura` (env-driven enable/disable per server, `internal/mcp/policy.go`) |

## Curated sets (not LLM-callable directly)

The `internal/agent/tools/sets/toolsets.go` file declares six named toolsets (`memory_read`, `wiki_review`, `skills_read`, `web_research`, `sandbox_code`, `scheduler_safe`) and five worker role presets (`librarian`, `critic`, `researcher`, `skillsmith`, `synthesizer`). These are consumed by:

- `spawn_aurabot.tools` arg (validated in `internal/agent/tools/swarm/tools.go:222` via `resolveRoleTools` → `toolsets.RoleTools`).
- `run_aurabot_swarm` role fan-out (`tools.go:99` `workerRolesForPolicy`).
- `task.action=schedule` with `kind=agent_job` (scheduler-safe toolset).

Toolset entries (`search_memory`, `file`, `source`, `web`, `execute_code`, `execute_shell`, `dev_tool`) all resolve to entries already in the static-registry section above. No additional LLM-visible tool names originate from this file.

## Skills

`internal/skills/loader.go` enumerates `SKILL.md` files under `SKILLS_PATH` (+ catalog roots from `cmd/aura/app.go:337 skillSearchRoots`). The system-prompt manifest (`internal/skills/loader.go:LoadAll`, max ~8 KiB) lists names + descriptions only. There is NO standalone `read_skill` tool registered with the Registry — when the LLM asks for a skill body it calls `file` with `action="read"` and a path under the skills root. `internal/agent/loop.go:411` detects this via `SkillNameFromReadFileArgs` and records the skill name in `stats.ReadSkills`. `internal/agent/untrusted.go:24` includes the synthetic `"read_skill"` name in the untrusted-output envelope set so prompt-injection in installed skill bodies stays labelled. Skill source count therefore contributes **0 distinct LLM-callable tool names**.

## Count summary

- static-registry: 19 tools (`file`, `source`, `web`, `doc`, `wiki_page`, `task`, `search_memory`, `recall_operational`, `recall_user_memory`, `agent_note`, `daily_briefing`, `propose_patch`, `ask_user`, `execute_code`, `execute_shell`, `dev_tool`, `tool_search`, `request_dashboard_token`, `subagent_dispatch`)
- curated-set: 0 directly-callable tools (6 named toolsets + 5 role presets, all resolving to static-registry names)
- skill-manifest: 0 (skills are read via `file action=read`, not via a dedicated tool)
- mcp-dynamic: N (one wrapped `MCPTool` per advertised upstream tool that passes `mcp.ToolEnabledForAura`; discovery via `internal/mcp/client.go:203 loadTools()` JSON-RPC `tools/list` at `bootstrap()`; registration callback `cmd/aura/app.go:498 tools.NewMCPTool(cli, name, t)` followed by `toolRegistry.Register`). Count is operator-dependent on `mcp.json`; the production registry shape is `mcp_<server>_<tool>`.
- swarm: 4 (`run_aurabot_swarm`, `spawn_aurabot`, `list_swarm_tasks`, `read_swarm_result`) — verified against `internal/agent/tools/swarm/tools.go` Name() returns at lines 33, 150, 317, 374.
- TOTAL: 23 static (19 registry + 4 swarm) + N MCP tools
- total_verified: true (re-counted via grep on `func (.+) Name() string { return "` across `internal/agent/tools/registry/` and `internal/agent/tools/swarm/` — yields 19 unique production-registered registry tools + 4 swarm tools; subordinate sub-tools `list_files`/`read_file`/`search_files`/`write_file`/`apply_patch`/`list_sources`/`lint_sources`/`store_source`/`read_source`/`ocr_source`/`delete_source`/`ingest_source`/`web_search`/`web_fetch`/`create_xlsx`/`create_docx`/`create_pdf` are present in registry but NOT independently `Register`ed by `cmd/aura/app.go`/`app_wire.go`/`main.go` — they are wired inside their unified parent tool (`file`, `source`, `web`, `doc`) and reachable only via the parent's `action=` dispatch; they appear independently in `cmd/debug_ingest/main.go` and `cmd/debug_tools/main.go` only)
