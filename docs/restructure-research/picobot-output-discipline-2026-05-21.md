# Picobot Output Discipline — Why It Doesn't Help With Aura's Wall-of-Text Bug

Date: 2026-05-21
Question: How does picobot ensure the agent SYNTHESIZES tool results instead of dumping them?

## TL;DR

Picobot has **no dedicated output-discipline machinery**. It relies on three soft signals (a one-line "be concise" personality cue in `SOUL.md`, a "Brief and concise" user preference in `USER.md`, no temperature being sent) plus the LLM's own habit. Worse: `internal/agent/loop.go:283-285` contains the **exact anti-pattern** that produces Aura's failure mode — when the model returns empty `content` after a tool call, picobot literally dumps the raw tool string to the user. Picobot is NOT a model for fixing Aura's drift; it would exhibit the same bug on the same prompt.

## What picobot actually does

### 1. Soft "be concise" personality, scattered across overlays — NOT enforced

`internal/config/onboard.go:60-81` writes `SOUL.md` containing only:

> "I am picobot 🤖, a personal AI assistant. ... Concise and to the point ... Be clear and direct"

`internal/config/onboard.go:83-141` writes `AGENTS.md`:

> "You are a helpful AI assistant. Be concise, accurate, and friendly."

`internal/config/onboard.go:143-182` writes `USER.md` with a literal checkbox:

> "### Response Length\n- [x] Brief and concise"

These are loaded as system parts in `internal/agent/context.go:41-52`. There is no "do not dump tool output", no row/token cap, no "summarize results before replying". The model is trusted to be brief. **This wouldn't have prevented the 90-row DataFrame dump** — the model already "knew" to be concise and chose to dump anyway.

### 2. NO post-LLM filter on assistant output

`internal/agent/loop.go:300-305` writes `finalContent` verbatim to `hub.Out`. No length check, no row-count detector, no "looks like a dump" heuristic. Slack/Discord/WhatsApp chunk for API limits (e.g. `internal/channels/discord.go:177` `splitMessage(out.Content, 2000)`), but **Telegram doesn't chunk at all** (`internal/channels/telegram.go:135` sends `out.Content` straight to `sendMessage`). So a 90-row wall lands as one giant Telegram bubble.

### 3. THE anti-pattern — empty-content fallback to raw tool result

`internal/agent/loop.go:283-285`:

```go
if finalContent == "" && lastToolResult != "" {
    finalContent = lastToolResult
} else if finalContent == "" {
    finalContent = "I've completed processing but have no response to give."
}
```

And identically in `ProcessDirect` at `loop.go:350-352`:

```go
if resp.Content != "" { return resp.Content, nil }
if lastToolResult != "" { return lastToolResult, nil }
```

If the LLM responds with a tool call and no synthesis, picobot **ships the raw tool output as the user-facing reply**. This is structurally identical to Aura's failure: the model gets `exec_code → 90 rows of pandas output`, decides synthesis isn't needed, and the raw blob escapes. Picobot would also fail "trova un cliente e stampalo" exactly the same way.

### 4. Tool descriptions are factual, NOT directive

Tool descriptions are one-liners with zero output-shape guidance. Examples:

- `internal/agent/tools/exec.go:36-38` — `"Execute shell commands (array form only, restricted for safety)"`
- `internal/agent/tools/web.go:18` — `"Fetch web content from a URL"`
- `internal/agent/tools/message.go:24` — `"Send a message to the current channel/chat"`

No description says "use this for internal processing, summarize the result before surfacing", no "results may be long — synthesize, don't paste". This is the *opposite* of what Aura needs.

### 5. Tool result fed back as raw `role:"tool"` message

`internal/agent/loop.go:273` appends the whole tool output verbatim:

```go
messages = append(messages, providers.Message{Role: "tool", Content: res, ToolCallID: tc.ID})
```

No truncation, no preview-only envelope, no "the user has not seen this — synthesize a reply" instruction injected alongside. The full 90-row blob lives in the assistant's context window and is then trivially echoable.

### 6. Temperature is dead config — never actually sent

`internal/config/schema.go:28` defines `Temperature float64`, `internal/config/onboard.go:20` defaults it to `0.7`, but the OpenAI request body in `internal/providers/openai.go:43-48` is:

```go
type chatRequest struct {
    Model     string        `json:"model"`
    Messages  []messageJSON `json:"messages"`
    Tools     []toolWrapper `json:"tools,omitempty"`
    MaxTokens int           `json:"max_tokens,omitempty"`
}
```

No `temperature` field. The config setting is a no-op — provider falls back to vendor default. So picobot can't even claim deterministic output discipline; it's whatever OpenAI/OpenRouter ships.

### 7. The ONE structural pattern worth lifting — formatted tool result with caps

`internal/agent/tools/web_search.go:101-167` `formatDDGResponse` is the only tool that disciplines its OWN output before the LLM sees it:

- Builds a small structured envelope (`Answer:`, `Definition:`, `Related results:`)
- `const maxTopics = 5` (line 150) hard-caps related results
- Adds an explicit "No instant answer found. Try the 'web' tool" hint when empty

This is a **tool-level cap**, not an agent-level one. It works because the tool author chose to summarize at source. Aura's `execute_code` does the opposite — it returns the full Python stdout including a 90-row DataFrame string.

## Patterns ranked by ROI for fixing Aura's wall-of-text

| # | Pattern | Source | Why it fixes Aura | ROI |
|---|---|---|---|---|
| 1 | **Tool-side output capping** (web_search.go:150 `maxTopics=5`) | picobot has it for web_search ONLY | Cap `execute_code` stdout to N lines + tail-truncate with `...(86 more rows)`. The model can't dump what it never receives in full. | HIGH — single-file change in Aura's exec tool |
| 2 | **Synthesis-required system prompt** (NOT present in picobot, but the gap is the lesson) | absent | Add explicit rule: "Tool results are internal context. NEVER paste them. Reply with a synthesized answer (≤200 tokens) unless the user asked to see raw data." Picobot's "be concise" is too weak. | HIGH — one prompt edit |
| 3 | **Tool descriptions as output contracts** (picobot doesn't do this — opposite lesson) | exec.go:37, web.go:18 | Aura's `execute_code` description must say: "Internal processing only. Surface a 1-2 sentence summary; never dump raw output." | MEDIUM — propagates via tool catalog |
| 4 | **Kill the empty-content fallback** (picobot HAS this bug at loop.go:283-285) | loop.go:283-285 | If Aura has a similar `if finalContent == "" { send lastToolResult }` branch, REMOVE IT. Re-prompt the model with "summarize the tool result for the user" instead. | HIGH — check Aura agent loop now |
| 5 | **Outbound length cap + auto-summary fallback** (picobot has Discord chunking but not Telegram) | telegram.go:135 vs discord.go:177 | Add a Telegram outbound guard in Aura: if reply > N chars, force a re-roll with "your previous reply was too long, summarize in ≤500 chars". | MEDIUM |
| 6 | **Channel-aware response shape** (picobot does NOT vary by channel for content shape, only chunking) | absent | For Telegram (mobile, narrow), set tighter token budget than dashboard. Drives the model to summarize. | LOW-MEDIUM |
| 7 | **`USER.md` checkbox preference for response length** (picobot has it as a placeholder) | onboard.go:163 | Aura already has prompt overlays. Add a per-chat `response_style` knob exposed in Telegram (`/style brief|normal|verbose`) that flips a prompt rule. | LOW (UX feature, not bug-fix) |

## Verdict

Picobot is **not** an exemplar of output discipline — it is a cautionary case. Its three "concise" mentions in overlays are exactly the kind of soft guidance that fails under tool-call pressure, and `loop.go:283-285` is the *literal* "ship the raw tool result" bug pattern Aura is exhibiting. The only structurally sound pattern is `formatDDGResponse` (web_search.go:101-167): **discipline the output at the tool boundary, not at the agent reply boundary**. For Aura's `execute_code` + DataFrame case, the highest-ROI fix is item 1 (cap stdout at the tool) combined with item 4 (audit and remove any empty-content fallback) and item 2 (explicit synthesis rule in the system prompt). Item 3 (treat tool descriptions as output contracts) is the multiplier — every tool in Aura's catalog that can produce >50 lines should declare a summarize-only contract.
