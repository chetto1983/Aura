# Agentic Tool Search And Programmatic Execution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Aura behave like a container-native planner/code-generator that discovers a few relevant tools on demand, writes orchestration scripts for multi-step work, keeps Telegram controllable, and prevents raw tool results from bloating future context.

**Architecture:** Keep the hot model-visible surface small: Telegram exposes core conversation tools plus `tool_search`, `execute_code`, and `execute_shell`; `tool_search` retrieves capability docs from Aura's registered tool metadata; `execute_code` becomes the preferred orchestration boundary for multi-tool workflows. Context hygiene compacts completed tool results after a turn, preserving valid assistant/tool-call protocol while dropping payload bulk. Telegram adds operator commands for reset/help/status/tool visibility.

**Tech Stack:** Go 1.x, SQLite/FTS initially, existing Mistral embedding settings for vector search, optional Qdrant later, existing `internal/tools.Registry`, existing `internal/agentruntime.SessionStore`, existing process sandbox in the Aura container, Telegram via `telebot.v4`.

---

## Status

Status: PLANNED
Started: 2026-05-09

## User Direction

- `AGENT.md` must not be injected into the system prompt.
- Pyodide is no longer the default runtime; Aura executes Python/shell directly inside the Aura container.
- The agent should stop asking the model for many individual tool calls and instead write orchestration code.
- The model should see only a few tools at a time, selected through semantic search over embedded tool descriptions.
- The only expansion path should be `tool_search`.
- Tool results must not accumulate as raw context.
- Telegram needs a real command menu, including a clear/reset conversation command.

Already implemented before this plan:

- Runtime prompt overlay injects only `SOUL.md`, `USER.md`, and `TOOLS.md`; `AGENT.md` and `AGENTS.md` are file-readable but not prompt-injected.
- Docker default runtime is `SANDBOX_RUNTIME_MODE=process`; Pyodide sidecar is removed from Compose.
- `execute_code` runs Python in the Aura container.
- `execute_shell` is registered in process mode.
- Container image includes Python, pip, git, ripgrep, jq, sqlite3, curl, zip/unzip, and related operator tools.

## Sources Checked

Official references used for this plan:

- Anthropic Tool Search Tool: <https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-search-tool>
- Anthropic Programmatic Tool Calling: <https://platform.claude.com/docs/en/agents-and-tools/tool-use/programmatic-tool-calling>
- Anthropic Manage Tool Context: <https://platform.claude.com/docs/en/agents-and-tools/tool-use/manage-tool-context>
- Anthropic Context Editing: <https://platform.claude.com/docs/en/build-with-claude/context-editing>
- OpenAI Function Calling: <https://developers.openai.com/api/docs/guides/function-calling>
- OpenAI Embeddings: <https://developers.openai.com/api/docs/guides/embeddings>
- OpenAI Prompt Caching: <https://developers.openai.com/api/docs/guides/prompt-caching>
- OpenAI Conversation State: <https://developers.openai.com/api/docs/guides/conversation-state>

Design translation for Aura:

- Anthropic's tool-search pattern maps to Aura's existing `internal/tools.Registry` plus a new searchable metadata index.
- Anthropic's programmatic tool calling maps to Aura's `execute_code` process runtime plus an injected tool SDK or generated tool-call manifest.
- Anthropic context editing maps to deterministic post-turn compaction of completed tool results.
- OpenAI function calling remains the wire-level tool-call protocol; any compaction must keep assistant tool calls and tool result messages valid.
- OpenAI embeddings map to Aura's dedicated `EMBEDDING_API_KEY`, `EMBEDDING_BASE_URL`, and `EMBEDDING_MODEL`; do not fall back to `LLM_API_KEY`.

## Current Code Baseline

- `internal/telegram/handlers.go` registers `/start`, `/login`, `/token`, `/status`, text messages, and documents. It does not register `/clear`, `/reset`, `/help`, `/tools`, or Telegram bot commands.
- `internal/agentruntime/session.go` stores per-user conversation contexts but has no `Clear` method.
- `internal/telegram/conversation.go` exposes `b.tools.Names()` through `modelToolNames()`, so the model currently sees every registered tool.
- `internal/telegram/conversation_tool_exec.go` appends full raw tool results through `convCtx.AddToolResultMessage(r.id, r.content)`.
- `internal/conversation/context.go` trims message count with tool-safe boundaries, but it does not compact completed tool result payloads.
- `internal/tools/definition.go` already has `ToolDefinition`, examples, and the LLM definition renderer. This is the right source for searchable tool docs.
- `internal/tools/registry.go` already supports categories, deterministic names, and definitions. It needs searchable metadata and a filtered exposure path.
- `internal/tools/tool_registry.go` is a persistent Python tool registry under `wiki/tools/`; it is not the same thing as the runtime registry. Avoid overloading its name in new code.

