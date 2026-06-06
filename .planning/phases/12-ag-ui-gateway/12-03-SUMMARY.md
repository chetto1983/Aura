---
phase: 12-ag-ui-gateway
plan: 03
subsystem: api
tags: [ag-ui, sse, http-server, serve-daemon, hitl-resume, error-redaction, goleak, db-integration]

# Dependency graph
requires:
  - phase: 12-01
    provides: "Translate(threadID, runID, idgen, seq) iter.Seq2[events.Event, error] + IDGenerator + the narrow ConversationStore interface + ValidateRunInput + the pinned AG-UI SDK"
  - phase: 12-02
    provides: "the cap-64 drop-on-full SSE pump shape (Plan 02 Fanout) reused by the server's per-connection pump; the agui.Event SDK alias"
  - phase: 12-06
    provides: "the REASONING_* lifecycle in the translator (server streams it transparently — no server-side reasoning handling)"
provides:
  - "internal/agui/server.go: the minimal Slice-8b HTTP gateway — POST /agent/run (SSE) + GET /threads/{id}/messages (JSON MESSAGES_SNAPSHOT) + Mux(), over the EXISTING Runner/ConversationStore/translator/SDK seams"
  - "agui.NewServer(run, conv, cfg) + (*Server).Mux() — wrap Translate per-connection (NOT via Fanout)"
  - "agui.Runner narrow interface (Turn + SubmitAnswers) — *runner.Runner satisfies it implicitly (D-A2-02); unit tests pass scripted fakes"
  - "agui.sanitizeErr + redactEvent — the T-12-10 RUN_ERROR/4xx DSN-redaction seam (server-side belt-and-suspenders over the pure translator)"
  - "AURA_AGUI_* config (AGUIBind 127.0.0.1:9080 / AGUICORSPermissive false / AGUIBufferCap 64) — loopback-only, auth-deferred posture"
  - "cmd/aura/serve.go: the http.Server mounted on the scheduler daemon with a fail-soft ListenAndServe goroutine + graceful Shutdown(10s) on ctx-cancel"
affects: [12-04-serve-wiring-ci, 12-verify, 13-telegram]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Per-connection SSE pump: a sole-sender producer goroutine ranges Translate's stream (drop+WARN on a full cap-N buffer, never blocks the Loop) while the handler drains onto sse.SSEWriter.WriteEventWithType; ctx.Done unwinds both (goleak-clean, Pitfall 4)"
    - "Server-side event redaction: redactEvent sanitizes a *RunErrorEvent.Message in-flight (the pure translator forwards the raw err.Error()) so a DSN/key never reaches the wire without reaching into the boundary-tested translator"
    - "Protocol-native HITL resume: RunAgentInput.Resume[] (resolved→accept / cancelled→cancel, payload→answer content) maps to Runner.SubmitAnswers BEFORE the Turn"
    - "Daemon co-mount: an http.Server shares aura serve's graceful ctx-cancel drain — fail-soft ListenAndServe (non-ErrServerClosed logged, never exits) + bounded Shutdown"

key-files:
  created:
    - "internal/agui/server.go — Server + NewServer + Mux + the two handlers + cap-N SSE pump + resumeAnswers/lastUserMessage/projectMessages + sanitizeErr/redactEvent"
    - "internal/agui/server_test.go — unit tier: SSE round-trip, 404, 400 (malformed/empty/over-cap), Resume mapping, GET snapshot, disconnect-leak, T-12-10 redaction, sanitizeErr"
    - "internal/agui/server_integration_test.go — db_integration tier: SC1 SSE round-trip + SC3 GET snapshot + 404 against a real Postgres conversations.Store"
  modified:
    - "internal/config/config.go — AGUIBind / AGUICORSPermissive / AGUIBufferCap fields + loadBase literal (all three)"
    - "internal/config/config_test.go — TestAGUIConfigDefaultsAndOverrides"
    - "cmd/aura/serve.go — serveEnv.httpSrv field; bootServe builds agui.NewServer; runServe starts/Shutdown the http.Server"

key-decisions:
  - "Server wraps Translate per-connection (NOT via Fanout) — Fanout is the Phase-13 Telegram seam; the SSE handler drives Translate directly over Runner.Turn (RESEARCH diagram line 134)"
  - "T-12-10 redaction needs a server-side redactEvent: the pure translator yields a RUN_ERROR EVENT carrying raw err.Error() (it never yields an error to the pump), so sanitizing only the pump's error slot would miss it — redactEvent sanitizes *RunErrorEvent.Message in-flight"
  - "Loopback bind hardcoded via the config default (no --bind flag this phase) — the auth-deferred compensating control (T-12-08, Pitfall 6, amendment #35)"
  - "db_integration tier uses a real conversations.Store + a scripted fake Runner — the integration value is the real Get/LoadHistory round-trip (SC1/SC3 + the 404 chokepoint), not the LLM stack"

