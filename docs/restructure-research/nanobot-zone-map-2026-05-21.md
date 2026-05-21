# nanobot zone map study — 2026-05-21

Source: `D:/tmp/nanobot` @ commit (local snapshot). Focus: how the agent is told **where data lives** and **which tool owns each zone**.

## TL;DR

nanobot does NOT use enumerated tool descriptions to say "I own zone X". Instead it ships a tiny, surgically pruned tool surface (~14 builtins) and a **single anchor document** — `templates/agent/identity.md` — that names every persistent zone with an **absolute filesystem path**. Tool ownership is then implicit: there is exactly one tool per verb (`read_file`, `write_file`, `edit_file`, `grep`, `cron`, `message`, `web_search`, `web_fetch`), and the prompt simply tells the LLM "the wiki lives at this path → use the file tools" rather than "use the wiki tool". The zone count is 5; the cleverness is that 4 of those 5 zones share the same 3 file tools because they are all just markdown files in the workspace.

## Zone count and tool count

**Zones (5)** — `templates/agent/identity.md:4-8` + `docs/memory.md:14-18`:

1. **Workspace root** — `{{ workspace_path }}` (anything the user/agent writes day-to-day).
2. **Long-term memory** — `{workspace}/memory/MEMORY.md` (managed by Dream — agent advised NOT to edit directly).
3. **History log** — `{workspace}/memory/history.jsonl` (append-only consolidated summaries).
4. **Skills** — `{workspace}/skills/{skill-name}/SKILL.md` (progressive disclosure).
5. **Web** — implicit (`web_search` / `web_fetch`).

**Tool count: ~14 builtins.** Enumerated via `D:/tmp/nanobot/nanobot/agent/tools/*.py` (`return "<name>"` in each `name` property):

`read_file`, `write_file`, `edit_file`, `list_dir` (`filesystem.py`); `grep` (`search.py:121`); `exec` (`shell.py:131`); `cron` (`cron.py:116`); `message` (`message.py:133`); `web_search` + `web_fetch` (`web.py:109,440`); `generate_image` (`image_generation.py:107`); `notebook_edit` (`notebook.py:65`); `my` (`self.py:124`); `long_task` + `complete_goal` (`long_task.py:118,193`); `spawn` (`spawn.py:49`). MCP tools are loaded dynamically and segregated under an `mcp_` name prefix (`registry.py:62-69`).

Compare Aura: ~30 primitive tools split across 3+ overlapping read-paths into the wiki.

## How nanobot communicates "where what is" to the LLM

The whole story lives in **one rendered template** — `templates/agent/identity.md:1-10`:

```
## Workspace
Your workspace is at: {{ workspace_path }}
- Long-term memory: {{ workspace_path }}/memory/MEMORY.md (automatically managed by Dream — do not edit directly)
- History log: {{ workspace_path }}/memory/history.jsonl (append-only JSONL; prefer built-in `grep` for search).
- Custom skills: {{ workspace_path }}/skills/{skill-name}/SKILL.md
```

Three reinforcements stack on top:

1. **`identity.md:26-28` — Search & Discovery section**: "Prefer built-in `grep` over `exec` for workspace search. On broad searches, use `grep(output_mode=\"count\")` to scope before requesting full content." → explicit verb-to-tool routing.
2. **`identity.md:31-34` — disambiguation rules** for `message` vs natural reply, and for `read_file` vs `message(media=…)`: "Do NOT use read_file to 'send' a file — reading a file only shows its content to you, it does NOT deliver the file to the user." → kills the most common confusion-bug between two adjacent tools.
3. **`templates/AGENTS.md:4-19`** — hard rules for two zones that LOOK like they need new tools but don't: cron reminders ("Do NOT just write reminders to MEMORY.md") and heartbeat tasks ("`edit_file` to append new tasks. `write_file` to replace all tasks"). The zone (HEARTBEAT.md) is owned by `edit_file`/`write_file`, not by a `heartbeat_add` tool.

