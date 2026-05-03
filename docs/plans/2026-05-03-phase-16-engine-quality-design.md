# Phase 16: Engine Quality & Performance

## Goal

Reduce perceived latency on Telegram and make the LLM self-correct tool errors instead of surfacing raw error strings to the user.

Two independent workstreams, 5 slices total.

## Workstream 1: Error Recovery

**Problem:** Tool errors are converted to `"(tool error) <raw Go error>"` and passed as tool-result content (`conversation.go:330-333`). The system prompt has zero guidance on retry. The LLM's behavior is emergent — sometimes apologizes, sometimes explains, rarely retries.

**Fix:** Structured error format + system prompt directive.

### Slice 16a — Structured tool errors

New `internal/tools/error.go`:
```go
type ToolError struct {
    OK        bool   `json:"ok"`
    Error     string `json:"error"`
    Retryable bool   `json:"retryable"`
    Hint      string `json:"hint,omitempty"`
}
```

`FormatToolError(err error) string` produces JSON like:
```json
{"ok":false, "error":"missing required field 'rows'", "retryable":true, "hint":"Provide the 'rows' argument as an array of objects"}
```

`executeToolCalls` in `conversation.go` calls `FormatToolError(err)` instead of `"(tool error) " + err.Error()`.

Default classification: all tool errors are `retryable:true` (validation, bad args). Only errors explicitly wrapped as fatal (disk full, permission denied) become `retryable:false`.

### Slice 16b — System prompt retry directive

New paragraph in `system_prompt.go` under "Tool use":
- When a tool result starts with `{"ok":false`, read `retryable` and `hint`
- If `retryable:true`, correct your arguments using `hint` and call the same tool again once
- If the retry also fails, or `retryable:false`, explain the problem to the user (in Italian)

## Workstream 2: Latency

**Problem:** Before the user sees anything on Telegram, `handleConversation` runs: system prompt render → overlay files → skills load → speculative wiki search → EnforceLimit (may fire a summarizer LLM call) → budget checks. Then streaming starts but waits 30 chars before showing anything, and edits are throttled at 800ms.

**Fix:** Immediate placeholder message + defer expensive work + tighten throttle.

### Slice 16c — Immediate "thinking" placeholder

At the top of `handleConversation`, right after parsing the user message, send `"⏳"` via `c.Send`. Pass the returned message ID through `runToolCallingLoop` → `consumeStream`. `consumeStream` edits this existing message instead of creating a new one.

When the provider doesn't support streaming, the placeholder gets edited to the final response text once `runToolCallingLoop` returns.

### Slice 16d — Defer EnforceLimit

Move `convCtx.EnforceLimit(ctx)` from before `runToolCallingLoop` to after the response is sent:
```go
go func() { convCtx.EnforceLimit(ctx) }()
```

Trade-off: the model may briefly see an over-80% context on the next turn if background summarization hasn't finished. In practice, MAX_HISTORY_MESSAGES=50 keeps context bounded and this edge case is rare.

### Slice 16e — Throttle 800ms → 600ms

Change `streamingEditThrottle` in `streaming.go` from 800ms to 600ms. Still safe under Telegram's ~1/sec edit rate limit.

## What this phase does NOT touch

- No LLM provider/model changes
- No wiki/source schema changes
- No dashboard/frontend changes (deferred to Phase 17)
- No new config keys (`.env.example` unchanged)
- No E2E test changes (deferred to Phase 17)

## Verification

- `go build ./... && go vet ./... && go test ./...` green after each slice
- 16a/16b: `go run ./cmd/debug_tools` with a deliberately-bad tool call
- 16c/16d/16e: live Telegram smoke with real user messages
