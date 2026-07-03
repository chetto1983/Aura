---
phase: 35-toolgateway-policy-engine
plan: 04
subsystem: api
tags: [policy-engine, gateway, reservation, idempotency, gate-03, gate-04, tool-invocations, sqlc, execrows, replay, fail-closed]

# Dependency graph
requires:
  - phase: 35-toolgateway-policy-engine (35-01)
    provides: "internal/gateway.classify(spec, rawArgs) → RiskTier + the trustworthy Mutating:true floor on the multiplexed tools"
  - phase: 35-toolgateway-policy-engine (35-03)
    provides: "Gateway.Decide PEP + the mutating-Allow funnel (auto-allow decision-fact AND routeApprove's post-resume Verdict{Allow, OperatorID}); the reservationStore seam; Verdict.OperatorID"
  - phase: 34 (tool-invocation ledger)
    provides: "toolinvocations.Store + the append-only aura.tool_invocations ledger (migration 0011) with the UNIQUE (conv,req,toolCall,event_kind) index + ON CONFLICT DO NOTHING"
provides:
  - "InsertToolInvocation :execrows (returns (int64,error)) — the rows-affected conditional-write IS the GATE-04 idempotency key"
  - "GetToolInvocationEnd :one (replay fetch) + ListInFlightToolInvocationsBefore :many (start∧¬end anti-join for the 35-05 reconciler); regenerated sqlc"
  - "toolinvocations.Store.Reserve (rows==1 acquire / rows==0 replay / err deny), Store.GetEnd (pgx.ErrNoRows→nil), Store.ListInFlightBefore"
  - "internal/gateway/reserve.go — the ONE synchronous fatal-on-failure pre-execution reservation; every mutating-Allow (auto-allow AND approved-resume) converges on it; operator_id rides the single start Meta"
  - "execTool returns Verdict.Replay without calling tool.Execute (GATE-04 idempotent replay); a failed reservation denies before Execute (GATE-03 fail-closed)"
affects: [35-05-reconciler]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "conditional-write rows-affected as the idempotency key (:exec→:execrows, mirrors paused_states.MarkPausedStateResumed)"
    - "unified reserve funnel: auto-allow AND approved-resume take exactly ONE reservation start row; the executed marker (operator_id) rides that start's Meta, never a competing Insert (D-03 point 2)"
    - "synchronous fatal-on-failure reservation BESIDE the async best-effort persistToolInvocationLedger (which becomes a harmless rows==0 no-op via the same UNIQUE key)"
    - "replay tolerates a GC'd sidecar (capped preview + result-expired marker), never extends sidecar retention (Pitfall 6)"

key-files:
  created:
    - internal/toolinvocations/store_reserve.go
    - internal/gateway/reserve.go
    - internal/gateway/reserve_test.go
    - internal/gateway/gateway_integration_test.go
  modified:
    - internal/db/queries/tool_invocations.sql
    - internal/db/sqlc/tool_invocations.sql.go
    - internal/db/sqlc/querier.go
    - internal/toolinvocations/store.go
    - internal/gateway/gateway.go
    - internal/gateway/decide.go
    - internal/gateway/decide_test.go
    - internal/agent/llm_agent_retry.go
    - internal/toolinvocations/store_integration_test.go

key-decisions:
  - "Verdict.Replay is a *tools.ToolResult (built in reserve.go from the recorded end), not a *toolinvocations.Event — keeps the agent seam DB-free; execTool just returns *v.Replay"
  - "A rows==0 with no recorded end yet (in-flight/crash-orphaned prior reservation) replays an in-flight marker instead of re-executing — the mutating side effect stays at-most-once even before the prior end lands"
  - "approve.go needed NO code change — 35-03 already returns Verdict{Allow, OperatorID} with no bare Insert; decide.go consumes that OperatorID and threads it into the single reserve call"
  - "the reservationStore seam gained Reserve + GetEnd; the unit fakeStore was extended to satisfy it and the mutating-auto-allow test was repointed from a decision-fact Insert to the reserve funnel"

patterns-established:
  - "Pattern 1: rows-affected conditional-write = idempotency key; rows==1 acquire → Execute, rows==0 → replay recorded end (no Execute), INSERT error → fail-closed deny"
  - "Pattern 2: unified mutating-Allow funnel — one reserve, one start row per executed call, operator_id folded into Meta so auto-allow and approved-resume are gated + idempotent identically"

