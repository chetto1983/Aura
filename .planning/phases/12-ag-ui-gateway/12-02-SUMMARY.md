---
phase: 12-ag-ui-gateway
plan: 02
subsystem: api
tags: [ag-ui, fanout, pub-sub, backpressure, goleak, iter-seq2, sse-adjacent, telegram-seam]

# Dependency graph
requires:
  - phase: 12-01
    provides: "Translate(threadID, runID, idgen, seq) iter.Seq2[events.Event, error] + IDGenerator + the pinned AG-UI community SDK"
  - phase: 12-05
    provides: "agent.LLMResponse.Reasoning data-plane field (forward-compat REASONING_* surface noted, not parsed this plan)"
provides:
  - "agui.Fanout: wraps a translated iter.Seq2[events.Event,error] and distributes to N cap-64 buffered, drop-on-full subscriber channels without blocking the producer"
  - "agui.fanoutBuffer const (= 64) — the per-subscriber channel cap"
  - "agui.Subscribe(ctx, threadID, runID, idgen, turn) <-chan Event — transport-free in-process subscriber composing Translate + Fanout (the Phase-13 Telegram consumer seam)"
  - "agui.Event / agui.EventType SDK type aliases — keep github.com/ag-ui-protocol out of consumer call sites"
affects: [13-telegram, phase-12-server, phase-12-verify]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Fan-out pub-sub over iter.Seq2: single producer goroutine, sole-sender-closes, three-arm select (deliver / ctx.Done abort / default-drop+WARN)"
    - "SDK type-alias shield: re-export Event/EventType under the consuming package so the external module name never appears at call sites"
    - "Drop-on-full backpressure (cap-64 default arm) decouples a slow/dead subscriber from the agent Loop (RESEARCH Pattern 4)"

key-files:
  created:
    - internal/agui/fanout.go
    - internal/agui/client.go
  modified:
    - internal/agui/fanout_test.go

key-decisions:
  - "Fanout.Run starts the single producer goroutine and returns immediately; the goroutine is the SOLE sender on every subscriber channel and defer-closes them all on ctx-cancel OR source end (no receiver-side close, no double-close)."
  - "Subscribe must be called before Run; subscribers registered after the producer starts are not retroactively fed (documented on the API). The single-consumer convenience helper (client.Subscribe) calls Subscribe()+Run() in order so this is invisible to callers."
  - "client.Subscribe is single-consumer; N-consumer fan-out is done by building a *Fanout directly and Subscribe()-per-consumer. The helper does not wrap a multi-subscriber API to keep the Phase-13 seam minimal."
  - "REASONING_* dual-field handling is a forward-compat comment only — no reasoning parsing added this plan (per plan scope + RESEARCH note)."

patterns-established:
  - "Three-arm fan-out send: case sub<-ev / case <-ctx.Done():return / default: slog.Warn drop — every send has both a cancel arm and a drop arm (Pitfall 4 + Pattern 4)."
  - "Alias type-pin in tests: a helper with a <-chan Event parameter proves Subscribe returns the alias (not events.Event) at compile time without a lint-flagged redundant var declaration."

requirements-completed: [UX-01]

# Metrics
duration: 24min
completed: 2026-06-06
---

# Phase 12 Plan 02: AG-UI Fanout + In-Process Subscriber Summary

**Cap-64 drop-on-full `Fanout` that distributes a translated AG-UI `iter.Seq2[events.Event,error]` to N never-blocking subscriber channels, plus a transport-free `Subscribe` helper (Translate+Fanout) with `agui.Event` SDK aliases — the goleak-clean Phase-13 Telegram consumer seam.**

## Performance

- **Duration:** ~24 min
- **Started:** 2026-06-06T23:30:00Z (approx)
- **Completed:** 2026-06-06T23:54:00Z (approx)
- **Tasks:** 2 (both TDD)
- **Files modified:** 3 (2 created, 1 extended)

