# Phase 16: Engine Quality & Performance — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Structured tool errors with LLM retry directive + reduced perceived Telegram latency via immediate placeholder, deferred EnforceLimit, and tighter edit throttle.

**Architecture:** Five independent slices with zero cross-dependencies beyond the file order within each workstream. 16a creates the error format; 16b teaches the LLM to use it. 16c rewires the streaming plumbing to show a placeholder immediately; 16d moves expensive context enforcement to after the user sees the response; 16e tightens the edit throttle by 200ms. No config keys, no schema changes, no dashboard touches.

**Tech Stack:** Go 1.24, telebot v4, existing `internal/tools`, `internal/conversation`, `internal/telegram`

---

### Task 1: Slice 16a — Structured tool error format

**Files:**
- Create: `internal/tools/error.go`
- Create: `internal/tools/error_test.go`
- Modify: `internal/telegram/conversation.go:331-332`

**Step 1: Write `internal/tools/error.go`**

```go
package tools

import (
	"encoding/json"
	"strings"
)

// ToolError is the structured format returned to the LLM when a tool call
// fails. The LLM reads retryable + hint to decide whether to self-correct.
type ToolError struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error"`
	Retryable bool   `json:"retryable"`
	Hint      string `json:"hint,omitempty"`
}

// FormatToolError converts a Go error into a JSON tool-error result string.
// Default classification: retryable=true with a generic hint. Callers that
// know the error is fatal (permission denied, disk full) should use
// FormatFatalToolError instead.
func FormatToolError(err error) string {
	msg := err.Error()
	te := ToolError{
		OK:        false,
		Error:     msg,
		Retryable: true,
		Hint:      hintForError(msg),
	}
	b, _ := json.Marshal(te)
	return string(b)
}

// FormatFatalToolError converts a non-retryable Go error.
func FormatFatalToolError(err error) string {
	te := ToolError{
		OK:        false,
		Error:     err.Error(),
		Retryable: false,
	}
	b, _ := json.Marshal(te)
	return string(b)
}

// hintForError returns a short hint based on the error message content.
// If no pattern matches, returns a generic retry hint.
func hintForError(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "missing") || strings.Contains(lower, "required"):
		return "Provide the required field mentioned in the error"
	case strings.Contains(lower, "invalid") || strings.Contains(lower, "malformed"):
		return "Fix the format of the argument mentioned in the error"
	case strings.Contains(lower, "not found"):
		return "Check whether the referenced resource exists before retrying"
	case strings.Contains(lower, "too large") || strings.Contains(lower, "too many"):
		return "Reduce the size or count mentioned in the error"
	default:
		return "Correct your arguments and retry the tool call once"
	}
}
```

**Step 2: Write `internal/tools/error_test.go`**

```go
package tools

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestFormatToolError_DefaultRetryable(t *testing.T) {
	result := FormatToolError(errors.New("schema validation failed: missing rows"))
	var te ToolError
	if err := json.Unmarshal([]byte(result), &te); err != nil {
		t.Fatalf("not valid JSON: %v (got %q)", err, result)
	}
	if te.OK {
		t.Error("OK should be false")
	}
	if !te.Retryable {
		t.Error("Retryable should be true by default")
	}
	if te.Error == "" {
		t.Error("Error should not be empty")
	}
	if te.Hint == "" {
		t.Error("Hint should not be empty")
	}
}

func TestFormatToolError_HintForMissing(t *testing.T) {
	result := FormatToolError(errors.New("missing required field 'rows'"))
	var te ToolError
	json.Unmarshal([]byte(result), &te)
	if te.Hint == "" {
		t.Fatal("expected a hint")
	}
	if !strings.Contains(te.Hint, "required field") {
		t.Errorf("hint should mention required field, got %q", te.Hint)
	}
}

func TestFormatToolError_HintForInvalid(t *testing.T) {
	result := FormatToolError(errors.New("invalid value for 'count'"))
	var te ToolError
	json.Unmarshal([]byte(result), &te)
	if !strings.Contains(te.Hint, "Fix the format") {
		t.Errorf("unexpected hint: %q", te.Hint)
	}
}

func TestFormatToolError_HintForNotFound(t *testing.T) {
	result := FormatToolError(errors.New("source not found"))
	var te ToolError
	json.Unmarshal([]byte(result), &te)
	if !strings.Contains(te.Hint, "exists") {
		t.Errorf("unexpected hint: %q", te.Hint)
	}
}

func TestFormatToolError_HintForTooLarge(t *testing.T) {
	result := FormatToolError(errors.New("too many rows"))
	var te ToolError
	json.Unmarshal([]byte(result), &te)
	if !strings.Contains(te.Hint, "Reduce") {
		t.Errorf("unexpected hint: %q", te.Hint)
	}
}

func TestFormatToolError_GenericHint(t *testing.T) {
	result := FormatToolError(errors.New("something unexpected happened"))
	var te ToolError
	json.Unmarshal([]byte(result), &te)
	if te.Hint == "" {
		t.Fatal("expected a generic hint")
	}
}

