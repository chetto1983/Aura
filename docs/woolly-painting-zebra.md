# Plan: Add Agentic Tool Calling with Web Search + Wiki Tools

## Context

Aura currently has no tool/function calling support. The LLM client only sends `model`, `messages`, `temperature`. The wiki is the only "tool" — implemented via prompt-based output detection (`looksLikeWikiContent`). This is fragile: the LLM must embed YAML frontmatter in its response text, which often breaks.

The goal is to add full agentic tool calling so the LLM can:
1. **Search the web** via Ollama's `web_search` and `web_fetch` APIs
2. **Read/write wiki** via proper tool calls instead of prompt-based detection
3. **Search wiki** via tool call instead of auto-injection

Reference: Picobot (`github.com/louisho5/picobot`) implements this exact pattern with `Tool` interface, `Registry`, and agent loop with `maxIterations`.

## Architecture

Follow Picobot's pattern:
- `Tool` interface with `Name()`, `Description()`, `Parameters()`, `Execute()`
- `Registry` holds tools, provides `Definitions()` for the LLM, dispatches `Execute()`
- Agent loop in bot: `Send` → if `HasToolCalls` → execute → append tool results → re-send → loop until final text response
- Non-streaming for tool call rounds (need full response to check tool_calls); stream only for the final text response

## Implementation Steps

### Step 1: Extend LLM types — `internal/llm/client.go`

Add tool-related types to the `llm` package:

```go
type ToolDefinition struct {
    Name        string                 `json:"name"`
    Description string                 `json:"description"`
    Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type ToolCall struct {
    ID        string                 `json:"id"`
    Name      string                 `json:"name"`
    Arguments map[string]interface{} `json:"arguments"`
}
```

Extend `Message`:
```go
type Message struct {
    Role       string     // "system", "user", "assistant", "tool"
    Content    string
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`  // set on assistant messages
    ToolCallID string     `json:"tool_call_id,omitempty"` // set on tool result messages
}
```

Extend `Request`:
```go
type Request struct {
    Messages    []Message
    Model       string
    Temperature *float64
    Tools       []ToolDefinition // tool definitions sent to the LLM
}
```

Extend `Response`:
```go
type Response struct {
    Content      string
    Usage        TokenUsage
    HasToolCalls bool
    ToolCalls    []ToolCall
}
```

### Step 2: Extend OpenAI wire format — `internal/llm/openai.go`

Add to `chatRequest`:
```go
Tools []toolWrapper `json:"tools,omitempty"`
```

New wire types:
```go
type toolWrapper struct {
    Type     string      `json:"type"` // always "function"
    Function functionDef `json:"function"`
}

type functionDef struct {
    Name        string                 `json:"name"`
    Description string                 `json:"description"`
    Parameters  map[string]interface{} `json:"parameters,omitempty"`
}
```

Extend `chatMessage` — make `Content` nullable (tool messages often have null content):
```go
type chatMessage struct {
    Role       string         `json:"role"`
    Content    *string        `json:"content,omitempty"`
    ToolCalls  []toolCallJSON `json:"tool_calls,omitempty"`
    ToolCallID string         `json:"tool_call_id,omitempty"`
}

type toolCallJSON struct {
    ID       string               `json:"id"`
    Type     string               `json:"type"` // always "function"
    Function toolCallFunctionJSON `json:"function"`
}

type toolCallFunctionJSON struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"` // JSON string
}
```

Extend `chatResponse`:
```go
type chatResponse struct {
    Choices []struct {
        Message      messageResponseJSON `json:"message"`
        FinishReason string              `json:"finish_reason"`
    } `json:"choices"`
    Usage struct { ... } `json:"usage"`
}

type messageResponseJSON struct {
    Role      string         `json:"role"`
    Content   string         `json:"content"`
    ToolCalls []toolCallJSON `json:"tool_calls,omitempty"`
}
```

Update `Send()`:
- Convert `ToolDefinition` → `toolWrapper` for request
- Convert `Message` → `chatMessage` with nullable Content, ToolCalls, ToolCallID
- Parse response: if `ToolCalls` present, set `HasToolCalls=true` and parse arguments

Update `Stream()`:
- No changes to streaming itself (streaming is only used for final text responses)
- Tool call rounds use `Send()` instead

### Step 3: Create tool registry — `internal/tools/registry.go`

Following Picobot's pattern:

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]interface{}
    Execute(ctx context.Context, args map[string]interface{}) (string, error)
}

type Registry struct {
    mu    sync.RWMutex
    tools map[string]Tool
}
```

Methods: `NewRegistry()`, `Register(t Tool)`, `Get(name) Tool`, `Definitions() []llm.ToolDefinition`, `Execute(ctx, name, args) (string, error)`

`Definitions()` converts `Tool` → `llm.ToolDefinition` for the LLM request.

### Step 4: Create web search tool — `internal/tools/websearch.go`

Uses Ollama's web search API (`POST https://ollama.com/api/web_search`):

```go
type WebSearchTool struct {
    apiKey  string
    client  *http.Client
}
```

- `Name()`: `"web_search"`
- `Parameters()`: `{query: string (required), max_results: int (optional, default 5, max 10)}`
- `Execute()`: POST to `https://ollama.com/api/web_search` with `Authorization: Bearer {apiKey}`
- Truncates result content to ~8000 chars for context management
- Returns formatted results with title, URL, and content snippet

