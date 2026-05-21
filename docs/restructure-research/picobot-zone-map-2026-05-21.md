# picobot zone-map study — how WHERE-WHAT-OWNS is communicated to the LLM

Date: 2026-05-21
Source: `D:/tmp/picobot` (commit on disk, no version pin)
Scope: zone/tool ownership only — output discipline already covered in `picobot-output-discipline-2026-05-21.md`

## TL;DR

picobot exposes **~17 built-in tools across ~5 data zones** but **does not enumerate zones in the system prompt** — instead each tool's name AND its required-parameter `enum` carries the zone identity (`target: today|long|YYYY-MM-DD`, `action: read|write|list`). Ownership is enforced by the simple fact that only one tool is named for each zone (no two tools both write memory). The closest thing to a zone map is the static bootstrap-file list (`SOUL.md / AGENTS.md / USER.md / TOOLS.md`) which the user — not the LLM — populates per workspace.

## Zone + tool inventory

Tool count (registered in `internal/agent/loop.go:78-117`, plus dynamic MCP at `:139`):

| Zone | Read tool | Write tool | Source |
|------|-----------|------------|--------|
| Workspace files (sandboxed `os.Root`) | `filesystem` (action=read/list) | `filesystem` (action=write) | `tools/filesystem.go:37-60` |
| Memory — today / long / dated | `read_memory`, `list_memory` | `write_memory`, `edit_memory`, `delete_memory` | `tools/memory.go:39-223`, `tools/write_memory.go:44-69` |
| Skills (workspace `skills/` subdir) | `list_skills`, `read_skill` | `create_skill`, `delete_skill` | `tools/skill.go:148-305` |
| Web | `web_search` (DuckDuckGo IA), `web` (fetch URL) | — (read-only) | `tools/web_search.go:28-44`, `tools/web.go:17-31` |
| Scheduling / cron | `cron` (action=list) | `cron` (action=add/cancel) | registered `loop.go:99` |
| Chat output | — | `message` (send to current channel) | `loop.go:80` |
| Subagent | — | `spawn` (stub) | `loop.go:97` |
| Shell | `exec` (array-form only) | `exec` | `loop.go:94` |
| MCP (dynamic) | per-server | per-server | `loop.go:121-141` |

Five clean zones (workspace / memory / skills / web / cron). Total built-in = 17 (filesystem, exec, web, web_search, spawn, message, cron, 5 memory, 4 skill, plus N MCP).

## HOW picobot tells the LLM "where what is"

### 1. System prompt — purely tool-agnostic

`internal/agent/context.go:38` — opens with a single bland line:

```go
sysParts = append(sysParts, "You are Picobot, a helpful assistant.")
```

Then concatenates four bootstrap markdown files in fixed order at `:41`:

```go
bootstrapFiles := []string{"SOUL.md", "AGENTS.md", "USER.md", "TOOLS.md"}
```

These files are **empty in the embedded repo** — only `embeds/skills/*/SKILL.md` ships pre-filled. The user (or `onboard` cmd) writes them per workspace. `TOOLS.md` is the canonical place to declare a zone map, but picobot does not provide one; it leaves the doctrine slot open.

Then a single line at `:55-57` about channel access, and at `:60` a single nudge for `write_memory` JSON shape. That is the entirety of the static system prompt — **no zone enumeration, no decision tree, no "if user asks Y use Z"**.

### 2. Tool descriptions — terse, one-line, never claim ownership

Every Description() is one sentence with no "I am the only tool that …" framing. Examples (grep over `tools/*.go`):

- `filesystem.go:38` — `"Read, write, and list files in the workspace"`
- `write_memory.go:45-47` — `"Write or append to memory (today's note or long-term MEMORY.md). NEVER store heartbeat status, health checks, or 'no pending tasks' results."`
- `memory.go:40-42` — `"List all memory files (daily notes and long-term memory)"`
- `web_search.go:30` — `"Search the web using DuckDuckGo and return relevant results"`
- `web.go:18` — `"Fetch web content from a URL"`

Ownership is implicit: only one tool is named `write_memory`, only one `filesystem`. The LLM picks by name + parameter schema, not by reading a prose ownership map.

### 3. Parameter `enum`s carry zone identity

This is picobot's main trick — the **JSON Schema `enum` field IS the zone selector**:

- `filesystem.go:47` — `"enum": []string{"read", "write", "list"}` for `action`
- `memory.go:88` — `target` description: `"'today' for today's note, 'long' for long-term memory, or a date 'YYYY-MM-DD'"`
- `write_memory.go:56` — `"enum": []string{"today", "long"}` on `target`