patterns-established:
  - "Pattern: cap-N per-connection SSE pump (sole-sender producer, drop+WARN, ctx.Done unwind) feeding sse.SSEWriter — the HTTP analog of Plan 02's Fanout, goleak-asserted via disconnect test"
  - "Pattern: in-flight RUN_ERROR redaction at the transport boundary (redactEvent) — belt-and-suspenders over D-28 agent-path key redaction"

requirements-completed: [UX-01]

# Metrics
duration: 1h 5m
completed: 2026-06-07
---

# Phase 12 Plan 03: AG-UI HTTP Server + serve Daemon Mount Summary

**Minimal Slice-8b gateway: `POST /agent/run` streams a translated agent turn as SSE (cap-N leak-clean pump, 404/400 guards, protocol-native Resume→SubmitAnswers, in-flight DSN-redacted RUN_ERROR) and `GET /threads/<id>/messages` returns the persisted history as a MESSAGES_SNAPSHOT JSON body, mounted on `aura serve` alongside the scheduler with graceful Shutdown and AURA_AGUI_* loopback config.**

## Performance

- **Duration:** ~1h 5m
- **Started:** 2026-06-06T23:55:00Z (approx)
- **Completed:** 2026-06-07T01:00:00Z (approx)
- **Tasks:** 3 (Task 1b is TDD)
- **Files modified:** 6 (3 created, 3 modified)

## Accomplishments
- `server.go` (318 LOC): the thinnest possible HTTP glue over EXISTING seams — `Mux()` registers `POST /agent/run` + `GET /threads/{id}/messages` via Go 1.22 method-pattern routing (no chi/gorilla). POST: `http.MaxBytesReader` (1 MiB) → SDK `UnmarshalJSON` → `ValidateRunInput` (400), `conv.Get` 404, `Resume[]→SubmitAnswers` before the Turn, drives `Runner.Turn` over the last user message, streams `Translate` output. GET: projects `[]llm.Message → []events.Message` into a one-shot `MESSAGES_SNAPSHOT` JSON body (not SSE, OQ2).
- The cap-N per-connection SSE pump: a sole-sender producer ranges the translated stream (drop+WARN on a full buffer, never blocking `Runner.Turn`, T-12-09) while the handler drains onto `sse.SSEWriter.WriteEventWithType`; `ctx.Done` unwinds both goroutines on client disconnect (goleak-clean, Pitfall 4) — proven by a `NumGoroutine`-baseline disconnect test.
- T-12-10 redaction proven by test: a fake Runner that errors with the synthetic DSN `postgresql://user:secret@host/db` yields a RUN_ERROR frame with NO `secret` on the wire — caught a gap where the pure translator forwards the raw `err.Error()` as a RUN_ERROR *event* (bypassing `sanitizeErr`), fixed with the in-flight `redactEvent` seam.
- `server_test.go` (unit) + `server_integration_test.go` (db_integration, live-verified against Postgres, `-p 1`, no-skip-as-green): SC1 SSE round-trip + SC3 GET snapshot + 404/400 guards in BOTH tiers, plus the disconnect-leak and the redaction test.
- `AURA_AGUI_*` config (all three fields incl. `AGUIBufferCap` — NOT dropped despite the PATTERNS.md excerpt omitting it) with loopback defaults; `cmd/aura/serve.go` mounts the `http.Server` on the scheduler daemon (fail-soft `ListenAndServe` goroutine + `ReadHeaderTimeout` slow-loris bound + graceful `Shutdown(10s)` on ctx-cancel), no `--bind` flag (auth-deferred posture).

## Task Commits

Each task was committed atomically (Task 1b is TDD — the impl landed in Task 1a, the test in Task 1b with a GREEN-phase redaction fix to server.go):

1. **Task 1a: server.go — POST /agent/run SSE + GET messages snapshot + cap-N pump** — `d339452b` (feat)
2. **Task 1b: server_test.go + server_integration_test.go (SC1/SC3 unit + db_integration) + redactEvent GREEN fix** — `3959bc37` (test)
3. **Task 2: AURA_AGUI_* config + serve.go daemon mount with graceful Shutdown** — `28c11685` (feat)

