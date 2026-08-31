---
phase: 49-memory-tiers
plan: 07
subsystem: memory
tags: [postgresql, arcadedb, conversation-projection, reconciliation, tdd]

requires:
  - phase: 49-02
    provides: "PostgreSQL-authoritative projection source, tenant graph schema, and ordered projector"
  - phase: 49-06
    provides: "Completed Wave-2 ownership of the shared EnsureMemorySchema aggregator"
provides:
  - "Production EnsureMemorySchema aggregation of the complete Conversation/ConversationTurn schema"
  - "Post-commit fail-soft projection offers for eligible user and final-assistant turns"
  - "Boot-one-shot and periodic identity-scoped replay from PostgreSQL with joined shutdown"
affects: [49-03, 49-08, 49-13, 49-14, MEM-01]

actuals:
  tokens: 12022
  tasks: 3
  commits: 6

tech-stack:
  added: []
  patterns:
    - "One process-lifetime projector consumes identity-level post-commit offers and repairs loss by authoritative replay"
    - "The existing DeleteReconciler owns immediate and periodic projection convergence as one joined worker"
    - "TenantClients resolves every graph mutation through the authenticated identity's server-enforced ArcadeDB database"

key-files:
  created:
    - cmd/aura/chat_memory_projection.go
    - internal/runner/runner_memory_projection_reconcile_test.go
    - internal/runner/runner_memory_projection_live_test.go
  modified:
    - internal/arcadedb/memory.go
    - internal/arcadedb/memory_test.go
    - internal/runner/runner.go
    - internal/runner/runner_deps.go
    - internal/runner/runner_persist.go
    - internal/runner/runner_memory_projection_test.go
    - internal/runner/runner_delete_reconcile.go
    - cmd/aura/chat_boot.go
    - cmd/aura/chat_boot_test.go

key-decisions:
  - "Offer identity-scoped work only after the authoritative append returns; the bounded worker pages the committed source instead of guessing or returning a sequence outside the PostgreSQL transaction."
  - "Construct exactly one projector in chat boot, inject that instance into Runner, deletion recovery, periodic reconciliation, and shutdown ownership."
  - "Reuse TenantClients for server-enforced per-identity graph routing and the existing DeleteReconciler lifecycle; no outbox or independent retention authority was added."

patterns-established:
  - "Projection lag is fail-soft for the source turn but observable through Flush, reconciliation warnings, and the live crash-recovery fixture."
  - "Full replay starts from a zero cursor per identity, applies bounded pages idempotently, then applies tombstones and prunes hard-deleted conversations."

requirements-completed: [MEM-01]

coverage:
  - id: D1
    description: "The production memory bootstrap registers every conversation type, property, relation, and retrieval index idempotently."
    requirement: MEM-01
    verification:
      - kind: unit
        ref: "internal/arcadedb/memory_test.go#TestEnsureMemorySchemaRegistersConversationSchema"
        status: pass
      - kind: unit
        ref: "internal/arcadedb/memory_test.go#TestSchemaIsIdempotentAndCarriesTheVectorIndex"
        status: pass
    human_judgment: false
  - id: D2
    description: "Eligible source turns offer projection only after commit, while failed appends and ineligible events offer nothing and graph failure stays fail-soft."
    requirement: MEM-01
    verification:
      - kind: unit
        ref: "internal/runner/runner_memory_projection_test.go#TestConversationProjectionPostCommit"
        status: pass
      - kind: unit
        ref: "internal/runner/runner_memory_projection_test.go#TestConversationProjectionFailSoft"
        status: pass
      - kind: unit
        ref: "cmd/aura/chat_boot_test.go#TestChatBootMemoryProjection"
        status: pass
    human_judgment: false
  - id: D3
    description: "Boot and periodic replay repair crash gaps, edits, and deletions from bounded PostgreSQL pages without duplicates or an outbox."
    requirement: MEM-01
    verification:
      - kind: unit
        ref: "internal/runner/runner_memory_projection_reconcile_test.go#TestConversationProjectionCrashRecovery, TestConversationProjectionBootReconcile, TestConversationProjectionPeriodicReconcile"
        status: pass
      - kind: integration
        ref: "internal/runner/runner_memory_projection_live_test.go#TestConversationProjectionLiveCrashRecovery (db_integration + arcadedb_integration, WSL -race)"
        status: pass
    human_judgment: false