## Non-Goals

- Do not silently enable new MCP tools or bypass v4.0 review gates.
- Do not put raw provider/MCP tool sprawl back into the default prompt.
- Do not mutate `data/aura.db` from the host while Compose `aura` is running.
- Do not use `LLM_API_KEY` for embeddings.
- Do not reintroduce Pyodide as the default execution path.
- Do not build a generic unrestricted remote-code system. Code runs through the existing bounded Aura process runtime and reviewed tool allowlists.

## Target Runtime Contract

Default model-visible tools after this phase:

- `search_memory`
- `schedule_task`
- `tool_search`
- `execute_code`
- `execute_shell` when process runtime is enabled
- a small set of explicitly core tools needed for auth/status/user flow

Tool expansion rule:

- If the model needs a capability that is not visible, it calls `tool_search` with a natural-language capability request.
- `tool_search` returns 3-5 compact candidates with name, description, schemas, tags, and examples.
- For multi-step work, the model writes one `execute_code` script that orchestrates selected tools through an SDK/manifest instead of doing many model/backend round-trips.

Context rule:

- During a tool-call loop, keep current assistant tool calls and matching tool results intact so OpenAI-compatible protocols remain valid.
- After the final assistant answer for a turn has been produced and archived, compact older completed tool result messages to bounded summaries.
- Never archive or show raw secrets, tool arguments, base64 blobs, OCR bodies, or giant shell logs.

## File Structure

Create:

- `internal/tools/registry_search.go` - searchable tool docs and lexical ranking directly on `Registry`.
- `internal/tools/tool_search.go` - LLM tool wrapper over `Registry.Search`.
- `internal/conversation/tool_compaction.go` - deterministic completed tool result compaction.
- `internal/conversation/tool_compaction_test.go` - protocol-safe compaction tests.
- `internal/telegram/commands.go` - `/help`, `/clear`, `/reset`, `/tools`, and command menu setup.
- `internal/telegram/commands_test.go` - handler-level command behavior tests.
- `scripts/test-agent-tool-search-smoke.ps1` - Docker-first smoke for `tool_search -> execute_code`.

Modify:

- `internal/tools/definition.go` - extend canonical metadata only if needed; prefer deriving from existing name/description/schema/examples first.
- `internal/tools/registry.go` - keep registry ownership of tool metadata; do not create an external search layer.
- `internal/telegram/setup.go` - build and register `tool_search`; keep `execute_code` and `execute_shell` registration as-is.
- `internal/telegram/conversation.go` - change `modelToolNames()` to the small hot surface and update prompt module copy.
- `internal/telegram/conversation_tool_exec.go` - keep execution allowed only for tools exposed in the current turn; add selected-tool expansion only through `tool_search` result state if implemented in this slice.
- `internal/agentruntime/session.go` - add `Clear(userID)` and clear snapshot/active markers.
- `internal/conversation/system_prompt.go` or `composeAgentPrompt` in `internal/telegram/conversation.go` - add concise programmatic tool calling rules.
- `docs/implementation-tracker.md` - append slice results after each implementation slice.
- `.env.example` - add tool search config only when embeddings/vector search are introduced.

## Implementation Tasks

### Task 1: Telegram Command Menu And Conversation Reset

Status: DONE 2026-05-09

Implementation notes:

- Added `SessionStore.Clear(userID)` to delete only the sender's in-memory conversation context, active marker, and runtime snapshot.
- Added `/clear` and `/reset` for allowlisted users.
- Added `/help` with the current compact command list.
- Added `/tools` to report the actually model-visible tools for the current runtime. It does not pretend `tool_search` exists before the next slice registers it.
- Added Telegram `setMyCommands` setup during bot construction; failures are logged and non-fatal.
- Preserved archived history, wiki/source memory, and database rows.

**Files:**
- Modify: `internal/agentruntime/session.go`
- Modify: `internal/agentruntime/session_test.go`
- Create: `internal/telegram/commands.go`
- Create: `internal/telegram/commands_test.go`
- Modify: `internal/telegram/handlers.go`
- Modify: `internal/telegram/setup.go` only if bot command setup belongs at construction time

- [x] **Step 1: Add a failing session clear test**

Add a test proving `Clear` deletes conversation context, active marker, and runtime snapshot:

```go
func TestSessionStoreClearDeletesConversationActiveAndSnapshot(t *testing.T) {
	store := NewSessionStore()
	session, loaded := store.Begin("123", conversation.Config{})
	if loaded {
		t.Fatal("loaded = true, want false")
	}
	session.Conversation().AddUserMessage("ciao")
	store.StoreSnapshot("123", Snapshot{PromptVersion: "v-test"})
	if !store.IsActive("123") {
		t.Fatal("active = false, want true")
	}

	store.Clear("123")

	if _, ok := store.Load("123"); ok {
		t.Fatal("Load after Clear ok = true, want false")
	}
	if store.IsActive("123") {
		t.Fatal("IsActive after Clear = true, want false")
	}
	if _, ok := store.Snapshot("123"); ok {
		t.Fatal("Snapshot after Clear ok = true, want false")
	}
}
```

