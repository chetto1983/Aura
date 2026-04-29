# Picobot Tools Audit for Aura

Date: 2026-04-29  
Reference path: `D:\tmp\picobot\internal\agent\tools`

## Summary

Picobot has a broad agent toolbox: messaging, memory files, web access, workspace filesystem, command execution, reminders, skills, MCP delegation, and a stub for subagents. Aura should not copy the whole surface directly. Aura's product is a standalone second brain, so Picobot's tools should be translated into source, wiki, review, scheduler, and admin capabilities.

Current Aura tools:

- `web_search`
- `web_fetch`
- `write_wiki`
- `read_wiki`
- `search_wiki`

Picobot tools found:

- `message`
- `filesystem`
- `exec`
- `web`
- `web_search`
- `spawn`
- `cron`
- `write_memory`
- `list_memory`
- `read_memory`
- `edit_memory`
- `delete_memory`
- `create_skill`
- `list_skills`
- `read_skill`
- `delete_skill`
- `mcp_<server>_<tool>`

## High-Level Decision

Copy the architectural pattern, not the full behavior.

- Keep Aura's current registry/tool-call loop.
- Keep Aura's `web_search` and `web_fetch`; do not add Picobot's duplicate `web` tool.
- Translate Picobot memory into Aura wiki/source tools.
- Promote Picobot cron into a SQLite-backed reminder and maintenance scheduler.
- Defer filesystem, exec, skills, MCP, and spawn until they have explicit trust gates.
- Never expose raw tool arguments in user-visible progress messages.

## Tool Audit

| Tool | Picobot behavior | Strengths | Risks / gaps | Aura decision |
| --- | --- | --- | --- | --- |
| `message` | Sends content to current chat through chat hub. | Enables multi-message tool-driven progress. | Can bypass final response path; can spam chat; content not policy-gated. | Optional `notify_user` later. Current Aura can send progress/final replies directly. |
| `filesystem` | Read/write/list files under workspace via `os.Root`. | Strong path containment model; useful for local source import. | Write access is broad; no review queue; no file type policy. | P2 admin-only. For P0 use dedicated `store_source` instead. |
| `exec` | Runs array-form commands with timeout and some blacklist checks. | Useful debug/admin tool; tests reject shell string and obvious dangerous commands. | Registered without workspace restriction in Picobot loop; blacklist is incomplete; no allowlist; output may leak secrets. | P2 admin/debug only, disabled by default. Prefer command allowlist and workspace restriction. |
| `web` | Raw HTTP GET for a URL. | Simple fetch. | No status handling, response size cap, content-type policy, SSRF protection, or timeout on default client. | Do not copy. Aura's `web_fetch` covers this with better API shape. |
| `web_search` | DuckDuckGo Instant Answer API. | Free, tested, no API key. | Low recall; Instant Answer is not full search; can return empty frequently. | Keep Aura's Ollama-backed `web_search`. Do not replace. |
| `spawn` | Stub returns acknowledgement. | Placeholder for background agents. | Not real execution; unclear state model. | Defer. Use deterministic jobs first. |
| `cron` | In-memory schedule/list/cancel for one-time or recurring reminders. | Good UX; useful for reminders and maintenance. | Lost on restart; recurring add ignores separate initial delay semantics; no database; no delivery audit. | P0/P1 as SQLite-backed `schedule_task`, `list_tasks`, `cancel_task`. |
| `write_memory` | Writes today's note or long-term `MEMORY.md`; blocks heartbeat-like content. | Useful daily/long memory split. | Appends raw memory, no schema, no linking, no source attribution. | Translate to `write_wiki`, `store_source`, and future journal/source inbox behavior. |
| `list_memory` | Lists files in `memory/`. | Simple visibility. | File-oriented, not graph/source aware. | Replace with `list_wiki` and `list_sources`. |
| `read_memory` | Reads long, today, or dated memory file. | Straightforward retrieval. | Not semantic; file targets only. | Covered by `read_wiki`; add `read_source`. |
| `edit_memory` | Global find/replace in memory file. | Basic correction mechanism. | Broad replace can damage notes; no diff/review; exact text brittle. | Replace with `update_wiki` and `propose_wiki_change`. |
| `delete_memory` | Deletes dated memory file, protects `MEMORY.md`. | Has guardrails for long-term memory. | Hard delete; no archive or review. | Add `archive_wiki` / `delete_source` later behind review. |
| `create_skill` | Creates `skills/<name>/SKILL.md` with frontmatter. | Agent can extend itself. | Self-modification risk; weak name validation; no approval workflow. | Defer, admin-only after review queue exists. |
| `list_skills` | Lists local skills. | Low risk read-only. | Depends on skill system decision. | P2 read-only admin tool. |
| `read_skill` | Reads a skill's full content. | Useful for introspection. | Can expose internal instructions if used carelessly. | P2 admin-only. |
| `delete_skill` | Removes skill directory. | Useful cleanup. | Destructive; no review; weak name policy. | Defer; require review and admin gate. |
| `mcp_<server>_<tool>` | Registers every configured MCP server tool dynamically. | Powerful ecosystem bridge. | Trust boundary is external; tool names/args/logging need policy; stdio servers spawn processes. | P2. Add allowlist per server/tool before enabling. |

