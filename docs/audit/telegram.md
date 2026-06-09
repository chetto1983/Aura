# Audit: internal/channels/telegram

**Verdict:** needs-work — one logic bug silently swallows user HITL answers on DB error; several not-wired functions and two dead methods/stubs accumulate noise.

**Counts:** critical 0 / high 1 / medium 2 / low 4

---

## Findings

### [HIGH][BUG] telegram-1: HITL text answer silently swallowed on SubmitAnswer error

**Location:** `internal/channels/telegram/bot_dispatch.go:419-433` (`hitlHandlesText`)

**Confidence:** high

**Detail:**

`hitlHandlesText` first confirms there are pending pauses (`PendingFor` at line 423). It then calls `handleTextReply`, which internally calls `SubmitAnswer`. If `SubmitAnswer` returns an error, `submit` (hitl.go:186-189) logs a warn and returns `false`. Back in `hitlHandlesText`, `!false` is true, so `promptPendingPause` re-renders the same pause — and the function returns `true`, telling `onText` that HITL "consumed" the user's message.

The user's text is silently discarded. The same pause is re-shown. On a persistent DB error the user is stuck in an infinite re-prompt loop with no visible error message. The `slog.Warn` is the only signal.

The same flaw applies to `hitlHandlesReply` (line 442-455): `markHitlReplyHandled` marks the reply key before calling `handleTextReply`, so even on a submission error the key is consumed — `onText`'s `takeHitlReplyHandled` guard fires, the message is fully silenced, and the user never gets an ordinary turn as fallback.

**Suggested fix:**

Propagate the submit error distinctly. Change `handleTextReply` to return `(resumed bool, err error)` and thread the error up. In `hitlHandlesText`/`hitlHandlesReply`, when `err != nil`, send the user a transient "risposta non registrata, riprova" message instead of re-prompting the same pause. Alternatively, distinguish the "no pending" false from the "submit failed" false with a third return value or a sentinel.

---

### [MEDIUM][BUG] telegram-2: TOCTOU window allows HITL to swallow a legitimate plain-text message

**Location:** `internal/channels/telegram/bot_dispatch.go:419-433` (`hitlHandlesText`) and `hitl.go:174-180` (`handleTextReply`)

**Confidence:** medium

**Detail:**

`hitlHandlesText` checks `PendingFor` (line 423) and, finding pauses, routes the message to HITL. `handleTextReply` calls `PendingFor` a second time (hitl.go:175). In the window between the two calls, a concurrent inline-button callback from another client device could resolve the last pause (the `onCallback` goroutine runs under a separate handler; although telebot serialises same-update-type deliveries, a callback is a separate update dispatched potentially on a concurrent poller iteration depending on bot settings). If the pause is resolved in that window, `handleTextReply` finds `len(pending) == 0` and returns `false (resumed=false)`, but `hitlHandlesText` — having already decided to intercept — returns `true`, silently dropping what is now an ordinary user message.

This is lower-risk under default single-bot-instance Telegram (the polling goroutine is one goroutine; callbacks and text arrive sequentially to their handlers), but becomes a real race if `Settings.Poller` is swapped for a concurrent webhook server.

**Suggested fix:**

Eliminate the second `PendingFor` call: pass the `pending` slice obtained in `hitlHandlesText` directly into `handleTextReply` so there is one consistent view per message.

---

### [MEDIUM][DEAD-CODE] telegram-3: EscapeMarkdownV2 defined and tested but never called in production

**Location:** `internal/channels/telegram/mdv2.go:34` (`EscapeMarkdownV2`)

**Confidence:** high

**Detail:**

`EscapeMarkdownV2` is an exported function with a full test suite (mdv2_test.go). The renderer (`renderer.go`) exclusively uses `RenderTelegramHTML` (the `gotg_md2html` library) and never calls `EscapeMarkdownV2`. A repo-wide grep confirms zero non-test, non-definition references to `EscapeMarkdownV2`. The function was likely scaffolded for a MarkdownV2 send-mode that was later replaced by the HTML send path.

`PlainTextFallback` in the same file is used in production (renderer.go:189, 201, 236) and is fine.

**Suggested fix:**

If the MarkdownV2 path is not planned for the near term, delete `EscapeMarkdownV2` and its test coverage. If it is planned, document the wiring gap with a `// TODO` and the slice that will call it.

---

### [LOW][DEAD-CODE] telegram-4: PreBlockTable defined and tested but never called in production

**Location:** `internal/channels/telegram/tables.go:248` (`PreBlockTable`)

**Confidence:** high

**Detail:**

