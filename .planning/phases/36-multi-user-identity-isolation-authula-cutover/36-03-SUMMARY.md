---
phase: 36-multi-user-identity-isolation-authula-cutover
plan: 03
subsystem: infra
tags: [background-jobs, shell-exec, crypto-rand, identityctx, capability-grants, ttl, process-group, goleak, MUSR-03, MUSR-04]

# Dependency graph
requires:
  - phase: 36-01
    provides: "seeded `local` admin caps (governance.write, migration 0026) — the admin capability the D-18 poll/kill exemption reuses"
provides:
  - "Unguessable 128-bit crypto/rand background-job IDs (no more sequential sh_%d)"
  - "(identity, session) owner binding on every bgShell at start, from identityctx.IdentityID(ctx) + the tool-call session key"
  - "Owner-OR-admin authority on shell_poll (foreign=404) and shell_kill (foreign=403), via a nil-safe capabilityChecker seam"
  - "Default 1h TTL (AURA_SHELL_BG_TTL) reaper: terminates the process group + records status 'expired' on expiry"
  - "shell_poll age_ms metric (now - startedAt)"
  - "shell_bg.go split on touch into shell_bg_owner.go (identity/authority) + shell_bg_ttl.go (reaper), all ≤600 LOC"
affects: [36-09 runner conversation-delete lifecycle + (identity,session) keying, 36-10 admin capability wiring, 36-12 two-identity live cross-deny E2E]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "crypto/rand 128-bit hex IDs, fail-closed on RNG error (no guessable fallback)"
    - "Owner binding at start from identityctx + the tool-call session key (D-23); empty principal -> the seeded `local` UUID (D-25 CLI fallback)"
    - "D-06 deny semantics in a tool: foreign read -> not-found shape (404), foreign mutate -> refusal (403)"
    - "accept-interfaces capability seam (D-A2-02): nil-safe capabilityChecker, fail-closed when unwired, zero-rework store swap (D-04)"
    - "Sweeper-style reaper: NOT started by the constructor (goleak parity), started once at serve boot, once-guarded stop folded into Shutdown"
    - "Refactor-on-touch file split to stay under the 600-LOC ceiling (RESEARCH Pitfall 7)"

key-files:
  created:
    - "internal/agent/tools/shell_bg_owner.go (crypto IDs + owner + authority + moved ShellPoll/ShellKill)"
    - "internal/agent/tools/shell_bg_ttl.go (TTL env helper + sweepExpired + StartReaper/stopReaper)"
    - "internal/agent/tools/shell_bg_owner_test.go (TestBackgroundJobID, TestBackgroundJobOwnerDeny)"
    - "internal/agent/tools/shell_bg_ttl_test.go (TestBackgroundJobTTLExpiry, TestBackgroundJobAge, +default/override, +ticker reap)"
  modified:
    - "internal/agent/tools/shell_bg.go (owner+ttl fields, ctx-threaded start, crypto ID, expired status, opportunistic sweep, Shutdown stop-fold)"
    - "internal/agent/tools/shell_exec.go (thread ctx into Background.start)"
    - "cmd/aura/serve.go (StartReaper wiring on the daemon work ctx)"
    - "internal/agent/tools/shell_bg_test.go (start(ctx,...) call sites + owned redact fixture)"
    - "internal/agent/tools/tool_hardening_test.go (start(ctx,...) call site)"

key-decisions:
  - "Reuse governance.write as the D-18 admin poll/kill capability (RESEARCH OQ resolution — no net-new settings.model.write)"
  - "Admin capability consulted via a nil-safe capabilityChecker seam on ShellPoll/ShellKill; nil in the current composition root => owner-only, fail-closed (foreign denied). Production store wiring is a zero-rework field-set deferred to the multi-user surface (36-10/36-12)"
  - "start() gains a callerCtx param (owner binding source) — its sole production caller (shell_exec.go) and 6 test call sites updated; the process-lifetime ctx stays a fresh background ctx so the job outlives the turn"
  - "TTL is an always-on safety bound: a missing/invalid/non-positive AURA_SHELL_BG_TTL falls back to 1h (not disable-able via env), mirroring the buf/max helpers"
  - "Reaper sweeps BOTH opportunistically on every start (works even if StartReaper was never wired) AND from a bounded ticker (serve boot); the ticker is goleak-safe by never self-starting in the constructor"

patterns-established:
  - "Tool-level owner binding + D-06 authority: bind (identity,session) at resource creation, gate access on owner-OR-admin, hide existence on foreign read"
  - "goleak-safe background worker: constructor spawns nothing; explicit Start at the composition root; once-guarded Stop folded into the existing teardown hook"

