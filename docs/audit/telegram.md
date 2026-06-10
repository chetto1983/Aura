# Audit: internal/channels/telegram

**Verdict:** needs-work — six findings across dead/not-wired code, one resource-leak, one logic inconsistency, one mild token-in-log. No critical defects. The lifecycle, concurrency, and dispatch logic are sound.

**Counts:** critical 0 / high 1 / medium 3 / low 2

---

## Findings

### [HIGH][NOT-WIRED] `EscapeMarkdownV2` and `PreBlockTable` are fully tested but never called in production

**Location:** `internal/channels/telegram/mdv2.go:34`, `internal/channels/telegram/tables.go:248`

**Confidence:** high

**Detail:**
The production renderer (`renderer.go`) switched to HTML parse-mode (`RenderTelegramHTML` via `tgmd2html.MD2HTMLV2`). Neither `EscapeMarkdownV2` nor `PreBlockTable` appears in any non-test `.go` file in the repo. Both are exported and heavily tested, but the test coverage is exercising dead production paths. `tables.go` even has a comment that says the renderer "falls back to PreBlockTable" but the code in `renderer.sendTable` falls through to plain `sendText`, never calling `PreBlockTable`. A reader of `renderer.go:20` is misled into believing `PreBlockTable` is reachable.

Verified with full-repo grep: only references are `mdv2_test.go` and `tables_test.go`.

**Suggested fix:**
Either wire `PreBlockTable` into `renderer.sendTable` as the intended text fallback (replace the plain `sendText` fallback with it, or use it when PNG render fails), OR delete both functions and their tests to eliminate the misleading comment. The MarkdownV2 path was superseded by the HTML path; keeping the code without a caller is a maintenance hazard.

---

### [MEDIUM][BUG] Status pane edit failures silently clear `dirty` and update `lastEdit`, suppressing retry

**Location:** `internal/channels/telegram/status_pane.go:269-270`

**Confidence:** high

**Detail:**
In `render()`, the `p.lastEdit = p.now()` and `p.dirty = false` assignments are executed unconditionally after the `p.bot.Edit` call, regardless of whether the edit succeeded:

```go
} // end of if/else for edit result
p.lastEdit = p.now()  // line 269 — always runs
p.dirty = false       // line 270 — always runs
```

A failed edit (network error, rate-limit, stale message) looks identical to a successful one from the pane's perspective. The pane then waits a full throttle window before attempting another edit — but because `dirty` is already false, no retry is attempted at all. The inconsistency with the initial `Send` path (lines 224-232) is notable: a failed `Send` returns early, leaving `dirty=true` and `lastEdit` unchanged, so it IS retried on the next event. Edit failures are silently discarded.

**Suggested fix:**
Only clear `dirty` and update `lastEdit` when the edit succeeded:
```go
if err == nil && out != nil {
    p.msg = out
    p.lastEdit = p.now()
    p.dirty = false
}
// on error: leave dirty=true so the next event triggers a retry
```

---

### [MEDIUM][NOT-WIRED] `commands.searchPages` map grows without bound and is never evicted

**Location:** `internal/channels/telegram/commands.go:221-222`

**Confidence:** high

**Detail:**
`commands.searchPages` (type `map[int64]searchPagerState`) is written on every paginated `/search` result (line 221) and read in `renderSearchPage` (line 232), but there is no delete or expiry path. Each unique `chatID` that issues a paginated search grows the map by one entry permanently. On a bot running continuously across many users, this is an unbounded memory accumulation. A large result set (20 hits, 5/page = 4 pages of strings) per active user accumulates indefinitely.

Neither `Stop()` nor any GC path touches this map. Contrast with `cancels`, which is cleaned up on `unregisterTurn`.

**Suggested fix:**
Delete the entry when the user closes the pager ("Chiudi" button, `closePager=true` path in `onSearchCallback`) — that's the natural expiry signal. Also add a TTL eviction for stale entries (e.g., on next `/search` from the same chat, overwrite; already done at line 221 — so per-chat it's bounded to the latest search, which is acceptable). For multi-user deployments, the close-pager delete is sufficient:
```go
if closePager {
    c.mu.Lock()
    delete(c.searchPages, cb.Message.Chat.ID)  // add this
    c.mu.Unlock()
    ...
}
```