requirements-completed: [GATE-03, GATE-04]

coverage:
  - id: D1
    description: "InsertToolInvocation flipped to :execrows (keeps ON CONFLICT DO NOTHING); GetToolInvocationEnd :one + ListInFlightToolInvocationsBefore :many added; sqlc regenerated with the (int64,error) signature"
    requirement: "GATE-04"
    verification:
      - kind: integration
        ref: "internal/toolinvocations/store_integration_test.go#TestReserveIdempotencyKey (live PG: rows==1 then rows==0 then replay)"
        status: pass
      - kind: integration
        ref: "internal/toolinvocations/store_integration_test.go#TestListInFlightBefore (start∧¬end anti-join)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Store.Reserve/GetEnd/ListInFlightBefore; the reserve→execute→replay gate wired synchronously into the gateway mutating path before Execute"
    requirement: "GATE-03"
    verification:
      - kind: integration
        ref: "internal/gateway/gateway_integration_test.go#TestReserveBeforeExecute (start row committed in PG before spy Execute)"
        status: pass
      - kind: unit
        ref: "internal/gateway/reserve_test.go#TestReserveAcquire/TestReserveReplayOnConflict/TestReserveFailClosed"
        status: pass
    human_judgment: false
  - id: D3
    description: "A failed reservation BLOCKS the mutating tool (Execute count == 0), verdict deny — for the operator-approved-and-resumed path too (GATE-03 fail-closed)"
    requirement: "GATE-03"
    verification:
      - kind: integration
        ref: "internal/gateway/gateway_integration_test.go#TestReservationFailBlocks + TestApprovedCallReservedAndIdempotent (forced FK-fail INSERT, spy Execute==0)"
        status: pass
    human_judgment: false
  - id: D4
    description: "Duplicate key replays the recorded outcome, Execute called exactly once (GATE-04); the approved-resume call takes ONE start (operator_id in Meta) + one end, no competing executed row (D-03 point 2)"
    requirement: "GATE-04"
    verification:
      - kind: integration
        ref: "internal/gateway/gateway_integration_test.go#TestIdempotentReplay + TestApprovedCallReservedAndIdempotent (spy Execute==1; exactly one start+one end for the triple)"
        status: pass
    human_judgment: false
  - id: D5
    description: "Replay tolerates a missing/GC'd sidecar — capped+redacted preview plus a result-expired marker, no error (Pitfall 6)"
    verification:
      - kind: integration
        ref: "internal/gateway/gateway_integration_test.go#TestReplayMissingSidecar"
        status: pass
      - kind: unit
        ref: "internal/gateway/reserve_test.go#TestReplayResultMissingSidecar/TestReplayResultInFlight"
        status: pass
    human_judgment: false

# Metrics
duration: ~55min
completed: 2026-07-03
status: complete
---

# Phase 35 Plan 04: Durable Reservation + Idempotency Summary

**The append-only `aura.tool_invocations` ledger is promoted (ZERO migration) into a synchronous, fatal-on-failure pre-execution reservation: `InsertToolInvocation` becomes `:execrows` so the rows-affected conditional-write IS the GATE-04 idempotency key, and every mutating-Allow outcome — decide.go's auto-allow AND routeApprove's post-resume `Verdict{Allow, OperatorID}` — converges on ONE `store.Reserve` call before `Execute` (rows==1 acquire → Execute / rows==0 → replay the recorded end, no Execute / INSERT error → fail-closed deny), the operator_id riding that single start's Meta.**

## Performance

- **Duration:** ~55 min
- **Completed:** 2026-07-03
- **Tasks:** 3
- **Files modified:** 13 (4 created, 9 modified)

## Accomplishments