duration: 1h 9m
completed: 2026-08-31
status: complete
---

# Phase 49 Plan 07: Production Conversation Projection Lifecycle Summary

**Committed conversation turns now enter one tenant-scoped projector after PostgreSQL success and converge after crashes through boot and periodic authoritative replay.**

## Performance

- **Duration:** 1h 9m
- **Started:** 2026-08-31T20:47:34Z
- **Completed:** 2026-08-31T21:56:52Z
- **Tasks:** 3
- **Files modified:** 12

## Accomplishments

- Aggregated the complete Conversation/ConversationTurn fragment through the real `EnsureMemorySchema` boot entry after the fact, batch, and vector fragments, with exact replay-safe inventory coverage.
- Wired eligible user and terminal-assistant persistence to one bounded projector only after PostgreSQL success; failed appends and reasoning/tool scaffolding produce no work, while graph errors never invalidate committed source turns.
- Extended the boot-owned reconciler with immediate and periodic per-identity full replay, tombstones, pruning, failure reporting, and cancellation-aware joined shutdown.
- Proved the crash window live under `-race`: a real PostgreSQL turn committed while the graph remained empty, then two reconciliations produced exactly one real ArcadeDB turn with its source reference.

## Task Commits

1. **Task 1 RED: Conversation schema aggregation contract** — `f4f9c2ab7` (`test`)
2. **Task 1 GREEN: Production schema registration** — `859aa165f` (`feat`)
3. **Task 2 RED: Post-commit and boot ownership contracts** — `a9863d7f5` (`test`)
4. **Task 2 GREEN: Post-commit projector wiring** — `192799ccc` (`feat`)
5. **Task 3 RED: Crash/boot/periodic reconciliation contracts** — `b36bafb17` (`test`)
6. **Task 3 GREEN: Authoritative lifecycle reconciliation** — `668ba30a0` (`feat`)

## Files Created/Modified

- `internal/arcadedb/memory.go` — Aggregates `conversationSchemaStatements` after the existing vector schema.
- `internal/arcadedb/memory_test.go` — Enumerates the complete conversation schema and exact once-per-initialization registration.
- `internal/runner/runner.go`, `runner_deps.go`, `runner_persist.go` — Inject and offer one projector after eligible source commits.
- `internal/runner/runner_delete_reconcile.go` — Owns identity roster replay at boot and on the periodic ticker with cancellation-aware shutdown.
- `internal/runner/runner_memory_projection_test.go` — Post-commit ordering, eligibility, graph failure, order, and close contracts.
- `internal/runner/runner_memory_projection_reconcile_test.go` — Crash, repeated replay, boot, periodic edit/delete, paging failure, and stop contracts.
- `internal/runner/runner_memory_projection_live_test.go` — Disposable PostgreSQL-to-ArcadeDB crash-recovery proof under combined integration tags.
- `cmd/aura/chat_memory_projection.go` — Tenant resolver adapter and composition helpers for the one production projector.
- `cmd/aura/chat_boot.go`, `chat_boot_test.go` — Single construction, shared injection, reconciliation start, and shutdown ownership.

## Decisions Made