requirements-completed: [MUSR-03, MUSR-04]

coverage:
  - id: D1
    description: "Background-job IDs are unguessable 128-bit crypto/rand hex, never the sequential sh_%d (MUSR-03)"
    requirement: "MUSR-03"
    verification:
      - kind: unit
        ref: "internal/agent/tools/shell_bg_owner_test.go#TestBackgroundJobID"
        status: pass
    human_judgment: false
  - id: D2
    description: "A job is bound to its (identity,session) owner; a foreign session cannot poll (404) or kill (403); owner and admin-cap holder can (MUSR-03/D-06/D-18)"
    requirement: "MUSR-03"
    verification:
      - kind: unit
        ref: "internal/agent/tools/shell_bg_owner_test.go#TestBackgroundJobOwnerDeny"
        status: pass
    human_judgment: false
  - id: D3
    description: "TTL expiry terminates the process group (cancel->killProcessGroup) and records status 'expired', idempotently (MUSR-04/D-17)"
    requirement: "MUSR-04"
    verification:
      - kind: unit
        ref: "internal/agent/tools/shell_bg_ttl_test.go#TestBackgroundJobTTLExpiry"
        status: pass
      - kind: unit
        ref: "internal/agent/tools/shell_bg_ttl_test.go#TestBackgroundJobReaperTicksAndReaps"
        status: pass
    human_judgment: false
  - id: D4
    description: "Default 1h TTL, AURA_SHELL_BG_TTL override, and a shell_poll age_ms metric (MUSR-04)"
    requirement: "MUSR-04"
    verification:
      - kind: unit
        ref: "internal/agent/tools/shell_bg_ttl_test.go#TestBackgroundJobAge"
        status: pass
      - kind: unit
        ref: "internal/agent/tools/shell_bg_ttl_test.go#TestBackgroundJobTTLDefaultAndOverride"
        status: pass
    human_judgment: false
  - id: D5
    description: "The `-race` tier for internal/agent/tools (owner-deny + TTL + goleak) under the full detector"
    verification:
      - kind: unit
        ref: "go test -race ./internal/agent/tools/"
        status: unknown
    human_judgment: true
    rationale: "No CGO/gcc on this Windows host — `-race` requires cgo. Untagged go test + goleak are green here; the race tier must run in WSL/CI before phase close (CLAUDE.md no-skip-as-green)."
  - id: D6
    description: "D-18 admin cross-session poll/kill exemption is wired to the live identity store in production (not just the test seam)"
    verification: []
    human_judgment: true
    rationale: "The capabilityChecker seam is present + unit-tested, but nil at the current composition root (owner-only, fail-closed). Production store wiring lands with the multi-user surface (36-10/36-12); verifier must confirm the fail-closed default is acceptable until then."

# Metrics
duration: ~35min
completed: 2026-07-05
status: complete
---

# Phase 36 Plan 03: Background Job Owner-Binding + 1h TTL Reaper Summary

**Crypto-random, (identity,session)-owned background shell jobs with owner-OR-admin poll/kill authority (foreign read=404, mutate=403) and a default-1h TTL reaper that terminates the process group and records status "expired" — closing the MUSR-03/04 guessable-ID / no-owner / no-TTL gap.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-07-05T20:44:00+02:00 (after 36-02 docs commit)
- **Completed:** 2026-07-05T21:16:00+02:00
- **Tasks:** 2 (both TDD)
- **Files modified:** 9 (4 created, 5 modified)

## Accomplishments
- **MUSR-03:** replaced the trivially-guessable sequential `sh_%d` IDs with 128-bit `crypto/rand` hex (fail-closed on RNG error); bound each job to its `(ownerID, sessionID)` at start from `identityctx.IdentityID(ctx)` + the tool-call session key (empty principal → the seeded `local` UUID, D-25).
- **MUSR-03 authority:** `shell_poll`/`shell_kill` now resolve the caller `(identity, session)` and enforce owner-OR-admin — a foreign non-admin poll returns the not-found shape (D-06 read=404), a foreign non-admin kill is refused (D-06 mutate=403). The admin exemption (D-18) consults `HasCapability(governance.write)` through a nil-safe `capabilityChecker` seam.
- **MUSR-04:** every job carries a default 1h TTL (`AURA_SHELL_BG_TTL`, Go-duration overridable); the reaper terminates the whole process group (`cancel` → `cmd.Cancel = killProcessGroup`) and records status `"expired"` (preserved through `finish()`), swept both opportunistically on start and from a bounded, goleak-safe background ticker wired at serve boot.
- **MUSR-04 metric:** `shell_poll` exposes an `age_ms` metric (`now - startedAt`) on both the Meta and the rendered footer.
- **Refactor-on-touch:** split the 499-LOC `shell_bg.go` into `shell_bg.go` (421) + `shell_bg_owner.go` (232) + `shell_bg_ttl.go` (150) — all under the 600-LOC ceiling (RESEARCH Pitfall 7).

