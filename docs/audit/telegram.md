# Audit: internal/channels/telegram

**Verdict:** needs-work — one confirmed race/double-dispatch bug, two dead-production symbols, one not-wired exported function, one goroutine-leak under Stop.

**Counts:** critical 0 / high 2 / medium 1 / low 2

---

## Findings

### [HIGH][BUG] ForceReply answer double-dispatches: OnReply + OnText both fire, producing an unintended LLM turn

**Location:** `internal/channels/telegram/bot_dispatch.go:93` (handler registration), `internal/channels/telegram/bot_dispatch.go:223-231` (`onReply`), `internal/channels/telegram/bot_dispatch.go:99-125` (`onText`)

**Confidence:** high

**Detail:**

telebot's `ProcessContext` (confirmed from source `gopkg.in/telebot.v4@v4.0.0-beta.7/update.go:84-89`) does NOT return after `OnReply` — it falls through:

```go
if m.ReplyTo != nil {
    b.handle(OnReply, c)
}
b.handle(OnText, c)   // always fires — no return above
```

With `synchronous=false` (the default), `runHandler` spawns a goroutine for each. Both `onReply` and `onText` execute concurrently on the same message.

In `onReply`, `hitlHandlesText` calls `Resume.PendingFor`, finds the pending pause, submits the answer via `SubmitAnswer`, and triggers a resume turn. Concurrently, `onText` calls `hitlHandlesText` which also calls `PendingFor`. Two outcomes are possible depending on timing:

1. `onText` wins the race — reads `PendingFor` before `SubmitAnswer` completes, sees the pending pause, and also submits the answer (`double submit`).
2. `onText` loses the race — reads `PendingFor` after `SubmitAnswer` completes, sees nothing pending, falls through to `runTurn(text)` — the ForceReply answer text becomes a **new LLM turn input**, not the HITL resolve.

The comment at line 221 ("telebot fires OnReply (then OnText) for a reply") acknowledges the double-dispatch but claims no double-handling; that claim only holds for sequential execution, not concurrent goroutines.

The tests `TestOnReplyForceReplyAnswerResumes` and `TestOnTextPendingPauseRoutesToHITL` exercise these handlers in strict isolation (one call each) and do not replicate the concurrent double-dispatch.

**Suggested fix:** In `onReply`, after `hitlHandlesText` consumes the pause, store a per-chat flag (e.g., in `commands.cancels` or a new `recentlyConsumedHITL sync.Map`) that `onText` checks before falling through to `runTurn`. Alternatively, make `onReply` responsible for both the HITL resolve AND the command/LLM routing (replacing `onText` for reply messages), so `onText` is never registered for messages where `ReplyTo != nil`.

---

### [HIGH][BUG] Goroutine leak: async document-convert turn escapes Stop's drain

**Location:** `internal/channels/telegram/bot.go:260-271` (`Stop`), `internal/channels/telegram/bot_dispatch.go:386-393` (async `asyncResult` closure), `internal/channels/telegram/bot_dispatch.go:509` (`t.wg.Add(1)`)

**Confidence:** medium

**Detail:**

`Stop` drains goroutines in two separate WaitGroups across two steps:

1. `t.wg.Wait()` — joins the polling goroutine (and any in-flight turn goroutines tracked by `t.wg`).
2. `docs.Stop(ctx)` — waits for in-flight async document-convert goroutines via `d.wg`.

The async convert goroutine (tracked in `d.wg`) can call the `asyncResult` closure, which calls `t.startTurn(...)`, which calls `t.wg.Add(1)` and spawns a new turn goroutine. This `t.wg.Add(1)` occurs AFTER step 1 (`t.wg.Wait()`) has already returned. `Stop` never calls `t.wg.Wait()` a second time, so the turn goroutine spawned by the async convert callback is never joined — it leaks.

In practice this requires: a document conversion was accepted async just before Stop was called, the sidecar responds successfully during `docs.Stop`, and the async callback drives a new turn. The window is narrow but real.