Run:

```powershell
go test ./internal/agentruntime -run TestSessionStoreClearDeletesConversationActiveAndSnapshot -count=1
```

Expected: fail because `Clear` does not exist.

- [x] **Step 2: Implement `SessionStore.Clear`**

Implementation shape:

```go
func (s *SessionStore) Clear(userID string) {
	if s == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	s.context.Delete(userID)
	s.active.Delete(userID)
	s.snapshots.Delete(userID)
}
```

Run the focused test again. Expected: pass.

- [x] **Step 3: Add Telegram command handlers**

Create command handlers with these semantics:

- `/clear` and `/reset`: allowlisted users only; clear only the sender's conversation session; reply with a short confirmation.
- `/help`: list supported commands and the current autonomy model in plain user language.
- `/tools`: show currently model-visible core tools and explain that extra tools are discovered through `tool_search`.

Handler shape:

```go
func (b *Bot) onClear(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)
	if !b.isAllowlisted(userID) {
		return nil
	}
	b.sessionStore().Clear(userID)
	return c.Send("Conversazione cancellata. Riparto pulito dal prossimo messaggio.")
}
```

Run:

```powershell
go test ./internal/telegram -run "TestClear|TestHelp|TestTools" -count=1
```

Expected before implementation: fail with missing handlers/tests.

- [x] **Step 4: Register handlers and Telegram command menu**

Update `registerHandlers()`:

```go
b.bot.Handle("/clear", b.onClear)
b.bot.Handle("/reset", b.onClear)
b.bot.Handle("/help", b.onHelp)
b.bot.Handle("/tools", b.onTools)
```

Add a small setup function that calls `SetCommands` when the bot starts:

```go
commands := []tele.Command{
	{Text: "start", Description: "Avvia Aura"},
	{Text: "status", Description: "Stato runtime"},
	{Text: "help", Description: "Comandi disponibili"},
	{Text: "clear", Description: "Cancella la conversazione"},
	{Text: "tools", Description: "Mostra strumenti disponibili"},
	{Text: "login", Description: "Apri dashboard"},
}
```

If `SetCommands` cannot be tested against a fake bot cleanly, isolate command construction in a pure function and test that.

- [x] **Step 5: Verify and commit**

Run:

```powershell
go test ./internal/agentruntime ./internal/telegram -count=1
go test ./...
go build ./...
go vet ./...
```

Commit:

```powershell
git add internal/agentruntime/session.go internal/agentruntime/session_test.go internal/telegram/commands.go internal/telegram/commands_test.go internal/telegram/handlers.go internal/telegram/setup.go docs/implementation-tracker.md
git commit -m "slice runtime: add telegram reset commands"
```

### Task 2: Protocol-Safe Tool Result Context Hygiene

Status: DONE 2026-05-09

Implementation notes:

- Added deterministic `Context.CompactCompletedToolResults` with no LLM call and no storage layer.
- Compaction preserves `tool` role and `ToolCallID`, so assistant/tool-call protocol remains intact.
- Compaction only touches tool results that have a later assistant message, leaving in-flight tool results alone.
- The newest two compactable tool results stay full in Telegram turns; older large or secret-like results become bounded previews.
- Preview redacts secret-like lines containing API keys, tokens, passwords, authorization headers, or bearer text.
- Telegram runs compaction after archive writes and before async context limit enforcement.

**Files:**
- Create: `internal/conversation/tool_compaction.go`
- Create: `internal/conversation/tool_compaction_test.go`
- Modify: `internal/conversation/context.go`
- Modify: `internal/telegram/conversation.go`
- Modify: `internal/telegram/conversation_tool_exec.go` only if tool-name metadata needs to be passed explicitly
- Modify: `internal/telegram/archive_test.go` if archive expectations need payload compaction boundaries

- [x] **Step 1: Add failing tests for completed tool compaction**

Test cases:

- compacts old `tool` message content while preserving role and `ToolCallID`;
- leaves the most recent current-turn tool results untouched until final assistant response exists;
- preserves assistant tool-call messages so OpenAI-compatible history remains valid;
- caps giant shell output and OCR-like text;
- redacts obvious secret patterns.

Core test shape:

