---
phase: 32-quality-cleanup-dead-code-shared-helpers
plan: 09
subsystem: test-gap-closure
tags: [qual-05, test-gap, throttle, sse-ordering, dsn-parse, goleak, race, refactor]

# Dependency graph
requires:
  - phase: 32-05
    provides: "envutil/pgnumeric leaf extractions landed — clean baseline on the touched packages"
  - phase: 32-06
    provides: "same-package agent extractions done — no overlap with these web/setup/webauth test files"
provides:
  - "internal/web/throttle_test.go — race+goleak concurrency pin for hostThrottle (acquire blocks at perHostLimit, release frees a waiter, ctx-cancel returns ok=false with a no-op release that frees NO token, per-host isolation, concurrent race)"
  - "internal/setup TestEventsInvalidateTokenBeforeSSEWrite — ordering regression pinning InvalidateToken-before-first-SSE-write (handlers.go:146); fails on a swapped order"
  - "internal/webauth/authula_test.go — by-value DSN table for ensureAuthulaSearchPath (empty/whitespace/malformed x2/append/append-keeps-siblings/idempotent)"
affects: [web, setup, webauth]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Recording-ResponseWriter seam (setup): a custom http.ResponseWriter snapshots a side-channel (token validity) at the instant of the FIRST body Write — pins write-ordering deterministically with NO production change and NO sleep/race."
    - "No-op-release correctness pin (web/throttle): after a ctx-cancelled acquire, assert the budget is STILL saturated (a short-deadline acquire still fails) to prove the no-op release freed no token."
    - "By-value DSN assertion (webauth): parse the result and assert exactly ONE search_path param + sibling-param survival, instead of substring matching (catches a double-append the old substring test could not)."

key-files:
  created:
    - internal/web/throttle_test.go
    - internal/webauth/authula_test.go
  modified:
    - internal/web/searxng_test.go
    - internal/setup/handlers_test.go
    - internal/webauth/webauth_test.go
  deleted: []

key-decisions:
  - "web/throttle and webauth/ensureAuthulaSearchPath BOTH already had weaker partial tests (the plan assumed they were untested gaps). Resolution: relocate+strengthen, not duplicate — the old weak copies were removed and the new co-located files are strict supersets (no test asilo nido, refactor-on-touch)."
  - "Authula DSN test source-conflict resolved by KEEPING IN Phase 32: ensureAuthulaSearchPath is a pure existing string helper that needs no Authula-cutover infrastructure (Phase-34 deferral was conditional on needing infra — it does not)."
  - "The setup ordering regression is pinned with a recording-ResponseWriter seam driving handleEvents directly (token still-valid at first write => fail), proven by a temporary swap experiment (FAIL on swap, PASS on shipped order) that was reverted and never committed."

requirements-completed: []  # QUAL-05 partial — orchestrator/verifier owns the flip.

# Metrics
duration: ~1h (sequential, no worktree; concurrent-Codex isolation held)
completed: 2026-06-30
status: complete
---

# Phase 32 Plan 09: QUAL-05 Test-Gap Closure (throttle / setup SSE ordering / Authula DSN) Summary

**Three test-only gap closures against already-shipped production functions, each committed atomically and green under `-race`: (1) a new `internal/web/throttle_test.go` pins `hostThrottle` under goleak — acquire blocks at `perHostLimit=2`, a release frees a blocked waiter, a ctx-cancelled acquire returns `ok=false` with a no-op release proven to free NO token, plus per-host isolation and a concurrent race; (2) a recording-ResponseWriter regression in `internal/setup/handlers_test.go` pins that `InvalidateToken` runs BEFORE the first SSE write (handlers.go:146) — it FAILS on a swapped order; (3) a by-value DSN table in the co-located `internal/webauth/authula_test.go` pins `ensureAuthulaSearchPath` (empty/whitespace/malformed×2/append/append-keeps-siblings/idempotent). Two of the three surfaces already had weaker partial tests, so the resolution was relocate+strengthen (no duplication), removing the old copies. No production code changed in any of the three packages.**

## Accomplishments

