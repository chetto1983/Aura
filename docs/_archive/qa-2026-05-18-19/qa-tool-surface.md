# Tool Surface — 2026-05-19 — git_head: 4e2eb06d3ce7c02044223b47d5e14a76d15ff4b3

Inventory of LLM-callable tools that Aura exposes through the production `*tools.Registry`. The count is based on production registration sites in `cmd/aura/app.go`, `cmd/aura/app_wire.go`, and `cmd/aura/main.go`, not every concrete helper type with a `Name()` method. The default registry execution gate is `tool.execute` via `internal/agent/tools/registry/definition.go:80` and `internal/agent/tools/registry/registry.go:264`; swarm spawn tools narrow this to `swarm.spawn`.

The cold per-turn pool starts with `tool_search` only (`internal/agent/toolsprovider.go:31`), while `internal/agent/tools/registry/manifest.go:33` lists every registered tool by name in the system prompt and `internal/agent/pool.go:79` permissive-loads schemas for direct calls by registered name.

## Inventory

| name | file:line | capability_gate | category | args | source | gate_flag |
|------|-----------|-----------------|----------|------|--------|-----------|
| `file` | `internal/agent/tools/registry/file.go:178` (helpers: `workspace_files.go:62`, `workspace_files.go:104`, `workspace_files.go:155`, `workspace_files.go:201`, `workspace_files.go:266`) | `tool.execute` | storage-write | action, path, limit, max_bytes, content, pattern, globs, old, new, replace_all | static-registry | `AURA_WORKSPACE_TOOLS` enabled (`cmd/aura/app.go:322`; default enabled in `internal/config/config.go:28`, `internal/config/config.go:132`, `internal/config/config.go:288`) |
| `source` | `internal/agent/tools/registry/source_unified.go:207` (helpers: `source_list.go:50`, `source_list.go:94`, `source_read.go:55`, `source_store.go:54`, `source_ocr.go:46`, `source_delete.go:56`, `ingest.go:45`, `source_unified.go:255`) | `tool.execute` | storage-write | action, source_id, status, kind, limit, max_bytes, mode, byte_start, byte_end, filename, content, stages | static-registry | always (`cmd/aura/app.go:415`) |
| `web` | `internal/agent/tools/registry/web.go:115` (helpers: `searxng.go:74`, `direct_fetch.go:168`) | `tool.execute` | external-API | action, query, max_results, category, language, time_range, url | static-registry | `WEB_SEARCH_PROVIDER=searxng` (`cmd/aura/app.go:343`; default disabled in `internal/config/config.go:66`, `internal/config/config.go:241`) |
| `doc` | `internal/agent/tools/registry/doc.go:150` (helpers: `files_xlsx.go:87`, `files_docx.go:98`, `files_pdf.go:99`) | `tool.execute` | storage-write | action, filename, deliver, caption, title, sheets, blocks | static-registry | always (`cmd/aura/app_wire.go:295`) |
| `wiki_page` | `internal/agent/tools/registry/wiki.go:200` (helpers: `wiki.go:237`, `wiki.go:266`, `wiki.go:308`, `wiki.go:351`, `wiki.go:398`) | `tool.execute` | storage-write | action, title, slug, body, expected_updated_at, category, tags, related, sources, old_text, new_text, heading, source_id | static-registry | always (`cmd/aura/app_wire.go:298`) |
| `task` | `internal/agent/tools/registry/scheduler.go:210` (helpers: `scheduler.go:239`, `scheduler.go:381`, `scheduler.go:419`, `scheduler.go:440`) | `tool.execute` | storage-write | action, name, kind, payload, in, at_local, at, daily, weekdays, every_minutes, status | static-registry | always (`cmd/aura/app.go:360`) |
| `search_memory` | `internal/agent/tools/registry/memory_search.go:154` | `tool.execute` | read-only | query, scope, limit, chat_id, source_id | static-registry | always (`cmd/aura/app_wire.go:556`) |
| `recall_operational` | `internal/agent/tools/registry/recall_operational.go:87` | `tool.execute` | read-only | query, tool_name, error_class, limit | static-registry | always (`cmd/aura/app_wire.go:562`) |
| `recall_user_memory` | `internal/agent/tools/registry/recall_user_memory.go:81` | `tool.execute` | read-only | query, category, limit | static-registry | always (`cmd/aura/app_wire.go:566`) |
| `agent_note` | `internal/agent/tools/registry/agent_note.go:99` | `tool.execute` | memory-write | action, content, line | static-registry | always (`cmd/aura/app_wire.go:255`) |
| `daily_briefing` | `internal/agent/tools/registry/daily_briefing.go:83` | `tool.execute` | read-only | limit | static-registry | always (`cmd/aura/app_wire.go:271`) |
| `propose_patch` | `internal/agent/tools/registry/propose_patch.go:165` (helpers: `propose_patch.go:189`, `propose_patch.go:218`, `propose_patch.go:247`) | `tool.execute` | memory-write | action, target_slug, body, fact, category, tool_name, error_class, lesson, priority, change_summary | static-registry | always (`cmd/aura/app_wire.go:203`) |
| `ask_user` | `internal/agent/tools/registry/ask_user.go:91` | `tool.execute` | read-only | question, options, kind | static-registry | always (`cmd/aura/app_wire.go:291`) |
| `execute_code` | `internal/agent/tools/registry/exec.go:129` (helpers: `exec.go:245`, `exec.go:261`, `exec.go:443`, `exec.go:481`) | `tool.execute` | sandbox-exec | code, timeout, tools_allowed, max_calls | static-registry | `SANDBOX_ENABLED=true` and process runtime available (`cmd/aura/helpers.go:77`, `cmd/aura/app_wire.go:279`; default true in `internal/config/config.go:163`, `internal/config/config.go:317`) |
| `execute_shell` | `internal/agent/tools/registry/exec.go:397` | `tool.execute` | sandbox-exec | command, timeout | static-registry | `SANDBOX_ENABLED=true` and process runtime available (`cmd/aura/helpers.go:77`, `cmd/aura/app_wire.go:282`; default true in `internal/config/config.go:163`, `internal/config/config.go:317`) |
| `dev_tool` | `internal/agent/tools/registry/tool_mgmt.go:119` (helpers: `tool_mgmt.go:145`, `tool_mgmt.go:160`, `tool_mgmt.go:172`) | `tool.execute` | storage-write | action, name, description, params, code, usage | static-registry | always (`cmd/aura/app_wire.go:285`) |
| `tool_search` | `internal/agent/tools/registry/tool_search.go:87` | `tool.execute` | read-only | query, limit | static-registry | always (`cmd/aura/app_wire.go:288`) |
| `request_dashboard_token` | `internal/agent/tools/registry/auth.go:65` | `tool.execute` | external-API | none | static-registry | always (`cmd/aura/app_wire.go:292`) |
| `subagent_dispatch` | `internal/agent/tools/registry/subagent.go:193` (helpers: `subagent.go:301`, `subagent.go:350`) | `tool.execute` | sandbox-exec | action, nodes, child_run_ids, goal, instruction, tool_allowlist, budget_secs, risk_tier | static-registry | always after Telegram Hub construction (`cmd/aura/main.go:369`) |
| `run_aurabot_swarm` | `internal/agent/tools/swarm/tools.go:71` | `swarm.spawn` | sandbox-exec | goal, mode, roles | swarm | `AURABOT_ENABLED=true` and LLM configured (`cmd/aura/app.go:436`, `cmd/aura/app.go:432`; default false in `internal/config/config.go:99`, `internal/config/config.go:265`) |
| `spawn_aurabot` | `internal/agent/tools/swarm/tools.go:202` (helper: `tools.go:222`) | `swarm.spawn` | sandbox-exec | name, role, task, tools, mode | swarm | `AURABOT_ENABLED=true` and LLM configured (`cmd/aura/app.go:451`) |
| `list_swarm_tasks` | `internal/agent/tools/swarm/tools.go:347` | `tool.execute` | read-only | run_id | swarm | `AURABOT_ENABLED=true` and LLM configured (`cmd/aura/app.go:457`) |
| `read_swarm_result` | `internal/agent/tools/swarm/tools.go:404` | `tool.execute` | read-only | task_id | swarm | `AURABOT_ENABLED=true` and LLM configured (`cmd/aura/app.go:460`) |