```go
func TestCompactCompletedToolResultsPreservesProtocol(t *testing.T) {
	ctx := NewContext(Config{})
	ctx.AddUserMessage("run check")
	ctx.AddAssistantToolCallMessage("", []llm.ToolCall{{ID: "call_1", Name: "execute_shell"}})
	ctx.AddToolResultMessage("call_1", strings.Repeat("line\n", 5000))
	ctx.AddAssistantMessage("done")

	changed := ctx.CompactCompletedToolResults(ToolResultCompactionPolicy{
		MaxChars:       400,
		KeepRecentFull: 0,
	})
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	msgs := ctx.Messages()
	if msgs[2].Role != "tool" || msgs[2].ToolCallID != "call_1" {
		t.Fatalf("tool protocol changed: %#v", msgs[2])
	}
	if len(msgs[2].Content) > 500 {
		t.Fatalf("compacted content too long: %d", len(msgs[2].Content))
	}
}
```

Run:

```powershell
go test ./internal/conversation -run TestCompactCompletedToolResults -count=1
```

Expected: fail because compaction does not exist.

- [x] **Step 2: Implement deterministic compaction**

Implementation contract:

```go
type ToolResultCompactionPolicy struct {
	MaxChars       int
	KeepRecentFull int
}

func (c *Context) CompactCompletedToolResults(policy ToolResultCompactionPolicy) int
```

Algorithm:

- scan messages and map tool call IDs to tool names from assistant `ToolCalls`;
- only compact tool messages that have a later assistant message after their tool result;
- keep the newest `KeepRecentFull` compactable tool messages full;
- replace content with a deterministic summary:

```text
[tool result compacted]
tool: execute_shell
original_chars: 18342
preview:
<first bounded lines>
```

- do not remove messages in this slice;
- never call the LLM for this compaction;
- redact `api_key`, `token`, `password`, `authorization`, and bearer-like lines in preview.

- [x] **Step 3: Call compaction after archive, before async limit enforcement**

In `handleConversation`, after `archiveConversationTurns(...)` and before `convCtx.EnforceLimit(...)`, compact the completed tool results:

```go
compacted := convCtx.CompactCompletedToolResults(conversation.ToolResultCompactionPolicy{
	MaxChars:       1200,
	KeepRecentFull: 2,
})
if compacted > 0 {
	b.logger.Info("conversation tool results compacted", "user_id", userID, "count", compacted)
}
```

Archive should still receive full current-turn loop messages unless a privacy test proves it must be compacted earlier. If archive bloat is a problem later, add a separate archive-specific redaction layer.

- [x] **Step 4: Add telemetry and smoke assertions**

Extend `turnStats` or logs only if needed:

- `tool_results_compacted`
- `tool_result_context_chars_saved`

Keep this optional unless tests need it.

- [x] **Step 5: Verify and commit**

Run:

```powershell
go test ./internal/conversation ./internal/telegram -count=1
go test ./...
go build ./...
go vet ./...
```

Commit:

```powershell
git add internal/conversation/tool_compaction.go internal/conversation/tool_compaction_test.go internal/conversation/context.go internal/telegram/conversation.go internal/telegram/conversation_tool_exec.go internal/telegram/archive_test.go docs/implementation-tracker.md
git commit -m "slice runtime: compact completed tool results"
```

### Task 3: Runtime Tool Catalog And Search MVP

Status: DONE 2026-05-09

Implementation notes:

- Rewrote the slice to stay inside `internal/tools`; no `internal/toolsearch` package was created.
- Added `Registry.Search(query, limit, excluded...)` in `internal/tools/registry_search.go`.
- Search derives text from the existing canonical tool definition: name, description, categories, schema, and examples.
- Ranking is deterministic lexical scoring, capped to 5, with name tie-breaks.
- Added `tool_search` as a normal tool wrapper around the existing registry.

**Files:**
- Create: `internal/tools/registry_search.go`
- Create: `internal/tools/tool_search.go`
- Modify: `internal/tools/registry_test.go`
- Modify: `internal/tools/args.go`

- [x] **Step 1: Add failing catalog tests**

Test required behavior:

- every registered tool becomes a document with name, description, JSON schema, examples, and categories;
- generated text is deterministic;
- docs do not include secret values or runtime env;
- existing tool examples appear in flattened text.

Expected document shape:

```go
type Document struct {
	Name        string
	Description string
	InputSchema map[string]any
	Tags        []string
	Examples    []tools.ToolCallExample
	Text        string
}
```

Run: `go test ./internal/tools -run "TestRegistrySearch|TestToolSearch" -count=1`

- [x] **Step 2: Implement catalog builder from `tools.Registry`**

Implemented directly as `Registry.Search`; no separate catalog service or package.

- [x] **Step 3: Implement SQLite FTS lexical search as the MVP**

Decision: do not add SQLite FTS yet. The MVP is deterministic in-process lexical search inside the registry to avoid a new layer.

Result:

```go
type Result struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
	Examples    []tools.ToolCallExample `json:"examples,omitempty"`
	Score       float64 `json:"score,omitempty"`
}
```

Tests:

