---
phase: 51-durable-delegation
reviewed: 2026-08-30T14:36:59Z
depth: standard
files_reviewed: 23
files_reviewed_list:
  - cmd/aura/serve_delegation.go
  - internal/assets/ingest_agent.go
  - internal/assets/service_test.go
  - internal/assets/types.go
  - internal/db/migrations/0112_agent_asset_idempotency.up.sql
  - internal/db/queries/assets.sql
  - internal/db/sqlc/assets.sql.go
  - internal/swarm/delegation_artifact.go
  - internal/swarm/delegation_artifact_test.go
  - internal/swarm/delegation_delivery_report_test.go
  - internal/swarm/delegation_queue.go
  - internal/swarm/delegation_queue_lifecycle_test.go
  - internal/swarm/delegation_terminal.go
  - internal/swarm/delegation_terminal_test.go
  - web/src/AppShell.tsx
  - web/src/chat/displays/SwarmReportTable.tsx
  - web/src/chat/workers/WorkerPane.test.tsx
  - web/src/chat/workers/WorkerPane.tsx
  - web/src/chat/workers/workerWatchControls.ts
  - web/src/chat/workers/WorkerWatchProvider.test.tsx
  - web/src/chat/workers/WorkerWatchProvider.tsx
  - web/src/shell/useWorkerPane.test.ts
  - web/src/shell/useWorkerPane.ts
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
---

# Phase 51: Code Review Report

**Reviewed:** 2026-08-30T14:36:59Z
**Depth:** standard
**Files Reviewed:** 23
**Status:** clean

## Summary

The focused remediation re-review found no remaining correctness, security,
data-loss, or lifecycle defects in the three paths identified by the previous
173-file review. Expired final attempts no longer rerun delegated workers,
terminal report assets are idempotent across the archive/checkpoint failure
window, and worker-pane restoration is scoped to the active conversation while
supporting delayed and multi-card registry hydration.

Two typed GSD reviewer dispatches remained non-terminal beyond their standard
review windows and were shut down without producing or modifying this report.
At the operator's direction, this review was then completed inline against the
same focused scope.

## Remediation Verification

| Previous finding | Result | Evidence |
| --- | --- | --- |
| CR-01 expired running delegation bypassed the retry cap | Resolved | processJob parses pending delivery first, preserving delivery-only recovery beyond the cap, then routes an ordinary post-claim attempt_count greater than max_attempts row through recordFailure before runWithHeartbeat. The real claim query increments the expired final attempt to max+1. |
| CR-02 archive checkpoint could lose or duplicate the required report asset | Resolved | Terminal state is staged before archive. Archive errors enter retryPendingDelivery; successful archive is checkpointed before projections. The stable jobID:terminal delivery key becomes a namespaced agent source_ref, backed by a matching partial unique index and conflict-returned persisted object placement. |
| WR-01 valid persisted panes closed before registry hydration | Resolved | Persistence stores conversationId, childId, and open atomically and route mismatch removes the pane before it can mount. The provider tracks independent report-card registrations, unions workers, cleans up by registration ID, and exposes readiness so WorkerPane waits for current-conversation hydration before ownership closure. |

## Cross-File Analysis

### Attempt-cap recovery

ClaimIngestionJobs intentionally keeps expired running delegation rows claimable
and increments their generation and attempt count under the database lock. In
processJob, a valid pending_delivery still takes precedence so projection
retries remain possible at any attempt count. Without pending delivery, a
reclaimed final attempt is dead-letter staged and projected without constructing
a worker. The database-backed regression test proves zero model calls across
both the initial dead-letter recovery and its delivery-only retry, one logical
conversation/steer projection, and a terminal dead_letter row.

### Report-asset idempotency

The archive key is persisted in terminal payload before object creation. The
composition-root archiver maps it to swarm-report:<deliveryKey>. Migration 0112
and CreateAsset use the same partial uniqueness predicate over identity, agent
source kind, and non-empty source reference. A conflict returns the existing row
without changing its object key. Accepted rows return immediately; incomplete
rows resume against their persisted bucket/key. Therefore archive success
followed by checkpoint failure can invoke the archiver again but resolves one
asset and one referenced filename. Projection starts only after a successful
archive checkpoint.

### Worker-pane lifecycle

AppShell passes the active route conversation into useWorkerPane. Initial
empty-route hydration preserves a matching stored intent, while a non-empty
different route synchronously makes the old pane ineligible and clears the
persisted state. SwarmReportTable owns a stable React registration ID and returns
its cleanup. WorkerWatchProvider scopes the registry to its conversation and
unions all mounted cards, so cleanup cannot erase another card's workers.
WorkerPane neither opens a stream nor closes itself before the registry is
ready, and closes without requesting a child once a hydrated conversation does
not own it.

## Verification

- go test -count=1 ./internal/assets ./internal/swarm ./cmd/aura passed in WSL.
- The db_integration TestExpiredFinalAttemptRecoversAsDeliveryOnly test passed
  against a disposable migrated PostgreSQL database.
- The asset source-reference database test and migration 0112 down/up
  reversibility had already passed against disposable databases during the
  remediation commit.
- The focused Vitest command passed 29 tests across WorkerPane,
  WorkerWatchProvider, useWorkerPane, and SwarmReportTable.
- Full-project TypeScript typecheck and scoped ESLint passed before the
  remediation commit.

## Remaining Gates

This clean code review does not complete Phase 51. The complete WSL Go, vet,
build, race, database-integration, frontend test/build matrix, final image
deployment, repository Playwright E2E, no-restart endpoint-switch witness, and
final GSD verifier remain mandatory.

---

_Reviewed: 2026-08-30T14:36:59Z_
_Reviewer: Codex inline after two non-terminal typed-reviewer dispatches_
_Depth: standard_