- **SQL + sqlc (D-01a/b):** flipped `InsertToolInvocation` to `:execrows` (kept `ON CONFLICT DO NOTHING`) so it returns `(int64, error)`; added `GetToolInvocationEnd :one` (replay fetch) and `ListInFlightToolInvocationsBefore :many` (start∧¬end anti-join for the 35-05 reconciler); ran `sqlc generate` (pinned v1.31.1) and committed the regenerated `tool_invocations.sql.go` + `querier.go`.
- **Store (D-01a):** `Store.Reserve` (rows==1 acquire / rows==0 → `GetEnd` replay / INSERT error → wrapped), `Store.GetEnd` (maps `pgx.ErrNoRows`→nil replay, a valid reserved-but-not-finished state), `Store.ListInFlightBefore`; `Insert` now ignores the new rowcount (behavior-preserving). Redaction runs for free inside `toParams`.
- **Gateway reserve (D-01c, GATE-03/04):** `internal/gateway/reserve.go` builds the ONE start Event (verdict in Meta; operator_id folded in for the approved-resume origin) and calls `store.Reserve`; `decide.go`'s mutating funnel routes GateRecommended → approve first, then BOTH the auto-allow and the post-resume `Allow` converge on the same reserve call; `execTool` returns `Verdict.Replay` WITHOUT calling `tool.Execute`. Replay tolerates a GC'd sidecar (preview + `result expired`).
- **Proof tier (live PG, `-race`):** `ReserveBeforeExecute` (order), `ReservationFailBlocks` (Execute==0), `IdempotentReplay` (Execute==1), `ApprovedCallReservedAndIdempotent` (approved-resume reserved + idempotent + exactly one start w/ operator_id in Meta + one end, no competing row), `ReplayMissingSidecar` — all ran live (~0.1s of real DB work each, not skipped).

## Task Commits

Each task was committed atomically:

1. **Task 1: SQL :execrows + replay/in-flight queries + sqlc generate + Store.Reserve/GetEnd/ListInFlightBefore** — `6bff2101` (feat)
2. **Task 2: funnel every mutating-Allow through ONE synchronous reserve (auto-allow AND approved-resume)** — `757fa220` (feat)
3. **Task 3: db_integration proof tier (reserve-before-execute, fail-closed, idempotent replay, approved-call, missing sidecar)** — `a5aeb9d9` (test)

## Files Created/Modified

- `internal/db/queries/tool_invocations.sql` — `:execrows` + `GetToolInvocationEnd :one` + `ListInFlightToolInvocationsBefore :many` [T1]
- `internal/db/sqlc/{tool_invocations.sql.go,querier.go}` — regenerated by sqlc v1.31.1 [T1]
- `internal/toolinvocations/store.go` — `Insert` ignores the new rowcount [T1]
- `internal/toolinvocations/store_reserve.go` — `Reserve`/`GetEnd`/`ListInFlightBefore` [T1]
- `internal/gateway/gateway.go` — widen the `reservationStore` seam (Reserve/GetEnd); add `Verdict.Replay` [T2]
- `internal/gateway/decide.go` — the unified mutating-Allow funnel → the single reserve call [T2]
- `internal/gateway/reserve.go` — gateway-side reserve orchestration + replay fidelity [T2]
- `internal/agent/llm_agent_retry.go` — execTool `Verdict.Replay` branch (return recorded outcome, no Execute) [T2]
- `internal/gateway/decide_test.go` — extend fakeStore for the seam; repoint the auto-allow test to the reserve funnel [T2]
- `internal/gateway/reserve_test.go` — unit rows==1/0/err mapping + operator_id folding + replay fidelity [T3]
- `internal/gateway/gateway_integration_test.go` — the live db_integration proof tier [T3]
- `internal/toolinvocations/store_integration_test.go` — Reserve idempotency key + GetEnd-absent + ListInFlightBefore [T3]

## Decisions Made