## Dynamic MCP Pattern

No concrete MCP tools are configured in the checked workspace, so no runtime MCP rows are counted. The LLM-visible name shape is `mcp_<server>_<tool>` from `internal/agent/tools/registry/mcp.go:24`; execution enters `internal/agent/tools/registry/mcp.go:79`.

Discovery and registration path:

1. `cmd/aura/app.go:473` loads servers from `MCP_SERVERS_PATH`.
2. `internal/mcp/client.go:198` calls `loadTools()`, which sends JSON-RPC `tools/list`.
3. `cmd/aura/app.go:494` iterates `cli.Tools()`.
4. `cmd/aura/app.go:495` filters each advertised tool through `mcp.ToolEnabledForAura`.
5. `cmd/aura/app.go:498` wraps with `tools.NewMCPTool`.
6. `cmd/aura/app.go:500` and `cmd/aura/app.go:502` register the wrapper.

Gate flags for MCP are data-dependent: `MCP_SERVERS_PATH` controls the server list (`internal/config/config.go:98`, `internal/config/config.go:264`), while `internal/mcp/policy.go:34` permits/blocks mail and database tools. Mail mutations depend on `AURA_MAIL_*_ENABLE_IMAP_MUTATIONS` or `MAIL_IMAP_WRITE_ENABLED`; mail send tools depend on configured `MAIL_SMTP_*_USER`; database tools are limited to the read allowlist in `internal/mcp/policy.go:22`.

