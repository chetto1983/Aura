---
phase: 35-toolgateway-policy-engine
plan: 05
subsystem: api
tags: [policy-engine, gateway, reconciler, crash-orphan, append-only, gate-03, gate-04, tool-invocations, goleak, background-worker]

# Dependency graph
requires:
  - phase: 35-toolgateway-policy-engine (35-01)
    provides: "the trustworthy Mutating:true floor on the multiplexed tools — the hard prerequisite that lets the reconciler treat a mutating orphan conservatively (never re-invoke)"
  - phase: 35-toolgateway-policy-engine (35-04)
    provides: "toolinvocations.Store.ListInFlightBefore (start∧¬end anti-join) + GetEnd + Insert; the unified reserve that makes an approved-resume mutating call produce the SAME single start∧¬end shape as an auto-allow"
  - phase: 34 (tool-invocation ledger)
    provides: "the append-only aura.tool_invocations ledger (migration 0011) — status CHECK IN ('ok','error'), end shape (ended_at+status NOT NULL), UNIQUE (conv,req,toolCall,event_kind) + ON CONFLICT DO NOTHING, append-only triggers"
provides:
  - "internal/gateway/reconcile.go — Reconciler (Start/Stop, boot one-shot + interval tick, bounded goleak-clean join) + reconcileOrphans (append-only, pre-append GetEnd re-check) + reservationOrphanGrace/reservationOrphanMargin consts + effectiveGrace(maxToolExecWindow)"
  - "the serve composition-root wiring: Reconciler.Start at boot + Stop in drainShutdown (beside conversations.Sweeper), constructed over the same *toolinvocations.Store the gateway reserves through, with the resolved maxToolExecWindow"
  - "the effectiveGrace>maxToolExecWindow collision-impossibility invariant: a start∧¬end older than the effective grace is a PROVABLE crash-orphan whose real end can never race the synthetic end into ON CONFLICT DO NOTHING"
affects: [phase-35-complete, gate-04-recovery]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "append-only crash-orphan reconciliation: close a stuck start∧¬end by APPENDING an end{status='error', meta.indeterminate} fact — never UPDATE (append-only triggers reject it), never re-invoke a mutating orphan (its side effect may already have fired)"
    - "effective-grace > run-lifetime invariant: effectiveGrace = max(orphanGrace, maxToolExecWindow + margin) so a within-lifetime reservation is never misrecorded as an orphan; a pre-append GetEnd re-check closes the list→append window"
    - "leak-clean background worker mirroring conversations.Sweeper (boot one-shot + interval tick, once-guarded stop, bounded wg join)"

key-files:
  created:
    - internal/gateway/reconcile.go
    - internal/gateway/reconcile_test.go
    - internal/gateway/reconcile_integration_test.go
    - cmd/aura/serve_dispatch.go
  modified:
    - cmd/aura/serve.go
    - cmd/aura/chat.go
    - cmd/aura/serve_drain.go

key-decisions:
  - "Wired the reconciler lifecycle into the serve composition root (cmd/aura/serve.go + serve_drain.go), NOT internal/runner/runner.go as the plan frontmatter listed — the conversations.Sweeper the plan mandates mirroring lives there, and runner.New is shared by the one-shot `aura chat`, which must not spawn a daemon reconciler"
  - "The reconciler has NO tool-execution seam by construction — 'never re-invoke a mutating orphan' is structural, not a runtime guard; it only lists→re-checks→appends through the ledger store"
  - "effectiveGrace floor 30min + 5min margin: for both default bounds (node timeout 0, wallclock 300s) the 30-min const dominates; a raised knob auto-expands the grace above the lifetime"
  - "No new env knob for the reconcile cadence (scope control): a fixed reconcileTickInterval=10min const; the boot one-shot covers immediate post-crash recovery"

patterns-established:
  - "Pattern 1: append-only reconciliation — list start∧¬end older than effectiveGrace → pre-append GetEnd re-check → append end{error,indeterminate,reconciled}; ON CONFLICT DO NOTHING is the final backstop"
  - "Pattern 2: grace>lifetime collision-impossibility — the effective grace strictly exceeds the resolved single-run tool-execution lifetime so the reconciler can never overwrite a legitimately slow tool's real outcome"

requirements-completed: [GATE-03, GATE-04]