- **Task 1 — `internal/web/throttle_test.go` (NEW):** five `Throttle`-prefixed tests covering `sem` (lazy per-host bounded channel, `cap == perHostLimit`, same-channel-on-reuse, distinct-channel-per-host), acquire-blocks-at-limit + release-frees-a-waiter, the ctx-cancel-no-op-release correctness case, per-host isolation, and a 256-goroutine concurrent race. Runs under the package's existing goleak `TestMain` (`main_test.go`) — no second `TestMain` added. `newHostThrottle`/`sem`/`acquire` all reach **100%**.
- **Task 2 — `internal/setup/handlers_test.go` (extended):** `TestEventsInvalidateTokenBeforeSSEWrite` drives `handleEvents` directly with an `orderRecordingWriter` that snapshots `token.Valid("tok")` at the first body write. With a `togglingStore{flipAt:1}` + 1ms poll, the terminal `onboarding_completed` frame is emitted once; the token must already be invalid at that write. `handleEvents` reaches **100%**, `Invalidate` **100%**.
- **Task 3 — `internal/webauth/authula_test.go` (NEW, relocated):** a by-value table for `ensureAuthulaSearchPath` that parses the result and asserts EXACTLY ONE `search_path` value plus sibling-param survival. `ensureAuthulaSearchPath` reaches **100%**.

## Task Commits

Each gap committed atomically (D-11), direct `git commit -o -F <msgfile> -- <paths>` (explicit `--only` pathspecs to stay isolated from the concurrent Codex session on master):

1. **Task 1 — throttle concurrency pin** — `a0373b46` `test(32-09): pin web/throttle acquire/release/ctx-cancel/per-host under race+goleak` (`internal/web/throttle_test.go` +new, `internal/web/searxng_test.go` -relocated copy).
2. **Task 2 — setup SSE ordering pin** — `c7980ea3` `test(32-09): pin setup InvalidateToken-before-SSE-write ordering (handlers.go:146)` (`internal/setup/handlers_test.go`).
3. **Task 3 — Authula DSN pin** — `bd462666` `test(32-09): pin ensureAuthulaSearchPath DSN parsing (kept in Phase 32)` (`internal/webauth/authula_test.go` +new, `internal/webauth/webauth_test.go` -relocated copy).

## Decisions Made

- **Relocate+strengthen, never duplicate.** Two of the three "gaps" already had partial coverage: `web/throttle`'s cancel arm was tested by a weaker `TestHostThrottle_AcquireCancelled` mis-located in `searxng_test.go`, and `ensureAuthulaSearchPath` had a substring-only `TestEnsureAuthulaSearchPath` in `webauth_test.go`. Per the project's "never duplicate / NO TEST ASILO NIDO / DEEP REFACTOR ON TOUCH" rules, the old weak copies were removed and the new co-located files are strict supersets (stronger assertions: no-op-release-frees-nothing; exactly-one-search_path).
- **Source-conflict resolved: Authula DSN test stays IN Phase 32.** ROADMAP/REQUIREMENTS scope it to Phase 32; audit §E routes it to Phase 34. `ensureAuthulaSearchPath` is a pure existing string helper with no Authula-cutover dependency, so the Phase-34 deferral (conditional on needing infra not yet present) does not apply.
- **Deterministic ordering pin without touching production.** A recording `http.ResponseWriter` snapshots token validity at the first write rather than relying on timing — the test is race-free and fails iff the burn lands after the write.

## Deviations from Plan

**1. [Rule 1/refactor — pre-existing partial test] throttle cancel arm already tested (relocated)**
- **Found during:** Task 1 — `-run Throttle` surfaced a pre-existing `TestHostThrottle_AcquireCancelled` in `internal/web/searxng_test.go` (the plan stated throttle had "NO test").
- **Fix:** That test covered only the ctx-cancel arm and merely asserted the no-op release "is safe to call"; it did NOT assert the no-op release frees no token. Its case is subsumed and strengthened by the new `TestThrottleCtxCancelNoOpRelease`. Removed the redundant copy from `searxng_test.go` (verified `context` still imported), added the comprehensive `throttle_test.go`.
- **Files:** `internal/web/searxng_test.go` (removal), `internal/web/throttle_test.go` (new). **Commit:** `a0373b46`.

