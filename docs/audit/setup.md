# Audit: internal/setup

**Verdict:** needs-work — one dead struct field, one intentional stub flagged for tracking; no bugs or races.
**Counts:** critical 0 / high 0 / medium 1 / low 1

## Findings

### [MEDIUM][NOT-WIRED] `Deps.Bind` is written by every caller but never read by `NewServer`

**Location:** `internal/setup/server.go:63`

**Confidence:** high

**Detail:**
`Deps` declares a `Bind string` field (line 63) and the doc-comment on line 55 says "Bind defaults to the loopback :9081 (config resolves AURA_SETUP_BIND)", implying `NewServer` should pick it up. But `NewServer` (lines 101-122) reads `deps.TokenOut`, `deps.QROut`, `deps.PollInterval`, `deps.Store`, `deps.Probe`, `deps.Token`, and `deps.IdentityID` — it never touches `deps.Bind`. The bind address is not stored in the `Server` struct and is not passed to `HTTPServer` from within `NewServer`. The composition root at `cmd/aura/serve_channels.go:184` correctly passes the bind twice — once as `Deps.Bind` (silently ignored) and once as the `bind` argument to `HTTPServer(chat.cfg.SetupBind)` on line 188. The field is purely phantom: it adds API surface that misleads callers into thinking the bind flows through `Deps`, and any future caller that sets `Deps.Bind` but omits the `HTTPServer(bind)` argument will silently bind to `":0"` (OS-chosen ephemeral port).

**Suggested fix:**
Either (a) remove `Bind` from `Deps` entirely — the composition root already passes it directly to `HTTPServer`, making `Deps.Bind` redundant — or (b) have `NewServer` store it in the `Server` struct and expose a `ListenAndServe()`/`Run()` method that uses it, eliminating the two-argument call pattern.

---

### [LOW][DEAD-CODE] `qrSVG` always returns `""` and discards its parameter

**Location:** `internal/setup/qr.go:10-13`

**Confidence:** high

**Detail:**
`qrSVG(deepLink string) string` receives `deepLink` only to blank-assign it (`_ = deepLink`) and return `""`. The function is called from `handleOnboardLink` (handlers.go:87) and the intent is documented (OQ4 deferral, forward-compat stub). The code is correct but the function signature accepts a parameter it cannot use: any static analyser flags it as a dead parameter, and future maintainers touching `qr.go` have to understand the stub contract from comments rather than the signature. When the SVG generator lands, the correct implementation must also wire the actual SVG library here.

This is intentional per the design notes (OQ4), so it is advisory rather than a hard defect. Flagging so the OQ4 resolution has a concrete file:line target.

**Suggested fix:**
Either (a) keep as-is with the comment (acceptable short-term), or (b) change the call site signature to `qrSVG() string` until the SVG body is implemented — this makes the stub nature explicit in the type system and eliminates the misleading parameter.

---

## What was checked (no finding)

- **Nil dereferences:** `s.probe` nil-guarded before calling (handlers.go:36-39); `s.store` set from `deps.Store` in constructor (no guard, but all callers set it); `s.token` always non-nil (`NewToken` always returns a valid pointer).
- **Mutex correctness:** `Server.mu` (RWMutex) guards `botUsername`/`botConfigured` on every read (`RLock`) and write (`Lock`); `Token.mu` guards `value`/`valid` on every read (`RLock`) and write (`Lock`). No missed path found.
- **SSE goroutine leak:** `handleEvents` selects on `ctx.Done()` as the primary exit; `ticker.Stop()` is deferred; goleak `TestMain` gate is in place (`main_test.go`). Clean.
- **Context propagation:** All store calls (`InsertPending`, `PendingConsumed`, `CountAccounts`) receive `r.Context()` or the passed `ctx`. `pollCompletion` receives the request context correctly.
- **Resource leaks:** `MaxBytesReader` wraps `r.Body` in `handleToken`; no `rows.Close()` calls needed (Store is an interface; the real store is in `internal/channels/telegram`). No file handles opened here.
- **Constant-time comparison:** `Token.Valid` uses `crypto/subtle.ConstantTimeCompare` (token.go:51). Correct.
- **Header/write ordering in SSE:** Headers are set before `w.WriteHeader(http.StatusOK)` in `handleEvents`. `writeJSON` sets Content-Type before the first `Encode` write (which triggers the implicit 200). Correct.
- **`Deps.Bind` in production:** The production call site passes the bind correctly to `HTTPServer` directly, so the nil-bind issue does not cause a runtime failure today — only a misleading API.
- **Wiring:** `setup.NewServer` is called at `cmd/aura/serve_channels.go:181`, the returned `*http.Server` is passed into `startChannelSubsystems` and `stopChannelSubsystems` for full lifecycle management (start + graceful shutdown). `InvalidateToken` is called from `handleEvents` after the SSE terminal event (handlers.go:150).
- **`loopvar` capture:** Go 1.26 module; no loop-variable capture issues possible.
