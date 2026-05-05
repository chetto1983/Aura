# v1 Telegram Regression Harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close `TEST-01` by adding focused hermetic regression tests for Aura's Telegram critical path: access control, conversation delivery, streaming edits, PDF/OCR triggers, and archive behavior.

**Architecture:** Keep the harness inside `internal/telegram` and test package-private functions directly where possible. Use existing temp SQLite/auth/source stores and `httptest` Telegram API doubles instead of live Telegram. Treat this as a regression confidence phase, not an arbitrary coverage campaign.

**Tech Stack:** Go, `testing`, `httptest`, `telebot.v4`, existing `internal/telegram` package tests, existing `loops/aura-implementation` verification scripts.

---

## Existing Coverage To Preserve

- `internal/telegram/archive_test.go`
  - Archives user turns, assistant tool-call turns, tool results, final assistant telemetry.
  - Logs archiver append failures with chat/turn/role/error metadata.
- `internal/telegram/documents_test.go`
  - Validates PDF MIME/size/name helpers.
  - Proves document handler `Stop` waits for registered and in-flight work.
- `internal/telegram/bot_test.go`
  - Proves allowlist lookup uses configured allowlist first.
  - Proves bootstrap auth store is used only when env allowlist is blank.
  - Proves owner collection merges env and DB owners without duplicates.
- `internal/telegram/scheduler_handlers_test.go`
  - Guards safe scheduled-agent tool allowlists.

## File Structure

- Create: `internal/telegram/streaming_test.go`
  - Hermetic tests for `consumeStream` against a local fake Telegram API.
  - Covers placeholder editing, final edit, send fallback, and tool-call non-delivery.
- Modify: `internal/telegram/bot_test.go`
  - Add handler-level access-control tests for `onMessage` so unauthorized text never starts a conversation and authorized text returns immediately.
- Modify: `internal/telegram/documents_test.go`
  - Add `onDocument` gate tests using a fake Telegram API and fake PDF input to prove unauthorized users are rejected before source/OCR work and authorized PDF messages register work.
- Modify: `internal/telegram/archive_test.go`
  - Add a max-index allocation regression if a practical helper seam exists; otherwise keep current archive helper coverage as the Phase 5 archive proof.
- Modify: `.planning/REQUIREMENTS.md`
  - Mark `TEST-01` done when focused tests land and pass.
- Modify: `.planning/STATE.md`
  - Advance to Phase 6 Release Gate after `TEST-01` is complete.
- Modify: `docs/implementation-tracker.md`
  - Add Phase 5 handoff: files touched, checks run, residual gaps.
- Modify: `docs/superpowers/plans/2026-05-04-v1-production-readiness-plan.md`
  - Mark Telegram Regression subplan/task complete.

## Task 1: Streaming Delivery Regression

**Files:**
- Create: `internal/telegram/streaming_test.go`
- Modify: `docs/superpowers/plans/2026-05-05-v1-telegram-regression-plan.md`

- [x] **Step 1: Add a fake Telegram API server helper**

Create a helper in `internal/telegram/streaming_test.go` that records Telegram method calls and returns valid Telegram-style JSON:

```go
type telegramAPICall struct {
	Method string
	Body   map[string]any
}

func newTelegramAPIServer(t *testing.T, calls *[]telegramAPICall) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		method := path.Base(r.URL.Path)
		*calls = append(*calls, telegramAPICall{Method: method, Body: body})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":123},"date":1760000000,"text":"ok"}}`))
	}))
}
```

- [x] **Step 2: Add `consumeStream` placeholder-edit test**

Add:

```go
func TestConsumeStreamEditsPlaceholderAndSuppressesDoubleSend(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123},
		Chat:   &tele.Chat{ID: 123},
		Text:   "hello",
	}})
	ch := make(chan llm.Token, 2)
	ch <- llm.Token{Content: strings.Repeat("x", streamingMinThreshold)}
	ch <- llm.Token{Done: true, Usage: llm.TokenUsage{TotalTokens: 7}}
	close(ch)

	b := &Bot{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	resp, delivered, err := b.consumeStream(ctx, ch, "123", &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}})
	if err != nil {
		t.Fatal(err)
	}
	if !delivered {
		t.Fatal("delivered = false, want true after placeholder edit")
	}
	if resp.Content != strings.Repeat("x", streamingMinThreshold) || resp.Usage.TotalTokens != 7 {
		t.Fatalf("response = %+v", resp)
	}
	if got := countTelegramMethods(calls, "editMessageText"); got != 2 {
		t.Fatalf("editMessageText calls = %d, want 2", got)
	}
	if got := countTelegramMethods(calls, "sendMessage"); got != 0 {
		t.Fatalf("sendMessage calls = %d, want 0", got)
	}
}
```

- [x] **Step 3: Add tool-call non-delivery test**

Add:

```go
func TestConsumeStreamToolCallDoesNotMarkDelivered(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{Sender: &tele.User{ID: 123}, Chat: &tele.Chat{ID: 123}}})
	ch := make(chan llm.Token, 1)
	ch <- llm.Token{Done: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_wiki"}}}
	close(ch)

	b := &Bot{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	resp, delivered, err := b.consumeStream(ctx, ch, "123", &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}})
	if err != nil {
		t.Fatal(err)
	}
	if delivered {
		t.Fatal("delivered = true, want false for tool call response")
	}
	if !resp.HasToolCalls || len(resp.ToolCalls) != 1 {
		t.Fatalf("response tool calls = %+v", resp.ToolCalls)
	}
	if len(calls) != 0 {
		t.Fatalf("telegram calls = %+v, want none before tool result", calls)
	}
}
```

- [x] **Step 4: Run streaming tests**

Run:

```powershell
go test ./internal/telegram -run TestConsumeStream -count=1
```

Expected: PASS.

- [x] **Step 5: Commit streaming regression slice**

Stage explicitly:

```powershell
git add docs/superpowers/plans/2026-05-05-v1-telegram-regression-plan.md internal/telegram/streaming_test.go
git commit -m "slice 5a: cover telegram streaming delivery"
```

## Task 2: Text Access-Control Regression

**Files:**
- Modify: `internal/telegram/bot_test.go`

- [x] **Step 1: Test unauthorized text is ignored**

Add a test that constructs `Bot{cfg: &config.Config{Allowlist: []string{"owner"}, AllowlistConfigured: true}}`, sends a fake context from user `999`, calls `onMessage`, and asserts:

- returned error is nil;
- `b.active` does not contain `"999"`;
- `b.ctxMap` does not contain `"999"`.

- [x] **Step 2: Test authorized text returns immediately**

Add a test that constructs an allowlisted bot with nil LLM and a fake context from `"owner"`, calls `onMessage`, and waits briefly for `b.ctxMap` to contain `"owner"`. This guards the goroutine launch contract without waiting for a live LLM.

- [x] **Step 3: Run access tests**

Run:

```powershell
go test ./internal/telegram -run "TestOnMessage" -count=1
```

Expected: PASS.

## Task 3: Document/OCR Trigger Regression

**Files:**
- Modify: `internal/telegram/documents_test.go`

- [x] **Step 1: Test unauthorized document is rejected before work registration**

Call `newDocHandler(docHandlerConfig{Allowlist: func(string) bool { return false }})` with a fake document context and assert:

- `onDocument` returns nil;
- no source directory is created;
- `beginWork` remains available afterward.

- [x] **Step 2: Test authorized PDF registers async work**

Use a fake Telegram API server that can answer `sendMessage` and `getFile`, configure a temp source store and OCR disabled, call `onDocument` with an allowlisted PDF, wait for the worker to complete, and assert a stored source exists with `status=stored`.

- [x] **Step 3: Run document tests**

Run:

```powershell
go test ./internal/telegram -run "TestDocHandler.*Document" -count=1
```

Expected: PASS.

## Task 4: Phase Handoff

**Files:**
- Modify: `.planning/REQUIREMENTS.md`
- Modify: `.planning/STATE.md`
- Modify: `docs/implementation-tracker.md`
- Modify: `docs/superpowers/plans/2026-05-04-v1-production-readiness-plan.md`
- Modify: `docs/superpowers/plans/2026-05-05-v1-telegram-regression-plan.md`

- [x] **Step 1: Run focused Phase 5 tests**

Run:

```powershell
go test ./internal/telegram -count=1
```

Expected: PASS.

- [x] **Step 2: Run full Go verification**

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1
```

Expected: PASS. Restore unrelated line-ending churn before staging.

- [x] **Step 3: Mark `TEST-01` done**

Update `.planning/REQUIREMENTS.md` traceability:

```markdown
| TEST-01 | Phase 5: Telegram Regression Harness | Done — focused Telegram conversation, streaming, document/OCR trigger, access, and archive tests landed on 2026-05-05 |
```

Set coverage to Complete `6`, Remaining `1`.

- [x] **Step 4: Advance state to Phase 6**

Set `.planning/STATE.md`:

```markdown
Phase: 6 of 6 (Release Gate)
Plan: Phase 6 - Release Gate
Status: Phase 5 Telegram Regression Harness complete; ready to run automated and manual release gates
Current focus: write/execute release gate plan covering Go, web, sandbox, migration, packaging, and Windows smoke
```

- [ ] **Step 5: Commit Phase 5 closure**

Stage explicitly:

```powershell
git add .planning/REQUIREMENTS.md .planning/STATE.md docs/implementation-tracker.md docs/superpowers/plans/2026-05-04-v1-production-readiness-plan.md docs/superpowers/plans/2026-05-05-v1-telegram-regression-plan.md internal/telegram/bot_test.go internal/telegram/documents_test.go internal/telegram/streaming_test.go
git commit -m "slice 5: close telegram regression harness"
```

## Self-Review

- Spec coverage: Maps `TEST-01` to streaming edits, text access control, document/OCR trigger, and existing archive coverage.
- Placeholder scan: No TBD/TODO placeholders.
- Type consistency: Uses existing `tele.NewContext`, `llm.Token`, `llm.ToolCall`, and `docHandler` names from current code.
