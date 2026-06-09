# Audit: cmd/aura

Auditor: Claude Code (claude-sonnet-4-6)
Date: 2026-06-10
Scope: every non-test `.go` file in `cmd/aura/` — 14 files, ~3 600 LOC of production CLI code.
Method: read every file, grep across the full repo to confirm cross-package usage, verify all claims with code evidence.

---

## Executive summary

`cmd/aura` is in good shape. No data races or critical bugs were found. Four lower-severity findings were confirmed:

1. **BUG (medium)** — `cmdfakes_test.go` `cmdConvFake.List` silently omits `StatusDeleted` filter present in the production fake, masking a divergence between test and production `List` semantics (test-only fake, no user impact).
2. **BUG (low)** — `mcp.go` `probeWhatsAppBridgeHTTP` closes the HTTP response body without draining it first, preventing connection reuse.
3. **DEAD CODE (low)** — `cachefakes.go` `setupAuditSkills` returns a no-op `func(){}` cleanup; the real cleanup is via `os.RemoveAll(runDir)` in the caller. The returned function is misleading but functionally correct.
4. **NOT-WIRED (low)** — `serve_channels.go` `stopChannelSubsystems` passes the already-cancelled signal context to `reg.StopAll`. The Telegram `Stop` discards the context (`docs.Stop` is `_ context.Context`), so no functional impact today — but the pattern is fragile and any future `ch.Stop` that performs context-sensitive teardown will silently fail.

No goroutine leaks, no actual data races, no orphan dead code of significance.

---

## Findings

### F-01: Response body not drained before close in `probeWhatsAppBridgeHTTP`

**File:** `cmd/aura/mcp.go:386`
**Severity:** low
**Category:** bug
**Confidence:** high

```go
defer func() { _ = resp.Body.Close() }()
```

The body is never read. `http.DefaultClient` uses keepalive connections; closing a body that has not been fully consumed prevents the underlying TCP connection from being returned to the pool. For a one-shot `mcp doctor` CLI command this is negligible — no pool is hot. However, it is technically incorrect and could matter if `probeWhatsAppBridgeHTTP` is called in a loop (e.g., repeated doctor runs in the same process lifetime).

**Suggested fix:**

```go
defer func() {
    _, _ = io.Copy(io.Discard, resp.Body)
    _ = resp.Body.Close()
}()
```

---

### F-02: Cancelled context forwarded to `reg.StopAll` on daemon shutdown

**File:** `cmd/aura/serve.go:114` → `cmd/aura/serve_channels.go:259`
**Severity:** medium
**Category:** bug
**Confidence:** high

```go
// serve.go:114
stopChannelSubsystems(ctx, env.channels, env.setupSrv)  // ctx is already cancelled
```

```go
// serve_channels.go:259
if err := reg.StopAll(ctx); err != nil {  // passes cancelled ctx
```

`reg.StopAll` (internal/channels/registry.go) passes this cancelled context to each `ch.Stop(ctx)`. Currently, `Telegram.Stop` and `documentsClient.Stop` both discard the context (`_ context.Context`), so the cancellation does not break anything today. However:

- This is an implementation contract that is not enforced — a future channel `Stop` that uses the context for cleanup (e.g., DB auto-resolve, flushing buffered messages) will receive an already-cancelled context and all its DB/IO operations will fail immediately.
- The `setupSrv.Shutdown` at line 264 in the same function correctly uses a fresh `context.Background()`-derived timeout context, making the inconsistency visible.

**Suggested fix:** pass a fresh timeout context to `stopChannelSubsystems`, or have `stopChannelSubsystems` itself create the drain context:

```go
// stopChannelSubsystems should create its own drain context:
drainCtx, cancel := context.WithTimeout(context.Background(), channelDrainTimeout)
defer cancel()
if err := reg.StopAll(drainCtx); err != nil { ... }
```

---

### F-03: `setupAuditSkills` returns a no-op cleanup closure

**File:** `cmd/aura/cache_audit.go` (line where `setupAuditSkills` is defined and returned)
**Severity:** low
**Category:** dead-code
**Confidence:** high

`setupAuditSkills` returns `func(){}` as the cleanup function. The actual cleanup of the temp skills directory is performed by `os.RemoveAll(runDir)` in the outer `replayAudit` function — the returned no-op is never actually needed and misleads a reader into thinking the caller is responsible for a resource the callee already cleans up.