- The runner offers the authenticated identity after a successful append instead of inventing an out-of-transaction committed sequence. The projector then pages the only authority, preserving both ordering and crash repair.
- The composition root uses `arcadedb.TenantClients`, including the existing derived credentials and optional admin provisioning, so every identity is routed to its own server-enforced database.
- `DeleteReconciler` remains the only periodic owner. Its start is idempotent because the shared chat boot now starts it for CLI and daemon paths while `serve` retains its existing call.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Project constraint] Split composition and reconciliation coverage to preserve the 600-line ceiling**
- **Found during:** Tasks 2 and 3
- **Issue:** `chat_boot.go` started at 559 lines, and adding all Task 3 cases to `runner_memory_projection_test.go` grew it to 669 lines; the normal commit hook rejected the oversized test file.
- **Fix:** Kept boot ownership calls in `chat_boot.go`, moved the tenant adapter into `chat_memory_projection.go`, and split reconciliation plus live-stack coverage into focused test files.
- **Files modified:** `cmd/aura/chat_memory_projection.go`, `internal/runner/runner_memory_projection_reconcile_test.go`, `internal/runner/runner_memory_projection_live_test.go`
- **Verification:** The file-size hook reports every committed file within 600 lines; vet, lint, unit, race, and live integration gates pass.
- **Committed in:** `a9863d7f5`, `b36bafb17`, `668ba30a0`

---

**Total deviations:** 1 auto-fixed (1 Rule 2 project-constraint split).
**Impact on plan:** Behavior and ownership are unchanged from the plan; the split makes the required lifecycle and live evidence reviewable without violating repository limits.

## Issues Encountered

- The first Task 1 RED commit attempt was rejected by the normal lint hook for a modernizable integer loop; it was corrected and recommitted with hooks enabled.
- The Task 2 test initially asserted strict append/worker alternation. Because the worker is intentionally asynchronous, the correct invariant is that each offer observes at least its corresponding committed source count; the test was corrected before GREEN.
- The first Task 3 RED commit attempt exceeded the 600-line hook limit and was split as documented above. No hook was bypassed.

## TDD Gate Compliance

- **Task 1 RED:** `f4f9c2ab7` failed only because `EnsureMemorySchema` emitted zero conversation schema statements.
- **Task 1 GREEN:** `859aa165f` passed the complete schema inventory and the automated tracer feedback rerun.
- **Task 2 RED:** `a9863d7f5` failed on missing post-commit offers, missing graph-lag observability, and absent boot construction.
- **Task 2 GREEN:** `192799ccc` passed post-commit, fail-soft, ineligible-event, ordering, boot ownership, full unit, and WSL race gates.
- **Task 3 RED:** `b36bafb17` failed on missing crash, boot, periodic, and composition reconciliation behavior.
- **Task 3 GREEN:** `668ba30a0` passed deterministic lifecycle tests and the real PostgreSQL-to-ArcadeDB crash fixture under WSL `-race`.
- **REFACTOR:** No behavior-neutral commit was needed; the file splits were correctness constraints applied during GREEN.

## Verification Evidence

- Named inventories discovered every required Task 1, Task 2, and Task 3 test in their target packages; no target package reported `no tests to run`.
- `go vet ./...`, `go build ./...`, and `go test ./internal/arcadedb ./internal/runner ./cmd/aura -count=1` pass.
- WSL Go 1.26.6: `go test -race ./internal/arcadedb ./internal/runner ./cmd/aura -count=1` passes.
- Live combined tier: `go test -tags='db_integration arcadedb_integration' -race -run '^TestConversationProjectionLiveCrashRecovery$' -count=1 -v ./internal/runner` passes against disposable PostgreSQL and ArcadeDB databases.
- No outbox, migration, environment variable, package, skipped test, or tracked-file deletion was introduced.

## Known Stubs

None.

## User Setup Required

None - no new package, environment variable, service, or manual configuration is required.

## Next Phase Readiness

- Plans 49-03/49-08/49-13 can rely on production conversation projection being initialized, identity-scoped, and repairable from PostgreSQL authority.
- The final Phase 49 evaluator still owns published mixed-tier recall quality; this plan closes the MEM-01 production lifecycle, not the later unified recall surface.

## Self-Check: PASSED

- All twelve declared implementation/test files and this SUMMARY exist.
- Task commits `f4f9c2ab7`, `859aa165f`, `a9863d7f5`, `192799ccc`, `b36bafb17`, and `668ba30a0` exist.
- Coverage classification reports 3/3 deliverables auto-covered with passing evidence.
- No tracked-file deletion, known stub, skipped test, or unrun verification remains.

---
*Phase: 49-memory-tiers*
*Completed: 2026-08-31*