- query `shell command container` returns `execute_shell` above unrelated tools;
- query `python script data transform` returns `execute_code`;
- query `email message read` returns mail/MCP read tools when registered;
- limit clamps to 5 by default.

- [x] **Step 4: Defer embeddings behind a clean interface**

Add config comments/tests for future vector search, but do not require live embeddings in this MVP:

- `TOOL_SEARCH_BACKEND=fts|vector|hybrid`
- `TOOL_SEARCH_TOP_K=5`
- vector backend uses `EMBEDDING_API_KEY`, `EMBEDDING_BASE_URL`, `EMBEDDING_MODEL`

Only update `.env.example` when the config is actually wired.

- [x] **Step 5: Verify and commit**

Run:

```powershell
go test ./internal/tools ./internal/agentloop ./internal/agentruntime ./internal/telegram -count=1
go test ./...
go build ./...
go vet ./...
```

Commit:

```powershell
git add internal/tools/registry_search.go internal/tools/tool_search.go internal/tools/registry_test.go internal/tools/args.go docs/implementation-tracker.md
git commit -m "slice runtime: add searchable tool catalog"
```

### Task 4: `tool_search` LLM Tool And Small Hot Surface

Status: DONE 2026-05-09

Implementation notes:

- Registered `tool_search` after all runtime tools are registered, so it can see enabled MCP/native tools while excluding itself from search results.
- Changed Telegram's initial model-visible surface to registered core tools only: `search_memory`, `schedule_task`, `tool_search`, `execute_code`, and `execute_shell`.
- Added dynamic tool definitions to the existing agent loop via `ToolsProvider`, so tools returned by `tool_search` can become visible on the next LLM pass in the same turn.
- `executeToolCalls` parses `tool_search` JSON results and adds returned tool names to the active turn allowlist.
- Updated the runtime prompt to tell the model to use `tool_search` for hidden capabilities and prefer code/shell orchestration for multi-step work.

**Files:**
- Create: `internal/tools/tool_search.go`
- Modify: `internal/tools/registry_test.go`
- Modify: `internal/telegram/setup.go`
- Modify: `internal/telegram/conversation.go`
- Modify: `internal/telegram/conversation_tool_exec.go`
- Modify: `internal/agentloop/loop.go`
- Modify: `internal/agentruntime/runner.go`
- Modify: `internal/telegram/debug_smoke_test.go`
- Modify: `internal/conversation/system_prompt.go` or `composeAgentPrompt` in `internal/telegram/conversation.go`

- [x] **Step 1: Add failing `tool_search` tool tests**

Expected inputs:

```json
{
  "query": "read email messages from configured mailbox",
  "limit": 5
}
```

Expected output:

```json
{
  "tools": [
    {
      "name": "mcp_mail_imap_search_messages",
      "description": "...",
      "input_schema": {},
      "examples": []
    }
  ]
}
```

Test caps:

- empty query returns a validation error;
- `limit > 5` clamps to 5;
- no raw secrets;
- result names are deterministic for equal scores.

- [x] **Step 2: Implement `tools.NewToolSearchTool(searcher)`**

Tool definition should be explicit:

```go
func (t *ToolSearchTool) Name() string { return "tool_search" }
```

Description should tell the model to search for capabilities, not exact tool names:

```text
Find Aura tools by natural-language capability. Use this before asking for any tool that is not currently visible. Returns compact tool docs; it does not execute tools.
```

- [x] **Step 3: Register `tool_search`**

In setup:

- build catalog after all tools are registered;
- exclude `tool_search` from its own results;
- include all registered tools, including MCP tools, but keep review-gated policy intact by indexing only tools that are actually registered/enabled.

- [x] **Step 4: Change `modelToolNames()` to the small surface**

Replace `return b.tools.Names()` with a deterministic core list:

```go
core := []string{"search_memory", "schedule_task", "tool_search", "execute_code", "execute_shell"}
return existingRegisteredOnly(core)
```

Do not expose raw MCP/provider tools by default.

Tests should assert:

- default exposed tools include `tool_search`;
- default exposed tools include `execute_code` when sandbox is available;
- raw MCP tools do not appear by default;
- `tool_search` can still find registered MCP tools.

- [x] **Step 5: Update system prompt**

Add concise rules:

```text
When you need a capability that is not currently visible, call tool_search with a natural-language description. Do not guess hidden tool names.
For multi-step tool work, prefer writing one Python script for execute_code that orchestrates the selected tools, instead of asking the model to call many tools one at a time.
Use direct answers when no tool is needed.
```

Avoid long few-shot blocks in the base prompt. Put detailed examples in `tool_search` and `execute_code` tool descriptions.

- [x] **Step 6: Verify and commit**

Run:

```powershell
go test ./internal/tools ./internal/telegram ./cmd/debug_telegram_sandbox -count=1
go test ./...
go build ./...
go vet ./...
```

Commit:

```powershell
git add internal/tools/tool_search.go internal/tools/tool_search_test.go internal/telegram/setup.go internal/telegram/conversation.go internal/telegram/debug_smoke_test.go internal/conversation/system_prompt.go docs/implementation-tracker.md
git commit -m "slice runtime: expose semantic tool search"
```

### Task 5: Programmatic Tool Calling Through `execute_code`

Status: DONE 2026-05-09

Implementation notes:

- Implemented the first SDK shape as the safe manifest MVP, without adding a new package/layer and without changing the sandbox runner.
- `execute_code` now accepts `tools_allowed` and `max_calls`.
- A script can write `/tmp/aura_out/aura_tool_calls.json` with `{"calls":[{"tool":"name","args":{...}}]}`.
- Aura removes that manifest from normal artifacts, validates requested tools against both `tools_allowed` and the current active model-visible turn allowlist, and then executes calls server-side through `internal/tools.Registry`.
- Internal orchestration blocks recursive/control tools: `execute_code`, `execute_shell`, and `tool_search`.
- Telegram now threads the active turn tool surface into tool execution context, so guessed hidden tool names are rejected even inside `execute_code`.
- Regular script artifacts still persist/deliver normally; the control manifest is not persisted or sent to Telegram.
- No `internal/sandbox/tool_sdk.go` was added in this slice because the existing `/tmp/aura_out` artifact collection already provides the manifest boundary. A richer SDK wrapper can be a later ergonomics layer over this contract.

**Files:**
- Modify: `internal/tools/exec.go`
- Modify: `internal/tools/exec_test.go`
- Modify: `internal/tools/context.go`
- Modify: `internal/telegram/conversation_tool_exec.go`
- Modify: `internal/telegram/setup.go`

- [x] **Step 1: Decide the first SDK shape**

Prefer the simplest safe MVP:

- `tool_search` returns docs;
- `execute_code` accepts `tools_allowed` and a script;
- the script writes a structured request file containing internal tool calls;
- the process runner executes the script;
- Aura reads the request file and executes only `tools_allowed`;
- Aura returns stdout plus structured tool results.

Decision: use the manifest MVP now. It is executable, bounded, and keeps the implementation in existing modules.

- [x] **Step 2: Add failing tests for `tools_allowed` validation**

Test behavior:

- `execute_code` rejects tool orchestration when `tools_allowed` contains a tool not returned by `tool_search` in this turn;
- `execute_code` allows pure Python scripts with no `tools_allowed`;
- max internal calls and timeout are enforced;
- tool arguments are redacted in logs.

- [x] **Step 3: Implement bounded internal tool invocation**

Contract:

```json
{
  "language": "python",
  "code": "...",
  "tools_allowed": ["search_memory", "mcp_database_read_query"],
  "max_calls": 10,
  "timeout_seconds": 30
}
```

The implementation:

- reads the invocation manifest from `/tmp/aura_out/aura_tool_calls.json`;
- filters the manifest out of normal artifacts;
- allows the script to emit final structured JSON through stdout while Aura appends internal tool results;
- execute internal tool calls server-side, not by exposing HTTP secrets to the script;
- enforce `tools_allowed`, `max_calls`, and timeout.

- [x] **Step 4: Add prompt/tool description guidance**

Update `execute_code` description:

- use it for loops, transforms, retries, and multi-tool orchestration;
- keep scripts deterministic;
- produce a short structured JSON result;
- do not install packages unless needed;
- prefer typed document tools for simple documents.

- [ ] **Step 5: Docker smoke**

Add smoke script:

```powershell
.\scripts\test-agent-tool-search-smoke.ps1
```

Expected:

- model calls `tool_search` once;
- model calls `execute_code` once;
- total LLM calls <= 2 for the scripted workflow;
- tool calls <= 2 model-visible calls;
- logs show internal tool calls separately if SDK is used.

Deferred to Task 7 regression smokes so this slice stays focused on the runtime contract and unit/integration checks.

- [x] **Step 6: Verify and commit**

Run:

```powershell
go test ./internal/sandbox ./internal/tools ./internal/telegram ./cmd/debug_telegram_sandbox -count=1
go test ./...
go build ./...
go vet ./...
docker compose config --quiet
```

Commit:

```powershell
git add internal/tools/exec.go internal/tools/exec_test.go internal/sandbox/process_runner.go internal/sandbox/process_runner_test.go internal/sandbox/tool_sdk.go internal/sandbox/tool_sdk_test.go internal/telegram/conversation_tool_exec.go scripts/test-agent-tool-search-smoke.ps1 docs/implementation-tracker.md
git commit -m "slice runtime: add programmatic tool orchestration"
```

### Task 6: Embedding/Hybrid Tool Search Backend