**2. [Rule 1/refactor — pre-existing partial test] ensureAuthulaSearchPath already tested (relocated to co-located file)**
- **Found during:** Task 3 — `TestEnsureAuthulaSearchPath` already existed in `internal/webauth/webauth_test.go` (the plan named a non-existent `authula_test.go`). A duplicate function name would not compile.
- **Fix:** Moved the test into the co-located `authula_test.go` (matching the `authula.go` ↔ `authula_test.go` convention the plan's artifact requires) and strengthened it from substring matching to by-value parse assertions (exactly-one `search_path`, sibling-param survival, two distinct malformed cases). Removed the old copy from `webauth_test.go` (verified `strings` still used by other tests there).
- **Files:** `internal/webauth/authula_test.go` (new), `internal/webauth/webauth_test.go` (removal). **Commit:** `bd462666`.

Both deviations touch only same-package `_test.go` files; production (`throttle.go`, `handlers.go`, `authula.go`) is unchanged. No architectural (Rule 4) decisions arose.

## TDD Gate Compliance

The plan is `type: tdd`, but all three tasks are **test-only gap closures of already-shipped production functions** — there is no new behavior to RED→GREEN. Accordingly the commits are correctly typed `test(...)` (no `feat` gate), and the production functions were not modified. The "fails-on-swap" swap experiment for Task 2 (revert-only, never committed) is the RED-equivalent evidence that the regression genuinely pins the fix.

## Coverage

| ID | Surface | Verification | Status |
|----|---------|--------------|--------|
| T1 | `hostThrottle` acquire/release/ctx-cancel/per-host/race | `go test -race -run Throttle ./internal/web/` green, no goleak/race; `newHostThrottle`/`sem`/`acquire` all **100.0%**; ctx-cancel-no-op-release asserted. | pass |
| T2 | setup InvalidateToken-before-first-SSE-write ordering | `go test -race ./internal/setup/` green; `handleEvents` **100.0%**, `token.Invalidate` **100.0%**; FAILS on swapped order, PASSES on shipped order (verified, reverted). | pass |
| T3 | `ensureAuthulaSearchPath` DSN parsing | `go test -race ./internal/webauth/` green; `ensureAuthulaSearchPath` **100.0%**; 7 subcases incl. exactly-one-search_path + sibling survival. | pass |

Combined: `go test -race ./internal/web/ ./internal/setup/ ./internal/webauth/` exits 0. All three pinned surfaces are at 100% statement coverage.

## Issues Encountered

- **Concurrent Codex session on master:** owns `internal/agui/**`, `internal/objectstore/**`, document/graphrag work, and `.planning/graphs/*`. Every commit here used explicit `--only` pathspecs; `git show --stat` confirmed each commit lists ONLY this plan's files (zero `internal/agui/**`, `internal/objectstore/**`, or `.planning/graphs/**` swept in). The parallel session's uncommitted `.planning/graphs/*` changes were left untouched throughout.
- **WSL `go env GOROOT` empty / `.exe` AV block:** all `go test`/`go vet`/`gofmt` ran in WSL against the main tree (never a native Windows `.exe`). The `go` shim auto-resolves the go1.26.4 toolchain and returns an empty `GOROOT`; `gofmt` was invoked from the toolchain bin. The pre-commit hook (gofmt + vet + file-size) passed on all three commits.

## User Setup Required

None — test-only additions against existing, unchanged production functions. No env, schema, or external-service changes.

## Next Phase Readiness

- **QUAL-05 web/throttle + setup-ordering + Authula-DSN gaps are closed.** Remaining QUAL-05 items (Telegram keyword fallback, `truncateTailBytes` UTF-8 table, `memory_integration` CI-leg documentation) are out of this plan's scope.
- No new package created (the two new files live in existing coverage-gated packages), so no `scripts/coverage_gate.sh` registration is needed.

## Self-Check: PASSED

- FOUND: internal/web/throttle_test.go (references perHostLimit/acquire/release/ctx-cancel; no duplicate TestMain)
- FOUND: internal/webauth/authula_test.go (contains `ensureAuthulaSearchPath`; 7 DSN subcases)
- FOUND: internal/setup/handlers_test.go `TestEventsInvalidateTokenBeforeSSEWrite` (fails on swapped order)
- CONFIRMED: production `internal/web/throttle.go`, `internal/setup/handlers.go`, `internal/webauth/authula.go` unchanged (empty `git diff`)
- FOUND commit: a0373b46 (Task 1 — throttle, isolated: 2 web test files, 0 agui/objectstore/graphs)
- FOUND commit: c7980ea3 (Task 2 — setup ordering, isolated: 1 test file)
- FOUND commit: bd462666 (Task 3 — Authula DSN, isolated: 2 webauth test files)
- CONFIRMED: STATE.md and ROADMAP.md NOT modified (orchestrator owns those writes)

---
*Phase: 32-quality-cleanup-dead-code-shared-helpers*
*Completed: 2026-06-30*