`PreBlockTable` is referenced only from `tables_test.go`. The production renderer falls back to plain-text rendering of the raw markdown when `RenderTablePNG` fails (renderer.go:155-162) — it does NOT fall through to `PreBlockTable`. The function comment claims "the renderer treats it as a fall back to PreBlockTable" which is incorrect: the renderer falls back to `sendText(content, final)` with the raw markdown, not `PreBlockTable`.

**Suggested fix:**

Either wire `PreBlockTable` as the actual text fallback inside `renderer.go` (replacing the current raw-markdown fallback) to align code with comment, or delete it along with its tests.

---

### [LOW][DEAD-CODE] telegram-5: Four Store methods with no production callers

**Location:** `internal/channels/telegram/store.go` — `GetAccountByTelegramID` (line 169), `TouchLastSeen` (line 181), `CleanupExpired` (line 145), `ListAccounts` (line 197)

**Confidence:** high

**Detail:**

A repo-wide grep finds references to these four methods only in `store_integration_test.go`. No production code path calls them:

- `GetAccountByTelegramID`: no caller outside tests; the onboarding handler does not look up accounts post-consume.
- `TouchLastSeen`: no caller; the "best-effort activity marker" semantics implies it was planned but not wired.
- `CleanupExpired`: no caller; GC of expired tokens is not scheduled anywhere.
- `ListAccounts`: no caller; admin/setup paths do not use it.

These are not technically harmful (they are exported from a concrete type, not an interface), but they are untested in production context and accumulate dead surface area.

**Suggested fix:**

Wire `CleanupExpired` to a scheduler job (or the setup SSE-pump GC scan mentioned in the comment). Wire `TouchLastSeen` in the `onText` handler. Delete or document `GetAccountByTelegramID` and `ListAccounts` if they have no planned consumer.

---

### [LOW][DEAD-CODE] telegram-6: Three unexported methods only referenced from tests

**Location:**
- `internal/channels/telegram/commands.go:110` — `commands.dispatch`
- `internal/channels/telegram/hitl.go:106` — `hitl.handleCallback`
- `internal/channels/telegram/status_pane.go:355` — dangling doc comment for `capRunesTail` (function body absent)

**Confidence:** high

**Detail:**

`commands.dispatch` (line 110-113) is a thin wrapper over `dispatchRich` that strips the `commandReply` markup. Production uses `dispatchRich` exclusively (bot_dispatch.go:115). `dispatch` is only called from `commands_test.go`. Since tests should test the same path production uses, these tests provide false coverage: they exercise only the `text` field of `commandReply`, never the `markup` field that `/search` produces. The test suite should migrate to `dispatchRich`.

`hitl.handleCallback` (line 106-108) is a one-liner wrapper over `handleCallbackResult`. Production calls `handleCallbackResult` directly (bot_dispatch.go:249). The wrapper exists solely for tests.

`capRunesTail` (status_pane.go:355) is a doc comment for a function that was never written (or was deleted): the file ends at that line. It is a dangling stub.

**Suggested fix:**

Delete `commands.dispatch` and update `commands_test.go` to call `dispatchRich` directly. Delete `hitl.handleCallback` and update `hitl_test.go` to call `handleCallbackResult`. Remove the dangling `capRunesTail` comment.

---

## What was checked and found clean

- **Goroutine leaks / ticker leaks**: `pulseChatAction` properly closes `done` + joins `finished` via `sync.Once`. `docs.Convert` async goroutines tracked by `wg`, drained by `Stop`. `startTurn` goroutines tracked by `t.wg`. No leaks found.
- **Context propagation**: all sidecar requests use `context.WithTimeout` from `withTimeout`; all long operations propagate `daemonCtx`. The async document goroutine correctly detaches via `context.WithoutCancel` (intentional).
- **Mutex coverage**: `t.mu` guards `hitlRepliesHandled` (mark + take both lock). `commands.mu` guards `cancels` and `searchPages`. No unguarded shared map writes found.
- **Resource leaks**: all HTTP `resp.Body` closed via `defer`. `io.ReadCloser` from `bot.File()` deferred-closed.
- **Nil dereferences**: all `msg`, `msg.Chat`, `msg.Voice/Photo/Document`, `cb`, `cb.Message` guarded before use.
- **Error wrapping**: `%w` used consistently; sentinel errors use `errors.Is` for classification, not string matching.
- **Integer conversions**: no unsafe int/int64 conversions identified.
- **Fanout wiring**: `Subscribe` × 3 before `Run` — correct. Cap-64 buffered channels prevent blocking on startup lag.
- **`callbackData` panic on oversized payload**: this is a compile-time invariant (UUIDs are 36 bytes + short action); panic is the correct guard for a bug that would produce a bot-API 400 if silently truncated.
- **`onReply` / `onText` double-dispatch**: sequential poller guarantees `onReply` runs before `onText`; `markHitlReplyHandled` + `takeHitlReplyHandled` correctly prevents double-handling in the success path.
