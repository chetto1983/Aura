# Audit: internal/channels

**Verdict:** needs-work — several dead-code and not-wired issues, one logic bug in caption stripping, no critical defects.

**Counts:** critical 0 / high 0 / medium 3 / low 3

---

## Findings

### [MEDIUM][BUG] tableCaption strips all pipe-containing lines, not just table rows

**Location:** `internal/channels/telegram/renderer.go:265-280`
**Confidence:** high

**Detail:**
`tableCaption` excludes every line that contains a `|` character under the assumption that such a line is a markdown table row. This is wrong for prose lines that legitimately contain a pipe character (e.g., `"See Section A | B for details"`, `"options: foo | bar | baz"`). Such prose is silently dropped from the sendPhoto caption, and the user sees a truncated or empty caption with no error.

**Suggested fix:**
Only skip lines that match the markdown table row structure (at least one `|`-delimited token that is not the separator row). A minimal fix is to additionally require the line trims to start/end with `|` or contain multiple `|` chars after trimming:
```go
// skip only lines that look like a table row (leading/trailing pipe or >=2 separators)
trimmed := strings.TrimSpace(line)
if isMDTableRow(trimmed) {
    continue
}
```
where `isMDTableRow` checks for the `| ... |` pattern or `strings.Count(line, "|") >= 2`.

---

### [MEDIUM][DEAD-CODE] `commands.dispatch` is a test-only passthrough never called in production

**Location:** `internal/channels/telegram/commands.go:110-113`
**Confidence:** high

**Detail:**
`dispatch` is an unexported method that simply calls `dispatchRich` and discards the `markup` field:
```go
func (c *commands) dispatch(ctx context.Context, chatID int64, text string) (handled bool, reply string) {
    handled, out := c.dispatchRich(ctx, chatID, text)
    return handled, out.text
}
```
All 13 call sites are in `commands_test.go`. Production code at `bot_dispatch.go:115` calls `dispatchRich` directly. The method exists only as a test convenience shim that hides the markup return value, causing tests to never exercise the markup path (pagination buttons) through the canonical call chain.

**Suggested fix:**
Remove `dispatch`. Update the handful of tests that only need the text reply to call `dispatchRich` and ignore `.markup`, or keep a local test helper in the test file itself (unexported to the test package).

---

### [MEDIUM][DEAD-CODE] `hitl.handleCallback` is never called in production

**Location:** `internal/channels/telegram/hitl.go:106-108`
**Confidence:** high

**Detail:**
```go
func (h *hitl) handleCallback(ctx context.Context, data, convID string) (resumed bool) {
    return h.handleCallbackResult(ctx, data, convID, nil).resumed
}
```
All 7 call sites are in `hitl_test.go`. Production dispatch at `bot_dispatch.go:249` calls `handleCallbackResult` directly with the `afterSubmit` callback (which disarms the keyboard). Tests that use `handleCallback` never exercise the keyboard-disarm path, so the production `afterSubmit` logic is untested through this method.

**Suggested fix:**
Remove `handleCallback`. Update tests to call `handleCallbackResult` (with `nil` for `afterSubmit`) — or, if the `afterSubmit` path deserves explicit coverage, add a test for it.

---

### [LOW][DEAD-CODE] Dangling orphan doc comment `capRunesTail` with no function body

**Location:** `internal/channels/telegram/status_pane.go:355`
**Confidence:** high

**Detail:**
The file ends with:
```go
// capRunesTail truncates s to at most n runes, preserving the newest content.
```
There is no `func capRunesTail(...)` definition anywhere in the repo. The symbol is not referenced by any call site. This is a leftover doc comment from a function that was either never written or was deleted without removing its comment.

**Suggested fix:**
Delete the trailing comment line.

---

### [LOW][NOT-WIRED] `channels.Registry.Register` has no mutex, creating an API footgun

**Location:** `internal/channels/registry.go:46-48`
**Confidence:** medium

**Detail:**
`Register` writes to `r.channels` map without holding `r.mu`:
```go
func (r *Registry) Register(c Channel) {
    r.channels[c.Name()] = c
}
```
`r.mu` protects `r.started` but not `r.channels`. If `Register` were ever called concurrently with `StartAll` (which iterates `r.channels`), a data race would occur. In the current composition root (`serve_channels.go:75-76`) `Register` is called synchronously before `StartAll`, so this is not a live race today. However the API contract is misleading — callers have no indication that `Register` is not goroutine-safe.