## Curated Sets

`internal/agent/tools/sets/toolsets.go:9` declares named toolsets, but these are not independent LLM tool definitions. They compose already-registered static tool names:

- `memory_read`: `search_memory`, `file`, `source`
- `wiki_review`: `search_memory`, `file`, `source`
- `skills_read`: `file`
- `web_research`: `web`
- `sandbox_code`: `execute_code`, `execute_shell`, `dev_tool`
- `scheduler_safe`: `search_memory`, `file`, `source`, `web`

Role presets in `internal/agent/tools/sets/toolsets.go:46` are consumed by `spawn_aurabot` and `run_aurabot_swarm`; agent-job schedules also resolve toolsets through `internal/cron/agent_job.go:103`. Count impact: `0` additional LLM-callable tool names.

## Skill Manifest

`internal/skills/loader.go:124` enumerates `SKILL.md` files from roots assembled by `cmd/aura/helpers.go:34`. `internal/skills/loader.go:247` renders names and descriptions into the system prompt manifest. Skill bodies are not registered as tools, and no standalone `read_skill` tool exists in the production registry.

Runtime skill-body reads are tracked synthetically: when the LLM calls `file` with `action=read` on a `SKILL.md` path, `internal/agent/loop.go:409` records the skill name via `internal/agent/loop.go:681`. `internal/agent/untrusted.go:19` includes synthetic `read_skill` in the untrusted-output set, but that string is not a registry tool name.

Important QA note: `internal/skills/loader.go:255` still tells the model to use `search_files` and `read_file`. Those helper names exist in `internal/agent/tools/registry/workspace_files.go`, but production registers the unified `file` tool at `cmd/aura/app.go:327` instead. Count impact: `0` additional LLM-callable tool names.

## Excluded Helper Tools

The following concrete `Name()` methods exist under `internal/agent/tools/registry/` but are delegated helpers, not production registry entries: `list_files`, `read_file`, `search_files`, `write_file`, `apply_patch`, `list_sources`, `lint_sources`, `read_source`, `store_source`, `ocr_source`, `delete_source`, `ingest_source`, `web_search`, `web_fetch`, `create_xlsx`, `create_docx`, `create_pdf`.

They are callable only through unified parent tools (`file`, `source`, `web`, `doc`) in the production registry. Debug binaries and tests may instantiate helpers directly; this inventory does not count those debug-only surfaces.

## Count Summary

- static-registry: 19 tools
- curated-set: 0
- skill-manifest: 0
- mcp-dynamic: 0 configured tools in the checked workspace; discovery mechanism documented in `Dynamic MCP Pattern`
- swarm: 4
- TOTAL: 23
- total_verified: true

Verification method: re-counted production registrations in `cmd/aura/app.go:322-505`, `cmd/aura/app_wire.go:199-299`, `cmd/aura/app_wire.go:556-568`, and `cmd/aura/main.go:369-373`; cross-checked concrete `Name()` methods under `internal/agent/tools/registry/` and `internal/agent/tools/swarm/`; then excluded delegated helper names that are not registered in production.