`ContextBuilder.build_system_prompt()` at `nanobot/agent/context.py:37-76` assembles the final prompt by concatenating `identity.md` + `AGENTS.md` + `SOUL.md` + `USER.md` + `TOOLS.md` (the four `BOOTSTRAP_FILES` at `context.py:25`) + memory + skills summary. The zone map is therefore part of the **system prompt header**, before any tool schema.

Tool descriptions stay short and do NOT claim zone ownership (e.g. `web_search` description at `web.py:109-114` is 3 sentences and references `web_fetch` once). Cross-references between tools are the disambiguation glue: `write_file` says "For partial edits, prefer edit_file instead" (`filesystem.py:367-371`); `web_search` says "Use web_fetch to read a specific page in full" (`web.py:111-114`); `message` says "Do NOT use read_file to send files" (`message.py:146`).

## Patterns Aura should lift

1. **One filesystem path = the zone declaration.** Put a `## Workspace` block at the very top of the system prompt that lists every persistent zone with its concrete path: wiki at `/workspace/wiki/`, sources at `/workspace/wiki/raw/`, user memory at `/workspace/memory/MEMORY.md`, scheduled tasks via tool, web via tool. The path IS the ownership signal — the LLM doesn't need a tool called `wiki_read` if it knows wiki pages are `.md` files at a known path and `read_file`/`grep` work there. Reference: `templates/agent/identity.md:4-8`.

2. **Verb-to-tool routing lines, not zone-to-tool sentences.** Add 3-5 lines like nanobot's "Prefer built-in `grep` over `exec` for workspace search" (`identity.md:27`). For Aura: "Prefer `wiki_search` over `web_search` for anything the user told you before" / "Prefer `read_source` over `read_file` for ingested PDFs/docx." This is decision-tree-by-prose, costs ~5 lines, and outperforms enumerating 30 tool descriptions.

3. **Tool cross-references inside descriptions.** Every adjacent-tool pair gets one sentence pointing to the sibling: `web_search.description` says "Use web_fetch to read a specific page in full" (`web.py:113`); `write_file.description` says "For partial edits, prefer edit_file instead" (`filesystem.py:370`). Aura's `search_memory` / `list_memory` / `read_memory` should cross-link the same way ("for full content use read_memory, for browsing use list_memory").

4. **Anti-bug warnings in the prompt, not in code.** nanobot puts the "Do NOT use read_file to send files" line (`identity.md:34`, `message.py:146`) and the "Do NOT just write reminders to MEMORY.md" line (`AGENTS.md:9`) in the prompt itself. Aura should bank the recurring confusion-bugs ("Do NOT use `web_search` for content already in the wiki" etc.) as one-liners in `AGENT.md`.

5. **Prune to one tool per verb.** nanobot has exactly one read tool (`read_file`), one write tool (`write_file`), one edit tool (`edit_file`), one search tool (`grep`), one fetch tool (`web_fetch`), one schedule tool (`cron`). The discoverable list at `registry.py:48-71` sorts builtins first then MCP tools — the LLM sees a stable, alphabetised, short surface. Aura today has `search_memory` + `list_memory` + `read_memory` + `forget_memory` PLUS `read_source` + `ocr_source` + `ingest_source` + `store_source`; collapsing per-verb (one read, one write, one search across BOTH zones, with a `zone` parameter) would shrink the surface by half.

## Anti-patterns nanobot has

1. **Zero machine-readable zone manifest.** The zone map exists only as a Jinja template the LLM reads as prose. There is no `zones.toml` or `ToolRegistry.zones()`. A new contributor adding a "calendar" zone has to remember to edit `identity.md` + the matching tool's description — nothing enforces consistency. For Aura's scale (more zones, more contributors, more drift) a tiny structured manifest (zone name → owner-tool-for-read → owner-tool-for-write → path glob) is worth the ceremony.

2. **`my` tool is a kitchen sink.** `self.py:124-154` defines a single `my` tool that handles "check model", "check token usage", "set scratchpad", "predict crash impact before changing model". Its description is 25 lines (`self.py:127-154`). It violates nanobot's own one-verb-per-tool rule and is the only tool in the registry whose name gives the LLM no clue what it does. Aura should NOT replicate this — keep introspection split from mutation, even if it grows the count by one.