**Suggested fix:** Replace the sequential `t.wg.Wait()` + `docs.Stop()` with a single drain loop: move `docs.Stop` before `t.wg.Wait`, or collect both WaitGroups into a single combined drain. Simplest: call `docs.Stop(ctx)` first (it blocks until converts complete and their callbacks fire), then call `t.wg.Wait()` to drain all turn goroutines including those spawned by callbacks.

---

### [MEDIUM][DEAD-CODE] `hitl.handleCallback` is production-dead; only test-referenced

**Location:** `internal/channels/telegram/hitl.go:106-108`

**Confidence:** high

**Detail:**

```go
func (h *hitl) handleCallback(ctx context.Context, data, convID string) (resumed bool) {
    return h.handleCallbackResult(ctx, data, convID, nil).resumed
}
```

The production `onCallback` handler in `bot_dispatch.go:247` calls `h.handleCallbackResult(...)` directly. `handleCallback` is never called in production code — grep across the entire repo (`D:/Aura`) confirms all seven call sites are in `internal/channels/telegram/hitl_test.go`.

**Suggested fix:** Delete `handleCallback`. Update the test call sites to call `handleCallbackResult(..., nil).resumed` inline, or keep a test-only helper local to the test file.

---

### [LOW][NOT-WIRED] `EscapeMarkdownV2` is exported but has zero production callers

**Location:** `internal/channels/telegram/mdv2.go:34`

**Confidence:** high

**Detail:**

`EscapeMarkdownV2` is exported. Grep across `D:/Aura` shows it is only referenced in `internal/channels/telegram/mdv2_test.go`. The renderer (`renderer.go`) uses `RenderTelegramHTML` (the `html.go` gotg_md2html wrapper), never MarkdownV2 escaping. The mdv2 escaper is well-tested but completely bypassed in the production render path.

`PlainTextFallback` (same file, line 113) is also exported and only referenced from `renderer.go` (production) and `renderer_protocol_test.go` (test) — that one is wired.

**Suggested fix:** Either unexport `EscapeMarkdownV2` (rename to `escapeMarkdownV2`) if it is internal to the package, or wire it into the render path if it was intended to replace `RenderTelegramHTML`.

---

### [LOW][NOT-WIRED] `PreBlockTable` is exported but has zero production callers; `TouchLastSeen` has zero callers at all

**Location:** `internal/channels/telegram/tables.go:248` (`PreBlockTable`), `internal/channels/telegram/store.go:181` (`TouchLastSeen`)

**Confidence:** high

**Detail:**

`PreBlockTable`: grep across `D:/Aura` finds it only in `tables_test.go`. The renderer's `sendTable` fallback path (renderer.go:158-160) falls through to `sendText` when PNG rendering fails — it does not call `PreBlockTable`. The comment on tables.go:20 ("the renderer treats it as "fall back to PreBlockTable"") is stale.

`TouchLastSeen`: grep across the entire repo (all `.go` files) finds it only in `store.go` itself (definition). No caller exists anywhere — not in production, not in tests. The method compiles and is exported but is a complete dead export.

**Suggested fix:**
- `PreBlockTable`: unexport and either delete or wire it into the renderer's table-send fallback path.
- `TouchLastSeen`: delete or add a caller (e.g., bump last_seen_at on each inbound turn via `GetAccountByTelegramID` + `TouchLastSeen`).

---

## Notes

- `searchPages map[int64]searchPagerState` in `commands.go` grows unbounded — each `/search` upserts an entry, the "Chiudi" callback deletes the message but not the map entry. For a single-user bot this is cosmetic; for a multi-user deployment it is a memory leak. Not filed as a separate finding given the current single-user scope, but worth noting.
- `route()` in `photo.go:143` declares `model :=` (short var) inside the `if` branch, shadowing the named return `model`. Correct logic (the explicit `return ..., model` returns the local), but the shadow makes the code harder to audit. `go vet` does not flag it.
- The `min` local variable in `renderer.go:414` (`splitBoundary`) shadows the Go 1.21+ builtin `min`. Correct behavior, no defect.