## Accomplishments
- `fanout.go` — `Fanout` struct wrapping the translated stream; `Subscribe()` returns a fresh cap-64 buffered channel; `Run(ctx)` drives one producer goroutine that fans every event to all subscribers with a deliver/ctx-cancel/drop-with-WARN select. Sole sender closes all channels on cancel or source end → goleak-clean.
- `client.go` — `type Event = events.Event` + `type EventType = events.EventType` aliases; `Subscribe(ctx, threadID, runID, idgen, turn) <-chan Event` composes `Translate` + a single-subscriber `Fanout`. Transport-free (no HTTP/SSE), forward-compat REASONING_* note only.
- `fanout_test.go` — 5 tests: all-subscribers-receive (no deadlock), slow-subscriber-dropped (concurrent reader gets all, slow peer overflow dropped, never back-pressures), ctx-cancel-closes-all (goroutines return to baseline), source-end-closes-all, and `TestClientSubscriberRoundTrip` (fake agent turn end-to-end RUN_STARTED…RUN_FINISHED incl. tool call, alias-typed channel, `goleak.VerifyNone`).

## Task Commits

Each task committed atomically:

1. **Task 1: fanout.go (cap-64 drop-on-full Fanout)** — `1856df05` (feat) — preceded by RED test attempt (see Deviations); landed as a single test+impl feat commit.
2. **Task 2: client.go (in-process subscriber + aliases) + round-trip test** — `389368dd` (feat)

_Note: the project pre-commit `vet` hook requires a compiling package, so the RED commits could not land standalone (documented below). RED was still demonstrated via a failing compile before each implementation._

## Files Created/Modified
- `internal/agui/fanout.go` (97 LOC) — `Fanout` pub-sub pump, `fanoutBuffer=64`, `NewFanout`/`Subscribe`/`Run`/`send`/`closeAll`.
- `internal/agui/client.go` (43 LOC) — `Event`/`EventType` aliases + `Subscribe` in-process helper.
- `internal/agui/fanout_test.go` (270 LOC) — fanout property/leak tests + client round-trip test.