**Suggested fix:**
Either lock `r.mu` inside `Register` (trivial: one-liner), or add a doc comment to `Register` that explicitly says "must be called before StartAll; not goroutine-safe after StartAll is invoked".

---

### [LOW][DEAD-CODE] `EscapeMarkdownV2` and `PreBlockTable` are exported but never called in production

**Location:** `internal/channels/telegram/mdv2.go:34`, `internal/channels/telegram/tables.go:248`
**Confidence:** high

**Detail:**
Both functions are exported (uppercase) but their only callers are in `*_test.go` files within the same package. `EscapeMarkdownV2` is superseded by the HTML render path (`RenderTelegramHTML` via `gotg_md2html`). `PreBlockTable` is documented as a "zero-dependency fallback" but the renderer never calls it — when `RenderTablePNG` fails, the renderer falls through to `sendText` (plain text), not `PreBlockTable`. The mdv2.go file comment acknowledges the MarkdownV2 mode is "locked in-tree by amendment #4" for a future switch, so these may be intentionally preserved. If that is the case, they should be documented as such rather than appearing to be active code.

**Suggested fix:**
If these are intentionally kept for a future mode switch: add a package-level comment or a `//nolint:deadcode` annotation explaining why. If the MarkdownV2 path is deferred indefinitely: remove and restore when the renderer switches modes.

---

## What was checked and found clean

- **Nil-pointer dereference risk:** Every handler guards `msg.Chat == nil` and similar before accessing fields. `t.sender(c)` has a fallback to `t.bot` when the assertion fails.
- **Resource leaks:** All HTTP response bodies are `defer Close()`'d. The fanout producer closes all subscriber channels via `defer closeAll(subs)`. The `pulseChatAction` goroutine is joined by the returned stop function before the outer goroutine exits. `voiceClient.sleep` uses a `time.NewTimer` + `defer timer.Stop()` correctly.
- **Context propagation:** `daemonCtx` flows from `Start` through all handlers; turn goroutines hold a child context cancelled by `/cancel`; sidecar calls use `withTimeout(ctx)`.
- **Goroutine leaks:** `t.wg` tracks all turn goroutines. `docs.wg` tracks async convert goroutines. `Stop()` drains both via `docs.Stop(ctx)` then `t.wg.Wait()`. The ordering is correct: the async convert goroutine calls its `onAsync` callback (which may call `t.wg.Add(1)`) before `docs.wg.Done()`, so `docs.Stop` cannot return until after any turn goroutine is registered in `t.wg`.
- **Race on `hitlRepliesHandled`:** All access is under `t.mu`.
- **OnReply vs OnText dispatch order:** Telebot v4 dispatches `OnReply` before `OnText` for the same message (`update.go:86,89`), so `markHitlReplyHandled` runs before `takeHitlReplyHandled` — the deduplication is correct.
- **Callback panic risk (`callbackData`):** UUID tokens (36 chars) + `|accept|yes` = 47 bytes < 64 ceiling. All production call sites are within budget. The panic is a correct compile-time guard against non-UUID tokens (tested in `hitl_test.go:430`).
- **`documentsClient.Stop` idempotency:** Protected by `sync.Once`; a second call is a no-op.
- **`Telegram.Start` idempotency:** Double-start guarded by `t.started` flag under `t.mu`.
- **Error wrapping:** All `fmt.Errorf` calls in the critical path use `%w`. Sentinel errors (`ErrTokenConsumed`, `ErrTokenNotFound`, `ErrAccountExists`) are correctly wrapped and unwrapped via `errors.Is`.
- **SQLSTATE classification in Store:** Uses `errors.As + pgErr.Code == "23505"`, not string matching.
- **`splitTelegramText` rune safety:** `capRunes` converts to `[]rune` before slicing. `splitBoundary` does the same. Multi-byte content is not cut mid-character.
- **`downscaleForVision` overflow:** `h * visionMaxEdge / w` — all operands are `int`, result is `int`. For a 10000×10000 image: `10000*1024 = 10_240_000` which fits in `int32` (let alone `int64`). No overflow.
