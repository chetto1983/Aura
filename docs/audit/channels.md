# Audit: internal/channels

**Verdict:** needs-work — three dead-code exports, one misplaced doc comment, one test-only unexported function; no critical bugs or data races found.

**Counts:** critical 0 / high 0 / medium 2 / low 3

## Findings

---

### [MEDIUM][DEAD-CODE] `EscapeMarkdownV2` is exported but never called in production

**Location:** `internal/channels/telegram/mdv2.go:34`

**Confidence:** high

**Detail:** `EscapeMarkdownV2` is an exported function for MarkdownV2 escaping. The production renderer (`renderer.go`) uses `RenderTelegramHTML` (HTML parse-mode) and never calls `EscapeMarkdownV2`. No cross-package caller exists anywhere in the repo (`grep telegram.EscapeMarkdownV2` returns zero results). The only callers are within `mdv2_test.go`. The function is dead code that may mislead maintainers into thinking a MarkdownV2 render path is wired.

**Suggested fix:** Unexport to `escapeMarkdownV2` so it's test-accessible in the same package but clearly not a public API, or remove it entirely if the MarkdownV2 path is definitively abandoned in favour of HTML mode.

---

### [MEDIUM][DEAD-CODE] `PreBlockTable` is exported but never called in production

**Location:** `internal/channels/telegram/tables.go:248`

**Confidence:** high

**Detail:** `PreBlockTable` renders a markdown grid inside a ``` monospace fence as a text fallback. The production `renderer.send` calls `RenderTablePNG` (PNG path) and falls through to `sendText` (HTML path) on PNG failure — it never calls `PreBlockTable`. The only callers are within `tables_test.go`. No cross-package reference exists in the repo. The function is exported dead code.

**Suggested fix:** Unexport to `preBlockTable` or remove. If it is intended as a future fallback, wire it into `sendTable` when `RenderTablePNG` fails and the bot cannot send a photo.

---

### [LOW][DEAD-CODE] `hitl.handleCallback` is an unexported method only called from tests

**Location:** `internal/channels/telegram/hitl.go:106–108`

**Confidence:** high

**Detail:** `handleCallback` is a thin wrapper around `handleCallbackResult(ctx, data, convID, nil)`. The production dispatch path (`bot_dispatch.go:249`) calls `handleCallbackResult` directly with a non-nil `afterSubmit`. Only `hitl_test.go` calls `handleCallback`. Since both functions live in the same package, the wrapper adds no test isolation; tests can call `handleCallbackResult` with `nil` directly.

**Suggested fix:** Delete `handleCallback`; update tests to call `h.handleCallbackResult(ctx, data, convID, nil).resumed` explicitly. Alternatively, keep it as an unexported convenience, but annotate with `// test helper` so it is not confused with a wired path.

---

### [LOW][DEAD-CODE] `commands.dispatch` is an unexported method only called from tests

**Location:** `internal/channels/telegram/commands.go:110–113`

**Confidence:** high

**Detail:** `dispatch` wraps `dispatchRich` and returns only the text field of `commandReply`, discarding the markup. The production text handler (`bot_dispatch.go:115`) calls `dispatchRich` directly. Only `commands_test.go` calls `dispatch`. It is test-only dead code.

**Suggested fix:** Delete `dispatch`; update tests to call `dispatchRich` directly (they can discard the markup field). This removes a wrapper that could mislead a reader into thinking plain-text dispatch is wired in production.

---

### [LOW][BUG] Doc comment for `promptPendingPause` is misplaced above `hitlHandlesReply`

**Location:** `internal/channels/telegram/bot_dispatch.go:435–441`

**Confidence:** high

**Detail:** The comment block at lines 435–441 ("promptPendingPause renders the FIRST unresolved pause for the chat…") describes `promptPendingPause` (defined at line 483), but is placed immediately before `hitlHandlesReply` (defined at line 442). Go tooling (godoc, IDEs) will render this as the doc comment for `hitlHandlesReply`, and `promptPendingPause` will appear undocumented. The misplacement is a copy-paste residue from a refactor.

**Suggested fix:** Move the comment block to line 483 (immediately before `func (t *Telegram) promptPendingPause`), and add an accurate doc comment for `hitlHandlesReply` explaining its deduplication role.

---

## Clean

The following areas were checked and found clean:

- **Goroutine lifecycle**: `pulseChatAction` (bot_typing.go) uses a done-channel + `sync.Once` + `<-finished` join — goleak-clean. Turn goroutines are tracked via `t.wg.Add(1)` / `defer t.wg.Done()`. Async document goroutines tracked via `documentsClient.wg`. All three are drained in `Stop`.
- **Mutex discipline**: `t.mu` guards `t.bot`, `t.started`, `t.cmds`, `t.docs`, `t.tts`, `t.hitlRepliesHandled`. Every field is read/written under the lock at handler registration time. Handler goroutines read these fields after `buildDispatch` completes under `mu`, so no race.
- **`hitlRepliesHandled` deduplication**: Telebot fires `OnReply` before `OnText` for a reply message (verified in `gopkg.in/telebot.v4@v4.0.0-beta.7/update.go:84–88`). `markHitlReplyHandled` is called in `onReply` before `takeHitlReplyHandled` in `onText`, so the dedup key is always present when `onText` checks it. The map is guarded by `t.mu`.
- **`callbackData` panic**: Only reachable at call sites that construct the data: approval (`token|accept|yes` ≈ 45 bytes), decline (`token|decline|` ≈ 45 bytes), cancel (`token|cancel|` ≈ 44 bytes), choice (`token|accept|<idx>` ≤ 46 bytes for 2-digit index). All token values come from UUID v7 (36 bytes fixed). The 64-byte ceiling is not reachable in normal operation; the guard is correct.
- **`documentsClient.Stop` + `stopOnce`**: The poller is stopped before `docs.Stop` is called. No new `OnDocument` messages arrive post-stop, so no new `wg.Add` races the completed `wg.Wait`.
- **Error classification**: SQLSTATE-based via `errors.As`+`pgErr.Code`, never string matching, in `store.go`. Consistent with the project idiom.
- **Resource cleanup**: `downloadFile` and all sidecar HTTP responses defer-close their `ReadCloser`/`Body`. No resource leaks found.
- **`Registry` locking**: `r.mu` guards only `r.started` (written by StartAll, read by StopAll). `r.channels` is written by `Register` and read by `StartAll`/`StopAll` — always sequentially (setup then start), never concurrently. No race in the daemon lifecycle.
- **`speakIfNeeded` empty convID**: `VoiceModePref("")` is a documented stub (Phase 14 deferred). The empty string is harmless until the real pref store lands; the call site will need updating then.