func TestFormatFatalToolError_NotRetryable(t *testing.T) {
	result := FormatFatalToolError(errors.New("permission denied"))
	var te ToolError
	json.Unmarshal([]byte(result), &te)
	if te.OK {
		t.Error("OK should be false")
	}
	if te.Retryable {
		t.Error("Retryable should be false for fatal errors")
	}
	if te.Error == "" {
		t.Error("Error should not be empty")
	}
	if te.Hint != "" {
		t.Error("Hint should be empty for fatal errors")
	}
}
```

Add `"strings"` to the import block in `error_test.go`.

**Step 3: Run tests, expect pass**

```bash
go test ./internal/tools/ -run TestFormat -v
```

Expected: 7 PASS

**Step 4: Modify `internal/telegram/conversation.go` line 332**

Old:
```go
result = "(tool error) " + err.Error()
```

New:
```go
result = tools.FormatToolError(err)
```

**Step 5: Run full test suite**

```bash
go build ./... && go vet ./... && go test ./...
```

All green.

**Step 6: Commit**

```bash
git add internal/tools/error.go internal/tools/error_test.go internal/telegram/conversation.go
git commit -m "slice 16a: structured tool errors with retryable/hint classification"
```

---

### Task 2: Slice 16b — System prompt retry directive

**Files:**
- Modify: `internal/conversation/system_prompt.go:17` (inside the "Tool Use" section)

**Step 1: Add retry paragraph to system prompt**

In `internal/conversation/system_prompt.go`, after line 17 (`Use tools deliberately...` paragraph) and before the `- search_wiki:` bullet list, insert:

```go
If a tool result is a JSON object with "ok":false, it means the tool call failed. Read "retryable" and "hint":
- If retryable is true, correct your arguments using the hint and call the same tool again once. Do not apologize.
- If retryable is false or the retry also fails, briefly explain the problem to the user (in Italian if the user writes in Italian) and stop.
```

This goes into `defaultSystemPrompt` as raw text (it's a Go const string). The insertion point is right after the `Use tools deliberately...` line and before the blank line that precedes `- search_wiki:`.

**Step 2: Verify**

```bash
go build ./... && go vet ./... && go test ./...
```

All green.

**Step 3: Commit**

```bash
git add internal/conversation/system_prompt.go
git commit -m "slice 16b: system prompt tool-error retry directive"
```

---

### Task 3: Slice 16c — Immediate "thinking" placeholder

**Files:**
- Modify: `internal/telegram/conversation.go:21-127` (handleConversation)
- Modify: `internal/telegram/conversation.go:219-295` (runToolCallingLoop signature + body)
- Modify: `internal/telegram/streaming.go:43-102` (consumeStream signature + body)

**Why this is the most invasive slice:** The placeholder message must flow through `handleConversation` → `runToolCallingLoop` → `consumeStream`. Today `consumeStream` creates the message internally; we need to thread a pre-created `*tele.Message` through so it edits instead of creates.

**Step 1: Send placeholder at top of handleConversation**

In `handleConversation`, after the budget checks (line 121-122) and before `response, stats := b.runToolCallingLoop(...)` (line 124), add:

```go
// Slice 16c: send an immediate placeholder so the user knows we received
// their message. consumeStream edits this instead of creating a new one.
placeholder, _ := c.Bot().Send(c.Recipient(), "⏳")
```

The underscore eats the error — if Send fails we just fall through to the normal non-placeholder path.

**Step 2: Pass placeholder through runToolCallingLoop**

Change `runToolCallingLoop` signature from:
```go
func (b *Bot) runToolCallingLoop(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string) (string, turnStats)
```
to:
```go
func (b *Bot) runToolCallingLoop(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, placeholder *tele.Message) (string, turnStats)
```

Update the call site on line 124:
```go
response, stats := b.runToolCallingLoop(context.Background(), c, convCtx, userID, placeholder)
```

**Step 3: Pass placeholder into consumeStream**

In `runToolCallingLoop`, change the `consumeStream` call from:
```go
resp, delivered, err := b.consumeStream(c, ch, userID)
```
to:
```go
resp, delivered, err := b.consumeStream(c, ch, userID, placeholder)
```

**Step 4: Modify consumeStream to use the placeholder**

Change `consumeStream` signature from:
```go
func (b *Bot) consumeStream(c tele.Context, ch <-chan llm.Token, userID string) (llm.Response, bool, error)
```
to:
```go
func (b *Bot) consumeStream(c tele.Context, ch <-chan llm.Token, userID string, placeholder *tele.Message) (llm.Response, bool, error)
```

Modify the `flush` closure inside `consumeStream`. The logic becomes:

```go
flush := func() {
    text := renderForTelegram(sb.String())
    if sb.Len() < streamingMinThreshold {
        return
    }
    if msg == nil {
        if placeholder != nil {
            // Edit the pre-existing placeholder instead of sending a new message.
            if _, err := c.Bot().Edit(placeholder, text, tele.ModeHTML); err != nil {
                b.logger.Debug("placeholder edit failed, falling back to new message", "user_id", userID, "error", err)
                sent, sendErr := c.Bot().Send(c.Recipient(), text, tele.ModeHTML)
                if sendErr != nil {
                    return
                }
                msg = sent
            } else {
                msg = placeholder
            }
        } else {
            sent, err := c.Bot().Send(c.Recipient(), text, tele.ModeHTML)
            if err != nil {
                b.logger.Warn("streaming initial send failed", "user_id", userID, "error", err)
                return
            }
            msg = sent
        }
        lastEdit = time.Now()
        return
    }
    if time.Since(lastEdit) < streamingEditThrottle {
        return
    }
    if _, err := c.Bot().Edit(msg, text, tele.ModeHTML); err != nil {
        b.logger.Debug("streaming edit failed", "user_id", userID, "error", err)
        return
    }
    lastEdit = time.Now()
}
```

The key diff from the current code: before doing the 30-char threshold check, if we have a placeholder and haven't used it yet, we edit it. If editing fails (placeholder was deleted), we fall back to creating a new message.

**Step 5: Handle the no-streaming case**

After `runToolCallingLoop` returns in `handleConversation`, if the placeholder was used (delivered=false, meaning consumeStream edited the placeholder), we should delete the placeholder if it wasn't used. Add after line 124-127:

```go
response, stats := b.runToolCallingLoop(context.Background(), c, convCtx, userID, placeholder)
if response != "" {
    b.sendAssistant(c, response)
    // If we had a placeholder that wasn't consumed by streaming, clean it up.
    if placeholder != nil {
        _ = c.Bot().Delete(placeholder)
    }
} else if placeholder != nil {
    // Streaming delivered the response; placeholder was edited in place.
    // Nothing to clean up.
}
```

Wait, that's not exactly right. The logic should be:
- If `response != ""` (non-streamed final message was sent), delete the placeholder
- If `response == ""` (streaming already delivered via placeholder edits), leave it

Actually looking at the current logic more carefully:
- When streaming delivers the text progressively and there are no tool calls, `consumeStream` returns `delivered=true`, and `runToolCallingLoop` returns `"", stats` (line 279)
- When there were tool calls, `consumeStream` returns `delivered=false`, the loop continues, and eventually a final response is returned or tool results are returned

So for the non-streamed case (no streaming support, or all-tool-calls turns), the placeholder stays as "⏳" and needs to be cleaned. Let me simplify:

```go
response, stats := b.runToolCallingLoop(context.Background(), c, convCtx, userID, placeholder)
if response != "" {
    // Non-streamed delivery: delete the placeholder, send the real response.
    if placeholder != nil {
        _ = c.Bot().Delete(placeholder)
    }
    b.sendAssistant(c, response)
}
// When response == "", streaming edited the placeholder in place — nothing to do.
```

**Step 6: Verify compilation**

```bash
go build ./...
```

Must succeed.

**Step 7: Run tests**

```bash
go test ./...
```

All green.

**Step 8: Commit**

```bash
git add internal/telegram/conversation.go internal/telegram/streaming.go
git commit -m "slice 16c: immediate Telegram placeholder before streaming"
```

---

### Task 4: Slice 16d — Defer EnforceLimit to after response

**Files:**
- Modify: `internal/telegram/conversation.go:91-94`

**Step 1: Move EnforceLimit call**

Remove lines 91-94:
```go
// Enforce context limits: summarize at 80%, trim at hard limit
if err := convCtx.EnforceLimit(context.Background()); err != nil {
    b.logger.Error("context enforcement failed", "user_id", userID, "error", err)
}
```

After the archiver block (after line 196), add:
```go
// Slice 16d: context enforcement runs after the user has seen the
// response so summarizer latency doesn't add to perceived wait time.
go func() {
    if err := convCtx.EnforceLimit(context.Background()); err != nil {
        b.logger.Error("context enforcement failed", "user_id", userID, "error", err)
    }
}()
```

**Step 2: Verify**

```bash
go build ./... && go vet ./... && go test ./...
```

All green.

**Step 3: Commit**

```bash
git add internal/telegram/conversation.go
git commit -m "slice 16d: defer EnforceLimit summarization to after Telegram delivery"
```

---

### Task 5: Slice 16e — Tighten edit throttle 800ms → 600ms

**Files:**
- Modify: `internal/telegram/streaming.go:34`

**Step 1: Change the constant**

```go
const streamingEditThrottle = 600 * time.Millisecond
```

Update the comment on lines 31-33:
```go
// streamingEditThrottle bounds how often we call Telegram's editMessage
// API. Telegram rate-limits edits to ~1/sec per chat; 600ms keeps us
// safely under the limit while feeling more responsive.
```

**Step 2: Verify**

```bash
go build ./... && go test ./...
```

All green.

**Step 3: Commit**

```bash
git add internal/telegram/streaming.go
git commit -m "slice 16e: tighten streaming edit throttle 800ms → 600ms"
```

---

## Quality Gates

After all 5 slices:

```bash
go build ./... && go vet ./... && go test ./... && go test -race ./...
```

Must be green. Then live Telegram smoke:
1. Send a message that triggers a tool call → verify error recovery if a tool fails
2. Observe the "⏳" placeholder appears immediately
3. Verify streaming edits feel responsive at 600ms throttle