## Registry and Loop Findings

Picobot registry:

- Stores tools by name.
- Returns provider-facing definitions.
- Executes by name.
- Logs raw JSON arguments.

Aura already improved this by logging only argument keys in `internal/tools/registry.go`. Keep that behavior. Do not copy Picobot's raw-argument logging because arguments may include source text, URLs with tokens, file paths, or private notes.

Picobot loop:

- Registers all default tools at startup.
- Sends user-visible tool activity messages with tool name and raw arguments.
- Has a regex shortcut for `remember ...` that writes directly to memory without LLM/tool path.
- Sets context for `message` and `cron` tools.
- Treats `heartbeat` and `cron` channels as stateless to avoid session bloat.

Aura should keep the tool loop model but avoid prompt sniffing and raw args in progress messages. Aura already removed memory prompt-sniffing and uses tool calls, which is the better direction.

## Supporting Package Findings

### Memory Store

Picobot memory is file-backed under `memory/`:

- `MEMORY.md` for long-term memory.
- `YYYY-MM-DD.md` for daily notes.
- Recent memory from today's and long-term files is injected into context.
- Delete is limited to explicit dated files.

Aura should not add a parallel memory directory. The wiki is the durable memory layer. Daily notes should become sources or journal pages, not a separate memory system.

### Cron Scheduler

Picobot cron is in-memory:

- One-time jobs are removed after firing.
- Recurring jobs reschedule in memory.
- Jobs contain channel/chat context.

Aura needs SQLite persistence because the user explicitly wants reliable reminders and Aura already has `DB_PATH`. Use the scheduler as UX reference, not as storage architecture.

### MCP Client

Picobot MCP supports stdio and HTTP:

- Initializes MCP.
- Lists tools.
- Wraps each MCP tool as `mcp_<server>_<tool>`.
- Calls tools and returns text content.

Aura should implement this only after core source/wiki tools. Stdio MCP starts child processes, so it belongs behind admin config and per-tool allowlists.

## Must-Have Aura Tool Set

Before standalone UI:

- Existing: `web_search`, `web_fetch`, `write_wiki`, `read_wiki`, `search_wiki`
- Add: `store_source`, `ocr_source`, `read_source`, `ingest_source`, `list_sources`
- Add: `append_log`, `rebuild_index`, `list_wiki`, `lint_wiki`
- Add: `schedule_task`, `list_tasks`, `cancel_task`

After core source/wiki/reminder flow:

- `update_wiki`
- `propose_wiki_change`
- `apply_wiki_change`
- `lint_sources`
- `graph_query`

Admin/deferred:

- `filesystem`
- `exec`
- `notify_user`
- skill tools
- MCP adapter
- `spawn`

## Security Rules for Aura

- Tools that mutate durable memory must either be schema-bound or reviewable.
- Tools that touch arbitrary files must be admin-only and workspace-contained.
- Tools that execute commands must be disabled by default and use an allowlist, not only a blacklist.
- Tools that call external services must never log API keys, raw documents, OCR base64, or full source content.
- User-visible tool activity must show only tool names, never raw arguments.
- Destructive tools need archive/review flows before hard delete.
- Dynamic MCP tools need per-server and per-tool allowlists.

## Test Requirements to Port

From Picobot:

- Registry definitions and dispatch.
- Web search success, empty result, and HTTP error behavior.
- Memory list/read/edit/delete equivalents.
- Exec string-command rejection and timeout behavior if exec is added.
- Skill create/list/read/delete tests if skill tools are added.
- MCP wrapper name, description, schema, and call behavior if MCP is added.
- Cron fire, list, cancel, and recurring behavior.

Aura-specific additions:

- Natural-prompt smoke tests for every LLM-facing tool.
- SQLite restart test for reminders.
- PDF source duplicate detection by sha256.
- Mistral OCR fake-server test.
- Wiki lint fixture with broken links, orphans, missing sources, and duplicates.

## Implementation Priority

P0:

- Source/PDF OCR pipeline.
- Wiki maintenance basics.
- SQLite-backed reminders.

P1:

- Reviewable wiki updates.
- Graph queries.
- Source lint.

P2:

- Admin filesystem.
- Admin exec.
- Skill management.
- MCP adapter.
- Spawn/background subagents.

The product should feel powerful because the wiki stays maintained, not because every low-level operating-system capability is exposed to the model on day one.