---

### [MEDIUM][DEAD-CODE] `Store.TouchLastSeen` is defined but never called in the repo

**Location:** `internal/channels/telegram/store.go:181`

**Confidence:** high

**Detail:**
`TouchLastSeen` bumps `last_seen_at` for a Telegram account. It is not called from any production code, not referenced in any test, and not wired in `cmd/aura/serve_channels.go`. Full-repo grep confirms zero references outside the definition.

This leaves `last_seen_at` permanently at its zero value for all accounts. Callers relying on this column (e.g., the setup-status handler, a future activity report) will see stale data.

**Suggested fix:**
Call `TouchLastSeen` in the `onText` / `onVoice` / `onPhoto` / `onDocument` handlers (e.g., in `runTurn` or `startTurn`) when the `Store` is non-nil, so the column reflects real activity. If the field is intentionally deferred to a later slice, add a `// TODO(slice-X)` comment so it is not silently forgotten.

---

### [LOW][DEAD-CODE] `onboarding == nil` guard in `handleStartPayload` is unreachable

**Location:** `internal/channels/telegram/bot_dispatch.go:191-197`

**Confidence:** high

**Detail:**
`handleStartPayload` checks `if onboard == nil` and constructs a fresh `onboarding` instance as a fallback. However, `t.onboard` is always set by `buildDispatch()` (line 65 of `bot_dispatch.go`), which is called from `Start()` under `t.mu` before any handler can fire. The nil branch can never execute in production.

**Suggested fix:**
Remove the nil guard and use `t.onboard` directly. If defensive nil-safety is desired, a package-level invariant comment suffices.

---

### [LOW][DEAD-CODE] Dangling `capRunesTail` doc comment with no implementation

**Location:** `internal/channels/telegram/status_pane.go:355`

**Confidence:** high

**Detail:**
The file ends with `// capRunesTail truncates s to at most n runes, preserving the newest content.` — a dangling doc comment for a function that was never implemented (or was deleted in an earlier refactor). It appears after the last complete function, suggesting an incomplete refactor pass. It causes no build error, but misleads any reader who checks for `capRunesTail` in the codebase (zero results anywhere).

**Suggested fix:**
Delete the comment. If the function is planned, convert it to a `// TODO` inline note rather than a stub doc comment for a missing symbol.

---

## What was checked and found clean

- **Goroutine lifecycle**: `pulseChatAction`, `startTurn`, async document convert all tracked by WaitGroups or joined via `stop()`. No leaks found.
- **Map concurrency**: All shared maps (`cancels`, `searchPages`, `hitlRepliesHandled`) are protected by the appropriate mutex or accessed from a single goroutine. No data races found.
- **Context propagation**: `daemonCtx` flows from `Start()` into all handlers; per-turn `turnCtx` is derived and cancelled at turn end. Context is not dropped anywhere.
- **HTTP body close**: All `resp.Body.Close()` calls are deferred in `voice.go`, `photo.go`, `documents.go`, `tts.go`. No body leaks found.
- **Ticker/timer leaks**: `pulseChatAction` defers `ticker.Stop()`. `voiceClient.sleep` defers `timer.Stop()`. Both clean.
- **HITL callback_data overflow**: `callbackData` panics if the payload exceeds 64 bytes. Tokens are UUIDs (36 chars); longest payload is `uuid|decline|` = ~45 bytes. No overflow path reachable with current usage.
- **JSON/SQL mishandling**: `pgtype` wrappers are correctly converted at the boundary in `store.go`. SQLSTATE classification is via `errors.As + pgErr.Code`, not string matching.
- **Error wrapping**: All errors use `%w`; sentinel errors are classified with `errors.Is`. No string-matching on errors.
- **Download size**: `downloadFile` in `bot_dispatch_file.go` defers `rc.Close()` and uses `io.ReadAll` (bounded upstream by the Bot-API 20MB ceiling). Clean.
- **Fanout ordering**: `fo.Subscribe()` × 3 before `fo.Run()` is correct per the fanout contract. The buffered (cap=64) channels handle the consumer goroutines starting after `Run()`.
