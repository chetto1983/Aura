---
phase: 42-llm-conversation-compaction
plan: 02
subsystem: conversations
tags: [go, postgres, compaction, serializable, rfc8785, recovery]
requires:
  - phase: 42-01
    provides: exact budgets, semantic units, disjoint selection manifests
provides:
  - durable branch-aware compaction claims, leases, immutable checkpoints, and active-pointer CAS
  - canonical versioned manifests with exactly-once reconstruction
  - bounded active/LKG/canonical recovery, quarantine, and audited restore
affects: [42-03, 42-04, 42-05, compaction-coordinator, conversation-history]
tech-stack:
  added: []
  patterns: [short durable claim transaction, inference-outside-transaction, serializable pointer CAS, version-dispatched reconstruction]
key-files:
  created: [internal/db/migrations/0036_compaction_checkpoints.up.sql, internal/conversations/compaction_claims.go, internal/conversations/compaction_manifest.go, internal/conversations/compaction_reconstruct.go, cmd/compaction-test-worker/main.go]
  modified: [internal/conversations/store_compaction.go, internal/db/sqlc/models.go]
key-decisions:
  - "Manual claims supersede only unstarted automatic claims; inference_started durably closes the preemption window."
  - "Restore and finalize serialize on the same branch pointer, with SQLSTATE 40001 handled as a bounded caller retry."
  - "Readers validate stored digest/schema/projection versions and quarantine corruption before reconstruction."
patterns-established:
  - "One durable operation ID is reused across process and database retries."
  - "Recovery order is active pointer, compatible LKG, bounded canonical rebuild, then context_unavailable without pointer mutation."
requirements-completed: [IC-04, IC-05, IC-11, IC-14]
coverage:
  - id: D1
    description: Durable cross-process claims, lease takeover, stale completion, governance invalidation, manual priority, pointer CAS, and rollback-compatible migration
    requirement: IC-04
    verification:
      - kind: integration
        ref: "go test -tags=db_integration ./internal/conversations -run 'TestCompactionMultiProcess...|TestMigration0036' -count=1"
        status: pass
    human_judgment: false
  - id: D2
    description: Canonical exactly-once manifests and branch-aware deterministic reconstruction
    requirement: IC-05
    verification:
      - kind: unit
        ref: "go test ./internal/conversations -run 'Manifest|Reconstruct' -count=1"
        status: pass
    human_judgment: false
  - id: D3
    description: Compatible LKG, bounded rebuild, restore preview, and corruption quarantine recovery
    requirement: IC-11
    verification:
      - kind: integration
        ref: "go test -race -tags=db_integration ./internal/conversations -run 'Compaction|Reconstruct|Recovery|Restore' -count=1"
        status: pass
    human_judgment: false
duration: 34min
completed: 2026-07-13
status: complete
---

# Phase 42 Plan 02: Durable Compaction Persistence and Recovery Summary

**Branch-aware durable claims and immutable checkpoint manifests with serializable activation, exactly-once reconstruction, audited restore, and bounded fail-closed recovery**

## Performance

- **Duration:** 34 min
- **Started:** 2026-07-13T13:18:00+02:00
- **Completed:** 2026-07-13T13:52:08+02:00
- **Tasks:** 2
- **Files modified:** 14

## Accomplishments

- Added additive migration 0036 for leases, immutable generation chains, active pointers, restore audit, quarantine, retention metadata, and idempotency uniqueness, with a down migration that leaves canonical conversations readable.
- Implemented short serializable claim/finalize transactions around inference, expired-owner takeover, manual priority, governance/pointer invalidation, idempotent outcomes, and restore contention.
- Added version-one canonical manifest hashing, exactly-once captured-turn validation, deterministic projection ordering, compatible LKG recovery, bounded canonical fallback, restore preview, and corruption quarantine.
- Proved concurrency with real Postgres, independent pools, compiled subprocess workers, an explicitly killed owner process, migration down/up, and WSL race detection.

## Task Commits

1. **Task 1: Add additive checkpoint schema and durable claim/CAS protocol** - `d425bb6f0` (feat)
2. **Task 2: Implement canonical manifests, reconstruction, and bounded recovery** - `f0d1ac96c` (feat)