- **`Verdict.Replay` is a `*tools.ToolResult`, not a `*toolinvocations.Event`.** reserve.go builds the replayed result from the recorded end so the agent seam stays DB-free — `execTool` just returns `*v.Replay`.
- **A rows==0 with no recorded end yet replays an in-flight marker, not a re-execute.** A prior reservation still in-flight (or crash-orphaned before its end) must not cause the duplicate to run the mutating side effect — the at-most-once guarantee holds even before the end lands.
- **approve.go needed no code change.** 35-03 already returns `Verdict{Allow, OperatorID}` with no bare Insert; decide.go consumes that `OperatorID` and threads it into the single reserve call, so the approve branch's executed marker rides the ONE reservation start's Meta (D-03 point 2) with zero new writes.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Extended the unit fakeStore + repointed the mutating auto-allow test for the widened seam**
- **Found during:** Task 2 (wiring)
- **Issue:** widening the `reservationStore` seam with `Reserve`/`GetEnd` broke the compile of the existing 35-03 `fakeStore` (Insert-only), and `TestDecideMutatingAutoAllowFact` asserted a decision-fact `Insert` that the plan intentionally replaces with the reserve funnel.
- **Fix:** added `Reserve`/`GetEnd` (+ knobs `reserveErr`/`notAcquired`/`replayEnd` and a `reserves()` accessor) to the unit fakeStore, and repointed `TestDecideMutatingAutoAllowFact` to assert exactly one reservation start (verdict in Meta) with NO decision-fact Insert.
- **Files modified:** internal/gateway/decide_test.go
- **Verification:** `go test -race ./internal/gateway/` green
- **Committed in:** `757fa220` (Task 2 commit)

**2. [Rule 3 - Blocking] Installed the pinned sqlc build tool (v1.31.1) in WSL**
- **Found during:** Task 1 (sqlc generate)
- **Issue:** `sqlc` was on no PATH (Windows or WSL); the plan mandates `sqlc generate` (not hand-editing the generated file).
- **Fix:** `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1` — the exact version pinned in the project Makefile (a documented, version-locked build tool, not an application dependency), then ran `sqlc generate`.
- **Files modified:** (tooling only) — generated `internal/db/sqlc/*`
- **Verification:** regenerated signature `InsertToolInvocation(...) (int64, error)` compiles; `go build ./...` green
- **Committed in:** `6bff2101` (Task 1 commit — generated files)

---

**Total deviations:** 2 auto-fixed (both Rule 3 blocking). **Impact:** no scope creep — one is the mandated build-tool install, the other is the compile/behavior fix the seam widening forces. Behavior matches the plan's must_haves exactly.

## Issues Encountered

- **`go test -race` + db_integration need cgo + the live stack, absent from the Windows PATH.** Ran the whole race + db_integration matrix natively in WSL (`CGO_ENABLED=1`, `go1.26.4`, DSNs composed from `.env`'s `POSTGRES_PASSWORD` → `AURA_DB_URL`/`AURA_DB_MIGRATE_URL`), reaching the Windows Docker stack via `127.0.0.1`.
- **Parallel-package `EnsureRoles` race (`tuple concurrently updated`).** Running `./internal/gateway ./internal/toolinvocations` together let both packages upsert the roles concurrently. Re-ran with `-p 1` (serial packages) — both tiers green live (1.5s each; the named proofs each do ~0.1s of real DB work, so no skip-as-green). Harness-only; no code impact.

## Known Stubs

None — the reservation, replay, and in-flight/missing-sidecar paths are all live-wired and proven against the real ledger. `ListInFlightBefore` is a read-only query provided for (and proven for) the 35-05 reconciler; it is a documented forward dependency, not a stub.

## Threat Flags

None — no new network endpoint, auth path, or trust boundary beyond the plan's `<threat_model>`. The reservation write reuses the existing append-only ledger + redaction chokepoint; verdict/operator_id ride `meta jsonb` (zero-migration).

## Next Phase Readiness

- 35-05's reconciler inherits `Store.ListInFlightBefore` (the start∧¬end anti-join) and the guarantee that approved and auto-allowed calls share ONE uniform start∧¬end shape, so a single synthetic-end sweep covers both — and ON CONFLICT DO NOTHING keeps the reconciler's synthetic end from racing a real end.
- GATE-03 + GATE-04 are now delivered end-to-end (reservation-before-Execute + idempotency-key replay), so REQUIREMENTS.md can mark both complete.

## Self-Check: PASSED

- `internal/toolinvocations/store_reserve.go` — FOUND
- `internal/gateway/reserve.go` — FOUND
- `internal/gateway/reserve_test.go` — FOUND
- `internal/gateway/gateway_integration_test.go` — FOUND
- `.planning/phases/35-toolgateway-policy-engine/35-04-SUMMARY.md` — FOUND
- Commit `6bff2101` (Task 1) — FOUND
- Commit `757fa220` (Task 2) — FOUND
- Commit `a5aeb9d9` (Task 3) — FOUND

---
*Phase: 35-toolgateway-policy-engine*
*Completed: 2026-07-03*