## Files Created/Modified
- `internal/agui/server.go` — `Server` over the narrow `Runner` + `ConversationStore` + `IDGenerator` + `ServerConfig`; `handleRun`/`handleMessages`; `streamSSE`/`pumpSend`/`bufferCap` (cap-N SSE pump); `resumeAnswers`/`payloadString`/`lastUserMessage`/`projectMessages`; `sanitizeErr`/`sanitizeString`/`redactEvent` (T-12-10).
- `internal/agui/server_test.go` — unit-tier fakes (`scriptedRunner`, `fakeConvStore`) + 8 tests (SSE round-trip, 404, 400×3, Resume mapping, GET snapshot, redaction, disconnect-leak, sanitizeErr).
- `internal/agui/server_integration_test.go` — `db_integration` tier (real `conversations.Store`): SC1 round-trip, SC3 snapshot, 404.
- `internal/config/config.go` — `AGUIBind`/`AGUICORSPermissive`/`AGUIBufferCap` struct fields + `loadBase` literal.
- `internal/config/config_test.go` — `TestAGUIConfigDefaultsAndOverrides`.
- `cmd/aura/serve.go` — `serveEnv.httpSrv`; `bootServe` builds `agui.NewServer`; `runServe` starts the fail-soft goroutine + `Shutdown` after `scheduler.Start` returns.