## Task Commits

Each task was committed atomically (TDD test-first, committed green per CLAUDE.md build-green discipline):

1. **Task 1: Crypto IDs + (identity,session) owner + authority-gated poll/kill** — `3a4ffc9d` (feat)
2. **Task 2: Default 1h TTL reaper + age metric** — `f7949e6e` (feat)

**Plan metadata:** (this SUMMARY + STATE/ROADMAP/REQUIREMENTS) — see final docs commit.

_TDD note: both tasks were developed test-first (write test → observe failure → implement → observe pass). They are committed as single `feat` commits per file rather than separate RED/GREEN commits, because a broken-RED commit would leave the package non-building and violate CLAUDE.md's post-edit `go build ./...` gate + atomic-green-commit discipline._

## Files Created/Modified
- `internal/agent/tools/shell_bg.go` — core registry; ctx-threaded `start`, crypto ID minting, owner/ttl/expired fields, `expired` status + `finish()` preservation, opportunistic sweep, `Shutdown` stop-fold.
- `internal/agent/tools/shell_bg_owner.go` — **created**; `localOwnerID`/`adminShellCapability` consts, `capabilityChecker` seam, `newBackgroundShellID`, `ownerFromContext`, `bgShell.authorize`, `authorizeCaller`, and the moved `ShellPoll`/`ShellKill` (with authority + `age_ms`).
- `internal/agent/tools/shell_bg_ttl.go` — **created**; `shellBackgroundTTL` env helper, `reaperIntervalFor`, `bgShell.age`, `sweepExpired`/`sweepExpiredLocked`, `StartReaper`/`stopReaper`.
- `internal/agent/tools/shell_exec.go` — thread `ctx` into `Background.start`.
- `cmd/aura/serve.go` — `StartReaper(ctx)` on the daemon work ctx beside the sweeper/reconciler (drain `Shutdown` joins it).
- `internal/agent/tools/shell_bg_owner_test.go` / `shell_bg_ttl_test.go` — **created**; the Wave-0 MUSR-03/04 tests.
- `internal/agent/tools/shell_bg_test.go`, `tool_hardening_test.go` — `start(ctx, …)` call-site updates + owned redaction fixture.

## Decisions Made
- **Admin capability = `governance.write`** (reused, RESEARCH OQ resolution) — no net-new `settings.model.write`.
- **Admin path is a nil-safe seam**: present + unit-tested, `nil` at the current composition root → owner-only / fail-closed. Rationale: injecting the identity store into the tool registry is a composition-root change beyond this plan's scope, and the multi-user surface it protects is not the default until 36-12; the fail-closed default is the secure interim (foreign callers denied even without the admin escape hatch). Zero-rework to wire (D-04).
- **TTL is always-on** (invalid/absent env → 1h), not env-disable-able — MUSR-04 is a DoS bound, not an opt-in.

## Deviations from Plan

### Auto-fixed / necessary adjustments

**1. [Rule 3 - Blocking] `start` signature gained a `callerCtx` parameter**
- **Found during:** Task 1 (owner binding must read `identityctx`/session from ctx, which `start` did not receive).
- **Fix:** Added `callerCtx context.Context` as `start`'s first param; updated its sole production caller (`shell_exec.go`, which already had `ctx`) and 6 test call sites (`shell_bg_test.go` ×5, `tool_hardening_test.go` ×1) to pass a ctx. The process-lifetime ctx stays a fresh `context.Background()` so the job still outlives the turn.
- **Files:** shell_bg.go, shell_exec.go, shell_bg_test.go, tool_hardening_test.go
- **Verification:** `go build ./...` + full `go test ./internal/agent/tools/` green.
- **Committed in:** `3a4ffc9d`

