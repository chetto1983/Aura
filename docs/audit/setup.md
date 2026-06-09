# Audit: internal/setup

**Verdict:** needs-work — one not-wired field misleads callers; one variable-shadows-function pattern is a maintenance footgun; SSE error-swallowing is intentional but undiscoverable.

**Counts:** critical 0 / high 0 / medium 2 / low 1

## Findings

---

### [MEDIUM][NOT-WIRED] `Deps.Bind` is documented but never consumed by `NewServer`

**Location:** `internal/setup/server.go:63` (field declaration), `internal/setup/server.go:101-122` (NewServer body)

**Confidence:** high

**Detail:**
`Deps.Bind` is declared with the comment "Bind defaults to the loopback :9081 (config resolves AURA_SETUP_BIND)" — implying it controls the bind address. However, `NewServer` reads `deps.TokenOut`, `deps.QROut`, `deps.PollInterval`, `deps.Token`, `deps.Store`, `deps.Probe`, and `deps.IdentityID`, but **never reads `deps.Bind`**. The `Server` struct has no `bind` field. The bind address only reaches `HTTPServer` when the caller passes it directly:

```go
// cmd/aura/serve_channels.go:164-171
srv := setup.NewServer(setup.Deps{
    ...
    Bind: chat.cfg.SetupBind,   // written here
    ...
})
return srv.HTTPServer(chat.cfg.SetupBind)  // but passed directly here, not through Deps.Bind
```

`Deps.Bind` is populated in the only production call site but has no effect inside `NewServer`. A future caller that sets `Deps.Bind` and expects it to route through `HTTPServer` automatically will silently bind the wrong address.

**Suggested fix:** Either (a) remove `Deps.Bind` from the struct and update the comment, accepting that callers pass bind to `HTTPServer` directly; or (b) store it in the `Server` struct and expose a no-arg `ListenAndServe()` / `HTTPServer()` that uses it, removing the redundant parameter from the call site.

---

### [MEDIUM][BUG] `handleOnboardLink` variable `deepLink` shadows the package-level function `deepLink`

**Location:** `internal/setup/handlers.go:80`

**Confidence:** high

**Detail:**
```go
deepLink := deepLink(bot, onboardingToken)
```
This is valid Go (RHS is evaluated before the LHS variable is declared), and the code produces the correct result. However, after line 80, the name `deepLink` in scope resolves to the `string` variable, not the package-level function. Any future code added between lines 80 and the end of the handler that calls `deepLink(...)` will fail to compile with a cryptic "cannot call non-function deepLink (variable of type string)" error, and the developer will need to understand the shadowing to diagnose it.

The same pattern appears when `deepLink` is passed to `qrSVG(deepLink)` on line 87 — this is the `string` variable, which is correct, but it reads ambiguously.

**Suggested fix:** Rename the local variable to avoid the shadow:
```go
link := deepLink(bot, onboardingToken)
qrterminal.Generate(link, qrterminal.L, s.qrOut)
writeJSON(w, OnboardLinkResponse{DeepLink: link, QRSVG: qrSVG(link)})
```

---

### [LOW][BUG] `pollCompletion` silently swallows `ErrTokenNotFound`, masking invalid `?onboarding=` tokens indefinitely

**Location:** `internal/setup/handlers.go:162-175`

**Confidence:** medium

**Detail:**
`pollCompletion` treats ALL store errors as "Unknown/transient — keep polling". In the real `telegram.Store`, `PendingConsumed` returns `ErrTokenNotFound` (not a transient error) when the token row does not exist. If a client opens `GET /setup/events?onboarding=garbage-token`, the SSE stream will poll at 2-second intervals forever (until the client disconnects), making no progress, with no log warning to the operator. The comment acknowledges the "unknown" case but does not distinguish it from transient errors.

Operationally this is low severity: the token gate already rejects unauthorized clients, so a garbage `?onboarding=` value requires a valid setup token to reach. The stream terminates on client disconnect. The issue surfaces as a silent resource hold on misconfigured wizard clients, not a security or data-integrity defect.

**Suggested fix:** Detect `ErrTokenNotFound` specifically and either (a) log a one-time warning and continue polling (the token may not be inserted yet if the client races), or (b) terminate the stream with a structured error SSE frame after N consecutive `ErrTokenNotFound` responses:
```go
if errors.Is(err, telegram.ErrTokenNotFound) {
    slog.Warn("setup: SSE poll for unknown onboarding token", "token_prefix", onboardingToken[:8])
    // optionally break after N misses
}
```
Note: `pollCompletion` would need to know about `ErrTokenNotFound` — this either requires importing the telegram package (breaks the no-telegram-import invariant) or threading a sentinel through the `Store` seam.

---

## What was checked

- All five non-test files: `types.go`, `token.go`, `server.go`, `handlers.go`, `qr.go`.
- Test files read to understand intended contracts: `main_test.go`, `server_test.go`, `handlers_test.go`, `events_test.go`.
- `go vet ./internal/setup/` — clean.
- `go test -race -count=1 ./internal/setup/` — clean (1.4 s, goleak verified).
- Grep across the full repo (`D:/Aura`) for all exported symbols, wiring points, and cross-package references.
- Mutex usage: `s.mu` (RWMutex) guards `botUsername`/`botConfigured` — Lock on write in `handleToken`, RLock on read in `handleOnboardLink` and `handleStatus`. Correct.
- `Token` concurrency: `t.mu` (RWMutex) guards `value`/`valid` — Lock in `Invalidate`, RLock in `Valid`. Correct.
- SSE pump lifecycle: `ticker.Stop()` via `defer`, goroutine exits on `ctx.Done()`. Goleak-clean.
- HTTP header ordering: `writeJSON` sets Content-Type before the implicit `WriteHeader`; `handleEvents` sets headers before `WriteHeader(200)`. Correct.
- Token constant-time compare: `subtle.ConstantTimeCompare` used for the gate. Correct.
- Bot token leak: error from probe is NOT surfaced verbatim in the response or logs. Correct.
- `Deps.Bind` not-wired confirmed by grepping all call sites (`cmd/aura/serve_channels.go:164-171`).
- `qrSVG` is called from `handlers.go:87` — not dead code, intentionally deferred stub.
- `onboardingCompletedEvent` constant used in `handlers.go` and `types.go` — not dead code.