coverage:
  - id: D1
    description: "effectiveGrace(w) strictly exceeds the run lifetime for every config (defaults 0/300s AND operator-raised), and the 30-min floor dominates the two default bounds — the WARNING-4 collision-impossibility invariant"
    requirement: "GATE-04"
    verification:
      - kind: unit
        ref: "internal/gateway/reconcile_test.go#TestEffectiveGraceExceedsRunLifetime"
        status: pass
    human_judgment: false
  - id: D2
    description: "A start∧¬end older than the effective grace is closed by APPENDING an end{status='error', meta.indeterminate:true, reconciled:true} — original start row untouched, exactly start+end (APPEND not UPDATE); the mark is an indeterminate error, never a re-invoked 'ok'"
    requirement: "GATE-03"
    verification:
      - kind: integration
        ref: "internal/gateway/reconcile_integration_test.go#TestReconcileAppendsIndeterminateEndForOrphan (live PG, -race)"
        status: pass
      - kind: unit
        ref: "internal/gateway/reconcile_test.go#TestReconcileAppendsIndeterminateEnd"
        status: pass
    human_judgment: false
  - id: D3
    description: "A legitimately slow (within-lifetime) tool is NOT swept: an in-grace orphan is left untouched, and its real end{status='ok'} then wins (GetEnd returns the real ok, never the synthetic indeterminate)"
    requirement: "GATE-04"
    verification:
      - kind: integration
        ref: "internal/gateway/reconcile_integration_test.go#TestReconcileInGraceOrphanUntouchedThenSlowToolRealEndWins (live PG, -race)"
        status: pass
    human_judgment: false
  - id: D4
    description: "The pre-append GetEnd re-check closes the list→append window: a real end present since the snapshot makes the reconciler SKIP (no synthetic end appended), preserving the real outcome"
    requirement: "GATE-04"
    verification:
      - kind: integration
        ref: "internal/gateway/reconcile_integration_test.go#TestReconcileRechecksBeforeAppendLiveRealEndWins (live PG, -race)"
        status: pass
      - kind: unit
        ref: "internal/gateway/reconcile_test.go#TestReconcileRechecksBeforeAppendSkips"
        status: pass
    human_judgment: false
  - id: D5
    description: "Reconciler.Start/Stop is goleak-clean (boot one-shot + tick, bounded join), incl. nil/zero-interval no-op and idempotent Stop; wired into serve boot/shutdown beside the Sweeper"
    requirement: "GATE-03"
    verification:
      - kind: integration
        ref: "internal/gateway/reconcile_integration_test.go#TestReconcileStartStopGoleakLive (live PG boot one-shot + tick + Stop, main_test goleak.VerifyTestMain)"
        status: pass
      - kind: unit
        ref: "internal/gateway/reconcile_test.go#TestReconcileStartStopGoleakClean"
        status: pass
    human_judgment: false

# Metrics
duration: ~40min
completed: 2026-07-03
status: complete
---

# Phase 35 Plan 05: Crash-Orphan Reconciler Summary

**An append-only, conservative crash-orphan reconciler (D-01d / GATE-03 durability + GATE-04 recovery): a `start`-without-`end` reservation older than an effective grace window is closed by APPENDING an `end{status='error', meta.indeterminate}` fact — never by re-invoking the orphaned tool and never by UPDATEing the ledger — with `effectiveGrace = max(reservationOrphanGrace, maxToolExecWindow + margin)` making a within-lifetime slow tool's real outcome un-overwritable, a pre-append `GetEnd` re-check closing the list→append window, and a leak-clean Start/Stop lifecycle mirroring `conversations.Sweeper` wired into the serve daemon.**

## Performance

- **Duration:** ~40 min
- **Completed:** 2026-07-03
- **Tasks:** 3 (+1 refactor-on-touch)
- **Files modified:** 7 (4 created, 3 modified)

## Accomplishments

