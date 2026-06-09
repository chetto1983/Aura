# Audit: internal/channels

**Verdict:** needs-work — two not-wired symbols (a dead struct field and a dangling comment), one dead method reachable only from tests, and three Store methods with no production callers. No critical bugs or races.

**Counts:** critical 0 / high 1 / medium 2 / low 3

---

## Findings

### [HIGH][NOT-WIRED] `Deps.ResumeTurn` field is populated but never consumed

**Location:** `internal/channels/telegram/bot.go:89`, `cmd/aura/serve_channels.go:70`
**Confidence:** high

`Deps.ResumeTurn resumeFunc` is declared in the `Deps` struct and wired at the composition root (`serve_channels.go:70`), but no code in the `telegram` package ever reads `t.deps.ResumeTurn`. The actual HITL resume rendering goes through the closure built in `hitlFor` (`bot_dispatch.go:451–461`), which constructs a local `resume` func that calls `t.startTurn`. `Deps.ResumeTurn` is a dead field — setting it at the composition root has zero effect.

As a corollary, `resumeTurnFunc` in `serve_channels.go` (lines 144–153) is dead code too: it is assigned to `Deps.ResumeTurn` which is never read.

**Suggested fix:** Remove the `ResumeTurn resumeFunc` field from `Deps`, remove the `resumeTurnFunc` helper from `serve_channels.go`, and delete the corresponding comment block in `bot.go:83–89`. The real resume path is already correct via `hitlFor`.

---

### [MEDIUM][DEAD-CODE] `(*hitl).handleCallback` is never called in production

**Location:** `internal/channels/telegram/hitl.go:106–108`
**Confidence:** high

```go
func (h *hitl) handleCallback(ctx context.Context, data, convID string) (resumed bool) {
    return h.handleCallbackResult(ctx, data, convID, nil).resumed
}
```

Production dispatch (`bot_dispatch.go:247`) calls `handleCallbackResult` directly. `handleCallback` is referenced only in `hitl_test.go` (7 call sites). It is a thin wrapper with no additional logic.

**Suggested fix:** Remove `handleCallback` from `hitl.go`. The 7 test call sites should call `handleCallbackResult(..., nil).resumed` directly, or the test helper can live in the test file.

---

### [MEDIUM][DEAD-CODE] Dangling comment stub `capRunesTail` — function body missing

**Location:** `internal/channels/telegram/status_pane.go:355–356`
**Confidence:** high

```
// capRunesTail truncates s to at most n runes, preserving the newest content.
```

The file ends after this comment with no function body following it. The function is never referenced anywhere in the repo (`grep` across D:/Aura returns no non-comment hits). This is either a leftover from a refactor that deleted the body, or a planned function that was never written.

**Suggested fix:** Delete the orphan comment.

---

### [LOW][NOT-WIRED] `Store.GetAccountByTelegramID`, `TouchLastSeen`, and `ListAccounts` have no production callers

**Location:** `internal/channels/telegram/store.go:169`, `:181`, `:198`
**Confidence:** high

All three exported Store methods are defined, documented, and exercised in `store_integration_test.go`, but have zero non-test, non-definition references anywhere in the repo. They are forward-provision for a future auth-gate or admin surface that has not yet been wired (the /setup admin panel, `/whoami`, per-account touch, etc.).

This is not a bug — the methods are correct — but they inflate the interface surface and the integration-test coverage baseline without providing any runtime value today.

**Suggested fix:** Accept as forward provision and document the intended consumer in each function's godoc, or defer to the phase that actually wires them.

---

### [LOW][NOT-WIRED] `PreBlockTable` is defined but never called in production

**Location:** `internal/channels/telegram/tables.go:248`
**Confidence:** high

`PreBlockTable` is an exported function that renders a grid to a monospace ``` block fallback. The renderer (`renderer.go`) never calls it — it falls through to `sendText` when `RenderTablePNG` fails. Only `tables_test.go` references `PreBlockTable`. The comment says "Used when PNG rendering is unavailable or the channel prefers text", but neither condition is wired in `renderer.go`.

**Suggested fix:** Either wire it as the fallback in `renderer.sendTable` when `RenderTablePNG` fails (replacing the current `sendText` fallback), or drop it and document the "plain text" fallback as intentional.

---

### [LOW][NOT-WIRED] `MultimodalConfig.TTSCaption` is never set by the composition root

**Location:** `internal/channels/telegram/sidecar.go:62`, `cmd/aura/serve_channels.go:93–111`
**Confidence:** high

`MultimodalConfig.TTSCaption` is read in `tts.go:79` (`asciiCaption(t.cfg.TTSCaption)`) and exercised in `tts_test.go`, but `multimodalConfig()` in `serve_channels.go` never populates it. TTS voice notes will always have an empty caption in production. This is a low-severity UX gap — an empty caption is valid — but the field's existence suggests it was meant to be configurable.

**Suggested fix:** Either add `TTSCaption: cfg.TTSCaption` to `multimodalConfig` (once `config.Config` adds the field + env var `AURA_TTS_CAPTION`), or drop the field and hardcode the caption as empty.

---

## What was checked and found clean

- **Nil-pointer derefs:** All handler entrypoints guard `msg == nil || msg.Chat == nil` before accessing fields. `sender()` falls back to `t.bot`. `cmds` is nil-checked in `onStatusCancelCallback`. `hitlHandlesText` checks `t.deps.Resume == nil` before calling `PendingFor`.
- **Resource leaks:** All HTTP response bodies are `defer`'d closed. `pulseChatAction` goroutines are joined by `stop()`. `documentsClient.wg` is drained by `Stop`. The `stopWaitPoller` goroutine drains on `bot.Stop()`. No ticker leaks found.
- **Context propagation:** Request contexts (`reqCtx`) are derived with `withTimeout`; all callers `defer cancel()`. `context.WithoutCancel` in async convert is intentional and documented.
- **Error wrapping:** All errors use `%w` where the sentinel is inspectable; SQLSTATE classification uses `errors.As + pgErr.Code`, never string matching.
- **Concurrency:** `commands` map accesses (`cancels`, `searchPages`) are protected by `c.mu`. `Telegram.started` map is protected by `t.mu`. `documentsClient.wg` does not need a mutex (sync.WaitGroup is goroutine-safe). No data races found.
- **Callback_data overflow:** `callbackData` panics on >64 bytes. For all current call sites (UUID 36b + action 7b + index/value ≤3b + 2 separators = ≤48b), the ceiling is never reached.
- **MarkdownV2 escaping:** Unterminated fence deterministically closed. Fallback to plain text on 400 via `isCantParseEntities`.
- **Registry:** `Register` and `SetEnabledOverride` are called sequentially before `StartAll` (single-goroutine composition root); `channels` map is not protected by mutex but is only written during setup, never concurrently with `StartAll`.