**2. [Rule 2 - Missing Critical] Wired `StartReaper` into `cmd/aura/serve.go`**
- **Found during:** Task 2 (a reaper that is never started is dead code — the MUSR-04 DoS bound would not run in the daemon).
- **Fix:** Added a nil-guarded `env.toolHandles.BackgroundShells.StartReaper(ctx)` beside the existing sweeper/reconciler starts (on the long-lived work ctx); the existing drain `BackgroundShells.Shutdown` joins it via the once-guarded `stopReaper` fold. `serve.go` was outside the plan's listed `files_modified`, but the reaper is inert without this one-line wiring.
- **Files:** cmd/aura/serve.go, shell_bg.go (Shutdown stop-fold), shell_bg_ttl.go (StartReaper/stopReaper)
- **Verification:** `go build ./...` + `go vet ./cmd/aura/` green; `TestBackgroundJobReaperTicksAndReaps` proves the ticker reaps; goleak clean.
- **Committed in:** `f7949e6e`

**3. [Test fixture — new ownership contract] `TestShellPollRedactsModelPreview`**
- **Found during:** Task 1. The pre-existing redaction fixture built a job with no owner and polled it from a session-scoped ctx; under the new ownership model that is a foreign (denied) poll.
- **Fix:** Gave the fixture job an explicit owner `(local, "sess-bg-redact")` matching the polling ctx, so the redaction assertion still exercises the poll path. This is a fixture correction for a new contract, not a test weakened to pass (the redaction assertion is unchanged).
- **Committed in:** `3a4ffc9d`

**Total deviations:** 2 necessary code adjustments (1 blocking signature ripple, 1 missing-critical wiring) + 1 fixture correction. **Impact:** no scope creep; both code adjustments are required for the plan's own success criteria to actually function. The only file touched beyond the plan's `files_modified` is `cmd/aura/serve.go` (the reaper's single wiring point).

## Issues Encountered
- **`-race` cannot run on this Windows host** (`CGO_ENABLED=0`, no gcc/w64devkit on PATH). Ran the untagged `go build` + `go vet` + `go test` + goleak tiers (all green) and honestly report the `-race` tier as `unknown` — it must run in WSL/CI before phase close (CLAUDE.md "Where to run what" / no-skip-as-green). Coverage entry D5 flags this for the verifier.
- **`TestBackgroundJobReaperTicksAndReaps` initially took 6s** because `Shutdown`'s shell-drain waited out its timeout on a process-less fixture shell that never reaches `done`. Switched the test teardown to `stopReaper()` (joins the ticker goroutine only) — ~4s and clean.

## Known Seams / Deferred Wiring
- **`ShellPoll.Caps` / `ShellKill.Caps` are `nil` in production** (the composition root does not yet inject an identity store). This is fail-closed (a foreign caller is denied with no admin escape hatch), not a broken stub — the MUSR-03 core deny property holds. Wiring the store is a zero-rework field-set for 36-10/36-12 when the multi-user surface goes live. Tracked as coverage D6 (human_judgment).

## Threat Flags
None — no new network endpoint, auth path, file-access pattern, or schema surface beyond the plan's `<threat_model>` (T-36-03-E/T/D/SC all mitigated in-scope; stdlib `crypto/rand` only, zero new packages).

## Next Phase Readiness
- MUSR-03/04 delivered at the code+unit level; the phase-level two-identity live E2E (success-criterion 2: "Session B cannot poll/kill session A's shell; jobs expire by TTL") is exercised end-to-end in 36-12.
- 36-09 (runner conversation-delete lifecycle + `(identity,session)` keying, D-23) can rely on the now-owner-bound jobs when it evicts/terminates session background work.
- 36-10 should wire `ShellPoll.Caps`/`ShellKill.Caps` to the identity store to activate the D-18 admin cross-session recovery path.
- **Before phase close:** run `go test -race ./internal/agent/tools/` in WSL/CI (D5).

## Self-Check: PASSED

- Created files verified present: `shell_bg_owner.go`, `shell_bg_ttl.go`, `shell_bg_owner_test.go`, `shell_bg_ttl_test.go`, `36-03-SUMMARY.md`.
- Task commits verified in history: `3a4ffc9d` (Task 1), `f7949e6e` (Task 2).
- Structural checks: `shell_bg.go` 421 LOC / `shell_bg_owner.go` 232 / `shell_bg_ttl.go` 150 (all ≤600); no residual `sh_%d` minting (only explanatory comments); `go build ./...` + `go vet` + untagged `go test ./internal/agent/tools/` + goleak green. `-race` deferred to WSL/CI (no CGO on host).

---
*Phase: 36-multi-user-identity-isolation-authula-cutover*
*Completed: 2026-07-05*