- **Reconciler (D-01d):** `internal/gateway/reconcile.go` — a `Reconciler` mirroring `conversations.Sweeper` VERBATIM (boot one-shot + interval tick, `once`-guarded stop, bounded `wg` join, goleak-clean). `reconcileOrphans` lists `Store.ListInFlightBefore(now - effectiveGrace)`, RE-CHECKS `Store.GetEnd` immediately before appending (skip if a real end landed since the snapshot), then APPENDS an `end{status='error', error, meta{indeterminate,reconciled}}` keyed on the orphan's triple — APPEND-only, never UPDATE, never a second `Execute`. The candidate set is provenance-agnostic: an approved-resume and an auto-allow share one start shape (35-04 unified reserve), so no approve-path special-casing.
- **Collision-impossibility invariant (checker WARNING 4):** `effectiveGrace(w) = max(reservationOrphanGrace=30m, w + reservationOrphanMargin=5m)`, where `w = maxToolExecWindow`. `effectiveGrace(w) > w` for every config, so a start∧¬end older than the effective grace is a PROVABLE crash-orphan whose real `end` can never race the synthetic end into `ON CONFLICT DO NOTHING` (35-04 semantics unchanged). The honest residual (no hard per-tool ctx deadline by default) is handled benignly — the conservative indeterminate mark is the correct D-01d outcome.
- **Serve wiring:** exposed `*toolinvocations.Store` on `chatEnv`; constructed `gateway.NewReconciler(chat.toolInvocations, reconcileTickInterval, resolveMaxToolExecWindow())` beside the sweeper; `Start(ctx)` at boot, `Stop()` in `drainShutdown` — bounded, idempotent, nil/disabled-safe. `maxToolExecWindow = max(AURA_LOOP_NODE_TIMEOUT_SEC, AURA_LOOP_MAX_WALLCLOCK_SEC)` (defaults 0/300s) so the invariant holds even if an operator raises either knob.
- **Proof tier (live PG, `-race`, `-p 1`):** unit `EffectiveGraceExceedsRunLifetime` + append-shape + re-check-skip + list-failure-recoverable + goleak lifecycle; db_integration `AppendsIndeterminateEndForOrphan` (append not update, start row intact, indeterminate not ok), `InGraceOrphanUntouchedThenSlowToolRealEndWins` (no discard), `RechecksBeforeAppendLiveRealEndWins` (raced end preserved), `StartStopGoleakLive` — all ran live (~0.1–0.2s of real DB work each, not skipped).

## Task Commits

Each task was committed atomically:

1. **Task 1: append-only reconciler + grace>lifetime invariant** — `3b6d49ed` (feat)
2. **Task 2: wire Reconciler into the serve composition root** — `31b92ea2` (feat)
3. **Refactor-on-touch: split cron dispatch out of serve.go (≤600 LOC)** — `e4261c7a` (refactor)
4. **Task 3: unit invariant + live db_integration/goleak proof** — `cce7cf66` (test)

## Files Created/Modified

- `internal/gateway/reconcile.go` — Reconciler + reconcileOrphans + syntheticEnd + effectiveGrace + grace/margin/interval consts [T1] (238 LOC)
- `cmd/aura/chat.go` — expose `*toolinvocations.Store` on chatEnv [T2]
- `cmd/aura/serve.go` — reconcileTickInterval const, resolveMaxToolExecWindow, serveEnv.reconciler field, construct + Start [T2] (648→555 after refactor)
- `cmd/aura/serve_drain.go` — reconciler.Stop() in drainShutdown [T2]
- `cmd/aura/serve_dispatch.go` — extracted buildDispatch + handlerAdapter (refactor-on-touch, ≤600 ceiling) [refactor]
- `internal/gateway/reconcile_test.go` — unit invariant + fake-store re-check/append/lifecycle proofs [T3]
- `internal/gateway/reconcile_integration_test.go` — live PG db_integration + goleak proofs [T3]

## Decisions Made

- **Lifecycle wired into the serve root, not runner.go.** The plan frontmatter listed `internal/runner/runner.go`, but the plan BODY (Task 2 action/acceptance/read_first + key_links "via") mandates mirroring `conversations.Sweeper` at "the serve composition root". The Sweeper's Start/Stop live in `cmd/aura/serve.go` + `serve_drain.go`, and `runner.New` is shared by the one-shot `aura chat` (which must not spawn a background reconciler). Wiring there is the pattern-faithful, architecturally correct resolution of the plan-internal contradiction.
- **No tool-execution seam by construction.** The Reconciler holds only the ledger store, so "never re-invoke a mutating orphan" (Pitfall 5) is a structural guarantee, not a runtime check — the appended fact is always an indeterminate error, never a fresh `ok`.
- **No new env knob for the cadence.** A fixed `reconcileTickInterval=10min` const (scope control); the boot one-shot covers immediate post-crash recovery.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Reconciler lifecycle wired into the serve root instead of internal/runner/runner.go**
- **Found during:** Task 2 (wiring)
- **Issue:** The plan frontmatter `files_modified` listed `internal/runner/runner.go`, but that file owns no daemon Start/Stop lifecycle — the `conversations.Sweeper` the plan explicitly mandates mirroring is constructed/started/stopped in `cmd/aura/serve.go` + `serve_drain.go`. `runner.New` is shared by the one-shot `aura chat`, so wiring a background reconciler there would leak a daemon worker into a CLI invocation.
- **Fix:** exposed `*toolinvocations.Store` on `chatEnv`; constructed + Started the Reconciler in `serve.go` (beside the sweeper) and Stopped it in `drainShutdown` — the exact call sites the sweeper uses.
- **Files modified:** cmd/aura/serve.go, cmd/aura/chat.go, cmd/aura/serve_drain.go
- **Verification:** `go build ./...`, `go vet ./...`, `go test -race ./internal/runner/ ./internal/gateway/` green; no goroutine leak
- **Committed in:** `31b92ea2` (Task 2 commit)

