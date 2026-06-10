# Audit: internal/setup

**Verdict:** needs-work — two confirmed functional bugs, one dead struct field, one code smell.
**Counts:** critical 0 / high 1 / medium 2 / low 1

## Findings

### [HIGH][BUG] SSE pump silently loops forever on unknown/invalid onboarding token
**Location:** `internal/setup/handlers.go:162-175` (`pollCompletion`)
**Confidence:** high

`pollCompletion` maps every non-nil error from `store.PendingConsumed` to `(false, _)` and continues polling. `PendingConsumed` returns `ErrTokenNotFound` (wrapped) when the token row does not exist — e.g. when the client supplies an expired, misspelled, or already-consumed token in `?onboarding=`. The SSE pump then silently polls the DB at the configured interval forever (until client disconnect), giving the client no signal that the token is invalid. A client waiting for onboarding confirmation will hang until it times out or disconnects, with no error event and no log line emitted.

**Suggested fix:** Distinguish transient DB errors from `ErrTokenNotFound` in `pollCompletion`. On `errors.Is(err, telegram.ErrTokenNotFound)` (or its wrapped form), emit a terminal SSE error event (e.g. `{"type":"error","message":"token_not_found"}`) and return `(true, errEvent)` to break the loop. Transient errors (network, pgx pool timeout) should keep the existing silent-retry behaviour.

---

### [MEDIUM][NOT-WIRED] `Deps.Bind` field is populated by the composition root but never read inside the package
**Location:** `internal/setup/server.go:63` (`Deps.Bind`); caller: `cmd/aura/serve_channels.go:167`
**Confidence:** high

`Deps` declares `Bind string` (line 63). `NewServer` reads every other `Deps` field but never reads `deps.Bind` — the `Server` struct has no corresponding `bind` field. The caller (`buildSetupServer`, `serve_channels.go:164-172`) sets `Bind: chat.cfg.SetupBind` and then separately calls `srv.HTTPServer(chat.cfg.SetupBind)`, passing the bind address directly. The field in `Deps` is thus purely decorative: populated by one caller, consumed by nobody. The contract implied by the field name and doc comment ("`Deps.Bind` defaults to the loopback :9081 (config resolves AURA_SETUP_BIND)") is unimplemented.

**Suggested fix:** Either (a) remove `Bind` from `Deps` and keep the current explicit parameter on `HTTPServer`, or (b) store `deps.Bind` on `Server` and expose it via `HTTPServer()` (no parameter), removing the redundant parameter from the call site.

---

### [MEDIUM][BUG] Persistent DB errors in `pollCompletion` are fully silenced — no log, no rate limiting
**Location:** `internal/setup/handlers.go:167-169` (`pollCompletion`)
**Confidence:** high

When `store.PendingConsumed` returns any error that is not `ErrTokenNotFound` (e.g. pgxpool exhausted, postgres down, network partition), `pollCompletion` silently returns `(false, _)` with no log line. The poll loop then hammers the DB at `pollInterval` intervals (2s default, 5ms in tests) until the client disconnects. A sustained DB error under production load will produce a flood of failed queries per connected SSE client with no visibility in the logs. The existing `slog.Error/Warn` calls on the other three handlers (`handleToken`, `handleOnboardLink`, `handleStatus`) all log DB errors — the SSE path is the sole exception.

**Suggested fix:** Add a `slog.Warn("setup: poll pending consumed", "err", err)` inside the error branch in `pollCompletion`. If rate-limiting the log is desirable (to avoid per-tick spam), deduplicate with a last-logged timestamp or use a counter (`if s.pollErrCount % 10 == 0`). This is a separate concern from the `ErrTokenNotFound` fix above.

---

### [LOW][DEAD-CODE] `qrSVG` function is a forward-compat stub with no real implementation and no path to extension
**Location:** `internal/setup/qr.go:10-13`
**Confidence:** medium

`qrSVG` is a single-line stub that ignores its argument and returns `""`. It is called from `handleOnboardLink` (handlers.go:87) so it is not technically dead. However, it occupies a dedicated file (`qr.go`) with a 9-line comment rationale (OQ4 deferral), and the parameter `deepLink string` is explicitly discarded (`_ = deepLink`). The function provides no current value and cannot be tested for correctness. If left unimplemented when the frontend lands, it will silently return an empty SVG body with no compile-time signal. This is not a runtime bug today, but the deferred contract is invisible to the type system.

**Suggested fix:** Consider replacing `qrSVG` with a build-tag-gated stub or an interface/hook that forces a conscious call-site decision when the frontend phase begins. Alternatively, a `TODO(OQ4)` comment inside the function body is sufficient to flag it for the next milestone, and the dedicated file can be collapsed into `handlers.go` to reduce surface area.