### Step 5: Create web fetch tool — `internal/tools/webfetch.go`

Uses Ollama's web fetch API (`POST https://ollama.com/api/web_fetch`):

```go
type WebFetchTool struct {
    apiKey  string
    client  *http.Client
}
```

- `Name()`: `"web_fetch"`
- `Parameters()`: `{url: string (required)}`
- `Execute()`: POST to `https://ollama.com/api/web_fetch` with `Authorization: Bearer {apiKey}`
- Returns title, content, and links from the fetched page

### Step 6: Create wiki tools — `internal/tools/wiki.go`

Three tools wrapping the existing wiki store:

**`write_wiki`**: Writes a wiki page
- Parameters: `{title: string (required), body: string (required), tags: []string, category: string, related: []string, sources: []string}`
- Execute: Creates a `wiki.Page`, validates, calls `store.WritePage`
- Auto-sets `schema_version`, `prompt_version`, timestamps, slug

**`read_wiki`**: Reads a wiki page
- Parameters: `{slug: string (required)}`
- Execute: Calls `store.ReadPage(slug)`, formats as markdown

**`search_wiki`**: Searches wiki pages
- Parameters: `{query: string (required)}`
- Execute: Calls `search.Engine.Search(query, 5)`, returns formatted results

### Step 7: Update config — `internal/config/config.go`

Add:
```go
OllamaAPIKey string // OLLAMA_API_KEY — for web search/fetch
MaxToolIterations int // MAX_TOOL_ITERATIONS — default 10
```

### Step 8: Update bot — `internal/telegram/bot.go`

**Bot struct**: Add `tools *tools.Registry` field

**New()**: 
- Create `tools.Registry`
- Register `web_search`, `web_fetch` (if OllamaAPIKey is set)
- Register `write_wiki`, `read_wiki`, `search_wiki` (if wiki/search available)
- Store registry on Bot

**handleConversation()** — Replace single-shot LLM call with tool-call loop:
1. Build `llm.Request` with `Tools: b.tools.Definitions()`
2. Use `Send()` (non-streaming) for tool-call rounds
3. If `resp.HasToolCalls`:
   - Add assistant message with tool calls to context
   - Execute each tool call via `b.tools.Execute()`
   - Add tool result messages to context
   - Send "Running: tool_name" notification to user
   - Loop back to step 1
4. When response has no tool calls, that's the final text response
5. Stream the final response for better UX (optional optimization)
6. Remove `looksLikeWikiContent` / `tryStoreWiki` — wiki is now a tool

**Remove**: `looksLikeWikiContent()`, `tryStoreWiki()` — no longer needed

**Remove**: Auto wiki search injection in `handleConversation` — wiki search is now a tool the LLM calls when needed

### Step 9: Update conversation context — `internal/conversation/context.go`

Add methods for tool messages:
```go
func (c *Context) AddToolResultMessage(toolCallID string, content string)
```

Update `trimOldest()` and `truncateMessages()` to keep tool messages paired with their assistant tool_call message (don't separate them).

### Step 10: Update system prompt — `internal/conversation/system_prompt.go`

Replace "Wiki Writing" section with "Tools" section:
- List available tools and their purpose
- Instruct the LLM to use `write_wiki` tool instead of embedding YAML
- Instruct the LLM to use `search_wiki` tool when it needs wiki knowledge
- Instruct the LLM to use `web_search` / `web_fetch` for current information
- Remove the YAML frontmatter format example

### Step 11: Update health server — `internal/health/server.go`

Register web search tool as a `StatusProvider` if configured.

## Files to Modify

| File | Change |
|------|--------|
| `internal/llm/client.go` | Add ToolDefinition, ToolCall types; extend Message, Request, Response |
| `internal/llm/openai.go` | Add tool wire types; extend chatRequest, chatMessage, chatResponse; update Send() conversion |
| `internal/tools/registry.go` | **New file** — Tool interface, Registry |
| `internal/tools/websearch.go` | **New file** — WebSearchTool (Ollama API) |
| `internal/tools/webfetch.go` | **New file** — WebFetchTool (Ollama API) |
| `internal/tools/wiki.go` | **New file** — WriteWikiTool, ReadWikiTool, SearchWikiTool |
| `internal/config/config.go` | Add OllamaAPIKey, MaxToolIterations |
| `internal/telegram/bot.go` | Add tools.Registry; replace handleConversation with tool-call loop; remove wiki detection |
| `internal/conversation/context.go` | Add AddToolResultMessage; update trimOldest/truncateMessages for tool message pairing |
| `internal/conversation/system_prompt.go` | Replace Wiki Writing with Tools section |
| `internal/wiki/parser.go` | Keep WriteFromLLMOutput but it's no longer called from bot (tools call store.WritePage directly) |
| All test files | Update for new types and flow |

## Verification

1. `go build ./...` — compiles without errors
2. `go test ./internal/llm/... ./internal/tools/... ./internal/conversation/...` — all tests pass
3. Set `OLLAMA_API_KEY`, start bot, send "search the web for X" → LLM calls `web_search` tool, returns results
4. Send "remember that X" → LLM calls `write_wiki` tool instead of embedding YAML
5. Send "what do you know about X" → LLM calls `search_wiki` tool
6. Tool-call loop terminates after max iterations with graceful fallback
7. Budget tracking works across multiple tool-call rounds