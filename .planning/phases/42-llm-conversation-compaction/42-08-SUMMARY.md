---
phase: 42-llm-conversation-compaction
plan: 08
subsystem: rollout-control-plane
tags: [go, postgres, sqlc, cas, rollout, durability, i18n]
requires:
  - phase: 42-06
    provides: privacy-safe durable compaction foundations
provides:
  - replica-shared versioned rollout effective state
  - immutable evaluator evidence and decision ledgers
  - transactional expected-version transitions and atomic LKG rollback
affects: [42-09, compaction-evaluation, compaction-rollout]
tech-stack:
  added: []
  patterns: [version-CAS state, immutable evidence ledger, locale-neutral reason codes, atomic LKG rollback]
key-files:
  created: [internal/db/migrations/0039_compaction_rollout.up.sql, internal/db/queries/compaction_rollout.sql, internal/conversations/compaction_rollout_store.go, internal/conversations/compaction_rollout_store_test.go]
  modified: [internal/db/sqlc/models.go, internal/db/sqlc/querier.go]
key-decisions:
  - "Rollout state is scoped per deployment/control plane and persisted in PostgreSQL, never process memory."
  - "Evidence and decisions are append-only; only the effective-state row is mutable through expected-version CAS."
  - "Reason values are stable locale-neutral codes; consuming operator surfaces own English/Italian localization."
  - "Plan 42-08 exposes a store only and performs no runtime activation wiring."
requirements-completed: [IC-13, IC-14]
coverage:
  - id: D1
    description: Migration 0039 is reversible and generated scoped CAS queries remain synchronized.
    requirement: IC-13
    verification:
      - kind: integration
        ref: "internal/db/migrate_0039_integration_test.go#TestMigration0039RoundTrip"
        status: pass
      - kind: unit
        ref: "internal/db/compaction_rollout_schema_test.go#TestCompactionRolloutSchemaContract"
        status: pass
    human_judgment: false
  - id: D2
    description: Restart, multi-replica race, stale decisions, failed transactions, and LKG rollback are durable and atomic.
    requirement: IC-13
    verification:
      - kind: integration
        ref: "internal/conversations/compaction_rollout_store_test.go"
        status: pass
    human_judgment: false
  - id: D3
    description: Persisted reasons remain locale-neutral machine codes for localized consumers.
    requirement: IC-14
    verification:
      - kind: integration
        ref: "internal/conversations/compaction_rollout_store_test.go#TestRolloutStoreAtomicRollbackRestoresLastKnownGood"
        status: pass
    human_judgment: false
duration: 29min
completed: 2026-07-13
status: complete
---

# Phase 42 Plan 08: Durable Rollout Control Plane Summary

**Replica-shared rollout state with immutable evidence, transactional version-CAS decisions, and atomic last-known-good rollback—without runtime activation**

## Performance

- **Duration:** 29 min
- **Completed:** 2026-07-13
- **Tasks:** 2
- **Files modified:** 10

## Accomplishments

- Added reversible migration 0039 with one versioned effective-state row per control-plane scope, immutable evidence and decision ledgers, evaluator/scorer/config/corpus versions, stratum and rolling-window snapshots, active configuration, and complete LKG config/pointer policy.
- Generated sqlc primitives for scoped load, evidence/decision append, expected-version transition, rollback, and ledger verification.
- Added a narrow transactional store that validates digests, JSON objects, version metadata, and locale-neutral reason codes before durable writes.
- Proved restart durability, independent-replica CAS races, stale-writer rejection, immutable ledger behavior, transaction rollback, and atomic LKG restoration under the race detector.

## Task Commits

1. **Task 1: Add rollout-state migration and sqlc CAS queries** - `5bcca36b7`
2. **Task 2: Implement transactional rollout store and distributed durability tests** - `5cb3ee42d`

## Decisions Made

- Database state, not a process singleton, is authoritative across restarts and replicas.
- State mutation occurs before evidence append inside one transaction, so a losing CAS returns stale without creating orphan evidence; any later ledger failure rolls the state mutation back.
- Rollout reasons are stable snake-case codes and evidence is structured data. User-facing English/Italian text remains in the existing i18n catalogs at consuming boundaries.
- No evaluator, coordinator, runner, or config activation path imports the new store in this plan.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added executable migration contract and round-trip tests**

- **Found during:** Task 1 verification
- **Issue:** The specified test regex initially returned `[no tests to run]`, which could falsely green-light an unexercised migration.
- **Fix:** Added static schema/query contract coverage and a live PostgreSQL 0039 up/down/up drill.
- **Files modified:** `internal/db/compaction_rollout_schema_test.go`, `internal/db/migrate_0039_integration_test.go`
- **Verification:** Both tests passed against a disposable isolated PostgreSQL instance.
- **Committed in:** `5bcca36b7`

**Total deviations:** 1 auto-fixed missing critical verification seam. **Impact:** Prevents skipped migration verification; no production scope expansion.

## Issues Encountered

- The first migration gate returned `[no tests to run]`; it was rejected as false green and replaced by executable coverage.
- The developer Postgres volume used an older credential than both current environment sources. Verification moved to a disposable dynamically mapped PostgreSQL container without resetting developer data.
- Native Windows rejected `-race` because CGO was disabled. The exact race suite passed under WSL/CGO.
- The plan regex also selected two pre-existing restart/rollback tests. Supplying all bootstrap and application DSNs made the full broad gate pass.
- The combined WSL command passed all Go tests but lacked `sqlc`; native `sqlc diff` then passed separately.
- Both hook-enabled commits completed normally after long lint/file-size gates; no hooks were bypassed or duplicated.

## Verification

- Combined WSL/CGO race gate: `internal/db` and `internal/conversations` passed every selected test with explicit output.
- Migration 0039 live up/down/up: passed.
- Multi-replica expected-version race: exactly one winner and one stale result, passed under race detector.
- Failed post-CAS evidence append: state and decision count unchanged, passed.
- Native `sqlc diff`: clean.
- Pre-commit gofmt, vet, lint (0 issues), and file-size gates: passed for both task commits.

## User Setup Required

None.

## Next Phase Readiness

- Plan 42-09 may consume this durable store for evaluation and rollout logic.
- Activation remains absent and disabled; this plan does not alter runtime behavior.

## Self-Check: PASSED

- Task commits `5bcca36b7` and `5cb3ee42d` exist.
- All key created files exist and generated sqlc output is synchronized.
- The disposable verification container was removed.
- Unrelated `.planning/graphs/` dirt remains unstaged.

---
*Phase: 42-llm-conversation-compaction*
*Completed: 2026-07-13*