## Decisions Made
- **Server drives Translate directly, NOT via Fanout** — Fanout is the Phase-13 Telegram in-process seam; the SSE handler reuses Plan 02's cap-N drop-on-full *pump shape* (a per-connection channel), not the `Fanout` struct itself (RESEARCH diagram line 134).
- **`redactEvent` is required server-side** — the pure translator (Plan 01, CI-boundary-tested) yields a runner error as a RUN_ERROR *event* with raw `err.Error()`; the pump never sees an error in its `err` slot, so the redaction must sanitize the `*RunErrorEvent.Message` in-flight before the wire (the belt-and-suspenders over D-28's agent-path key redaction).
- **Loopback bind via config default, no `--bind` flag** — the compensating control for the auth-deferred posture (T-12-08, Pitfall 6, amendment #35); a non-loopback bind cannot ship without auth (deferred, documented in the config block comment).
- **`events.Message.Role` needs an explicit `types.Role(m.Role)` conversion** — the SDK `Role` is a distinct string type (the RESEARCH/plan snippet's `Role: m.Role` would not compile); used the conversion.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] RUN_ERROR DSN leak: the pure translator bypasses sanitizeErr**
- **Found during:** Task 1b (the T-12-10 redaction test)
- **Issue:** Task 1a's `sanitizeErr` only routed the pump's *error slot* (the `err != nil` branch of the translated stream). But the pure translator catches a `Runner.Turn` error first and yields it as a RUN_ERROR *event* carrying the raw `err.Error()` — so the synthetic DSN `postgresql://user:secret@host/db` reached the wire unredacted (test FAILed). This is exactly the T-12-10 surface the plan flags.
- **Fix:** Added `redactEvent(ev)` in `streamSSE` just before `WriteEventWithType`: it type-asserts `*events.RunErrorEvent` and sanitizes its `Message` field in-flight (factored `sanitizeString` out of `sanitizeErr`). The pure translator is untouched (it stays boundary-tested and owned by Plan 01).
- **Files modified:** internal/agui/server.go (committed with Task 1b's test as the TDD GREEN refinement).
- **Verification:** `TestServer_RunErrorRedaction` asserts no `secret` on the wire; `TestSanitizeErr` covers the helper across DSN schemes + nil.
- **Committed in:** `3959bc37`.

**2. [Rule 3 - Blocking] `events.Message.Role` requires an explicit types.Role conversion**
- **Found during:** Task 1a (projectMessages build)
- **Issue:** The plan/RESEARCH snippet uses `events.Message{... Role: m.Role ...}`, but `m.Role` is a plain `string` (llm.Message) while `events.Message.Role` is the SDK's distinct `types.Role` type — a direct assignment does not compile.
- **Fix:** `Role: types.Role(m.Role)` in `projectMessages`.
- **Files modified:** internal/agui/server.go.
- **Verification:** `go build ./...` clean; the GET snapshot test asserts the projected roles round-trip.
- **Committed in:** `d339452b`.

---

**Total deviations:** 2 auto-fixed (1 bug, 1 blocking). **Impact on plan:** both necessary for correctness/security (the redaction is the explicit T-12-10 mitigation, proven by test rather than prose; the Role conversion is a compile blocker). No scope creep — deliverables match the plan's artifacts exactly.

## Issues Encountered
- `strings.Builder` has no `ReadFrom`; switched the test body reads to `io.ReadAll`/`io.Copy` (a within-test mechanical correction, no behavior change).
- `fixedIDGen` already existed in `translator_test.go` (same package); reused it rather than redeclaring (the duplicate was removed before the first test run).
- The SDK SSE writer logs at ERROR when writing to a cancelled connection during the disconnect test — benign (the pump correctly exits; the goroutine-baseline assertion is the ground truth, not the log line).

## Threat Model Compliance
- **T-12-08 (EoP / unauthenticated endpoint):** mitigated — `AGUIBind` hardcoded `127.0.0.1:9080` via the config default; no `--bind` flag; the loopback bind IS the compensating control (documented in the config block comment).
- **T-12-09 (DoS / slow-loris):** mitigated — cap-N buffered SSE pump + drop-with-WARN + `http.Server.ReadHeaderTimeout` on the daemon; the producer never blocks the Loop.
- **T-12-10 (Info disclosure / RUN_ERROR):** mitigated — `redactEvent`/`sanitizeErr` strip DSN/credential substrings before the wire; proven by `TestServer_RunErrorRedaction` (synthetic DSN → no `secret` on the wire).
- **T-12-11 (Info disclosure / cross-thread read):** mitigated — `conv.Get` is the chokepoint; unknown thread → 404 (db_integration tier proves it against the real store).
- **T-12-12 (Tampering/DoS / malformed/oversized body):** mitigated — `http.MaxBytesReader` (1 MiB) → 400; SDK `UnmarshalJSON` + `ValidateRunInput` reject bad shapes/empty fields → 400 (unit tier covers all three).
- **T-12-13 (CSRF / permissive CORS):** mitigated — `Access-Control-Allow-Origin: *` only when `AGUICORSPermissive` (default false); loopback-only bind reduces blast radius.

No new threat surface introduced beyond the register.

## Known Stubs
None — every handler is wired to the real seams (Translate, conv.Get/LoadHistory, Runner.Turn/SubmitAnswers). The serve mount uses the already-composed `chat.run`/`chat.conv`.

## Verification Evidence
- `go vet ./...` clean; `go build ./...` ok.
- `go test -race ./internal/agui/ ./internal/config/` green (unit tier).
- `go test -tags db_integration -race -p 1 ./internal/agui/` green live against Postgres (SC1/SC3 + 404) — per-test runtimes 0.08–0.18s confirm real DB round-trips, not skip-as-green; `t.Fatal`s under `CI=true` when the DSN is unset.
- `golangci-lint run ./internal/agui/ ./cmd/aura/ ./internal/config/` 0 issues (both default and `--build-tags db_integration`).
- `bash scripts/agui_boundary_check.sh` exit 0 (agent closure free of agui).
- `bash scripts/check-file-size.sh` pass (server.go 318 / serve.go 253 / config.go 432, all < 600).
- Task acceptance greps all pass: `http.MaxBytesReader`=1, `ErrConversationNotFound`=2, CORS gated, `sanitizeErr`≥1, pump has `ctx.Done()`+`default:` arms, `AURA_AGUI_BIND`/`AURA_AGUI_BUFFER_CAP` present (default `127.0.0.1:9080`), `agui.NewServer`=1, `Shutdown`≥1, `--bind`=0.

## User Setup Required
None - no external service configuration required. (CI wiring of the agui `db_integration` tier + the SC2/SC4 gates lands in Plan 04 per the plan; no CI YAML was added here.)

## Next Phase Readiness
- The HTTP gateway is operator-observable: `aura serve` runs the SSE + JSON endpoints on `127.0.0.1:9080` alongside the scheduler with graceful drain (SC1 infra). The Plan-04 wave can wire the agui `db_integration` tier into CI and add the boundary/pin gates' agui coverage.
- The `agui.Runner` narrow interface + the server's per-connection pump are independent of the Phase-13 Telegram Fanout seam (no overlap).
- No blockers.

## Self-Check: PASSED
- `internal/agui/server.go` — FOUND
- `internal/agui/server_test.go` — FOUND
- `internal/agui/server_integration_test.go` — FOUND
- `internal/config/config.go` (AGUI fields) — FOUND
- `cmd/aura/serve.go` (httpSrv mount) — FOUND
- Commit `d339452b` (Task 1a) — FOUND
- Commit `3959bc37` (Task 1b) — FOUND
- Commit `28c11685` (Task 2) — FOUND
- All `<verification>` commands re-run green; all task `<acceptance_criteria>` re-verified.

## TDD Gate Compliance
Task 1b is TDD. The implementation (server.go) landed in Task 1a's `feat` commit (`d339452b`); the test landed in Task 1b's `test` commit (`3959bc37`), which also carried the GREEN-phase `redactEvent` refinement the redaction test forced. RED was demonstrated by the redaction test FAILing against the Task-1a server (the DSN leaked) before `redactEvent` was added — the project pre-commit `vet` hook requires a compiling package, so the RED test could not land standalone (same constraint Plan 02 documented); no test was weakened to pass.

---
*Phase: 12-ag-ui-gateway*
*Completed: 2026-06-07*