One tool covers the zone; the `action`/`target` enum routes inside it. This collapses what Aura currently has as e.g. `search_memory + list_memory + read_memory + forget_memory` (4 separate names) into one tool with `action` enum, or two tools (read-side + write-side) if mutation is risky.

### 4. Cross-tool routing via prose hint in tool output

One concrete decision tree, written into a tool *result*, not the system prompt — `web_search.go:163`:

```go
fmt.Fprintf(&sb, "\nNo instant answer found. Try the 'web' tool to visit a specific URL.\n")
```

So when `web_search` finds nothing, the *response text* tells the model the next-hop tool by name. Routing is downstream, lazy, and only when needed.

### 5. Skills as overflow zone — progressive disclosure

`embeds/skills/*/SKILL.md` (`example`, `cron`, `weather`) ship pre-loaded. The README at `internal/agent/skills/README.md:109-114` states all skills are pulled into context per turn — NOT progressive disclosure. Picobot eagerly inlines all `SKILL.md` bodies (`context.go:67-74`). This is fine at 3 demo skills, but does not scale — Aura's progressive disclosure (manifest-then-read) is the better pattern.

## 3-5 patterns Aura should lift

1. **One tool per zone, `action` enum inside.** Aura's `search_memory / list_memory / read_memory / forget_memory` should collapse into `memory` with `action: search|list|read|forget` — and similarly for sources, workspace, tasks. Surface goes 30 → ~10. Pattern: `tools/filesystem.go:40-60`.

2. **`target` parameter enum as zone selector.** When a zone has named sub-locations (today/long/dated, or wiki/sources/workspace), make them `enum` choices on a single `target` param with the description spelling them out (`memory.go:86-91`). The LLM picks by argument, not by tool name.

3. **Reserve `TOOLS.md` as the canonical, user-editable zone-map overlay.** picobot leaves it empty; Aura ships it pre-filled with the explicit ownership table ("wiki = read via `wiki.search`, write via `wiki.upsert`; sources = read via `source.read`, ingest via `source.ingest`; …"). This is exactly the surface Aura's `PROMPT_OVERLAY_PATH` already supports — fill it instead of inflating the static system prompt.

4. **Cross-tool routing via tool output, not system prompt.** When a read tool finds nothing, append `"Try X for Y"` to the response (`web_search.go:163`). No prompt bloat, just-in-time signal. Aura's `search_memory` empty hit should tell the model to try `web_search` or `read_source`.

5. **Ownership by name uniqueness, not by prose claim.** No tool description in picobot says "I'm the only writer for zone X". The registry guarantees it (`registry.go:39` — `r.tools[t.Name()] = t` last-write-wins by name). Aura should rely on the same invariant: one canonical name per zone-action, not 3 verbs ("store", "create", "save") for the same write.

## Anti-patterns picobot has — DO NOT copy

1. **Empty bootstrap files at install time.** `SOUL.md / AGENTS.md / USER.md / TOOLS.md` are referenced in `context.go:41` but the repo ships none of them. A fresh picobot install gives the LLM zero zone map; the first user is expected to author the overlay. Aura should **ship default overlays** so day-1 LLM already has a zone map; the user can edit later.

2. **Skills are eagerly inlined every turn.** `context.go:67-74` walks `LoadAll()` and dumps every `SKILL.md` body into the system message. Three demo skills today, but no manifest tier, no lazy fetch. Aura's existing `read_skill` progressive-disclosure pattern is strictly better — keep it.

3. **Cron + filesystem + exec exposed as single mega-tools with `enum` actions.** Good for zone clarity, but `exec` (`loop.go:94`, `tools/exec.go`) has no zone — it's a wild-card "shell". Aura should keep `execute_code` behind a sandbox boundary, NOT widen it into a multi-action tool — the action enum pattern only works when the zone has a meaningful inside.

---

Files referenced:
- `D:/tmp/picobot/internal/agent/context.go:38-94` (system-prompt assembly)
- `D:/tmp/picobot/internal/agent/loop.go:78-141` (tool registration — total surface)
- `D:/tmp/picobot/internal/agent/tools/registry.go:36-62` (name-uniqueness ownership)
- `D:/tmp/picobot/internal/agent/tools/filesystem.go:40-60` (action-enum pattern)
- `D:/tmp/picobot/internal/agent/tools/memory.go:39-223` (zone-via-target pattern)
- `D:/tmp/picobot/internal/agent/tools/write_memory.go:44-69` (negative-discipline in Description)
- `D:/tmp/picobot/internal/agent/tools/web_search.go:163` (lazy cross-tool routing)
- `D:/tmp/picobot/internal/agent/skills/README.md:109-114` (eager-load anti-pattern)

Word count: ~990.
