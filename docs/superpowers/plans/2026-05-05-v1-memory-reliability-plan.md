# v1 Memory Reliability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Telegram conversation archive append failures observable and prove successful and failed archive writes with focused tests.

**Architecture:** Keep the existing archive storage and buffered appender behavior. Extract the turn-archiving loop from `handleConversation` into a focused helper that can be tested without a real Telegram context, and log every direct append failure with enough chat/turn metadata to diagnose memory loss.

**Tech Stack:** Go, `log/slog`, existing `internal/conversation` archive types, existing `internal/telegram` package tests.

---

## File Structure

- Modify: `internal/telegram/bot.go`
  - Replace the concrete buffered appender field with a small package-local interface so tests can inject append failures while production still uses `*conversation.BufferedAppender`.
- Modify: `internal/telegram/conversation.go`
  - Move the archive append loop into `archiveConversationTurns`.
  - Call the helper from `handleConversation` after `MaxTurnIndex`.
  - Keep summarizer extraction and context enforcement behavior unchanged.
- Modify: `internal/conversation/archive.go`
  - Log buffered archive drain/drop failures at error level with turn metadata.
- Create: `internal/telegram/archive_test.go`
  - Test successful user, assistant tool-call, tool-result, and final assistant archive turns.
  - Test append failures are logged with `chat_id`, `turn_index`, `role`, and `error`.
- Modify: `internal/conversation/buffered_test.go`
  - Assert buffered appender drain failures log at error level with `chat_id`, `turn_index`, `role`, and `error`.
- Modify: `docs/implementation-tracker.md`
  - Add the Phase 3 handoff with files touched and verification.
- Modify: `.planning/REQUIREMENTS.md`
  - Mark `MEM-01` complete after tests pass.

## Task 1: Extract Testable Archive Helper

**Files:**
- Modify: `internal/telegram/bot.go`
- Modify: `internal/telegram/conversation.go`
- Create: `internal/telegram/archive_test.go`

- [x] **Step 1: Write the failing success-path test**

Add `TestArchiveConversationTurnsAppendsUserAndLoopMessages` to `internal/telegram/archive_test.go`. The test should call:

```go
archiveConversationTurns(context.Background(), logger, appender, archiveTurnInput{
	ChatID:    42,
	UserID:    7,
	NextIndex: 10,
	UserText:  "please compute",
	LoopMessages: []llm.Message{
		{Role: "assistant", Content: "calling", ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "execute_code", Arguments: map[string]any{"code": "print(5050)"}}}},
		{Role: "tool", ToolCallID: "call-1", Content: "5050"},
		{Role: "assistant", Content: "done"},
	},
	Stats:    turnStats{llmCalls: 2, toolCalls: 1},
	ElapsedMS: 123,
	TokensIn:  456,
})
```

Assert four turns were appended, indexes are `10..13`, the final assistant turn carries `LLMCalls`, `ToolCallsCount`, `ElapsedMS`, and `TokensIn`, the assistant tool call JSON is non-empty, and the tool result keeps `ToolCallID`.

- [x] **Step 2: Run the targeted test and watch it fail**

Run: `go test ./internal/telegram -run TestArchiveConversationTurnsAppendsUserAndLoopMessages -count=1`

Expected: FAIL because `archiveConversationTurns` and `archiveTurnInput` are not defined.

- [x] **Step 3: Write the minimal helper and interface**

Add a package-local interface in `internal/telegram/bot.go`:

```go
type conversationArchiver interface {
	Append(context.Context, conversation.Turn) error
	Close(context.Context) error
}
```

Change `Bot.archiver` to `conversationArchiver`.

Add `archiveTurnInput` and `archiveConversationTurns` in `internal/telegram/conversation.go`. The helper appends the user turn first, then each loop message. If `Append` returns an error, log:

```go
logger.Error("archive: append failed", "chat_id", turn.ChatID, "turn_index", turn.TurnIndex, "role", turn.Role, "error", err)
```

- [x] **Step 4: Run the targeted test and watch it pass**

Run: `go test ./internal/telegram -run TestArchiveConversationTurnsAppendsUserAndLoopMessages -count=1`

Expected: PASS.

## Task 2: Prove Failure Observability

**Files:**
- Modify: `internal/telegram/archive_test.go`
- Modify: `internal/telegram/conversation.go`

- [x] **Step 1: Write the failing failure-path test**

Add `TestArchiveConversationTurnsLogsAppendFailures` with a fake appender returning `errors.New("disk full")` on every append and a `slog.NewTextHandler` backed by `bytes.Buffer`. Assert the log contains:

```text
archive: append failed
chat_id=42
turn_index=10
role=user
error="disk full"
```

- [x] **Step 2: Run the targeted test and watch it fail if logging is missing**

Run: `go test ./internal/telegram -run TestArchiveConversationTurnsLogsAppendFailures -count=1`

Expected before implementation: FAIL because append errors are not logged.

- [x] **Step 3: Keep the helper logging all append failures**

Use the helper from Task 1 and keep the log at `Error` level because durable memory loss is production-significant.

- [x] **Step 4: Run the targeted archive tests**

Run: `go test ./internal/telegram -run TestArchiveConversationTurns -count=1`

Expected: PASS.

## Task 3: Wire Handler and Complete Phase

**Files:**
- Modify: `internal/telegram/conversation.go`
- Modify: `.planning/REQUIREMENTS.md`
- Modify: `.planning/STATE.md`
- Modify: `docs/implementation-tracker.md`

- [x] **Step 1: Replace inline archive loop**

In `handleConversation`, keep `MaxTurnIndex` lookup in place, then call:

```go
archiveConversationTurns(ctx, b.logger, b.archiver, archiveTurnInput{
	ChatID:       chatID,
	UserID:       c.Sender().ID,
	NextIndex:    nextIdx,
	UserText:     userText,
	LoopMessages: convCtx.MessagesSince(preLoopIdx),
	Stats:        stats,
	ElapsedMS:    time.Since(turnStart).Milliseconds(),
	TokensIn:     convCtx.TotalTokensUsed(),
})
```

- [x] **Step 2: Run package tests**

Run: `go test ./internal/telegram ./internal/conversation -count=1`

Expected: PASS.

- [x] **Step 3: Run full Go verification**

Run: `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Expected: PASS.

- [x] **Step 4: Update durable state and commit**

Mark `MEM-01` done in `.planning/REQUIREMENTS.md`, advance `.planning/STATE.md` to Phase 4, and append a concise Phase 3 handoff to `docs/implementation-tracker.md`.

Commit only the slice files:

```powershell
git add docs/superpowers/plans/2026-05-05-v1-memory-reliability-plan.md docs/superpowers/plans/2026-05-04-v1-production-readiness-plan.md internal/conversation/archive.go internal/conversation/buffered_test.go internal/telegram/bot.go internal/telegram/conversation.go internal/telegram/archive_test.go .planning/REQUIREMENTS.md .planning/STATE.md docs/implementation-tracker.md
git commit -m "slice 3: make archive failures observable"
```

## Self-Review

- Spec coverage: Covers `MEM-01` and `.planning/ROADMAP.md` Phase 3 success criteria.
- Placeholder scan: No TBD/TODO placeholders.
- Type consistency: Helper uses existing `llm.Message`, `llm.ToolCall`, `conversation.Turn`, and `turnStats` types.