**Files:**
- Modify: `internal/tools/registry_search.go`
- Create: `internal/tools/registry_search_vector.go`
- Create: `internal/tools/registry_search_vector_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `.env.example`
- Modify: `docker-compose.yml` only if a new sidecar/cache is required

- [ ] **Step 1: Add config tests**

Settings:

- `TOOL_SEARCH_BACKEND=fts|vector|hybrid`
- `TOOL_SEARCH_TOP_K=5`
- `TOOL_SEARCH_INDEX_PATH=/data/tool_search.db`
- `TOOL_SEARCH_SYNC_ON_START=true`

Embedding provider:

- reuse `EMBEDDING_API_KEY`;
- reuse `EMBEDDING_BASE_URL`;
- reuse `EMBEDDING_MODEL`;
- never use `LLM_API_KEY`.

- [ ] **Step 2: Implement offline index rebuild**

Add a rebuild service:

```go
func Rebuild(ctx context.Context, docs []Document) error
```

It should:

- hash flattened tool docs;
- skip unchanged embeddings;
- store doc metadata and vector;
- tolerate missing embedding key by falling back to FTS with a health warning.

- [ ] **Step 3: Implement hybrid rank**

Hybrid scoring:

- FTS candidate score;
- vector cosine score when available;
- deterministic tie-break by tool name;
- top-K capped by config and per-call limit.

- [ ] **Step 4: Health and telemetry**

Expose:

- index backend;
- doc count;
- last rebuild time;
- last error;
- embedding model;
- fallback status.

Prefer adding this to existing `/status` and API health rollups only after the service exists.

- [ ] **Step 5: Verify and commit**

Run:

```powershell
go test ./internal/tools ./internal/config ./internal/api ./internal/telegram -count=1
go test ./...
go build ./...
go vet ./...
docker compose config --quiet
```

Commit:

```powershell
git add internal/tools/registry_search.go internal/tools/registry_search_vector.go internal/tools/registry_search_vector_test.go internal/config/config.go internal/config/config_test.go .env.example docs/implementation-tracker.md
git commit -m "slice runtime: add hybrid tool search backend"
```

### Task 6a: Agent Loop Guardrails And Smoke Metrics

Status: DONE 2026-05-10

Implementation notes:

- Added tiered loop budgets so simple answers stay short while `tool_search` and code execution can still complete bounded workflows.
- Added a hidden-tool spiral breaker: when the model guesses a non-visible tool, Aura stops the loop with explicit `tool_search` guidance instead of spending more calls on the same dead path.
- Counted retry guidance only from real tool error results. No synthetic `tool` messages are inserted, preserving assistant/tool-call protocol safety.
- Improved shell syntax error hints to steer the model toward `execute_code` with Python when `/bin/sh` rejects complex command syntax.
- Extended runtime snapshots, conversation logs, debug smoke output, and debug expectation flags with `retry_nudges_sent`, `spiral_breaker_fired`, and `tiered_budget_tier`.
- Added agent-loop tests covering protocol-safe retry counting, hidden-tool spiral breaking, and tier expansion from `tool_search` to `execute_code`.

**Files:**
- Modify: `internal/agentloop/loop.go`
- Modify: `internal/agentloop/loop_test.go`
- Modify: `internal/agentruntime/session.go`
- Modify: `internal/telegram/conversation.go`
- Modify: `internal/telegram/conversation_snapshot.go`
- Modify: `internal/telegram/debug_smoke.go`
- Modify: `internal/telegram/debug_smoke_test.go`
- Modify: `internal/tools/error.go`
- Modify: `cmd/debug_telegram_sandbox/main.go`
- Modify: `cmd/debug_telegram_sandbox/main_test.go`

- [x] **Step 1: Add loop guardrail tests**

Tests prove:

- retry guidance does not append a synthetic unmatched `tool` message;
- hidden-tool rejection can stop the loop immediately with `tool_search` guidance;
- tiered budgets expand from simple QA to orchestration/code execution.

- [x] **Step 2: Implement protocol-safe guardrails**

Implementation:

- preserve existing tool-result protocol;
- use structured tool error hints already returned by tools;
- expose only counters and final guidance as ordinary assistant text.

- [x] **Step 3: Add smoke metrics**

Debug smoke now prints and can assert:

- `retry_nudges_sent`;
- `spiral_breaker_fired`;
- `tiered_budget_tier`.

- [x] **Step 4: Verify and commit**

Run:

```powershell
go test ./internal/agentloop ./internal/telegram ./cmd/debug_telegram_sandbox -count=1
go test ./...
go build ./...
go vet ./...
docker compose config --quiet
```

### Task 7: Metrics, Regression Smokes, And v4.0 Integration

**Files:**
- Modify: `cmd/debug_telegram_sandbox/main.go`
- Modify: `cmd/debug_telegram_sandbox/main_test.go`
- Create: `scripts/test-agent-tool-search-smoke.ps1`
- Modify: `.planning/phases/v4.0-mcp-plugin-marketplace/PLAN.md`
- Modify: `.planning/STATE.md` only if this plan becomes the active slice
- Modify: `docs/implementation-tracker.md`

- [ ] **Step 1: Add debug smoke counters**

Expose:

- `tool_search_calls`
- `execute_code_calls`
- `execute_shell_calls`
- `model_visible_tool_count`
- `tool_result_context_chars`
- `tool_results_compacted`
- `internal_orchestration_tool_calls` if SDK exists

- [ ] **Step 2: Add budget gates**

Add flags:

```text
-expect-visible-tools-max
-expect-tool-search-calls-max
-expect-execute-code-calls-min
-expect-tool-result-context-chars-max
```

- [ ] **Step 3: Docker regression smoke**

Smoke prompts:

- "Controlla quali strumenti servono per leggere una tabella database e prepara una query read-only."
- "Trova il modo migliore per cercare email e riassumere i messaggi importanti."
- "Esegui una diagnostica container con Python e shell in un solo giro."

Expected:

- default visible tools stay small;
- raw MCP tools do not appear in the initial model-visible surface;
- `tool_search` returns relevant candidates;
- scripted container diagnostic completes in <= 2 LLM calls;
- no raw giant tool result remains in the next prompt context.

- [ ] **Step 4: Update v4.0 integration notes**

In the v4.0 plan, record:

- MCP provider tools are indexed by tool search only after review-gated registration;
- raw provider tools are not default-visible;
- canonical provider capabilities remain the preferred public contract;
- `tool_search` is discovery, not enablement.

- [ ] **Step 5: Verify and commit**

Run:

```powershell
go test ./cmd/debug_telegram_sandbox ./internal/telegram ./internal/tools -count=1
go test ./...
go build ./...
go vet ./...
docker compose config --quiet
```

Commit:

```powershell
git add cmd/debug_telegram_sandbox/main.go cmd/debug_telegram_sandbox/main_test.go scripts/test-agent-tool-search-smoke.ps1 .planning/phases/v4.0-mcp-plugin-marketplace/PLAN.md docs/implementation-tracker.md
git commit -m "slice runtime: add tool search regression smokes"
```

## Acceptance Criteria

- Telegram exposes `/clear`, `/reset`, `/help`, `/tools`, and an actual Telegram command menu.
- `/clear` deletes only the sender's in-memory conversation context and snapshot; it does not delete archived history, wiki memory, source files, or database rows.
- Completed raw tool results are compacted after turn completion without breaking OpenAI-compatible assistant/tool-call history.
- The model-visible hot surface is small and deterministic.
- `tool_search` is the only path for discovering non-visible capabilities.
- Search results include name, description, input schema, tags/categories, and examples.
- MCP/provider tools are searchable only if they are already registered/enabled by Aura's review-gated setup.
- The model is instructed to use `tool_search -> execute_code` for multi-tool orchestration.
- `execute_code` can support bounded programmatic tool orchestration or the plan explicitly defers SDK support until the next slice.
- Embedding search uses dedicated embedding config and falls back cleanly to FTS when credentials/vector backend are unavailable.
- Debug smokes report model-visible tool count, LLM calls, tool calls, and compaction counters.

## Safety Gates

- Never expose unregistered, disabled, or review-blocked MCP tools through `tool_search`.
- Never execute a hidden tool solely because the model guessed its name.
- Keep execution rooted in Aura's process sandbox and workspace boundaries.
- Do not log raw secrets, bearer tokens, mail bodies, base64 payloads, or database query results beyond bounded summaries.
- Keep destructive mail/database/filesystem operations behind existing policy/review gates.
- Do not mutate live SQLite from host-side commands while Docker Aura is running.

## Recommended Execution Order

1. Telegram command menu and `/clear`.
2. Tool result context hygiene.
3. FTS tool catalog and `tool_search`.
4. Small model-visible hot surface.
5. Programmatic `execute_code` orchestration SDK.
6. Hybrid embedding backend.
7. Docker smokes and v4.0 integration gates.

This order gives the user immediate control first, then fixes context bloat, then narrows the prompt, and only then adds the more ambitious internal orchestration layer.

## Open Decisions

- Whether the first `execute_code` orchestration SDK should call Aura tools through a manifest file or through an internal localhost-only RPC endpoint. Prefer manifest until a real need for streaming internal calls appears.
- Whether `tool_search` should return raw provider tools or only canonical Aura capabilities for mail/database. Prefer canonical capabilities for user-facing domains; allow raw tools only for admin/debug profiles.
- Whether to persist the tool-search index in SQLite only or mirror vectors into Qdrant. Prefer SQLite FTS for MVP and hybrid vector later.
- Whether archive storage should keep full raw tool results or compacted/redacted results. Current plan keeps archive behavior unchanged and compacts active context only.