This is safe but confusing. If a caller were ever added that does not call `os.RemoveAll(runDir)`, the cleanup would be missed.

**Suggested fix:** Either return `func() { os.RemoveAll(skillsDir) }` from `setupAuditSkills` and remove the `os.RemoveAll(runDir)` call from the outer function (clean separation of concerns), or document clearly that cleanup is always the outer `runDir` removal.

---

### F-04: `cmdConvFake.List` missing `StatusDeleted` filter present in `memConvStore.List`

**File:** `cmd/aura/cmdfakes_test.go:46-57` vs `cmd/aura/cachefakes.go:68-83`
**Severity:** low
**Category:** bug
**Confidence:** high

`memConvStore.List` (production fake used by `cache-audit`) filters `StatusDeleted` conversations:
```go
if c.Status == conversations.StatusDeleted { continue }
```

`cmdConvFake.List` (test fake used by REPL tests) does NOT filter `StatusDeleted`:
```go
// Only filters StatusArchived, not StatusDeleted
if c.Status == conversations.StatusArchived && !includeArchived { continue }
```

If a REPL test exercises `delete` followed by `list`, the deleted conversation would appear in the list, masking the bug from test coverage. The production `conversations.Store.List` (postgres) presumably does filter deleted rows.

**Suggested fix:** Add the deleted filter to `cmdConvFake.List`:
```go
if c.Status == conversations.StatusDeleted { continue }
```

---

## Verified-clean items (investigation log)

These were investigated and confirmed NOT to be issues:

| Concern | Verdict |
|---|---|
| `loadLLMConfigTolerant` `os.Setenv`/`os.Unsetenv` without defer | Safe in single-threaded CLI context; `llm.Load()` does not panic; `Unsetenv` executes unconditionally on the sequential path |
| `dbReset` double-gate logic (`!slices.Contains` \|\| `os.Getenv`) | Correct — De Morgan's law: BOTH conditions must be satisfied (allow = `Contains("--yes")` AND `AURA_RESET_YES=1`) |
| `buildRegistryWithMCP` early-return on `cfg.MCPServersErr != nil` | No pool leak — early-return fires before any MCP client is allocated |
| `chatLoop` `defer d.run.Stop(context.Background(), ...)` | Correct — must use `context.Background()` not the turn context (which is cancelled on exit) |
| `memConvStore.AppendAssistantTurnWithCacheMetric` double `assignTurnSeqLocked` | Idempotent — second call is a no-op if `p.Seq > 0` |
| `mcpProfileRemove` in-place slice filter + `append([]string(nil), next...)` | Correct pattern — read-ahead loop never aliases write position; final copy breaks aliasing |
| `telegramGetMeProbe` / `tele.NewBot` goroutine concern | Confirmed `tele.NewBot` does only a synchronous getMe HTTP call; no goroutines are started |
| `cron.NewScheduler` ephemeral in `buildDispatch` | Just struct initialization; no goroutines until `Start` is called |
| `stopChannelSubsystems` cancelled-ctx → `Telegram.Stop` → `docs.Stop` | `docs.Stop(_ context.Context)` discards the context; `bot.Stop()` is unconditional; no DB operations in `Telegram.Stop` |
| `mcp.go` `wslProbePrefixArgs` `append([]string(nil), args[:i]...)` | Always allocates a fresh backing array; no aliasing |
| `mcp.go` `renderMCPCommand` `append([]string{cfg.Command}, cfg.Args...)` | `[]string{cfg.Command}` is a fresh 1-element slice; always reallocates |
| `serve_channels.go` `buildDispatch` ephemeral `cron.NewScheduler` for `DuringQuietHours` | Both ephemeral and real scheduler default `Now=time.Now`; behavior is identical |
| `Telegram.Stop` unbounded `wg.Wait()` | Bounded by `bot.Stop()` which closes the stop channel causing `bot.Start()` to return, which calls `wg.Done()` |
| `signalTurnCtx` goroutine leak | Confirmed goleak-clean — `signal.NotifyContext` goroutine is cleaned up by the cancel returned and deferred |
| `rebuildMessages` (cachefakes.go) dead code | Used by `messagesLocked` which is called by `LoadHistory`/`LoadManagedHistory` |

---

## Statistics

| Severity | Count |
|---|---|
| Critical | 0 |
| High | 0 |
| Medium | 1 (F-02) |
| Low | 3 (F-01, F-03, F-04) |