## Decisions Made
- **Sole-sender-closes via a single producer goroutine** (golang-concurrency principle #4): `Run` defers `closeAll(subs)`; no `sync.Once` needed since the producer is the only caller. A `ctx.Err()` guard at the top of each range iteration plus the `<-ctx.Done()` send arm guarantee exit-on-cancel.
- **Drop-on-full not block-or-cancel** (unlike openai_compat's two-arm pump): fanout has N consumers, so a slow one must be dropped (`default:` arm + WARN), never allowed to back-pressure the shared producer (RESEARCH Pattern 4 / T-12-05 mitigation).
- **Alias-only SDK re-export:** only `Event`/`EventType` are aliased (the subscriber surface), not the whole SDK, satisfying the PRD "avoid leaking the external package" goal without a blanket re-export.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] TDD RED commits cannot land standalone under the pre-commit vet hook**
- **Found during:** Task 1 (fanout RED commit)
- **Issue:** The TDD flow calls for a standalone `test(...)` RED commit, but the repo's lefthook pre-commit `vet` hook fails on a package that references undefined symbols (`NewFanout`/`fanoutBuffer`), so a pure-RED commit is rejected (`vet.exe: undefined: NewFanout`).
- **Fix:** RED was still demonstrated (the test compile-failed before the implementation existed — captured in tooling output). The implementation was then written to GREEN and the test+impl committed together as one `feat(...)` commit per task. No test was weakened to pass; the tests assert the full required behavior (drop semantics, cancel-leak, source-end, round-trip alias-shield).
- **Files modified:** internal/agui/fanout_test.go, internal/agui/fanout.go (Task 1); internal/agui/client.go, internal/agui/fanout_test.go (Task 2).
- **Verification:** `go test -race -count=10 ./internal/agui/` green; `goleak` clean; the project hook (gofmt+vet+file-size) passes on each landed commit.
- **Committed in:** `1856df05`, `389368dd`.

**2. [Rule 1 - Bug] Slow-subscriber-drop test was flaky-prone (sequential post-Run drain could itself drop)**
- **Found during:** Task 1 (writing the slow-subscriber test)
- **Issue:** Draining the reading subscriber sequentially AFTER `Run` races the producer; with a cap-64 buffer and a 260-event source, the reader's own buffer could fill on a slow machine and drop events, making `len(got) == want` flaky (false fail).
- **Fix:** The reader is now drained in a goroutine started before `Run`, so it keeps pace and never overflows (the realistic concurrent-consumer usage); a 2s timeout guards against a blocked producer. Deterministic across `-count=10 -race`.
- **Files modified:** internal/agui/fanout_test.go.
- **Verification:** `go test -race -count=10 ./internal/agui/` green (10/10).
- **Committed in:** `1856df05`.

**3. [Rule 1 - Bug] Removed dead `sync.Once` in `closeAll`**
- **Found during:** Task 1 (fanout impl review)
- **Issue:** Initial draft wrapped `closeAll` in a `sync.Once`, but the producer goroutine is the only caller (via a single `defer`), so the Once was dead complexity (and an unused `sync` import path).
- **Fix:** Simplified `closeAll` to a plain loop; updated the doc comment to state the sole-caller invariant.
- **Files modified:** internal/agui/fanout.go.
- **Verification:** `golangci-lint run ./internal/agui/` 0 issues; race clean.
- **Committed in:** `1856df05`.

**4. [Rule 3 - Blocking] staticcheck ST1023/QF1011 on the alias type-pin**
- **Found during:** Task 2 (round-trip test)
- **Issue:** Expressing the alias contract via `var ch <-chan Event = Subscribe(...)` (and then `var _ <-chan Event = ch`) tripped staticcheck "omit type, it will be inferred" (ST1023/QF1011), failing the lint gate.
- **Fix:** Moved the type-pin into a `drainAlias(ch <-chan Event) []Event` helper whose parameter type is the compile-time proof that `Subscribe` returns `<-chan Event` (the alias), not `<-chan events.Event` — lint-clean and a stronger assertion.
- **Files modified:** internal/agui/fanout_test.go.
- **Verification:** `golangci-lint run ./internal/agui/` 0 issues.
- **Committed in:** `389368dd`.

---

**Total deviations:** 4 auto-fixed (2 blocking, 2 bug). **Impact on plan:** all necessary for correctness, test determinism, and passing the project lint/vet gates. No scope creep — the deliverables match the plan's artifacts exactly.

## Issues Encountered
- Whole-repo `go vet ./...` is slow on this Windows host; per-package vet/build/race were used during the loop and the full-repo vet was run once at the end (clean).

## Threat Model Compliance
- **T-12-05 (DoS / Fanout producer):** mitigated — cap-64 buffered channels + `default:` drop-with-WARN arm; producer never blocks on a slow subscriber (`TestFanoutSlowSubscriberDropped`).
- **T-12-06 (DoS / goroutine leak):** mitigated — sole sender closes; every send has a `<-ctx.Done()` arm; `goleak` (package TestMain + inline VerifyNone) proves exit on cancel and on source end (`TestFanoutCtxCancelClosesAll`, `TestClientSubscriberRoundTrip`).
- **T-12-07 (Info disclosure / aliased SDK types):** accepted — `client.go` only re-exports `Event`/`EventType` type aliases; no new data crosses a trust boundary vs. the translator.

No new threat surface introduced beyond the register.

## Known Stubs
None — both files are fully wired (`Subscribe` composes the real `Translate`+`Fanout`; no placeholder data sources).

## Verification Evidence
- `go vet ./...` clean; `go build ./...` ok.
- `go test -race -count=10 ./internal/agui/` green (10/10), `goleak` clean.
- Per-function coverage of new code: `client.Subscribe` 100%, `fanout.NewFanout`/`Subscribe`/`closeAll` 100%, `Run` 77.8%, `send` 80.0% (uncovered = the cancel-mid-loop micro-race arms, exercised behaviorally by the cancel test). Package coverage 76.6% includes Plan-01 translator surface.
- `golangci-lint run ./internal/agui/` 0 issues.
- `bash scripts/agui_boundary_check.sh` exit 0 (agent closure free of agui).
- `bash scripts/check-file-size.sh` pass (97 / 43 / 270 LOC, all < 600).

## Next Phase Readiness
- The in-process subscriber seam (`agui.Subscribe`) is ready for Phase 13 (Telegram) to consume an agent turn as AG-UI events without importing the SDK.
- Plan 03 (server.go + `aura serve` wiring) is independent — Fanout is explicitly NOT on the HTTP path; server.go drives Translate directly for SSE.
- No blockers.

## Self-Check: PASSED
- `internal/agui/fanout.go` — FOUND
- `internal/agui/client.go` — FOUND
- `internal/agui/fanout_test.go` — FOUND
- Commit `1856df05` (Task 1) — FOUND
- Commit `389368dd` (Task 2) — FOUND
- All `<verification>` commands re-run green; all task `<acceptance_criteria>` re-verified.

---
*Phase: 12-ag-ui-gateway*
*Completed: 2026-06-06*
