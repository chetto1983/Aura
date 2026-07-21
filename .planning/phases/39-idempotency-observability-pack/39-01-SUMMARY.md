---
phase: 39-idempotency-observability-pack
plan: 01
subsystem: database
tags: [go, postgres, sqlc, idempotency, concurrency, replay, sha256]

# Dependency graph
requires:
  - phase: 35-toolgateway-policy-engine
    provides: "Append-only aura.tool_invocations execution/audit tuple and durable-before-effect gateway precedent"
  - phase: 36-multi-user-identity-isolation-authula-cutover
    provides: "Trusted aura.identities ownership root used by the registry foreign key"
provides:
  - "Typed identity-scoped operation key, finite scope/state/decision vocabulary, and deterministic typed-payload SHA-256 fingerprints"
  - "Migration 0043 aura.idempotency_operations registry with atomic acquisition, conditional terminal transitions, bounded replay material, and independent payload expiry"
  - "sqlc query surface and Store for Begin, Complete, MarkIndeterminate, replay decisions, conflicts, and bounded expiry"
  - "Unit, SQL-contract, and disposable-Postgres race coverage proving exactly one owner among 48 concurrent Begin contenders"
affects: ["39-02 ingress and gateway idempotency propagation", "39-06 retention engine"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Atomic insert-with-conflict followed by a durable read; failed reads never infer effect ownership"
    - "Terminal transitions predicate on identity, scope, operation key, expected in-progress state, and the original payload hash"
    - "Replay expiry clears bounded body fields in deterministic batches while preserving the registry row and audit tuple"

key-files:
  created:
    - internal/idempotency/types.go
    - internal/idempotency/fingerprint.go
    - internal/idempotency/store.go
    - internal/db/migrations/0043_idempotency_operations.up.sql
    - internal/db/migrations/0043_idempotency_operations.down.sql
    - internal/db/queries/idempotency_operations.sql
    - internal/db/sqlc/idempotency_operations.sql.go
    - internal/db/idempotency_operations_contract_test.go
    - internal/idempotency/store_integration_test.go
  modified:
    - internal/db/sqlc/models.go
    - internal/db/sqlc/querier.go

key-decisions:
  - "Expected existing-operation outcomes are typed BeginDecision values; only a same-key/different-fingerprint result additionally returns ErrConflict, while persistence failures return no decision and fail closed."
  - "An in-progress row always remains non-owning even after its retry timestamp; crash reconciliation must explicitly mark it indeterminate rather than silently reacquiring it."
  - "The disposable integration guard validates both app and migration DSNs target the same non-aura database and reads schema_migrations only through the migration role."

patterns-established:
  - "Only DecisionAcquired authorizes an external effect; replay, in_progress, indeterminate, conflict, and read failure never do."
  - "A test seam is the smallest generated-query interface needed by the store, while production construction accepts sqlc.DBTX."

requirements-completed: []

coverage:
  - id: D1
    description: "Identity/scope/key validation, exact state and decision vocabulary, bounded replay shape, and deterministic typed SHA-256 fingerprints"
    verification:
      - kind: unit
        ref: "internal/idempotency/types_test.go and fingerprint_test.go (`go test ./internal/idempotency`)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Migration 0043 and six generated sqlc queries provide identity-scoped atomic acquisition, conditional terminal transitions, and non-destructive replay expiry"
    verification:
      - kind: unit
        ref: "internal/db/idempotency_operations_contract_test.go (`go test ./internal/db`)"
        status: pass
      - kind: integration
        ref: "repository `aura db migrate` path applied all 43 migrations to disposable database aura_idempotency_3901"
        status: pass
    human_judgment: false
  - id: D3
    description: "Store returns acquired/replay/in_progress/indeterminate/conflict decisions, fails closed on durable-read errors, and rejects stale terminal transitions"
    verification:
      - kind: unit
        ref: "internal/idempotency/store_test.go#TestStore*"
        status: pass
      - kind: integration
        ref: "internal/idempotency/store_integration_test.go#TestStorePostgresContract (48 contenders, -race -count=1 -p 1)"
        status: pass
    human_judgment: false
  - id: D4
    description: "Expiry processes before/equal/after fixtures at a configured cap and preserves completed state plus the full audit tuple"
    verification:
      - kind: integration
        ref: "internal/idempotency/store_integration_test.go#TestStorePostgresContract/expiry_cutoff_and_bounded_batch_preserve_metadata"
        status: pass
    human_judgment: false

duration: 41min
completed: 2026-07-21
status: complete
---

# Phase 39 Plan 01: Durable Idempotency Registry Summary

**An identity-scoped PostgreSQL operation registry now elects one effect owner, fingerprints typed intent, replays completed results, preserves terminal ambiguity, and expires replay payloads without deleting durable operation or audit metadata.**

## Performance

- **Duration:** 41 min
- **Started:** 2026-07-21T09:33:01Z
- **Completed:** 2026-07-21T10:13:31Z
- **Tasks:** 3 TDD tasks
- **Files modified:** 14

## Accomplishments

- Defined strict operation identity, finite scope/state/decision types, bounded replay contracts, sanitized fingerprint failures, and deterministic typed JSON SHA-256 fingerprints in a transport-independent package.
- Added live migration 0043 plus six generated sqlc queries for atomic acquisition, durable reads, hash-and-state-conditional terminal transitions, and deterministic bounded replay expiry; the migration does not alter or delete `aura.tool_invocations`.
- Implemented a fail-closed store and proved with a real disposable PostgreSQL database under `-race` that exactly one of 48 concurrent contenders acquires an operation, other identities remain independent, terminal indeterminate work is never reacquired, and expiry preserves state/audit linkage.

## Task Commits

Each TDD task was committed RED then GREEN:

1. **Task 1: Typed operation identity, state machine, and fingerprint contract**
   - RED - `9a47ed01d` (test): failing key/state/decision/fingerprint contracts.
   - GREEN - `b409239dc` (feat): validated types and deterministic, secret-safe fingerprints.
2. **Task 2: Migration and atomic sqlc operation queries**
   - RED - `1d9c3893a` (test): failing SQL invariants for the missing registry.
   - GREEN - `77b000823` (feat): migration 0043, queries, and generated sqlc surface.
3. **Task 3: Registry store and real concurrency/expiry proof**
   - RED - `0a9b8e671` (test): failing fake-query and tagged PostgreSQL store contract.
   - GREEN - `c381d5e0c` (feat): store implementation and passing unit/integration behavior.

## Files Created/Modified

- `internal/idempotency/types.go` - trusted operation identity, scopes, durable states, typed decisions, audit link, replay shape, and validation bounds.
- `internal/idempotency/fingerprint.go` - deterministic typed JSON SHA-256 hashing and sanitized marshal failures.
- `internal/idempotency/store.go` - narrow sqlc-backed atomic acquisition, replay/conflict/progress/indeterminate projection, conditional completion, and bounded expiry.
- `internal/idempotency/*_test.go` - unit and real PostgreSQL race/boundary coverage; package statement coverage is 86.1%.
- `internal/db/migrations/0043_idempotency_operations.{up,down}.sql` - durable identity-scoped registry, checks, indexes, and least-privilege grants.
- `internal/db/queries/idempotency_operations.sql` - six acquisition/read/transition/expiry queries.
- `internal/db/sqlc/idempotency_operations.sql.go`, `models.go`, `querier.go` - generated query and model surface.
- `internal/db/idempotency_operations_contract_test.go` - executable migration/query invariants, including the prohibition on altering or deleting `tool_invocations`.

## Decisions Made

- Conflict is both a typed `DecisionConflict` and `ErrConflict`, allowing callers to inspect a stable decision while `errors.Is` drives protocol mapping; normal replay/progress/indeterminate observations are not persistence errors.
- An expired or stale-looking in-progress row is never reacquired by `Begin`; a separate reconciliation owner must resolve ambiguity to terminal `indeterminate`.
- Expiry reports only successfully cleared rows/bytes. Concurrently changed rows with zero affected rows are skipped safely, while unexpected multi-row updates fail closed.
- Tests compare JSON replay bodies semantically because PostgreSQL `jsonb` intentionally normalizes textual formatting.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added an executable SQL contract test for the TDD-marked migration task**
- **Found during:** Task 2 (migration and atomic sqlc queries)
- **Issue:** The plan marked Task 2 `tdd=true` but listed no test artifact capable of producing a meaningful RED state or guarding its critical schema/query invariants.
- **Fix:** Added `internal/db/idempotency_operations_contract_test.go` to pin the actual 0043 slot, composite key/hash/state checks, six query names, atomic conflict handling, conditional terminal updates, deterministic expiry, non-deleting replay clears, and the `tool_invocations` retention prohibition.
- **Files modified:** `internal/db/idempotency_operations_contract_test.go`
- **Verification:** The test failed before migration/query creation and passes in the final `go test ./internal/db` gate.
- **Committed in:** `1d9c3893a` (RED) and `77b000823` (GREEN)

---

**Total deviations:** 1 auto-fixed (1 missing-critical TDD coverage gap)
**Impact on plan:** The additional test is narrowly scoped to the plan's own database invariants and creates no production scope beyond the requested registry.

## Issues Encountered

- The first disposable-DB guard attempted to read `public.schema_migrations` through `aura_app`, which correctly lacks that privilege. The guard now proves both DSNs target the same non-live database and reads migration metadata through `aura_migrate`.
- PostgreSQL `jsonb` normalizes whitespace, so the replay integration assertion was corrected from byte formatting equality to JSON semantic equality.
- The pre-commit duplicate-code detector found the fake query callback signatures mirrored the production interface; named callback types removed the duplicate without weakening the interface or tests.
- Windows lacks the race detector's C toolchain, so all race gates ran under WSL with native cgo, matching the repository's documented primary verification path.

## Threat Surface Scan

- Caller-controlled operation keys, payloads, and audit values are never embedded in store errors; fingerprint marshal failures expose only the Go type.
- Only finite Aura-owned scopes are accepted, hashes must be exactly 32 bytes at the schema boundary, and failed acquisition reads return no ownership decision.
- `indeterminate` is terminal and cannot be automatically reacquired; `Complete` and `MarkIndeterminate` require the original hash and current `in_progress` state.
- The registry only links to the Phase 35 audit tuple. Migration/query contract checks confirm no `tool_invocations` alteration or deletion.
- Trusted identity injection remains the ingress adapter's responsibility in Plan 39-02; this package validates the UUID but has no public payload parsing path.

## Known Stubs

None. A scan of the changed idempotency/migration/query surface found no TODO, FIXME, HACK, panic, or "not implemented" markers. The temporary RED compile shims were deleted in their GREEN commits.

## User Setup Required

None. Migration 0043 is applied by the existing `aura db migrate` deployment path; no new environment variables or external services were introduced.

## Next Phase Readiness

- Plan 39-02 can inject trusted runtime identity/scope/key values and project the typed store decisions across web, CLI, scheduler, built-in tool, and MCP mutation adapters.
- Plan 39-06 can call the bounded replay expiry seam without deleting registry or append-only audit metadata.
- No blockers.

---
*Phase: 39-idempotency-observability-pack*
*Completed: 2026-07-21*

## Self-Check: PASSED

**Files verified to exist:**
- FOUND: `internal/idempotency/types.go`
- FOUND: `internal/idempotency/fingerprint.go`
- FOUND: `internal/idempotency/store.go`
- FOUND: `internal/idempotency/store_integration_test.go`
- FOUND: `internal/db/migrations/0043_idempotency_operations.up.sql`
- FOUND: `internal/db/migrations/0043_idempotency_operations.down.sql`
- FOUND: `internal/db/queries/idempotency_operations.sql`
- FOUND: `internal/db/sqlc/idempotency_operations.sql.go`
- FOUND: `internal/db/idempotency_operations_contract_test.go`

**Commits verified to exist:**
- FOUND: `9a47ed01d` (Task 1 RED)
- FOUND: `b409239dc` (Task 1 GREEN)
- FOUND: `1d9c3893a` (Task 2 RED)
- FOUND: `77b000823` (Task 2 GREEN)
- FOUND: `0a9b8e671` (Task 3 RED)
- FOUND: `c381d5e0c` (Task 3 GREEN)

**Fresh plan-level verification:**
- `sqlc generate` - exit 0.
- `go test ./internal/idempotency ./internal/db` - both packages pass.
- `go vet ./...` - exit 0.
- `go build ./...` - exit 0.
- `go test -race ./internal/idempotency ./internal/db` under WSL - both packages pass.
- `go test -tags db_integration -race -count=1 -p 1 ./internal/idempotency` with `CI=1` against CLI-migrated disposable database `aura_idempotency_3901` - pass, then database dropped and the previously stopped Postgres service restored to stopped state.
- `go test ./internal/idempotency -coverprofile=...` - 86.1% statement coverage.