TDD RED for Task 2 was observed as missing manifest/reconstruction symbols before GREEN. RED-only commit was not retained because repository pre-commit vet/lint requires the package to compile.

## Files Created/Modified

- `internal/db/migrations/0036_compaction_checkpoints.up.sql` - Additive claims, checkpoints, pointer, restore, and quarantine schema.
- `internal/db/migrations/0036_compaction_checkpoints.down.sql` - Removes only migration 0036 objects.
- `internal/db/queries/compaction.sql` and generated sqlc files - Typed checkpoint/pointer readers.
- `internal/conversations/compaction_claims.go` - Durable operation IDs and typed claim lifecycle.
- `internal/conversations/store_compaction.go` - Claim, inference-start, finalize, restore, and quarantine transactions.
- `internal/conversations/compaction_manifest.go` - Canonical digest and exactly-once partition validation.
- `internal/conversations/compaction_reconstruct.go` - Pure versioned reconstruction and bounded recovery.
- `cmd/compaction-test-worker/main.go` - Cross-process DB claim fixture.

## Decisions Made

- Persist `inference_started` so manual priority is safe across processes and never inferred from process-local state.
- Treat serializable conflicts as bounded caller retry outcomes; never weaken isolation or bypass pointer locking.
- Keep activation disabled: this plan supplies persistence and recovery seams only.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added durable inference-start phase**
- **Found during:** Task 2 integration review
- **Issue:** A lease alone could not distinguish an unstarted automatic claim from active inference for manual priority.
- **Fix:** Added `inference_started`, a one-pending-claim-per-branch index, and a durable transition method.
- **Files modified:** migration 0036, `store_compaction.go`, generated sqlc models.
- **Verification:** Manual-priority subprocess tests pass.
- **Committed in:** `f0d1ac96c`

**2. [Rule 3 - False-success gate] Replaced transport-only worker tests with real DB-backed subprocess proof**
- **Found during:** Completion verification
- **Issue:** Initial named tests only echoed JSON phases and could falsely pass without PostgreSQL.
- **Fix:** Made the suite fail closed on missing DB configuration; added real subprocess claims, independent pools, process kill, CAS races, and migration rollback proof.
- **Files modified:** worker and multiprocess integration test.
- **Verification:** Exact named suite and WSL race suite pass against compose Postgres.
- **Committed in:** `f0d1ac96c`

**Total deviations:** 2 auto-fixed (1 missing critical, 1 blocking false-success). **Impact:** Both changes were required to prove the plan's concurrency contract; no later-plan coordinator or activation work was added.

## Issues Encountered

- Gate retry: Task 1 lint rejected exported symbols without comments and staticcheck S1016; documented APIs and used direct conversion, then normal hooks passed.
- Gate retry: Task 2 lint rejected an exported recovery constant without a comment; documented the constant block, then normal hooks passed.
- Gate retry: fresh compose DB initially rejected the stale migrate-role DSN; the test fixture now uses the supported `EnsureRoles` bootstrap path without logging credentials.
- Gate retry: restore/finalize produced expected SQLSTATE 40001; the restore caller now performs one bounded retry and the test requires one pointer plus one audit event.
- The repository file-size hook consistently took about 105-117 seconds. Each commit was kept as one in-flight process and monitored; no duplicate commit or hook bypass was used.

## Known Stubs

None. Activation remains intentionally disabled by phase design.

## User Setup Required

None. Verification used the repository compose Postgres fixture.

## Next Phase Readiness

- Plan 42-03 can persist structured summaries and authority ledgers through the immutable checkpoint/finalize boundary.
- Active-pointer reconstruction, restore preview, and quarantine are available for later coordinator and UI plans.

## Self-Check: PASSED

- All key files exist; commits `d425bb6f0` and `f0d1ac96c` are present.
- Fresh exact DB integration suite passed (`11.401s`).
- Fresh WSL/CGO race suite passed (`12.932s`).
- No tracked files were deleted, no dependency was added, and unrelated graph changes remain unstaged.

---
*Phase: 42-llm-conversation-compaction*
*Completed: 2026-07-13*