**2. [Rule 3 - Blocking] Split cron dispatch out of serve.go to hold the ≤600-LOC ceiling**
- **Found during:** Task 2 (wiring)
- **Issue:** serve.go was already 603 LOC; the reconciler wiring pushed it to 648, over the CLAUDE.md hard ≤600 cap (deep-refactor-on-touch).
- **Fix:** extracted the cohesive cron-dispatch assembly (`buildDispatch` + `handlerAdapter` + the `ChannelDeliverer` assertion) into `cmd/aura/serve_dispatch.go` — zero behavior change; dropped the now-unused `handlers`/`scoring` imports from serve.go (648→555).
- **Files modified:** cmd/aura/serve.go, cmd/aura/serve_dispatch.go
- **Verification:** `go build ./...`, `go vet ./...`, `go test ./cmd/aura/` green
- **Committed in:** `e4261c7a` (refactor commit)

---

**Total deviations:** 2 auto-fixed (both Rule 3 blocking). **Impact:** no scope creep — one resolves a plan-internal file-location contradiction toward the pattern the plan text mandates; the other is the mandatory refactor-on-touch to hold the LOC ceiling. Behavior matches the plan's must_haves exactly.

## Issues Encountered

- **`go test -race` + db_integration need cgo + the live stack, absent from the Windows PATH.** Ran the whole race + db_integration matrix natively in WSL (`go1.26.4`, `/usr/local/go/bin`, DSNs composed from `.env`'s `POSTGRES_PASSWORD` → `AURA_DB_URL`/`AURA_DB_MIGRATE_URL`), reaching the Windows Docker stack via `127.0.0.1`. Multi-line commands piped through Git Bash → WSL mangled variables (CRLF), so the verification was driven from an LF-normalized script file.
- **Parallel-package `EnsureRoles` race (`tuple concurrently updated`).** Per the 35-04 note, ran the integration packages with `-p 1` (serial package-level DB setup) — gateway + toolinvocations both green live (~1.5–2s each; the named reconcile proofs each do ~0.1s of real DB work, so no skip-as-green).

## Known Stubs

None — the reconciler is fully live-wired against the real append-only ledger and the serve daemon lifecycle. It consumes `Store.ListInFlightBefore`/`GetEnd`/`Insert` (35-04) with no new query or migration.

## Threat Flags

None — no new network endpoint, auth path, or trust boundary beyond the plan's `<threat_model>`. The reconciler only appends to the existing append-only ledger via the redaction chokepoint; the indeterminate marker rides `meta jsonb` (zero-migration). All three high threats (append-only cover-up, slow-tool collision, mutating re-invoke) are mitigated in-plan and proven.

## Next Phase Readiness

- Phase 35 (toolgateway-policy-engine) is now complete end-to-end: classification (35-01), the PEP + approval routing (35-03), the durable reservation + idempotent replay (35-04), and the crash-orphan recovery reconciler (35-05). GATE-03 (durability, fail-closed) + GATE-04 (recovery, idempotency) are delivered and live-proven.
- The reconciler is a serve-daemon worker; `aura chat` (one-shot) does not run it, and dev/local_trusted write no reservations, so it is a harmless no-op there.

## Self-Check: PASSED

- `internal/gateway/reconcile.go` — FOUND
- `internal/gateway/reconcile_test.go` — FOUND
- `internal/gateway/reconcile_integration_test.go` — FOUND
- `cmd/aura/serve_dispatch.go` — FOUND
- Commit `3b6d49ed` (Task 1) — FOUND
- Commit `31b92ea2` (Task 2) — FOUND
- Commit `e4261c7a` (refactor) — FOUND
- Commit `cce7cf66` (Task 3) — FOUND

---
*Phase: 35-toolgateway-policy-engine*
*Completed: 2026-07-03*
